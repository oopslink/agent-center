package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// decodeTasks reads the nested {"tasks":[...]} envelope from
// GET /api/projects/{id}/tasks.
func decodeTasks(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	var env struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Tasks
}

// TestPMListTasks_UnplannedFilter pins the v2.9 Work Board Backlog filter:
// GET .../tasks?unplanned=1 returns ONLY tasks not yet in a Plan (empty
// plan_id); GET .../tasks (no param) returns all project tasks; org/project
// gating is preserved (only members of a project in the caller's org reach it).
func TestPMListTasks_UnplannedFilter(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Acme", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Two tasks: one will be selected into a Plan, one stays in the backlog.
	plannedID, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "planned", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: caller}); err != nil {
		t.Fatal(err)
	}

	// Put the planned task into a Plan via the repo (empty→non-empty plan_id).
	tr := pmsql.NewTaskRepo(db)
	planned, err := tr.FindByID(ctx, plannedID)
	if err != nil {
		t.Fatal(err)
	}
	planned.SetPlan("PL-1", planned.UpdatedAt())
	if err := tr.Update(ctx, planned); err != nil {
		t.Fatal(err)
	}

	base := s.URL + "/api/projects/" + string(pid) + "/tasks"

	// No param → all project tasks (2).
	all := decodeTasks(t, orgScopedGet(t, base, sess))
	if len(all) != 2 {
		t.Fatalf("GET tasks (no param) = %d, want 2", len(all))
	}

	// ?unplanned=1 → only the backlog task.
	backlog := decodeTasks(t, orgScopedGet(t, base+"?unplanned=1", sess))
	if len(backlog) != 1 || backlog[0]["title"] != "backlog" {
		t.Fatalf("GET tasks?unplanned=1 = %+v, want only the backlog task", backlog)
	}
	if string(plannedID) == backlog[0]["id"].(string) {
		t.Fatal("planned task leaked into ?unplanned=1 result")
	}

	// ?unplanned=true behaves the same.
	if got := decodeTasks(t, orgScopedGet(t, base+"?unplanned=true", sess)); len(got) != 1 {
		t.Fatalf("GET tasks?unplanned=true = %d, want 1", len(got))
	}

	// Project gating preserved: a project in a DIFFERENT org is not reachable by
	// this caller's org-scoped request.
	op, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: "org-other-9999", Name: "OtherCo", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(op)+"/tasks?unplanned=1", sess)
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("cross-org project tasks reachable (status=200), want gating to reject")
	}
}
