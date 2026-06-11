package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// The P2-3 reconcile tests use the BASE planAdvanceHarness (planAdvanceSetup) —
// whose relay does NOT include the PlanOrchestratorProjector — so the push path
// never fires. This lets us simulate a MISSED event (a node ready but never
// dispatched) and assert the reconciliation sweep (ReconcileRunningPlans) is the
// safety net that dispatches it.

// TestReconcile_DispatchesMissedReadyNode simulates a missed pm.plan.started /
// pm.task.state_changed: a running plan A→B with A done leaves B `ready` but
// undispatched (no orchestrator on this relay). ReconcileRunningPlans dispatches
// the ready nodes (A initially, then B after A completes).
func TestReconcile_DispatchesMissedReadyNode(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "miss", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t) // no orchestrator → plan.started NOT acted on → A undispatched.

	recs, _ := h.plans.ListDispatchRecords(ctx, planID)
	if dispatchCount(recs, a) != 0 {
		t.Fatalf("A dispatched before reconcile (count=%d), want 0 (missed-event sim)", dispatchCount(recs, a))
	}

	// Reconcile sweep dispatches the missed ready node A.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	recs, _ = h.plans.ListDispatchRecords(ctx, planID)
	if dispatchCount(recs, a) != 1 {
		t.Fatalf("reconcile dispatched A %d times, want 1", dispatchCount(recs, a))
	}
	if dispatchCount(recs, b) != 0 {
		t.Fatalf("B dispatched %d times while A not done, want 0 (blocked)", dispatchCount(recs, b))
	}

	// A completes (still no orchestrator) → B becomes ready but undispatched; the
	// next reconcile sweep dispatches it.
	h.setTaskStatus(t, a, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans (2): %v", err)
	}
	recs, _ = h.plans.ListDispatchRecords(ctx, planID)
	if dispatchCount(recs, b) != 1 {
		t.Fatalf("reconcile dispatched B %d times after A done, want 1", dispatchCount(recs, b))
	}
}

// TestReconcile_Idempotent asserts the sweep dispatches NOTHING when the ready
// nodes are already dispatched (no double @mention). Run ReconcileRunningPlans
// twice over the same plan; the second sweep adds no records and posts no
// messages (§9.3: INSERT-OR-IGNORE record + derived NodeDispatched).
func TestReconcile_Idempotent(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "idem", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	_ = h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// First sweep dispatches the ready (no-upstream) nodes A and B.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	afterFirst, _ := h.plans.ListDispatchRecords(ctx, planID)
	msgsAfterFirst := h.planConvMsgCount(t, planID)
	if dispatchCount(afterFirst, a) != 1 {
		t.Fatalf("first sweep dispatched A %d times, want 1", dispatchCount(afterFirst, a))
	}

	// Second sweep: everything ready is already dispatched → no new records, no
	// new messages.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans (2): %v", err)
	}
	afterSecond, _ := h.plans.ListDispatchRecords(ctx, planID)
	if len(afterSecond) != len(afterFirst) {
		t.Fatalf("second sweep changed dispatch record count: %d → %d (want stable — idempotent)", len(afterFirst), len(afterSecond))
	}
	if got := h.planConvMsgCount(t, planID); got != msgsAfterFirst {
		t.Fatalf("second sweep posted %d extra messages, want 0 (idempotent)", got-msgsAfterFirst)
	}
}

// TestReconcile_SkipsDraftAndDone asserts the sweep touches ONLY running plans:
// a draft plan and a done plan are not dispatched into.
func TestReconcile_SkipsDraftAndDone(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// Draft plan with a ready node — must NOT be dispatched (never started).
	draftID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "draft", CreatedBy: "user:a"})
	h.drain(t)
	_ = h.seedAssignedTask(t, pid, draftID, "D", "user:x")

	// Done plan: a single pre-done task; start then reconcile marks it done.
	doneID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "done", CreatedBy: "user:a"})
	h.drain(t)
	dt := h.seedAssignedTask(t, pid, doneID, "X", "agent:42")
	h.setTaskStatus(t, dt, pm.TaskCompleted) // pre-done
	if err := h.svc.StartPlan(ctx, doneID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// Reconcile: the all-done plan transitions running→done and dispatches nothing.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	donePlan, _ := h.plans.FindByID(ctx, doneID)
	if donePlan.Status() != pm.PlanDone {
		t.Fatalf("all-done plan status %q after reconcile, want done", donePlan.Status())
	}

	// A second sweep: the done plan is excluded by ListRunningPlans → no dispatch;
	// the draft plan was never running → no dispatch.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans (2): %v", err)
	}
	if recs, _ := h.plans.ListDispatchRecords(ctx, draftID); len(recs) != 0 {
		t.Fatalf("draft plan got %d dispatch records, want 0 (never running)", len(recs))
	}
	if recs, _ := h.plans.ListDispatchRecords(ctx, doneID); dispatchCount(recs, dt) != 0 {
		t.Fatal("done plan's pre-done task was dispatched, want 0 (done node never ready)")
	}
}

// TestListRunningPlans_OnlyRunning asserts ListRunningPlans returns exactly the
// running plans across projects — not draft, not done.
func TestListRunningPlans_OnlyRunning(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// draft (never started)
	draftID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "draft", CreatedBy: "user:a"})
	h.drain(t)
	_ = h.seedAssignedTask(t, pid, draftID, "D", "user:x")

	// running
	runID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "run", CreatedBy: "user:a"})
	h.drain(t)
	_ = h.seedAssignedTask(t, pid, runID, "R", "user:y")
	if err := h.svc.StartPlan(ctx, runID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// done (single pre-done task → reconcile marks done)
	doneID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "done", CreatedBy: "user:a"})
	h.drain(t)
	dt := h.seedAssignedTask(t, pid, doneID, "X", "agent:42")
	h.setTaskStatus(t, dt, pm.TaskCompleted)
	if err := h.svc.StartPlan(ctx, doneID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}

	running, err := h.plans.ListRunningPlans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := map[pm.PlanID]bool{}
	for _, p := range running {
		gotIDs[p.ID()] = true
		if p.Status() != pm.PlanRunning {
			t.Fatalf("ListRunningPlans returned a %q plan", p.Status())
		}
	}
	if !gotIDs[runID] {
		t.Fatalf("ListRunningPlans missing the running plan %s", runID)
	}
	if gotIDs[draftID] {
		t.Fatalf("ListRunningPlans included the draft plan %s", draftID)
	}
	if gotIDs[doneID] {
		t.Fatalf("ListRunningPlans included the done plan %s", doneID)
	}
}
