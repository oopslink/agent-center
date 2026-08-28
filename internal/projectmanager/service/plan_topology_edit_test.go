package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// edit_plan_topology tests (2026-07-05 plan-live-topology-edit design §9):
// CAS conflict, terminal-only validation (reorder mid-cycle passes / terminal
// cycle rejected whole-batch), running-plan in-flight rejection, edit×advance tx
// isolation (both orderings consistent), done-node retention, draft equivalence.
// =============================================================================

// seedBacklogAssignedTask creates + assigns a task but does NOT select it into any
// plan — a backlog node ready to be add_node'd by edit_plan_topology.
func (h *planAdvanceHarness) seedBacklogAssignedTask(t *testing.T, pid pm.ProjectID, title, assignee string) pm.TaskID {
	t.Helper()
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	a := assignee
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatal(err)
	}
	return tid
}

func (h *planAdvanceHarness) planVersion(t *testing.T, planID pm.PlanID) int {
	t.Helper()
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	return p.Version()
}

func (h *planAdvanceHarness) edit(t *testing.T, planID pm.PlanID, base int, ops ...TopologyOp) ([]pm.TaskID, error) {
	t.Helper()
	return h.svc.EditPlanTopology(h.ctx, EditPlanTopologyCommand{
		PlanID: planID, BaseVersion: base, Ops: ops, Actor: "user:a",
	})
}

func (h *planAdvanceHarness) nodeStatus(t *testing.T, planID pm.PlanID) map[pm.TaskID]pm.NodeStatus {
	t.Helper()
	detail, err := h.svc.GetPlanDetail(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[pm.TaskID]pm.NodeStatus{}
	for _, n := range detail.View.Nodes {
		out[n.TaskID] = n.NodeStatus
	}
	return out
}

// TestEditPlanTopology_DraftEquivalence: building a DAG via edit_plan_topology on a
// DRAFT plan (add_node ×2 + add_edge) is equivalent to the old per-op flow — the plan
// starts and dispatches A (root) first, B only after A completes. Each edit is exactly
// ONE version increment.
func TestEditPlanTopology_DraftEquivalence(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "draft", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedBacklogAssignedTask(t, pid, "A", "user:a1")
	b := h.seedBacklogAssignedTask(t, pid, "B", "user:b1")

	base := h.planVersion(t, planID)
	if _, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpAddNode, TaskID: a},
		TopologyOp{Kind: OpAddNode, TaskID: b},
		TopologyOp{Kind: OpAddEdge, FromTaskID: b, ToTaskID: a}, // B depends_on A
	); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := h.planVersion(t, planID); got != base+1 {
		t.Fatalf("version=%d want %d (one commit = one increment)", got, base+1)
	}
	// Both tasks are now nodes of the plan.
	tasks, _ := h.tasks.ListByPlan(h.ctx, planID)
	if len(tasks) != 2 {
		t.Fatalf("plan has %d nodes, want 2", len(tasks))
	}
	h.drain(t)

	// Start + dispatch: A first (root), B blocked on A.
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	d1, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if len(d1) != 1 || d1[0] != a {
		t.Fatalf("dispatch #1 = %v, want [A]=%v", d1, a)
	}
	h.setTaskStatus(t, a, pm.TaskRunning)
	h.setTaskStatus(t, a, pm.TaskCompleted)
	d2, _ := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if len(d2) != 1 || d2[0] != b {
		t.Fatalf("dispatch #2 = %v, want [B]=%v (unblocked after A)", d2, b)
	}
}

