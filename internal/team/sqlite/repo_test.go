package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/team"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	return db
}

var fixedTS = time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)

func newTeam(t *testing.T, id team.TeamID, org, name string, roles ...team.RoleConfig) *team.Team {
	t.Helper()
	tm, err := team.NewTeam(team.NewTeamInput{
		ID: id, OrgID: org, Name: name, Roles: roles, CreatedAt: fixedTS,
	})
	if err != nil {
		t.Fatalf("NewTeam(%s): %v", name, err)
	}
	return tm
}

func devRole() team.RoleConfig {
	return team.RoleConfig{Role: "dev", CLI: "claude-code", Model: "claude-opus-4-8", CapabilityTags: []string{"go"}, MaxConcurrency: 2}
}

func TestCreateAndGetTeam_RoundTrip(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	ctx := context.Background()
	tm := newTeam(t, "team-1", "org-1", "Alpha", devRole(), team.RoleConfig{Role: "review", MaxConcurrency: 1})

	if err := r.CreateTeam(ctx, tm); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	got, err := r.GetTeam(ctx, "team-1")
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if got.Name() != "Alpha" || got.OrgID() != "org-1" {
		t.Fatalf("unexpected team: %+v", got)
	}
	if len(got.Roles()) != 2 {
		t.Fatalf("roles: got %d want 2", len(got.Roles()))
	}
	if !got.HasRole("dev") || !got.HasRole("review") {
		t.Fatalf("missing declared roles: %+v", got.Roles())
	}
	// capability tags round-trip through JSON.
	for _, rc := range got.Roles() {
		if rc.Role == "dev" {
			if len(rc.CapabilityTags) != 1 || rc.CapabilityTags[0] != "go" {
				t.Fatalf("dev tags: %+v", rc.CapabilityTags)
			}
			if rc.MaxConcurrency != 2 {
				t.Fatalf("dev concurrency: got %d want 2", rc.MaxConcurrency)
			}
		}
	}
}

func TestCreateTeam_NameTakenWithinOrg(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	ctx := context.Background()
	if err := r.CreateTeam(ctx, newTeam(t, "team-1", "org-1", "Alpha")); err != nil {
		t.Fatal(err)
	}
	err := r.CreateTeam(ctx, newTeam(t, "team-2", "org-1", "Alpha"))
	if !errors.Is(err, team.ErrTeamNameTaken) {
		t.Fatalf("dup name: got %v want ErrTeamNameTaken", err)
	}
	// Same name, different org is fine.
	if err := r.CreateTeam(ctx, newTeam(t, "team-3", "org-2", "Alpha")); err != nil {
		t.Fatalf("same name other org: %v", err)
	}
}

// TestAgentExclusivity_DBLevel proves the partial unique index: an agent may
// belong to at most one team across the whole store.
func TestAgentExclusivity_DBLevel(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	ctx := context.Background()
	if err := r.CreateTeam(ctx, newTeam(t, "team-a", "org", "A", devRole())); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateTeam(ctx, newTeam(t, "team-b", "org", "B", devRole())); err != nil {
		t.Fatal(err)
	}
	agent := team.MemberRef("agent:007")

	if err := r.AddMember(ctx, &team.TeamMember{TeamID: "team-a", Ref: agent, Kind: team.MemberKindAgent, Role: "dev", CreatedAt: fixedTS}); err != nil {
		t.Fatalf("add agent to A: %v", err)
	}
	// Second team → rejected by the agent-exclusivity index.
	err := r.AddMember(ctx, &team.TeamMember{TeamID: "team-b", Ref: agent, Kind: team.MemberKindAgent, Role: "dev", CreatedAt: fixedTS})
	if !errors.Is(err, team.ErrAgentAlreadyInTeam) {
		t.Fatalf("agent second team: got %v want ErrAgentAlreadyInTeam", err)
	}
	// Same team twice → rejected. At the raw DB layer this violates BOTH the
	// (team,ref) PK and the agent-exclusivity index; SQLite may report either,
	// so accept both "already a member" signals. The crisp same-team semantics
	// (ErrMemberAlreadyInTeam) is asserted at the service layer, which
	// pre-checks before insert.
	err = r.AddMember(ctx, &team.TeamMember{TeamID: "team-a", Ref: agent, Kind: team.MemberKindAgent, Role: "dev", CreatedAt: fixedTS})
	if !errors.Is(err, team.ErrMemberAlreadyInTeam) && !errors.Is(err, team.ErrAgentAlreadyInTeam) {
		t.Fatalf("agent same team twice: got %v want a membership-exists error", err)
	}
	// After removal the agent is free to join another team.
	if err := r.RemoveMember(ctx, "team-a", agent); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := r.AddMember(ctx, &team.TeamMember{TeamID: "team-b", Ref: agent, Kind: team.MemberKindAgent, Role: "dev", CreatedAt: fixedTS}); err != nil {
		t.Fatalf("re-add after removal: %v", err)
	}
}

