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

// TestEditPlanTopology_RunningInFlightRejected: on a RUNNING plan, editing the in-edges
// of a dispatched/running node is rejected with ErrPlanNodeInFlight; removing an
// in-flight node is likewise rejected.
func TestEditPlanTopology_RunningInFlightRejected(t *testing.T) {
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
	if !errors.Is(err, pm.ErrPlanNodeInFlight) {
		t.Fatalf("add_edge on running node A: err=%v, want ErrPlanNodeInFlight", err)
	}
	// remove_node of the in-flight node A → rejected.
	_, err = h.edit(t, planID, base, TopologyOp{Kind: OpRemoveNode, TaskID: a})
	if !errors.Is(err, pm.ErrPlanNodeInFlight) {
		t.Fatalf("remove_node of running A: err=%v, want ErrPlanNodeInFlight", err)
	}
	// Version unchanged (both rejected, nothing committed).
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want %d (rejected edits do not commit)", got, base)
	}
}

// TestEditPlanTopology_EditAdvanceIsolation covers BOTH orderings of the edit×advance
// race with a consistent outcome (design §4, tx isolation):
//
//   - ADVANCE-first: the advance dispatched A; a later edit that restructures A sees
//     A's dispatch record → A immutable → ErrPlanNodeInFlight (the advance "won").
//   - EDIT-first: an edit adds a new ROOT node C and commits the new topology; C is
//     dispatched inside the SAME commit, and a following advance is idempotent — the
//     new node is live under the new topology (the edit "won", advance sees it).
func TestEditPlanTopology_EditAdvanceIsolation(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// --- ADVANCE-first: advance dispatched A, edit on A rejected. ---
	planA, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "advance-first", CreatedBy: "user:a"})
	h.drain(t)
	a, _ := h.startRunningPlanAB(t, pid, planA) // A dispatched by baseline advance
	baseA := h.planVersion(t, planA)
	d := h.seedBacklogAssignedTask(t, pid, "D", "user:d1")
	_, err := h.edit(t, planA, baseA,
		TopologyOp{Kind: OpAddNode, TaskID: d},
		TopologyOp{Kind: OpAddEdge, FromTaskID: a, ToTaskID: d}, // restructures dispatched A
	)
	if !errors.Is(err, pm.ErrPlanNodeInFlight) {
		t.Fatalf("advance-first: edit err=%v, want ErrPlanNodeInFlight (advance dispatched A)", err)
	}

	// --- EDIT-first: edit adds a new root C, dispatched in the edit; advance idempotent. ---
	planB, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "edit-first", CreatedBy: "user:a"})
	h.drain(t)
	a2, _ := h.startRunningPlanAB(t, pid, planB) // A dispatched; B blocked (undispatched)
	_ = a2
	baseB := h.planVersion(t, planB)
	c := h.seedBacklogAssignedTask(t, pid, "C", "user:c1")
	dispatched, err := h.edit(t, planB, baseB, TopologyOp{Kind: OpAddNode, TaskID: c}) // new root, no deps
	if err != nil {
		t.Fatalf("edit-first: %v", err)
	}
	if len(dispatched) != 1 || dispatched[0] != c {
		t.Fatalf("edit-first: dispatched=%v, want [C]=%v (new root dispatched in the edit)", dispatched, c)
	}
	// A following advance sees the new topology and is idempotent (C already dispatched).
	if again, _ := h.svc.AdvancePlan(h.ctx, planB, "user:a"); len(again) != 0 {
		t.Fatalf("edit-first: follow-up advance dispatched %v, want [] (idempotent under new topology)", again)
	}
	if ns := h.nodeStatus(t, planB); ns[c] != pm.NodeDispatched {
		t.Fatalf("edit-first: node C status=%s, want dispatched", ns[c])
	}
}

// TestEditPlanTopology_DoneNodeRetainedAcrossEdit: completing A (node done) then editing
// the plan (add a new node) keeps A's done state — an executed node is not disturbed by
// a live topology edit.
func TestEditPlanTopology_DoneNodeRetainedAcrossEdit(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "retain", CreatedBy: "user:a"})
	h.drain(t)
	a, b := h.startRunningPlanAB(t, pid, planID)
	h.setTaskStatus(t, a, pm.TaskRunning)
	h.setTaskStatus(t, a, pm.TaskCompleted)
	// advance so A's node syncs to completed and B becomes ready.
	if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("advance after A done: %v", err)
	}
	if ns := h.nodeStatus(t, planID); ns[a] != pm.NodeDone {
		t.Fatalf("A node status=%s, want done before edit", ns[a])
	}
	// Edit: add a new node C depending on B. A (done) is untouched structurally.
	c := h.seedBacklogAssignedTask(t, pid, "C", "user:c1")
	base := h.planVersion(t, planID)
	if _, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpAddNode, TaskID: c},
		TopologyOp{Kind: OpAddEdge, FromTaskID: c, ToTaskID: b}, // C depends_on B (B is dispatched but `to` may be any state)
	); err != nil {
		t.Fatalf("edit adding C: %v", err)
	}
	ns := h.nodeStatus(t, planID)
	if ns[a] != pm.NodeDone {
		t.Fatalf("A node status=%s after edit, want done (retained across edit)", ns[a])
	}
	if _, ok := ns[c]; !ok {
		t.Fatal("C not present after edit")
	}
}
