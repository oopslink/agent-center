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

func seedSystemRAMRole(t *testing.T, svc *Service, id, name string) {
	t.Helper()
	_, err := svc.db.Exec(`INSERT INTO authorization_roles (id,org_id,kind,name,description,created_by,created_at,updated_at,version) VALUES (?,'','system',?,'','system',datetime('now'),datetime('now'),1)`, id, name)
	if err != nil {
		t.Fatalf("seed system RAM role: %v", err)
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

func TestRAMRoleStableKeyResolver_TargetOrgWinsThenSystemAndRejectsBadStates(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	seedSystemRAMRole(t, svc, "role-system-reviewer", "Reviewer")
	seedRAMRole(t, svc, "role-org-reviewer", "org-1", "Reviewer")
	seedSystemRAMRole(t, svc, "role-system-observer", "Observer")
	seedRAMRole(t, svc, "role-revoked", "org-1", "Retired reviewer")
	if _, err := db.Exec(`UPDATE authorization_roles SET revoked_at=datetime('now') WHERE id='role-revoked'`); err != nil {
		t.Fatal(err)
	}

	tm, err := svc.CreateTeam(ctx, CreateTeamInput{OrgID: "org-1", Name: "Resolver", Roles: []team.RoleConfig{
		{Role: "review", RAMRoleKeys: []string{"Reviewer"}},
		{Role: "observe", RAMRoleKeys: []string{"Observer"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	review, err := svc.GetRAMRoleMapping(ctx, tm.ID(), "review")
	if err != nil {
		t.Fatal(err)
	}
	if len(review.RAMRoleIDs) != 1 || review.RAMRoleIDs[0] != "role-org-reviewer" {
		t.Fatalf("target org role should win over system role: %+v", review)
	}
	observe, err := svc.GetRAMRoleMapping(ctx, tm.ID(), "observe")
	if err != nil {
		t.Fatal(err)
	}
	if len(observe.RAMRoleIDs) != 1 || observe.RAMRoleIDs[0] != "role-system-observer" {
		t.Fatalf("system fallback failed: %+v", observe)
	}

	for _, tc := range []struct {
		name string
		key  string
		want error
	}{
		{name: "unknown", key: "Missing", want: team.ErrRAMRoleKeyNotFound},
		{name: "revoked", key: "Retired reviewer", want: team.ErrRAMRoleKeyRevoked},
	} {
		_, err := svc.CreateTeam(ctx, CreateTeamInput{OrgID: "org-1", Name: "Bad " + tc.name, Roles: []team.RoleConfig{{Role: "dev", RAMRoleKeys: []string{tc.key}}}})
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s key error = %v, want %v", tc.name, err, tc.want)
		}
	}

	if _, err := db.Exec(`DROP INDEX idx_authorization_roles_custom_org_name`); err != nil {
		t.Fatal(err)
	}
	seedRAMRole(t, svc, "role-dup-a", "org-1", "Duplicated")
	seedRAMRole(t, svc, "role-dup-b", "org-1", "Duplicated")
	_, err = svc.CreateTeam(ctx, CreateTeamInput{OrgID: "org-1", Name: "Bad ambiguous", Roles: []team.RoleConfig{{Role: "dev", RAMRoleKeys: []string{"Duplicated"}}}})
	if !errors.Is(err, team.ErrRAMRoleKeyAmbiguous) {
		t.Fatalf("ambiguous key error = %v, want %v", err, team.ErrRAMRoleKeyAmbiguous)
	}
}

func TestBuiltInRAMRoleContract(t *testing.T) {
	svc, db := newService(t)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_roles WHERE id='team-basic' AND name='Team basic'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("built-in team-basic role missing: count=%d err=%v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_permissions WHERE role_id='team-basic' AND permission_key IN ('team.read','team.memory.read')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("built-in team-basic permissions missing: count=%d err=%v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='access_profiles'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("access_profiles table must not remain in the final RAM Role schema")
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
		t.Fatalf("preview should accept RAM role id: %v", err)
	}
}
