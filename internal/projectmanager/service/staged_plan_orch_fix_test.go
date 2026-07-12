package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// ============================================================================
// issue-77d9beff — three staged-plan orchestration bugs (plan-2846c4cf).
// These tests are RED on the pre-fix code and GREEN after the fixes.
// ============================================================================

// nodeStatusForTask returns the orchestration node status bound to a task id (via
// Task.NodeID → GetNode).
func nodeStatusForTask(t *testing.T, h *planAdvanceHarness, orchSvc *orch.Service, taskID pm.TaskID) orch.NodeStatus {
	t.Helper()
	tk, err := h.tasks.FindByID(h.ctx, taskID)
	if err != nil {
		t.Fatalf("FindByID(%s): %v", taskID, err)
	}
	n, err := orchSvc.GetNode(h.ctx, orch.NodeID(tk.NodeID()))
	if err != nil {
		t.Fatalf("GetNode(%s): %v", tk.NodeID(), err)
	}
	return n.Status()
}

// readyNodeTaskIDs returns the set of business-task ids the ENGINE considers ready.
func readyNodeTaskIDs(t *testing.T, h *planAdvanceHarness, orchSvc *orch.Service, graphID string) map[pm.TaskID]bool {
	t.Helper()
	ready, err := orchSvc.GetReadyNodes(h.ctx, orch.GraphID(graphID))
	if err != nil {
		t.Fatalf("GetReadyNodes: %v", err)
	}
	out := map[pm.TaskID]bool{}
	for _, n := range ready {
		if v, ok := n.Metadata()["task_id"].(string); ok {
			out[pm.TaskID(v)] = true
		}
	}
	return out
}

// Bug ① (self-heal) — reset_task on a STRUCTURED staged-plan node no longer wedges it.
// PD replaced the forbid-guard (ErrResetNotPoolTask) with self-heal: reset runs normally
// (task running→open) and the shared reconcile (reopenStuckPlanNode) reopens the graph
// node + clears its dispatch record so plan advance re-dispatches it.
//
// PRE-FIX (forbid-guard): ResetTask returned ErrResetNotPoolTask — no recovery, no clean-up
// of existing wedges. BEFORE that (the original bug): reset left the node wedged Running
// while the task sat open, never re-dispatchable. This test asserts the self-heal: the node
// heals OFF Running and re-enters the engine ready-set.
func TestResetTask_StructuredStagePlanNode_SelfHeals(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, _, _, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	graphID := p.GraphID()

	// Dispatch a1 (stage A entry) and drive its node to Running.
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); len(d) != 1 || d[0] != a1 {
		t.Fatalf("dispatch #1 = %v, want [a1]", d)
	}
	h.setTaskStatus(t, a1, pm.TaskRunning)
	h.svc.AdvancePlan(ctx, planID, "user:a") // sync flips node open→running
	if got := nodeStatusForTask(t, h, orchSvc, a1); got != orch.NodeRunning {
		t.Fatalf("a1 node = %q, want running before reset", got)
	}

	// Reset the confirmed-dead running task (nil lease → passes the live-lease guard).
	if err := h.svc.ResetTask(ctx, a1, "user:a", false); err != nil {
		t.Fatalf("ResetTask on structured node = %v, want nil (self-heal, not forbid)", err)
	}
	ta1, _ := h.tasks.FindByID(ctx, a1)
	if ta1.Status() != pm.TaskOpen {
		t.Fatalf("a1 task = %q, want open after reset", ta1.Status())
	}
	// The shared reconcile healed the node OFF Running (no open wedge) in the reset tx.
	if got := nodeStatusForTask(t, h, orchSvc, a1); got == orch.NodeRunning {
		t.Fatalf("a1 node still %q after reset — wedged Running while task is open (bug ①)", got)
	}
	// The dispatch record was cleared, so the node re-enters the engine ready-set.
	if dispatchedSet(t, h, planID)[a1] {
		t.Fatal("a1 still has a dispatch record after reset — cannot re-enter the ready-set (bug ①)")
	}
	if !readyNodeTaskIDs(t, h, orchSvc, graphID)[a1] {
		t.Fatal("a1 not in engine ready-set after reset — the reset node is not re-dispatchable (bug ①)")
	}
	// End-to-end: plan advance re-dispatches the reset node.
	d, _ := h.svc.AdvancePlan(ctx, planID, "user:a")
	sawA1 := false
	for _, id := range d {
		if id == a1 {
			sawA1 = true
		}
	}
	if !sawA1 && !dispatchedSet(t, h, planID)[a1] {
		t.Fatal("a1 not re-dispatched by plan advance after reset (bug ①)")
	}
}

// Bug ① (self-heal, unblock path) — unblock_task on a STRUCTURED staged-plan node whose
// graph node is wedged Running (the block terminated the prior WorkItem; the task never left
// running) must ALSO re-dispatch it. Pre-fix unblock only cleared blocked_reason + re-woke
// the (dead) assignee and left the node wedged Running with a stale dispatch record → the
// node never re-entered graphReadySet (block→unblock was a name-only recovery). The shared
// reconcile now reopens the node + clears the record so plan advance re-dispatches it.
func TestUnblockTask_StructuredStagePlanNode_SelfHeals(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, _, _, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	graphID := p.GraphID()

	// Dispatch a1, drive it Running (dispatch record present), then BLOCK it (obstacle):
	// status stays running, lease cleared — the node stays Running (executor gone).
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); len(d) != 1 || d[0] != a1 {
		t.Fatalf("dispatch #1 = %v, want [a1]", d)
	}
	h.setTaskStatus(t, a1, pm.TaskRunning)
	h.svc.AdvancePlan(ctx, planID, "user:a")
	if err := h.svc.BlockTask(ctx, a1, "external blocker", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatalf("BlockTask a1: %v", err)
	}
	h.svc.AdvancePlan(ctx, planID, "user:a") // node stays Running (task still running)
	if got := nodeStatusForTask(t, h, orchSvc, a1); got != orch.NodeRunning {
		t.Fatalf("a1 node = %q, want running while blocked (the wedge)", got)
	}
	if !dispatchedSet(t, h, planID)[a1] {
		t.Fatal("a1 lost its dispatch record before unblock — bad setup")
	}

	// Unblock → the shared reconcile reopens the wedged node + clears its record.
	if err := h.svc.UnblockTask(ctx, UnblockTaskCommand{TaskID: a1, Comment: "resolved", Actor: "user:a"}); err != nil {
		t.Fatalf("UnblockTask a1: %v", err)
	}
	if got := nodeStatusForTask(t, h, orchSvc, a1); got == orch.NodeRunning {
		t.Fatalf("a1 node still %q after unblock — wedged Running, not re-dispatched (bug ①)", got)
	}
	if dispatchedSet(t, h, planID)[a1] {
		t.Fatal("a1 still has a dispatch record after unblock — cannot re-enter the ready-set (bug ①)")
	}
	if !readyNodeTaskIDs(t, h, orchSvc, graphID)[a1] {
		t.Fatal("a1 not in engine ready-set after unblock — the node is not re-dispatchable (bug ①)")
	}
	// End-to-end: plan advance re-dispatches the unblocked node.
	h.svc.AdvancePlan(ctx, planID, "user:a")
	if !dispatchedSet(t, h, planID)[a1] {
		t.Fatal("a1 not re-dispatched by plan advance after unblock (bug ①)")
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
