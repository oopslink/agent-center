package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ============================================================================
// issue-77d9beff — three staged-plan orchestration bugs (plan-2846c4cf).
// These tests are RED on the pre-fix code and GREEN after the fixes.
// ============================================================================

// startedStageEntry drives stage A's entry a1 to RUNNING (dispatch + StartTask under a
// live lease) in a two-stage plan, returning the harness + the running structured node.
func startedStageEntry(t *testing.T, h *planAdvanceHarness) (pm.ProjectID, pm.PlanID, pm.TaskID) {
	t.Helper()
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, _, _, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	addMember(t, h, pid, "user:a1")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	if err := h.svc.StartTask(ctx, a1, "user:a1"); err != nil {
		t.Fatalf("StartTask a1: %v", err)
	}
	return pid, planID, a1
}

// Bug ① — reset_task on a STRUCTURED staged-plan node is rejected (it is a built-in
// pool-recovery tool only). Pre-fix ResetTask returned nil and dropped the node into an
// unrecoverable open-wedge (open, no assignee, no re-dispatch path).
func TestResetTask_RejectsStructuredStagePlanNode(t *testing.T) {
	h, _, _ := autoAssignInject(t)
	pid, _, a1 := startedStageEntry(t, h)
	addMember(t, h, pid, "agent:pd")

	err := h.svc.ResetTask(h.ctx, a1, "user:a1", true) // owner-confirmed-dead
	if !errors.Is(err, pm.ErrResetNotPoolTask) {
		t.Fatalf("ResetTask on structured node = %v, want ErrResetNotPoolTask", err)
	}
	got, _ := h.svc.GetTask(h.ctx, a1)
	if got.Status() != pm.TaskRunning {
		t.Fatalf("structured node status=%s after rejected reset, want running (untouched)", got.Status())
	}
	if got.Assignee() != "user:a1" {
		t.Fatalf("structured node assignee=%q after rejected reset, want user:a1 (untouched)", got.Assignee())
	}
}

// Bug ② — get_plan / DerivePlanView is stage-aware for the READ model: a downstream
// stage entry held behind an UNRESOLVED upstream stage gate shows `blocked`, NOT `ready`,
// and is out of ready_set. Pre-fix it derived `ready` and entered ready_set (read model
// lied, misleading the orchestrator to assign+start a stage-blocked node).
func TestGetPlanDetail_StageBarrierHeldEntryNotReady(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	_, _, b1, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}

	detail, err := h.svc.GetPlanDetailForMember(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("GetPlanDetailForMember: %v", err)
	}
	// b1 (stage B entry) is behind the unresolved stage A gate → must be blocked.
	for _, n := range detail.View.Nodes {
		if n.TaskID == b1 && n.NodeStatus == pm.NodeReady {
			t.Fatalf("b1 (stage-barrier held) NodeStatus=ready, want blocked (read model must be stage-aware)")
		}
	}
	for _, id := range detail.View.ReadySet {
		if id == b1 {
			t.Fatalf("b1 (stage-barrier held) in ready_set, want excluded")
		}
	}
	// The gate node(s) must be surfaced so the plan owner can resolve them.
	if len(detail.Gates) == 0 {
		t.Fatalf("get_plan surfaced no gate nodes; want the stage gate exposed for the owner to resolve")
	}
}

// Bug ③ — the dispatcher must NOT run a downstream stage's business node ahead of its
// upstream stage gates. A two-stage plan: b1 must not be dispatched nor runnable while
// stage A's gate is unresolved.
func TestDispatch_NoStageRunAhead(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	// Drive stage A's members to done WITHOUT resolving the gate.
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	d, _ := h.svc.AdvancePlan(ctx, planID, "user:a")
	for _, id := range d {
		if id == b1 {
			t.Fatalf("b1 dispatched while stage A gate unresolved — run-ahead (dispatch)")
		}
	}
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatalf("b1 has a dispatch record while stage A gate unresolved — run-ahead")
	}
	if err := h.svc.EnsureTaskRunnable(ctx, b1); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("EnsureTaskRunnable(b1) = %v, want ErrTaskNotRunnable (barrier holds)", err)
	}
}