// TestHumanMultiTeam_DBLevel proves a human is NOT bound by exclusivity.
func TestHumanMultiTeam_DBLevel(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	ctx := context.Background()
	if err := r.CreateTeam(ctx, newTeam(t, "team-a", "org", "A", devRole())); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateTeam(ctx, newTeam(t, "team-b", "org", "B", devRole())); err != nil {
		t.Fatal(err)
	}
	human := team.MemberRef("user:jane")
	if err := r.AddMember(ctx, &team.TeamMember{TeamID: "team-a", Ref: human, Kind: team.MemberKindHuman, Role: "dev", CreatedAt: fixedTS}); err != nil {
		t.Fatalf("human to A: %v", err)
	}
	if err := r.AddMember(ctx, &team.TeamMember{TeamID: "team-b", Ref: human, Kind: team.MemberKindHuman, Role: "dev", CreatedAt: fixedTS}); err != nil {
		t.Fatalf("human to B: %v", err)
	}
	tid, ok, err := r.FindAgentTeam(ctx, human)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("FindAgentTeam should ignore humans, got %s", tid)
	}
	// Same human twice in one team is still rejected (PK).
	err = r.AddMember(ctx, &team.TeamMember{TeamID: "team-a", Ref: human, Kind: team.MemberKindHuman, Role: "dev", CreatedAt: fixedTS})
	if !errors.Is(err, team.ErrMemberAlreadyInTeam) {
		t.Fatalf("human same team twice: got %v want ErrMemberAlreadyInTeam", err)
	}
}

func TestAddMember_UndeclaredRoleRejected(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	ctx := context.Background()
	if err := r.CreateTeam(ctx, newTeam(t, "team-a", "org", "A", devRole())); err != nil {
		t.Fatal(err)
	}
	err := r.AddMember(ctx, &team.TeamMember{TeamID: "team-a", Ref: "agent:1", Kind: team.MemberKindAgent, Role: "ghost", CreatedAt: fixedTS})
	if !errors.Is(err, team.ErrRoleNotDeclared) {
		t.Fatalf("undeclared role: got %v want ErrRoleNotDeclared", err)
	}
}

func TestDeleteTeam_Cascades(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	ctx := context.Background()
	if err := r.CreateTeam(ctx, newTeam(t, "team-a", "org", "A", devRole())); err != nil {
		t.Fatal(err)
	}
	if err := r.AddMember(ctx, &team.TeamMember{TeamID: "team-a", Ref: "agent:1", Kind: team.MemberKindAgent, Role: "dev", CreatedAt: fixedTS}); err != nil {
		t.Fatal(err)
	}
	if err := r.AssociateProject(ctx, "team-a", "proj-1"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteTeam(ctx, "team-a"); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	if _, err := r.GetTeam(ctx, "team-a"); !errors.Is(err, team.ErrTeamNotFound) {
		t.Fatalf("get after delete: got %v want ErrTeamNotFound", err)
	}
	// Membership + role + project rows are gone (agent free to rejoin elsewhere).
	if _, ok, _ := r.FindAgentTeam(ctx, "agent:1"); ok {
		t.Fatal("agent membership survived team delete")
	}
	assertNoRows(t, db, "SELECT COUNT(*) FROM team_roles WHERE team_id='team-a'")
	assertNoRows(t, db, "SELECT COUNT(*) FROM team_projects WHERE team_id='team-a'")
}

func TestAssociateProject_Dedup(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	ctx := context.Background()
	if err := r.CreateTeam(ctx, newTeam(t, "team-a", "org", "A")); err != nil {
		t.Fatal(err)
	}
	if err := r.AssociateProject(ctx, "team-a", "proj-1"); err != nil {
		t.Fatal(err)
	}
	err := r.AssociateProject(ctx, "team-a", "proj-1")
	if !errors.Is(err, team.ErrProjectAlreadyAssociated) {
		t.Fatalf("dup project: got %v want ErrProjectAlreadyAssociated", err)
	}
	projects, err := r.ListProjects(ctx, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects: got %d want 1", len(projects))
	}
}

func TestRemoveMember_NotFound(t *testing.T) {
	db := openTestDB(t)
	r := NewRepo(db)
	if err := r.CreateTeam(context.Background(), newTeam(t, "team-a", "org", "A")); err != nil {
		t.Fatal(err)
	}
	err := r.RemoveMember(context.Background(), "team-a", "agent:absent")
	if !errors.Is(err, team.ErrMemberNotFound) {
		t.Fatalf("remove absent: got %v want ErrMemberNotFound", err)
	}
}

func assertNoRows(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows for %q, got %d", query, n)
	}
}
