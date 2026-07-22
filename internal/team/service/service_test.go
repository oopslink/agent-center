package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/team"
	teamsqlite "github.com/oopslink/agent-center/internal/team/sqlite"
)

func newService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC))
	svc := New(teamsqlite.NewRepo(db), db, idgen.NewGenerator(clk), clk)
	return svc, db
}

func createTeam(t *testing.T, svc *Service, name string, roles ...team.RoleConfig) *team.Team {
	t.Helper()
	tm, err := svc.CreateTeam(context.Background(), CreateTeamInput{OrgID: "org-1", Name: name, Roles: roles})
	if err != nil {
		t.Fatalf("CreateTeam(%s): %v", name, err)
	}
	return tm
}

func devRole() team.RoleConfig {
	return team.RoleConfig{Role: "dev", CLI: "claude-code", MaxConcurrency: 3}
}

func reviewRole() team.RoleConfig {
	return team.RoleConfig{Role: "review", CLI: "codex", MaxConcurrency: 1}
}

func TestService_CreateTeam_GeneratesIDAndPersistsRoles(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	tm := createTeam(t, svc, "Alpha", devRole())
	if tm.ID().String() == "" {
		t.Fatal("expected generated id")
	}
	got, err := svc.GetTeam(ctx, tm.ID())
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if !got.HasRole("dev") {
		t.Fatalf("declared role missing: %+v", got.Roles())
	}
}

func TestService_UpdateTeamRoles_AllowsReplaceAndEmpty(t *testing.T) {
	svc, _ := newService(t)
	tm := createTeam(t, svc, "Alpha", devRole())
	roles := []team.RoleConfig{{Role: "reviewer", CLI: "codex", Model: "gpt-5", MaxConcurrency: 2}}
	updated, err := svc.UpdateTeam(context.Background(), tm.ID(), UpdateTeamInput{Roles: &roles})
	if err != nil {
		t.Fatalf("UpdateTeam roles: %v", err)
	}
	if updated.HasRole("dev") || !updated.HasRole("reviewer") {
		t.Fatalf("unexpected roles: %+v", updated.Roles())
	}
	empty := []team.RoleConfig{}
	updated, err = svc.UpdateTeam(context.Background(), tm.ID(), UpdateTeamInput{Roles: &empty})
	if err != nil || len(updated.Roles()) != 0 {
		t.Fatalf("clear roles: roles=%+v err=%v", updated.Roles(), err)
	}
}

