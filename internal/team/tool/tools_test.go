package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/team"
	"github.com/oopslink/agent-center/internal/team/service"
	teamsqlite "github.com/oopslink/agent-center/internal/team/sqlite"
)

func newTools(t *testing.T) *Tools {
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
	svc := service.New(teamsqlite.NewRepo(db), db, idgen.NewGenerator(clk), clk)
	return NewTools(svc)
}

func TestDefinitions_CoverAllTools(t *testing.T) {
	names := map[string]bool{}
	for _, d := range Definitions() {
		names[d.Name] = true
	}
	for _, want := range []string{
		ToolCreateTeam, ToolUpdateTeam, ToolDeleteTeam, ToolGetTeam,
		ToolListTeams, ToolAddMember, ToolRemoveMember, ToolAssociateProject,
	} {
		if !names[want] {
			t.Errorf("Definitions missing %q", want)
		}
	}
}

func TestTools_EndToEnd(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()

	view, err := tools.CreateTeam(ctx, CreateTeamArgs{
		OrgID: "org-1", Name: "Alpha",
		Roles: []RoleArg{{Role: "dev", CLI: "claude-code", CapabilityTags: []string{"go"}, MaxConcurrency: 2}},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if view.ID == "" || len(view.Roles) != 1 {
		t.Fatalf("bad create view: %+v", view)
	}

	// Result views serialize cleanly (agent tool contract).
	if _, err := json.Marshal(view); err != nil {
		t.Fatalf("marshal view: %v", err)
	}

	m, err := tools.AddMember(ctx, AddMemberArgs{TeamID: view.ID, MemberRef: "agent:1", Role: "dev"})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if m.Kind != string(team.MemberKindAgent) {
		t.Fatalf("member kind: got %q", m.Kind)
	}

	// Agent exclusivity surfaces through the tool as a domain error.
	other, _ := tools.CreateTeam(ctx, CreateTeamArgs{OrgID: "org-1", Name: "Beta", Roles: []RoleArg{{Role: "dev"}}})
	_, err = tools.AddMember(ctx, AddMemberArgs{TeamID: other.ID, MemberRef: "agent:1", Role: "dev"})
	if !errors.Is(err, team.ErrAgentAlreadyInTeam) {
		t.Fatalf("exclusivity via tool: got %v want ErrAgentAlreadyInTeam", err)
	}

	if err := tools.AssociateProject(ctx, view.ID, "proj-1"); err != nil {
		t.Fatalf("AssociateProject: %v", err)
	}
	teams, err := tools.ListTeams(ctx, "org-1")
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) != 2 {
		t.Fatalf("list: got %d want 2", len(teams))
	}
	if err := tools.RemoveMember(ctx, view.ID, "agent:1"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if err := tools.DeleteTeam(ctx, view.ID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	if _, err := tools.GetTeam(ctx, view.ID); !errors.Is(err, team.ErrTeamNotFound) {
		t.Fatalf("get after delete: got %v want ErrTeamNotFound", err)
	}
}