// TestEditPlanTopology_CASConflict: a stale base_version is rejected with
// ErrPlanVersionConflict; the first commit at the fresh version succeeds.
func TestEditPlanTopology_CASConflict(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "cas", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedBacklogAssignedTask(t, pid, "A", "user:a1")
	b := h.seedBacklogAssignedTask(t, pid, "B", "user:b1")

	base := h.planVersion(t, planID)
	if _, err := h.edit(t, planID, base, TopologyOp{Kind: OpAddNode, TaskID: a}); err != nil {
		t.Fatalf("first edit: %v", err)
	}
	// Second edit reusing the STALE base_version → conflict.
	_, err := h.edit(t, planID, base, TopologyOp{Kind: OpAddNode, TaskID: b})
	if !errors.Is(err, pm.ErrPlanVersionConflict) {
		t.Fatalf("stale edit err=%v, want ErrPlanVersionConflict", err)
	}
	// B was NOT added (whole batch rolled back).
	tasks, _ := h.tasks.ListByPlan(h.ctx, planID)
	if len(tasks) != 1 {
		t.Fatalf("plan has %d nodes, want 1 (conflicting edit rolled back)", len(tasks))
	}
	// Retrying at the fresh version succeeds.
	if _, err := h.edit(t, planID, base+1, TopologyOp{Kind: OpAddNode, TaskID: b}); err != nil {
		t.Fatalf("retry at fresh version: %v", err)
	}
}

// TestEditPlanTopology_ReorderMidCycleTerminalLegal: a batch whose ops pass through a
// transient cycle (add the reverse edge, THEN remove the original) is accepted because
// only the TERMINAL shape is validated — and it is acyclic.
func TestEditPlanTopology_ReorderMidCycleTerminalLegal(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "reorder", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedBacklogAssignedTask(t, pid, "A", "user:a1")
	b := h.seedBacklogAssignedTask(t, pid, "B", "user:b1")
	base := h.planVersion(t, planID)
	// Establish A depends_on B (edge from=A,to=B).
	if _, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpAddNode, TaskID: a},
		TopologyOp{Kind: OpAddNode, TaskID: b},
		TopologyOp{Kind: OpAddEdge, FromTaskID: a, ToTaskID: b},
	); err != nil {
		t.Fatalf("setup edit: %v", err)
	}
	base = h.planVersion(t, planID)
	// Reverse the edge in one batch: add B→A first (⇒ transient A↔B cycle), then remove
	// A→B. Terminal = {B depends_on A}, acyclic → must PASS.
	if _, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpAddEdge, FromTaskID: b, ToTaskID: a},
		TopologyOp{Kind: OpRemoveEdge, FromTaskID: a, ToTaskID: b},
	); err != nil {
		t.Fatalf("reorder edit rejected: %v (want pass — terminal is acyclic)", err)
	}
	edges, _ := h.plans.ListDependencies(h.ctx, planID)
	if len(edges) != 1 || edges[0].FromTaskID != b || edges[0].ToTaskID != a {
		t.Fatalf("terminal edges=%v, want single B→A", edges)
	}
}

// TestEditPlanTopology_TerminalCycleRejected: a batch whose TERMINAL shape is cyclic is
// rejected whole (ErrPlanCycle); nothing is persisted and the version is unchanged.
func TestEditPlanTopology_TerminalCycleRejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "cycle", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedBacklogAssignedTask(t, pid, "A", "user:a1")
	b := h.seedBacklogAssignedTask(t, pid, "B", "user:b1")
	base := h.planVersion(t, planID)
	_, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpAddNode, TaskID: a},
		TopologyOp{Kind: OpAddNode, TaskID: b},
		TopologyOp{Kind: OpAddEdge, FromTaskID: a, ToTaskID: b}, // A→B
		TopologyOp{Kind: OpAddEdge, FromTaskID: b, ToTaskID: a}, // B→A ⇒ terminal cycle
	)
	if !errors.Is(err, pm.ErrPlanCycle) {
		t.Fatalf("err=%v, want ErrPlanCycle", err)
	}
	// Nothing persisted: no nodes, version unchanged.
	if tasks, _ := h.tasks.ListByPlan(h.ctx, planID); len(tasks) != 0 {
		t.Fatalf("plan has %d nodes, want 0 (whole batch rejected)", len(tasks))
	}
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want %d (unchanged on rejection)", got, base)
	}
}

