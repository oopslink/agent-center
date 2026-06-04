package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// v2.7.1 #239 PR2: create_task error precision — a missing project is reported
// as "not found" (with the agent's available projects as a hint), distinct from
// the "not a member" message a real-but-inaccessible project gives. Before this,
// requireProjectMember returned the misleading "not a member" for BOTH (a
// membership miss can't tell them apart) — the @oopslink screenshot pain.

// seedAgentToolsPM wires a minimal PMService (project + member repos) onto the
// fixture and seeds agent AG3 (on W1, with a member id so its project-member ref
// is valid). Error paths fail at requireProjectMember before touching tasks, so
// Tasks/Outbox are not needed.
func seedAgentToolsPM(t *testing.T, f *agentToolsFixture) (projRepo *pmsql.ProjectRepo, memRepo *pmsql.ProjectMemberRepo, ag3, ag3Member string) {
	t.Helper()
	ag3, ag3Member = "AG3", "AG3-mem"
	a3, err := agent.NewAgent(agent.NewAgentInput{
		ID: agent.AgentID(ag3), OrganizationID: atTestOrg, Profile: agent.Profile{Name: ag3},
		WorkerID: atWorker1, CreatedBy: "system", IdentityMemberID: ag3Member, CreatedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.agents.Save(t.Context(), a3); err != nil {
		t.Fatal(err)
	}
	projRepo = pmsql.NewProjectRepo(f.db)
	memRepo = pmsql.NewProjectMemberRepo(f.db)
	f.deps.PMService = pmservice.New(pmservice.Deps{
		DB: f.db, Projects: projRepo, Members: memRepo,
		IDGen: idgen.NewGenerator(f.clk), Clock: f.clk,
	})
	return projRepo, memRepo, ag3, ag3Member
}

func TestCreateTask_239_ProjectNotFound_WithAvailableHint(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	projRepo, memRepo, ag3, ag3Member := seedAgentToolsPM(t, f)

	// AG3 IS a member of proj-1 ("Alpha") — so it appears in the available hint.
	proj, _ := pm.NewProject(pm.NewProjectInput{ID: "proj-1", OrganizationID: atTestOrg, Name: "Alpha", CreatedBy: "system", CreatedAt: atNow})
	if err := projRepo.Save(t.Context(), proj); err != nil {
		t.Fatal(err)
	}
	mem, _ := pm.NewProjectMember(pm.NewProjectMemberInput{ID: "pm-1", ProjectID: "proj-1", IdentityID: pm.IdentityRef("agent:" + ag3Member), Role: pm.RoleMember, AddedBy: "system", CreatedAt: atNow})
	if err := memRepo.Save(t.Context(), mem); err != nil {
		t.Fatal(err)
	}

	s := f.server(t)
	// Create against a project that DOES NOT exist → not_found, not "not member".
	status, body := postBearer(t, s.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": ag3, "project_id": "ghost", "title": "x"})
	if status != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%v", status, body)
	}
	if body["error"] != "project_not_found" {
		t.Fatalf("error=%v, want project_not_found", body["error"])
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "ghost") || !strings.Contains(msg, "not found") {
		t.Fatalf("message=%q, want it to name the missing project + 'not found'", msg)
	}
	// The hint must list the agent's actual project so it can self-correct.
	if !strings.Contains(msg, "available") || !strings.Contains(msg, "Alpha") {
		t.Fatalf("message=%q, want available-projects hint listing Alpha", msg)
	}
	// MUST NOT mislead with "member".
	if strings.Contains(strings.ToLower(msg), "member") {
		t.Fatalf("message=%q must not say 'member' for a missing project", msg)
	}
}

func TestCreateTask_239_ExistingProjectNotMember(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	projRepo, _, ag3, _ := seedAgentToolsPM(t, f)

	// proj-2 exists in the org, but AG3 is NOT a member.
	proj, _ := pm.NewProject(pm.NewProjectInput{ID: "proj-2", OrganizationID: atTestOrg, Name: "Beta", CreatedBy: "system", CreatedAt: atNow})
	if err := projRepo.Save(t.Context(), proj); err != nil {
		t.Fatal(err)
	}

	s := f.server(t)
	status, body := postBearer(t, s.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": ag3, "project_id": "proj-2", "title": "x"})
	if status != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%v", status, body)
	}
	if body["error"] != "not_a_project_member" {
		t.Fatalf("error=%v, want not_a_project_member", body["error"])
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "proj-2") || !strings.Contains(strings.ToLower(msg), "not a member") || !strings.Contains(strings.ToLower(msg), "owner") {
		t.Fatalf("message=%q, want 'not a member of project proj-2, ask owner'", msg)
	}
}
