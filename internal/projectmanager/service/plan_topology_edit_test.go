package service

import (
	"errors"
	"sync"
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

func TestEditPlanTopology_RemoveNodeWithIncidentEdgesValidatesFinalTopology(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "remove", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	c := h.seedAssignedTask(t, pid, planID, "C", "user:c1")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AddPlanDependency(h.ctx, planID, a, c, "user:a"); err != nil {
		t.Fatal(err)
	}
	base := h.planVersion(t, planID)

	if _, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpRemoveEdge, FromTaskID: b, ToTaskID: a},
		TopologyOp{Kind: OpRemoveNode, TaskID: a},
	); err != nil {
		t.Fatalf("remove node plus incident edge: %v", err)
	}
	if got := h.planVersion(t, planID); got != base+1 {
		t.Fatalf("version=%d want %d", got, base+1)
	}
	tk, _ := h.tasks.FindByID(h.ctx, a)
	if tk.PlanID() != "" {
		t.Fatalf("removed task plan_id=%q want backlog", tk.PlanID())
	}
	edges, _ := h.plans.ListDependencies(h.ctx, planID)
	if len(edges) != 0 {
		t.Fatalf("edges=%v, want final topology without incident edges", edges)
	}
}

func TestEditPlanTopology_RemoveNodeHistoryBlockerRollsBackBatch(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "remove-history", CreatedBy: "user:a"})
	h.drain(t)
	removable := h.seedAssignedTask(t, pid, planID, "removable", "user:a1")
	blocked := h.seedAssignedTask(t, pid, planID, "blocked", "user:b1")
	if err := h.plans.RecordDispatch(h.ctx, planID, blocked, h.clk.Now(), "msg-blocked"); err != nil {
		t.Fatal(err)
	}
	base := h.planVersion(t, planID)

	_, err := h.edit(t, planID, base,
		TopologyOp{Kind: OpRemoveNode, TaskID: removable},
		TopologyOp{Kind: OpRemoveNode, TaskID: blocked},
	)
	var nodeErr *pm.PlanNodeNotRemovableError
	if !errors.As(err, &nodeErr) || nodeErr.TaskID != blocked || !hasStringPrefix(nodeErr.HistoryBlockers, "dispatch_record") {
		t.Fatalf("err=%v node=%+v", err, nodeErr)
	}
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want=%d", got, base)
	}
	for _, id := range []pm.TaskID{removable, blocked} {
		tk, _ := h.tasks.FindByID(h.ctx, id)
		if tk.PlanID() != planID {
			t.Fatalf("task %s plan=%q want %q", id, tk.PlanID(), planID)
		}
	}
}

func TestEditPlanTopology_RemoveNodeMissingIsNotSilentNoOp(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "remove-missing", CreatedBy: "user:a"})
	h.drain(t)
	missing := h.seedBacklogAssignedTask(t, pid, "missing", "user:m")
	base := h.planVersion(t, planID)
	_, err := h.edit(t, planID, base, TopologyOp{Kind: OpRemoveNode, TaskID: missing})
	if !errors.Is(err, ErrTaskNotInPlan) {
		t.Fatalf("err=%v, want ErrTaskNotInPlan", err)
	}
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want=%d", got, base)
	}
}

func TestEditPlanTopology_RemoveNodeConcurrentStartLinearizes(t *testing.T) {
	for i := 0; i < 10; i++ {
		h := planAdvanceSetup(t)
		pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "remove-race", CreatedBy: "user:a"})
		h.drain(t)
		a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
		b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
		if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
			t.Fatal(err)
		}
		base := h.planVersion(t, planID)

		var wg sync.WaitGroup
		wg.Add(2)
		var startErr, editErr error
		go func() {
			defer wg.Done()
			startErr = h.svc.StartPlan(h.ctx, planID, "user:a")
		}()
		go func() {
			defer wg.Done()
			_, editErr = h.edit(t, planID, base, TopologyOp{Kind: OpRemoveNode, TaskID: a})
		}()
		wg.Wait()

		tk, err := h.tasks.FindByID(h.ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		records, err := h.plans.ListDispatchRecords(h.ctx, planID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range records {
			if tk.PlanID() == "" && r.TaskID == a {
				t.Fatalf("iteration %d: removed node also has dispatch record; startErr=%v editErr=%v", i, startErr, editErr)
			}
		}
		edges, err := h.plans.ListDependencies(h.ctx, planID)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range edges {
			if tk.PlanID() == "" && (e.FromTaskID == a || e.ToTaskID == a) {
				t.Fatalf("iteration %d: removed node still referenced by edge %+v; startErr=%v editErr=%v", i, e, startErr, editErr)
			}
		}
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