func TestEditPlanTopology_RemovePendingNodeWithIncidentEdges_OK(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "remove pending", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	c := h.seedAssignedTask(t, pid, planID, "C", "user:c1")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency B->A: %v", err)
	}
	if err := h.svc.AddPlanDependency(h.ctx, planID, c, b, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency C->B: %v", err)
	}

	base := h.planVersion(t, planID)
	if _, err := h.edit(t, planID, base, TopologyOp{Kind: OpRemoveNode, TaskID: b}); err != nil {
		t.Fatalf("remove pending node with incident edges: %v", err)
	}
	tb, err := h.tasks.FindByID(h.ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if tb.PlanID() != "" {
		t.Fatalf("removed task plan_id=%q, want backlog", tb.PlanID())
	}
	edges, err := h.plans.ListDependencies(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 0 {
		t.Fatalf("incident edges after remove = %+v, want none", edges)
	}
	if got := h.planVersion(t, planID); got != base+1 {
		t.Fatalf("version=%d want %d", got, base+1)
	}
}

func TestEditPlanTopology_RemoveEdgeThenRemoveNode_ValidatesFinalTopology(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "batch final", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency: %v", err)
	}

	base := h.planVersion(t, planID)
	if _, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpRemoveEdge, FromTaskID: b, ToTaskID: a},
		TopologyOp{Kind: OpRemoveNode, TaskID: b},
	); err != nil {
		t.Fatalf("batch remove_edge+remove_node rejected: %v", err)
	}
	tb, _ := h.tasks.FindByID(h.ctx, b)
	if tb.PlanID() != "" {
		t.Fatalf("B plan_id=%q want backlog", tb.PlanID())
	}
	edges, _ := h.plans.ListDependencies(h.ctx, planID)
	if len(edges) != 0 {
		t.Fatalf("edges=%+v want none", edges)
	}
}

func TestEditPlanTopology_RemoveNode_DispatchHistoryBlocksAndRollsBack(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "dispatch history", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency: %v", err)
	}
	if err := h.plans.RecordDispatch(h.ctx, planID, b, h.clk.Now(), "msg-b"); err != nil {
		t.Fatalf("RecordDispatch: %v", err)
	}

	base := h.planVersion(t, planID)
	_, err := h.edit(t, planID, base, TopologyOp{Kind: OpRemoveNode, TaskID: b})
	if !errors.Is(err, pm.ErrPlanNodeInFlight) {
		t.Fatalf("remove dispatched node err=%v, want ErrPlanNodeInFlight", err)
	}
	tb, _ := h.tasks.FindByID(h.ctx, b)
	if tb.PlanID() != planID {
		t.Fatalf("B plan_id=%q want %q after rollback", tb.PlanID(), planID)
	}
	edges, _ := h.plans.ListDependencies(h.ctx, planID)
	if len(edges) != 1 || edges[0].FromTaskID != b || edges[0].ToTaskID != a {
		t.Fatalf("edges after rejected remove=%+v, want original B->A", edges)
	}
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want unchanged %d", got, base)
	}
}

