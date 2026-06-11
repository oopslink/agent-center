package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// v2.9 P3 Stage C — agent MCP Plan passthrough tools (create_plan,
// add_task_to_plan, remove_task_from_plan, add_plan_dependency,
// remove_plan_dependency, start_plan, stop_plan, get_plan, list_plans).
//
// These reuse the writeToolsFixture (now Plan-capable: Plans repo + a REAL
// PlanDispatcher + plan participant projector). The tests assert the PASSTHROUGH
// WIRING — args parsed, the right pm AppService called with actor=agent, plan
// domain errors mapped, and the requireAgentOnWorker guardrail enforced — NOT the
// plan domain itself (covered in internal/projectmanager).
// =============================================================================

// seedPlanMember creates a project + draft Plan as the AGENT (actor=agent:AG1, a
// member via #5a after seedMemberProject), draining the relay so the plan
// conversation exists. Returns the project id + plan id.
func (f *writeToolsFixture) seedPlanMember(t *testing.T) (pm.ProjectID, string) {
	t.Helper()
	ctx := context.Background()
	pid, _ := f.seedMemberProject(t) // AG1 is now a member of pid.
	planID, err := f.pmSvc.CreatePlan(ctx, pmservice.CreatePlanCommand{
		ProjectID: pid, Name: "Plan A", CreatedBy: pm.IdentityRef("agent:" + atAgent1),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	return pid, string(planID)
}

// seedPlanTask creates a fresh task in pid, assigns it to AG1 (so a started plan's
// §9.6c assignee check passes), selects it into the plan, and drains. Returns tid.
func (f *writeToolsFixture) seedPlanTask(t *testing.T, pid pm.ProjectID, planID string) string {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "node", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1), owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.SelectTaskIntoPlan(ctx, pm.PlanID(planID), tid, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	return string(tid)
}

// --- create_plan -------------------------------------------------------------

func TestCreatePlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "My Plan", "description": "d"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	planID, _ := body["plan_id"].(string)
	if planID == "" {
		t.Fatalf("no plan_id in body: %v", body)
	}
	// The plan exists with actor=agent as creator.
	p, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(p.CreatorRef()); got != "agent:"+atAgent1 {
		t.Fatalf("creator_ref = %q, want agent:%s", got, atAgent1)
	}
	if p.Status() != pm.PlanDraft {
		t.Fatalf("status = %s, want draft", p.Status())
	}
}

func TestCreatePlan_ForeignProject_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // AG1 resolves (is a member somewhere).
	pid := f.seedForeignProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "nope"})
	// requireProjectMember in the AppService → ErrNotMember → 403.
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (pm ErrNotMember); body = %v", status, body)
	}
}

func TestCreatePlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)
	// W1 token operating AG2 (bound to W2) → guardrail 403, no AppService call.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "project_id": string(pid), "name": "x"})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- add_task_to_plan / remove_task_from_plan --------------------------------

func TestAddTaskToPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid, _ := f.pmSvc.CreateTask(context.Background(), pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "to select", CreatedBy: pm.IdentityRef("user:owner"),
	})
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_task_to_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": string(tid)})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	tk, _ := f.pmSvc.GetTask(context.Background(), tid)
	if string(tk.PlanID()) != planID {
		t.Fatalf("task plan_id = %q, want %s", tk.PlanID(), planID)
	}
}

func TestRemoveTaskFromPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID) // selected into the plan.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/remove_task_from_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": tid})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	tk, _ := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if tk.PlanID() != "" {
		t.Fatalf("task plan_id = %q, want empty after removal", tk.PlanID())
	}
}

func TestAddTaskToPlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_task_to_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID, "task_id": "T-x"})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- add_plan_dependency / remove_plan_dependency ----------------------------

func TestAddPlanDependency_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	from := f.seedPlanTask(t, pid, planID)
	to := f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_plan_dependency", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "from_task_id": from, "to_task_id": to})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
}

// TestAddPlanDependency_Cycle_Surfaced: from→to then to→from would cycle →
// ErrPlanCycle must be surfaced as a tool error (422 invalid_transition).
func TestAddPlanDependency_Cycle_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	a := f.seedPlanTask(t, pid, planID)
	b := f.seedPlanTask(t, pid, planID)
	// a depends_on b (legal).
	if err := f.pmSvc.AddPlanDependency(context.Background(), pm.PlanID(planID),
		pm.TaskID(a), pm.TaskID(b), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)
	// b depends_on a → cycle.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_plan_dependency", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "from_task_id": b, "to_task_id": a})
	if status != http.StatusUnprocessableEntity || body["error"] != "invalid_transition" {
		t.Fatalf("status = %d err=%v, want 422 invalid_transition (ErrPlanCycle)", status, body["error"])
	}
}

func TestRemovePlanDependency_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	from := f.seedPlanTask(t, pid, planID)
	to := f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.AddPlanDependency(context.Background(), pm.PlanID(planID),
		pm.TaskID(from), pm.TaskID(to), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/remove_plan_dependency", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "from_task_id": from, "to_task_id": to})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
}

// --- start_plan / stop_plan --------------------------------------------------

func TestStartPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID) // ≥1 task, assigned to AG1 (resolvable).
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/start_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	p, _ := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if p.Status() != pm.PlanRunning {
		t.Fatalf("status = %s, want running", p.Status())
	}
}

// TestStartPlan_NoTasks_Surfaced: starting an empty draft plan → ErrPlanNoTasks
// surfaced as a tool error (422). Exercises start-validation passthrough.
func TestStartPlan_NoTasks_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t) // no tasks selected.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/start_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusUnprocessableEntity || body["error"] != "invalid_transition" {
		t.Fatalf("status = %d err=%v, want 422 invalid_transition (ErrPlanNoTasks)", status, body["error"])
	}
}

func TestStopPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/stop_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	p, _ := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if p.Status() != pm.PlanDraft {
		t.Fatalf("status = %s, want draft after stop", p.Status())
	}
}

func TestStartPlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/start_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- get_plan ----------------------------------------------------------------

func TestGetPlan_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "plan_id": planID})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["id"] != planID {
		t.Fatalf("id = %v, want %s", body["id"], planID)
	}
	// Spot-check the DERIVED Plan DTO shape (nodes + ready_set + has_failed + progress).
	for _, k := range []string{"project_id", "name", "status", "nodes", "ready_set", "has_failed", "progress"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("plan detail missing %q: %v", k, body)
		}
	}
}

// TestGetPlan_WrongProject_404: a plan named under the wrong project is not found.
func TestGetPlan_WrongProject_404(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	other := f.seedForeignProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(other), "plan_id": planID})
	if status != http.StatusNotFound || body["error"] != "not_found" {
		t.Fatalf("status = %d err=%v, want 404 not_found (plan not in project)", status, body["error"])
	}
}

func TestGetPlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "project_id": string(pid), "plan_id": planID})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- list_plans --------------------------------------------------------------

func TestListPlans_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedPlanMember(t) // one plan in pid.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_plans", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid)})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	plans, _ := body["plans"].([]any)
	if len(plans) != 1 {
		t.Fatalf("plans len = %d, want 1; body = %v", len(plans), body)
	}
	row, _ := plans[0].(map[string]any)
	for _, k := range []string{"id", "name", "status", "progress", "has_failed", "node_count", "nodes_preview"} {
		if _, ok := row[k]; !ok {
			t.Fatalf("plan summary missing %q: %v", k, row)
		}
	}
}

func TestListPlans_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_plans", "acat_w1",
		map[string]any{"agent_id": atAgent2, "project_id": string(pid)})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}
