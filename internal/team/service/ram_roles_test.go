package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/team"
)

func seedRAMRole(t *testing.T, svc *Service, id, orgID, name string) {
	t.Helper()
	_, err := svc.db.Exec(`INSERT INTO authorization_roles (id,org_id,kind,name,description,created_by,created_at,updated_at,version) VALUES (?,?, 'custom',?,'','user:owner',datetime('now'),datetime('now'),1)`, id, orgID, name)
	if err != nil {
		t.Fatalf("seed RAM role: %v", err)
	}
}

func TestRAMRoleMapping_ReplaceIsAtomicCASAndAudited(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	tm := createTeam(t, svc, "Alpha", devRole())
	seedRAMRole(t, svc, "role-reader", "org-1", "Reader")
	seedRAMRole(t, svc, "role-writer", "org-1", "Writer")

	got, err := svc.ReplaceRAMRoleMapping(ctx, tm.ID(), "dev", ReplaceRAMRoleMappingInput{ActorRef: "user:owner", RAMRoleIDs: []string{"role-writer", "role-reader", "role-reader"}, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || len(got.RAMRoleIDs) != 2 || got.RAMRoleIDs[0] != "role-reader" {
		t.Fatalf("mapping = %+v", got)
	}

	_, err = svc.ReplaceRAMRoleMapping(ctx, tm.ID(), "dev", ReplaceRAMRoleMappingInput{ActorRef: "user:stale", RAMRoleIDs: nil, ExpectedVersion: 1})
	if !errors.Is(err, ErrRAMRoleMappingConflict) {
		t.Fatalf("stale replace = %v", err)
	}
	after, err := svc.GetRAMRoleMapping(ctx, tm.ID(), "dev")
	if err != nil || len(after.RAMRoleIDs) != 2 || after.Version != 2 {
		t.Fatalf("stale write changed state: %+v err=%v", after, err)
	}

	var prevRaw, nextRaw string
	if err := db.QueryRow(`SELECT previous_role_ids,next_role_ids FROM team_role_ram_role_audit_events WHERE team_id=? AND team_role=?`, tm.ID().String(), "dev").Scan(&prevRaw, &nextRaw); err != nil {
		t.Fatal(err)
	}
	var next []string
	if err := json.Unmarshal([]byte(nextRaw), &next); err != nil || len(next) != 2 {
		t.Fatalf("audit next=%q err=%v", nextRaw, err)
	}
	if prevRaw != "[]" {
		t.Fatalf("audit previous=%s", prevRaw)
	}
}

func TestRAMRoleMapping_RejectsCrossOrgAndDanglingReferences(t *testing.T) {
	svc, _ := newService(t)
	tm := createTeam(t, svc, "Alpha", devRole())
	seedRAMRole(t, svc, "role-other", "org-2", "Other")
	for _, id := range []string{"role-other", "role-missing"} {
		_, err := svc.ReplaceRAMRoleMapping(context.Background(), tm.ID(), "dev", ReplaceRAMRoleMappingInput{ActorRef: "user:owner", RAMRoleIDs: []string{id}, ExpectedVersion: 1})
		if !errors.Is(err, ErrRAMRoleNotFound) {
			t.Fatalf("role %s: got %v", id, err)
		}
	}
	if _, err := svc.GetRAMRoleMapping(context.Background(), tm.ID(), "missing"); !errors.Is(err, team.ErrRoleNotDeclared) {
		t.Fatalf("undeclared role: %v", err)
	}
}

func TestRAMRoleMapping_PreviewReportsMembersProjectsAndDiff(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	tm := createTeam(t, svc, "Alpha", devRole())
	seedRAMRole(t, svc, "role-reader", "org-1", "Reader")
	seedRAMRole(t, svc, "role-writer", "org-1", "Writer")
	if _, err := svc.AddMember(ctx, tm.ID(), "user:one", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssociateProject(ctx, tm.ID(), "project-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceRAMRoleMapping(ctx, tm.ID(), "dev", ReplaceRAMRoleMappingInput{ActorRef: "user:owner", RAMRoleIDs: []string{"role-reader"}, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	impact, err := svc.PreviewRAMRoleMapping(ctx, tm.ID(), "dev", []string{"role-writer"})
	if err != nil {
		t.Fatal(err)
	}
	if impact.MemberCount != 1 || len(impact.ProjectIDs) != 1 || impact.ProjectIDs[0] != "project-1" {
		t.Fatalf("impact scope = %+v", impact)
	}
	if len(impact.AddedRoleIDs) != 1 || impact.AddedRoleIDs[0] != "role-writer" || len(impact.RemovedRoleIDs) != 1 || impact.RemovedRoleIDs[0] != "role-reader" {
		t.Fatalf("impact diff = %+v", impact)
	}
}

func TestCreateAndUpdateTeam_PersistRAMRoleKeysAtomically(t *testing.T) {
	svc, db := newService(t)
	seedRAMRole(t, svc, "role-contributor", "org-1", "Project contributor")
	tm, err := svc.CreateTeam(context.Background(), CreateTeamInput{OrgID: "org-1", Name: "Mapped", Roles: []team.RoleConfig{{Role: "dev", RAMRoleKeys: []string{"Project contributor"}}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetTeam(context.Background(), tm.ID())
	if err != nil || len(got.Roles()[0].RAMRoleKeys) != 1 || got.Roles()[0].RAMRoleKeys[0] != "Project contributor" {
		t.Fatalf("loaded roles=%+v err=%v", got.Roles(), err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM team_role_ram_role_mappings WHERE team_id=? AND ram_role_id='role-contributor'`, tm.ID().String()).Scan(&n); err != nil || n != 1 {
		t.Fatalf("mapping rows=%d err=%v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM team_role_ram_role_audit_events WHERE team_id=? AND actor_ref='system'`, tm.ID().String()).Scan(&n); err != nil || n != 1 {
		t.Fatalf("create-path mapping audit rows=%d err=%v", n, err)
	}

	_, err = svc.CreateTeam(context.Background(), CreateTeamInput{OrgID: "org-1", Name: "Broken", Roles: []team.RoleConfig{{Role: "dev", RAMRoleKeys: []string{"Missing role"}}}})
	if !errors.Is(err, team.ErrRAMRoleKeyNotFound) {
		t.Fatalf("missing stable key: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM teams WHERE name='Broken'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed create was not atomic: count=%d err=%v", n, err)
	}
}

func TestBuiltInAccessProfileRAMRoleContract(t *testing.T) {
	svc, db := newService(t)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_roles WHERE id='team-basic' AND name='Team basic'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("built-in team-basic role missing: count=%d err=%v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_permissions WHERE role_id='team-basic' AND permission_key IN ('team.read','team.memory.read')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("built-in team-basic permissions missing: count=%d err=%v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM access_profiles WHERE id='team-basic'`).Scan(&n); err != nil || n != 1 {
		t.Fatal(err)
	}

	tm, err := svc.CreateTeam(context.Background(), CreateTeamInput{
		OrgID: "org-1",
		Name:  "Builtin RAM",
		Roles: []team.RoleConfig{{Role: "dev", RAMRoleKeys: []string{"Team basic"}}},
	})
	if err != nil {
		t.Fatalf("create with built-in RAM role key: %v", err)
	}
	mapping, err := svc.GetRAMRoleMapping(context.Background(), tm.ID(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.RAMRoleIDs) != 1 || mapping.RAMRoleIDs[0] != "team-basic" {
		t.Fatalf("mapping ids = %+v, want team-basic", mapping)
	}
	if _, err := svc.PreviewRAMRoleMapping(context.Background(), tm.ID(), "dev", []string{"team-basic"}); err != nil {
		t.Fatalf("preview should accept access profile id as RAM role id: %v", err)
	}
}