func TestService_UpdateTeamRoles_RejectsRemovingRoleInUse(t *testing.T) {
	svc, _ := newService(t)
	tm := createTeam(t, svc, "Alpha", devRole())
	if _, err := svc.AddMember(context.Background(), tm.ID(), "agent:42", "dev"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	empty := []team.RoleConfig{}
	_, err := svc.UpdateTeam(context.Background(), tm.ID(), UpdateTeamInput{Roles: &empty})
	if !errors.Is(err, team.ErrRoleInUse) {
		t.Fatalf("got %v, want ErrRoleInUse", err)
	}
	got, getErr := svc.GetTeam(context.Background(), tm.ID())
	if getErr != nil || !got.HasRole("dev") {
		t.Fatalf("failed update must preserve roles: roles=%+v err=%v", got.Roles(), getErr)
	}
}

// TestService_AgentExclusivity is the headline requirement: an agent is bound to
// a single team; joining a second is rejected.
func TestService_AgentExclusivity(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	a := createTeam(t, svc, "A", devRole(), reviewRole())
	b := createTeam(t, svc, "B", devRole())

	if _, err := svc.AddMember(ctx, a.ID(), "agent:42", "dev"); err != nil {
		t.Fatalf("add agent to A: %v", err)
	}
	_, err := svc.AddMember(ctx, b.ID(), "agent:42", "dev")
	if !errors.Is(err, team.ErrAgentAlreadyInTeam) {
		t.Fatalf("agent to second team: got %v want ErrAgentAlreadyInTeam", err)
	}
	// Re-adding to the same team reports the dedup error, not exclusivity.
	_, err = svc.AddMember(ctx, a.ID(), "agent:42", "dev")
	if !errors.Is(err, team.ErrMemberAlreadyInTeam) {
		t.Fatalf("agent to same team: got %v want ErrMemberAlreadyInTeam", err)
	}
	if _, err := svc.AddMember(ctx, a.ID(), "agent:42", "review"); err != nil {
		t.Fatalf("agent to same team under second role should add: %v", err)
	}
	members, err := svc.ListMembers(ctx, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("agent same team multi-role rows: got %d want 2 (%+v)", len(members), members)
	}
	// Freeing the agent lets it join B.
	if err := svc.RemoveMember(ctx, a.ID(), "agent:42"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := svc.AddMember(ctx, b.ID(), "agent:42", "dev"); err != nil {
		t.Fatalf("re-add to B after removal: %v", err)
	}
}

// TestService_HumanMultiTeam is the paired requirement: a human may join many
// teams.
func TestService_HumanMultiTeam(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	a := createTeam(t, svc, "A", devRole())
	b := createTeam(t, svc, "B", devRole())

	if _, err := svc.AddMember(ctx, a.ID(), "user:jane", "dev"); err != nil {
		t.Fatalf("human to A: %v", err)
	}
	if _, err := svc.AddMember(ctx, b.ID(), "user:jane", "dev"); err != nil {
		t.Fatalf("human to B: %v", err)
	}
	for _, tid := range []team.TeamID{a.ID(), b.ID()} {
		members, err := svc.ListMembers(ctx, tid)
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 1 || members[0].Ref != "user:jane" {
			t.Fatalf("team %s members: %+v", tid, members)
		}
	}
}

func TestService_AddMember_RoleNotDeclared(t *testing.T) {
	svc, _ := newService(t)
	a := createTeam(t, svc, "A", devRole())
	_, err := svc.AddMember(context.Background(), a.ID(), "agent:1", "reviewer")
	if !errors.Is(err, team.ErrRoleNotDeclared) {
		t.Fatalf("undeclared role: got %v want ErrRoleNotDeclared", err)
	}
}

func TestService_AddMember_InvalidRef(t *testing.T) {
	svc, _ := newService(t)
	a := createTeam(t, svc, "A", devRole())
	_, err := svc.AddMember(context.Background(), a.ID(), "bogus", "dev")
	if !errors.Is(err, team.ErrInvalidMemberRef) {
		t.Fatalf("bad ref: got %v want ErrInvalidMemberRef", err)
	}
}

func TestService_AddMember_TeamNotFound(t *testing.T) {
	svc, _ := newService(t)
	_, err := svc.AddMember(context.Background(), "team-missing", "agent:1", "dev")
	if !errors.Is(err, team.ErrTeamNotFound) {
		t.Fatalf("missing team: got %v want ErrTeamNotFound", err)
	}
}

func TestService_UpdateTeam(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	a := createTeam(t, svc, "A", devRole())
	newName := "Alpha-Renamed"
	desc := "the alpha squad"
	updated, err := svc.UpdateTeam(ctx, a.ID(), UpdateTeamInput{Name: &newName, Description: &desc})
	if err != nil {
		t.Fatalf("UpdateTeam: %v", err)
	}
	if updated.Name() != newName || updated.Description() != desc {
		t.Fatalf("update not applied: %+v", updated)
	}
	if updated.Version() <= a.Version() {
		t.Fatalf("version not bumped: %d <= %d", updated.Version(), a.Version())
	}
	reloaded, _ := svc.GetTeam(ctx, a.ID())
	if reloaded.Name() != newName {
		t.Fatalf("persisted name: got %q", reloaded.Name())
	}
}

func TestService_UpdateTeam_DuplicateName(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	createTeam(t, svc, "A", devRole())
	b := createTeam(t, svc, "B", devRole())
	name := "A"
	_, err := svc.UpdateTeam(ctx, b.ID(), UpdateTeamInput{Name: &name})
	if !errors.Is(err, team.ErrTeamNameTaken) {
		t.Fatalf("rename collision: got %v want ErrTeamNameTaken", err)
	}
}

func TestService_DeleteTeam_Idempotent(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	a := createTeam(t, svc, "A")
	if err := svc.DeleteTeam(ctx, a.ID()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := svc.DeleteTeam(ctx, a.ID()); err != nil {
		t.Fatalf("delete again (idempotent): %v", err)
	}
}

func TestService_ListTeams_ScopedByOrg(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	createTeam(t, svc, "A")
	if _, err := svc.CreateTeam(ctx, CreateTeamInput{OrgID: "org-2", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	org1, err := svc.ListTeams(ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(org1) != 1 || org1[0].Name() != "A" {
		t.Fatalf("org-1 teams: %+v", org1)
	}
	all, err := svc.ListTeams(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all teams: got %d want 2", len(all))
	}
}

func TestService_AssociateProject(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	a := createTeam(t, svc, "A")
	if err := svc.AssociateProject(ctx, a.ID(), "proj-1"); err != nil {
		t.Fatalf("associate: %v", err)
	}
	if err := svc.AssociateProject(ctx, a.ID(), ""); !errors.Is(err, team.ErrInvalidProject) {
		t.Fatalf("empty project: got %v want ErrInvalidProject", err)
	}
	if err := svc.AssociateProject(ctx, "team-missing", "proj-1"); !errors.Is(err, team.ErrTeamNotFound) {
		t.Fatalf("missing team: got %v want ErrTeamNotFound", err)
	}
	projects, err := svc.ListProjects(ctx, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ProjectID != "proj-1" {
		t.Fatalf("projects: %+v", projects)
	}
}
