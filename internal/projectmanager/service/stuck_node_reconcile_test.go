package service

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// ============================================================================
// issue-6ff12523 — the THIRD auto trigger for reopenStuckPlanNode: the periodic
// lease sweep auto-reopens a structured plan node wedged Running while its executor
// is confirmed dead, WITHOUT reversing the T456 / issue-21ba5b78 nudge-not-reclaim
// anti-orphan invariant (a slow-but-alive owner is never reopened).
// ============================================================================

// stuckNodeHarness builds a single-node graphed plan and drives its node to NodeRunning
// with a live execution lease under `assignee`. Returns the harness, orch service, ids.
type stuckNodeFixture struct {
	h        *planAdvanceHarness
	orch     *orch.Service
	pid      pm.ProjectID
	planID   pm.PlanID
	tid      pm.TaskID
	graphID  string
	assignee pm.IdentityRef
}

func setupStuckNode(t *testing.T, assignee string) stuckNodeFixture {
	t.Helper()
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stuck", CreatedBy: "user:a"})
	h.drain(t)
	tid := h.seedAssignedTask(t, pid, planID, "work", assignee)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	graphID := p.GraphID()

	// Dispatch the root node, then start the task (grants the lease) and sync the node
	// to NodeRunning.
	if d, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil || len(d) != 1 || d[0] != tid {
		t.Fatalf("dispatch = %v, err=%v; want [%s]", d, err, tid)
	}
	if err := h.svc.StartTask(ctx, tid, "user:a"); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan (sync to running): %v", err)
	}
	h.drain(t)
	if got := nodeStatusForTask(t, h, orchSvc, tid); got != orch.NodeRunning {
		t.Fatalf("node = %q, want running after setup", got)
	}
	return stuckNodeFixture{h: h, orch: orchSvc, pid: pid, planID: planID, tid: tid, graphID: graphID, assignee: pm.IdentityRef(assignee)}
}

// sweep runs one lease-checker sweep (the confirmed-dead accounting + T456 nudge).
func (f stuckNodeFixture) sweep(t *testing.T) {
	t.Helper()
	if _, err := f.h.svc.NudgeExpiredLeases(f.h.ctx); err != nil {
		t.Fatalf("NudgeExpiredLeases: %v", err)
	}
	f.h.drain(t)
}

func (f stuckNodeFixture) nodeStatus(t *testing.T) orch.NodeStatus {
	t.Helper()
	return nodeStatusForTask(t, f.h, f.orch, f.tid)
}

func (f stuckNodeFixture) task(t *testing.T) *pm.Task {
	t.Helper()
	tk, err := f.h.tasks.FindByID(f.h.ctx, f.tid)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	return tk
}

// TestStuckNode_TrueDead_AutoReopened — the CONFIRMED-DEAD slow path: a node whose lease
// lapsed and then stayed FROZEN (no heartbeat / worker renewal) across N sweeps and
// ≥ T_dead is auto-reopened (Running→NodeReopen + dispatch record cleared) and re-enters
// the engine ready-set, with NO human reset/unblock.
func TestStuckNode_TrueDead_AutoReopened(t *testing.T) {
	f := setupStuckNode(t, "agent:dead")
	clk := f.h.clk

	// Lapse the lease (executor died — no more renewals).
	clk.Advance(DefaultExecutionLeaseTTL + time.Minute)

	// Sweep #1: first lapse observed → tracker bootstraps, T456 nudge renews the lease.
	f.sweep(t)
	if got := f.nodeStatus(t); got != orch.NodeRunning {
		t.Fatalf("node = %q after first sweep, want still running (only bootstrap, no verdict)", got)
	}

	// Subsequent sweeps at 4-min steps: the (nudge-renewed) lease is now frozen — no
	// executor to advance it — so each sweep is a strike. Reopen fires once BOTH N=3
	// strikes AND ≥ T_dead(10m) since the last activity are met.
	reopened := false
	for i := 0; i < 4; i++ {
		clk.Advance(4 * time.Minute)
		f.sweep(t)
		if f.nodeStatus(t) != orch.NodeRunning {
			reopened = true
			break
		}
	}
	if !reopened {
		t.Fatal("node still NodeRunning — confirmed-dead auto-reopen never fired (bug)")
	}
	if got := f.nodeStatus(t); got != orch.NodeReopen {
		t.Fatalf("node = %q after auto-reconcile, want reopen", got)
	}
	// The task itself never left running (reopen is a graph-node action, not a reclaim).
	if tk := f.task(t); tk.Status() != pm.TaskRunning {
		t.Fatalf("task = %q, want still running (no orphaning — T456 invariant)", tk.Status())
	}
	// The reopened node re-enters the engine ready-set (re-dispatchable).
	if ready := readyNodeTaskIDs(t, f.h, f.orch, f.graphID); !ready[f.tid] {
		t.Fatalf("reopened node not in ready-set: %v", ready)
	}
}

