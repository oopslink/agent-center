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

func TestCreateStage_RejectsMissingHumanGateContract(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_stage", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "name": "Acceptance", "evaluator_kind": "human"})
	if status != http.StatusBadRequest || body["error"] != "missing_gate_contract" {
		t.Fatalf("status=%d body=%v, want 400 missing_gate_contract", status, body)
	}
}

func TestCreateStage_PersistsHumanGateContract(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_stage", "acat_w1", map[string]any{
		"agent_id": atAgent1, "plan_id": planID, "name": "Acceptance",
		"evaluator_kind": "human", "assignee_ref": "agent:" + atAgent1,
		"role_ref": "reviewer", "acceptance_contract": "Verify API, DB, and browser evidence.",
		"pass_route": "downstream", "reject_route": "reopen_stage", "exhausted_route": "escalate",
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	detail, err := f.pmSvc.GetStage(context.Background(), pm.StageID(body["stage_id"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if got := detail.Stage.GateSpec(); got.AcceptanceContract != "Verify API, DB, and browser evidence." || got.RoleRef != "reviewer" {
		t.Fatalf("gate spec = %+v", got)
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

// --- delete_plan -------------------------------------------------------------

func TestDeletePlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID) // a task selected into the plan.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/delete_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["deleted"] != true {
		t.Fatalf("status = %d body=%v, want 200 deleted=true", status, body)
	}
	// The plan is gone.
	if _, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID)); err == nil {
		t.Fatalf("plan still exists after delete")
	}
	// The task is unloaded back to the backlog (not deleted), plan_id cleared.
	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatalf("task should survive plan delete: %v", err)
	}
	if tk.PlanID() != "" {
		t.Fatalf("task plan_id = %q, want empty after plan delete", tk.PlanID())
	}
}

// TestDeletePlan_Running_Surfaced: a RUNNING plan can't be deleted → ErrPlanRunning
// surfaced as 409 plan_conflict.
func TestDeletePlan_Running_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/delete_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusConflict || body["error"] != "plan_conflict" {
		t.Fatalf("status = %d err=%v, want 409 plan_conflict (ErrPlanRunning)", status, body["error"])
	}
}

func TestDeletePlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/delete_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- archive_plan ------------------------------------------------------------

func TestArchivePlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/archive_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	// Returns the archived Plan detail (same shape get_plan emits).
	if body["id"] != planID || body["status"] != string(pm.PlanArchived) {
		t.Fatalf("archive body id/status = %v/%v, want %s/archived", body["id"], body["status"], planID)
	}
	p, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != pm.PlanArchived {
		t.Fatalf("plan status = %s, want archived", p.Status())
	}
	// CASCADE: the plan's task is archived too.
	tk, _ := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if !tk.IsArchived() {
		t.Fatalf("task not archived after plan archive")
	}
}

// TestArchivePlan_Running_Surfaced: a RUNNING plan can't be archived → ErrPlanRunning
// surfaced as 409 plan_conflict.
func TestArchivePlan_Running_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/archive_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusConflict || body["error"] != "plan_conflict" {
		t.Fatalf("status = %d err=%v, want 409 plan_conflict (ErrPlanRunning)", status, body["error"])
	}
}

func TestArchivePlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/archive_plan", "acat_w1",
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

// TestGetPlan_NonMember_403: issue I44 — get_plan now enforces caller membership
// (GetPlanDetailForMember), closing the prior gap where only a plan-in-project name
// match was checked (any agent on a worker could read a plan whose project_id +
// plan_id it could name). An agent that is NOT a member of the plan's project gets
// 403 (ErrNotMember → terminal), even when it names the plan's true project_id.
func TestGetPlan_NonMember_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // AG1 resolves (member of SOME project)…
	// …but the plan lives in a project AG1 is NOT a member of.
	foreign := f.seedForeignProject(t)
	planID, err := f.pmSvc.CreatePlan(context.Background(), pmservice.CreatePlanCommand{
		ProjectID: foreign, Name: "secret plan", CreatedBy: pm.IdentityRef("user:other"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(foreign), "plan_id": string(planID)})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d body=%v, want 403 (non-member may not read a foreign project's plan)", status, body)
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
	// "Plan A" + the ADR-0047 auto-created "[Built-in]" pool.
	if len(plans) != 2 {
		t.Fatalf("plans len = %d, want 2; body = %v", len(plans), body)
	}
	// Locate the structured "Plan A" row (not the built-in pool).
	var row map[string]any
	for _, p := range plans {
		m, _ := p.(map[string]any)
		if m["name"] == "Plan A" {
			row = m
		}
	}
	if row == nil {
		t.Fatalf("Plan A missing from list: %v", plans)
	}
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

// TestListPlans_Pagination verifies the page window + total/has_more + cap for
// list_plans. The page is applied to the plan rows before view derivation; the
// builtin pool plan (if any) counts toward total, so the test pages through and
// asserts the unique ids collected equal the reported total.
func TestListPlans_Pagination(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := f.pmSvc.CreatePlan(ctx, pmservice.CreatePlanCommand{
			ProjectID: pid, Name: "P", CreatedBy: pm.IdentityRef("agent:" + atAgent1),
		}); err != nil {
			t.Fatal(err)
		}
		f.drain(t)
	}
	srv := f.server(t)

	call := func(body map[string]any) map[string]any {
		status, resp := postBearer(t, srv.URL, "/admin/agent-tools/list_plans", "acat_w1", body)
		if status != http.StatusOK {
			t.Fatalf("list_plans status=%d body=%v", status, resp)
		}
		return resp
	}
	ids := func(resp map[string]any) []string {
		raw, _ := resp["plans"].([]any)
		out := make([]string, 0, len(raw))
		for _, x := range raw {
			out = append(out, x.(map[string]any)["id"].(string))
		}
		return out
	}

	p1 := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 2})
	total := int(p1["total"].(float64))
	if total < 4 { // the 4 created (plus possibly the builtin pool)
		t.Fatalf("plans total=%d want >=4", total)
	}
	if int(p1["page_size"].(float64)) != 2 {
		t.Fatalf("plans page_size=%v want 2", p1["page_size"])
	}
	// Page through; the unique ids collected must equal the reported total (no dups).
	seen := map[string]bool{}
	for off := 0; off < total; off += 2 {
		for _, id := range ids(call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 2, "offset": off})) {
			if seen[id] {
				t.Fatalf("duplicate plan id across pages: %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("paged unique plans=%d want %d", len(seen), total)
	}
	capped := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 100000})
	if int(capped["page_size"].(float64)) != agentListMaxPageSize {
		t.Fatalf("plans page_size cap=%v want %d", capped["page_size"], agentListMaxPageSize)
	}
}