func TestEditPlanTopology_RemoveNode_HistoryBlocks(t *testing.T) {
	tests := []struct {
		name string
		mark func(*testing.T, *planAdvanceHarness, pm.ProjectID, pm.PlanID, pm.TaskID)
	}{
		{
			name: "action",
			mark: func(t *testing.T, h *planAdvanceHarness, _ pm.ProjectID, _ pm.PlanID, taskID pm.TaskID) {
				t.Helper()
				tk, err := h.tasks.FindByID(h.ctx, taskID)
				if err != nil {
					t.Fatal(err)
				}
				if err := tk.RecordAgentStarted("user:a", h.clk.Now()); err != nil {
					t.Fatal(err)
				}
				if err := h.actionLogs.Append(h.ctx, taskID, tk.ActionLogs()); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "review",
			mark: func(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, _ pm.PlanID, taskID pm.TaskID) {
				t.Helper()
				h.svc.recordChange(h.ctx, pm.AuditEntry{
					ProjectID: pid, ObjectType: pm.AuditObjectTask,
					ObjectID: string(taskID), ChangeType: pm.AuditTaskReviewVerdict, ActorRef: "user:a",
				})
			},
		},
		{
			name: "gate",
			mark: func(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, planID pm.PlanID, taskID pm.TaskID) {
				t.Helper()
				v, err := pm.NewGateVerdict(pm.GateVerdict{
					ID: "verdict-1", ProjectID: pid, PlanID: planID, StageID: "stage-1",
					GateTaskID: taskID, Outcome: pm.GateVerdictPass, Evidence: "accepted",
					ReviewedSHA: "abc123", ActorRef: "user:a", IdempotencyKey: "gate-key", CreatedAt: h.clk.Now(),
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.svc.remediation.SaveVerdict(h.ctx, v); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "terminal",
			mark: func(t *testing.T, h *planAdvanceHarness, _ pm.ProjectID, _ pm.PlanID, taskID pm.TaskID) {
				t.Helper()
				h.setTaskStatus(t, taskID, pm.TaskRunning)
				h.setTaskStatus(t, taskID, pm.TaskCompleted)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := planAdvanceSetup(t)
			pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
			planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: tc.name, CreatedBy: "user:a"})
			h.drain(t)
			taskID := h.seedAssignedTask(t, pid, planID, "node", "user:a1")
			tc.mark(t, h, pid, planID, taskID)

			base := h.planVersion(t, planID)
			_, err := h.edit(t, planID, base, TopologyOp{Kind: OpRemoveNode, TaskID: taskID})
			if !errors.Is(err, pm.ErrPlanNodeInFlight) {
				t.Fatalf("remove node with %s history err=%v, want ErrPlanNodeInFlight", tc.name, err)
			}
			tk, _ := h.tasks.FindByID(h.ctx, taskID)
			if tk.PlanID() != planID {
				t.Fatalf("task plan_id=%q want %q after rejected remove", tk.PlanID(), planID)
			}
			if got := h.planVersion(t, planID); got != base {
				t.Fatalf("version=%d want unchanged %d", got, base)
			}
		})
	}
}

// startRunningPlanAB builds + starts a running plan with B depends_on A and dispatches
// the root A (so A has a dispatch record). Returns the ids.
func (h *planAdvanceHarness) startRunningPlanAB(t *testing.T, pid pm.ProjectID, planID pm.PlanID) (a, b pm.TaskID) {
	t.Helper()
	a = h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b = h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency: %v", err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if d, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil || len(d) != 1 || d[0] != a {
		t.Fatalf("baseline advance d=%v err=%v, want [A]", d, err)
	}
	return a, b
}

// TestEditPlanTopology_RunningFailClosed: live topology edits are no longer
// accepted once execution has started. Evolution owns running-plan changes.
func TestEditPlanTopology_RunningFailClosed(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "inflight", CreatedBy: "user:a"})
	h.drain(t)
	a, _ := h.startRunningPlanAB(t, pid, planID)
	h.setTaskStatus(t, a, pm.TaskRunning) // A is dispatched + running → immutable
	c := h.seedBacklogAssignedTask(t, pid, "C", "user:c1")
	base := h.planVersion(t, planID)

	// add_edge whose `from` is the running node A → structure of A changes → rejected.
	_, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpAddNode, TaskID: c},
		TopologyOp{Kind: OpAddEdge, FromTaskID: a, ToTaskID: c}, // A depends_on C — touches A's in-edges
	)
	if !errors.Is(err, pm.ErrPlanNotPending) {
		t.Fatalf("add_edge on running plan: err=%v, want ErrPlanNotPending", err)
	}
	// remove_node of the in-flight node A is rejected by the same status guard.
	_, err = h.edit(t, planID, base, TopologyOp{Kind: OpRemoveNode, TaskID: a})
	if !errors.Is(err, pm.ErrPlanNotPending) {
		t.Fatalf("remove_node on running plan: err=%v, want ErrPlanNotPending", err)
	}
	// Version unchanged (both rejected, nothing committed).
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want %d (rejected edits do not commit)", got, base)
	}
}

func TestEditPlanTopology_PausedFailClosed(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "paused", CreatedBy: "user:a"})
	h.drain(t)
	h.startRunningPlanAB(t, pid, planID)
	if err := h.svc.PausePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("PausePlan: %v", err)
	}
	base := h.planVersion(t, planID)
	c := h.seedBacklogAssignedTask(t, pid, "C", "user:c1")
	_, err := h.edit(t, planID, base, TopologyOp{Kind: OpAddNode, TaskID: c})
	if !errors.Is(err, pm.ErrPlanNotPending) {
		t.Fatalf("edit on paused plan err=%v, want ErrPlanNotPending", err)
	}
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want %d (paused edit rejected)", got, base)
	}
}