// TestStuckNode_SlowButAlive_NotReopened — the MOST IMPORTANT guard (T456 /
// issue-21ba5b78): a node whose lease lapsed but whose worker keeps auto-renewing (a
// live-but-heads-down agent) is NEVER auto-reopened — every observed renewal clears the
// strike count.
func TestStuckNode_SlowButAlive_NotReopened(t *testing.T) {
	f := setupStuckNode(t, "agent:slow")
	clk := f.h.clk

	// Lapse the lease once (a brief stall), then keep the worker renewing every 4 min —
	// exactly what a live runtime's process-alive auto-renew (60s cadence) does — for
	// well past N sweeps and T_dead. The node must never be reopened.
	clk.Advance(DefaultExecutionLeaseTTL + time.Minute)
	f.sweep(t) // first lapse → bootstrap + nudge

	for i := 0; i < 8; i++ {
		clk.Advance(4 * time.Minute)
		// Worker attests the process is alive BEFORE the sweep → lease advances → activity.
		if err := f.h.svc.WorkerRenewLease(f.h.ctx, f.tid, f.assignee); err != nil {
			t.Fatalf("WorkerRenewLease: %v", err)
		}
		f.sweep(t)
		if got := f.nodeStatus(t); got != orch.NodeRunning {
			t.Fatalf("slow-but-alive node reopened to %q at step %d — T456 invariant violated", got, i)
		}
	}
	if tk := f.task(t); tk.Status() != pm.TaskRunning || tk.BlockedReason() != "" {
		t.Fatalf("slow-but-alive task disturbed: status=%q blocked=%q", tk.Status(), tk.BlockedReason())
	}
}

// redispatchToRunning re-drives an auto-reopened node back to NodeRunning (mirrors
// production: reopen → next AdvancePlan re-dispatches → the re-forked executor's
// StartTask... here the task is already running, so two AdvancePlan passes flip the node
// Reopen→(dispatched)→Running via syncGraphToTasks).
func (f stuckNodeFixture) redispatchToRunning(t *testing.T) {
	t.Helper()
	for i := 0; i < 2; i++ {
		if _, err := f.h.svc.AdvancePlan(f.h.ctx, f.planID, "user:a"); err != nil {
			t.Fatalf("re-dispatch AdvancePlan: %v", err)
		}
		f.h.drain(t)
	}
	if got := f.nodeStatus(t); got != orch.NodeRunning {
		t.Fatalf("node = %q after re-dispatch, want running", got)
	}
}

// driveToReopen advances the clock in 4-min sweep steps (no renewals) until the node is
// auto-reopened, or fails after a bound.
func (f stuckNodeFixture) driveToReopen(t *testing.T) {
	t.Helper()
	for i := 0; i < 6; i++ {
		f.h.clk.Advance(4 * time.Minute)
		f.sweep(t)
		if f.nodeStatus(t) != orch.NodeRunning {
			return
		}
	}
	t.Fatal("node not reopened within bound")
}

// TestStuckNode_CircuitBreaker_BlocksAfterRmax — under a re-dying node the reconcile
// auto-reopens up to R_max, then STOPS and blocks the task for human triage (reusing the
// T862 reset-exhaustion mechanism: obstacle block + reset_exhausted log), instead of
// reopening forever.
func TestStuckNode_CircuitBreaker_BlocksAfterRmax(t *testing.T) {
	f := setupStuckNode(t, "agent:flap")
	clk := f.h.clk

	// Round 1: lapse + confirmed-dead → auto-reopen #1.
	clk.Advance(DefaultExecutionLeaseTTL + time.Minute)
	f.sweep(t) // bootstrap
	f.driveToReopen(t)
	if got := f.nodeStatus(t); got != orch.NodeReopen {
		t.Fatalf("round1 node = %q, want reopen", got)
	}

	// Rounds 2..R_max: re-dispatch back to running, let it die again (respecting the
	// cooldown, which driveToReopen's clock advance covers), reopen again.
	for round := 2; round <= StuckNodeMaxAutoReopens; round++ {
		f.redispatchToRunning(t)
		f.driveToReopen(t)
		if got := f.nodeStatus(t); got != orch.NodeReopen {
			t.Fatalf("round%d node = %q, want reopen", round, got)
		}
	}

	// One more dead cycle: now past R_max → the breaker trips → BLOCK for triage, no reopen.
	f.redispatchToRunning(t)
	for i := 0; i < 6; i++ {
		clk.Advance(4 * time.Minute)
		f.sweep(t)
		if f.task(t).BlockedReason() != "" {
			break
		}
	}
	tk := f.task(t)
	if tk.BlockedReason() == "" {
		t.Fatalf("circuit breaker did not block after R_max=%d reopens", StuckNodeMaxAutoReopens)
	}
	if tk.BlockedReasonType() != pm.BlockReasonObstacle {
		t.Fatalf("block reasonType = %q, want obstacle (triage)", tk.BlockedReasonType())
	}
	if tk.Status() != pm.TaskRunning {
		t.Fatalf("blocked task status = %q, want running (block is an annotation)", tk.Status())
	}
	// The reset-exhaustion circuit-breaker log is the reused mechanism's fingerprint.
	logs, err := f.h.actionLogs.ListByTask(f.h.ctx, f.tid)
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	sawExhausted := false
	for _, l := range logs {
		if l.Action == pm.TaskActionResetExhausted {
			sawExhausted = true
		}
	}
	if !sawExhausted {
		t.Fatal("expected a reset_exhausted action log (reused T862 breaker)")
	}
}
