package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// findBuiltinPlan returns the project's auto-created built-in pool (ADR-0047), or
// fails the test if absent.
func findBuiltinPlan(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID) *pm.Plan {
	t.Helper()
	plans, err := h.svc.ListPlans(h.ctx, pid)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	for _, p := range plans {
		if p.IsBuiltin() {
			return p
		}
	}
	t.Fatalf("no built-in pool plan found for project %s", pid)
	return nil
}

// --- Increment 3: auto-create the built-in pool on CreateProject ------------

// TestCreateProject_AutoCreatesBuiltinPool proves CreateProject auto-creates the
// per-project built-in pool: exactly one builtin plan exists, status=running,
// IsBuiltin()==true.
func TestCreateProject_AutoCreatesBuiltinPool(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	plans, err := h.svc.ListPlans(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	builtins := 0
	var pool *pm.Plan
	for _, p := range plans {
		if p.IsBuiltin() {
			builtins++
			pool = p
		}
	}
	if builtins != 1 {
		t.Fatalf("builtin pool count=%d, want exactly 1", builtins)
	}
	if pool.Status() != pm.PlanRunning {
		t.Fatalf("builtin pool status=%s, want running", pool.Status())
	}
	if !pool.IsBuiltin() {
		t.Fatal("pool.IsBuiltin()=false, want true")
	}
}

// --- Increment 4: service guards reject the built-in pool --------------------

// TestBuiltinPool_GuardsReject proves the user-facing service guards reject
// mutating the built-in pool: AddPlanDependency → ErrBuiltinPlanNoEdges,
// DeletePlan + ArchivePlan → ErrBuiltinPlanImmutable.
func TestBuiltinPool_GuardsReject(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)

	// Two tasks selected into the pool (allowed) — endpoints for the edge attempt.
	t0 := h.seedAssignedTask(t, pid, pool.ID(), "p0", "user:x")
	t1 := h.seedAssignedTask(t, pid, pool.ID(), "p1", "user:y")

	if err := h.svc.AddPlanDependency(h.ctx, pool.ID(), t1, t0, "user:a"); err != pm.ErrBuiltinPlanNoEdges {
		t.Fatalf("AddPlanDependency on builtin = %v, want ErrBuiltinPlanNoEdges", err)
	}
	if err := h.svc.DeletePlan(h.ctx, pool.ID(), "user:a"); err != pm.ErrBuiltinPlanImmutable {
		t.Fatalf("DeletePlan on builtin = %v, want ErrBuiltinPlanImmutable", err)
	}
	if err := h.svc.ArchivePlan(h.ctx, pool.ID(), "user:a"); err != pm.ErrBuiltinPlanImmutable {
		t.Fatalf("ArchivePlan on builtin = %v, want ErrBuiltinPlanImmutable", err)
	}
}

// TestBuiltinPool_SelectTaskAllowedWhileRunning proves SelectTaskIntoPlan works on
// the (running) built-in pool — that is how a task ENTERS the claimable pool — even
// though SelectTaskIntoPlan otherwise requires a draft plan (ErrPlanNotDraft).
func TestBuiltinPool_SelectTaskAllowedWhileRunning(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)
	if pool.Status() != pm.PlanRunning {
		t.Fatalf("precondition: pool must be running, got %s", pool.Status())
	}

	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pool task", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	a := "agent:bot"
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Selecting into the RUNNING builtin pool must be allowed (no ErrPlanNotDraft).
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan into running builtin pool = %v, want nil", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.PlanID() != pool.ID() {
		t.Fatalf("task plan_id=%q, want %q", got.PlanID(), pool.ID())
	}
}

// --- Increment 5: dispatch is PULL (no @mention, no wake) for the pool -------

// TestBuiltinPool_DispatchIsPullNoWake proves the built-in pool's dispatch is a
// pull: a task selected into the pool (assigned, open) becomes claimable after a
// dispatch sweep (a dispatch record exists → node_status=dispatched → TaskClaimable
// true) WITHOUT posting an @mention or emitting EvtTaskAssigned (no work-item/wake).
// In contrast, a structured plan's ready node DOES post an @mention.
func TestBuiltinPool_DispatchIsPullNoWake(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)

	// Select an assigned, open task into the pool.
	tid := h.seedAssignedTask(t, pid, pool.ID(), "pool work", "agent:bot")

	// Run the pull dispatch over the pool (the reconcile loop / orchestrator drives
	// this in production; ReconcileRunningPlans sweeps every running plan incl. the
	// always-running pool).
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}

	// A dispatch record exists for the task → node_status=dispatched → claimable.
	detail, err := h.svc.GetPlanDetail(h.ctx, pool.ID())
	if err != nil {
		t.Fatal(err)
	}
	var node *pm.PlanNodeView
	for i := range detail.View.Nodes {
		if detail.View.Nodes[i].TaskID == tid {
			node = &detail.View.Nodes[i]
		}
	}
	if node == nil {
		t.Fatalf("task %s missing from pool view", tid)
	}
	if node.NodeStatus != pm.NodeDispatched {
		t.Fatalf("pool node status=%s, want dispatched (claimable)", node.NodeStatus)
	}
	if !node.Dispatched {
		t.Fatal("pool node Dispatched=false, want true")
	}
	// Pull: the dispatch record carries NO message id (no @mention was posted).
	if node.DispatchMessageID != "" {
		t.Fatalf("pool dispatch message id=%q, want empty (pull = no @mention)", node.DispatchMessageID)
	}
	// The task is claimable.
	got, _ := h.svc.GetTask(h.ctx, tid)
	if !pm.TaskClaimable(got, node.NodeStatus) {
		t.Fatalf("pool task should be claimable: archived=%v status=%s assignee=%s planID=%s node=%s",
			got.IsArchived(), got.Status(), got.Assignee(), got.PlanID(), node.NodeStatus)
	}
	// NO @mention posted: the pool has no bound conversation, so no plan conversation
	// message exists. (planConvMsgCount would fail finding the conversation — assert
	// the pool has no conversation id instead.)
	if pool.ConversationID() != "" {
		t.Fatalf("builtin pool should have no conversation (pull/no-wake), got %q", pool.ConversationID())
	}
}

// TestStructuredPlan_DispatchPostsMention is the contrast case: a structured
// (non-builtin) plan's ready node DOES post an @mention into its conversation (the
// push path), proving the pull/push split is real.
func TestStructuredPlan_DispatchPostsMention(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	plan, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "structured", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, plan, "root", "agent:bot")

	before := h.planConvMsgCount(t, plan)
	if err := h.svc.StartPlan(h.ctx, plan, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	if _, err := h.svc.AdvancePlan(h.ctx, plan, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	h.drain(t)
	after := h.planConvMsgCount(t, plan)
	if after <= before {
		t.Fatalf("structured plan ready node should post an @mention: msgs before=%d after=%d", before, after)
	}
}
