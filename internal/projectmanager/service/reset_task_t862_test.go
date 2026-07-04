package service

import (
	"sync"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// runningLeasedPoolTask drives a dispatched builtin-pool task to running under `owner`
// with a live execution lease (StartTask grants DefaultExecutionLeaseTTL). Returns the
// project + task ids. Caller advances the clock past the lease to simulate a dead
// executor whose lease has lapsed.
func runningLeasedPoolTask(t *testing.T, h *planAdvanceHarness, org, proj, owner string) (pm.ProjectID, pm.TaskID) {
	t.Helper()
	pid, _, tid := poolTaskCaps(t, h, org, proj, nil) // no required caps → anyone eligible
	addMember(t, h, pid, pm.IdentityRef(owner))
	a := owner
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatalf("assign to %s: %v", owner, err)
	}
	if err := h.svc.StartTask(h.ctx, tid, pm.IdentityRef(owner)); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	return pid, tid
}

// (2) ResetTask on a confirmed-dead running task: running→open, assignee cleared,
// recovery_reset_count incremented by 1.
func TestResetTask_ReturnsToPoolAndIncrementsCount(t *testing.T) {
	h, _, _ := autoAssignInject(t)
	pid, tid := runningLeasedPoolTask(t, h, "org-1", "P", "agent:dead")
	addMember(t, h, pid, "agent:pd")
	h.clk.Advance(DefaultExecutionLeaseTTL + time.Hour) // lease lapses → confirmed dead

	if err := h.svc.ResetTask(h.ctx, tid, "agent:pd", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Status() != pm.TaskOpen {
		t.Fatalf("status=%s, want open", got.Status())
	}
	// With no online candidates wired, it stays ownerless in the pool.
	if got.Assignee() != "" {
		t.Fatalf("assignee=%q, want cleared", got.Assignee())
	}
	if got.RecoveryResetCount() != 1 {
		t.Fatalf("recovery_reset_count=%d, want 1", got.RecoveryResetCount())
	}
}

// (3) Mis-fire guard / lease-nudge non-interference: ResetTask on a task whose lease is
// STILL LIVE is rejected with ErrLeaseStillLive and does NOT mutate the task.
func TestResetTask_LiveLeaseRejected(t *testing.T) {
	h, _, _ := autoAssignInject(t)
	pid, tid := runningLeasedPoolTask(t, h, "org-1", "P", "agent:owner")
	addMember(t, h, pid, "agent:pd")
	// Do NOT advance the clock — the lease granted by StartTask is still live.

	if err := h.svc.ResetTask(h.ctx, tid, "agent:pd", false); err != pm.ErrLeaseStillLive {
		t.Fatalf("ResetTask with live lease = %v, want ErrLeaseStillLive", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Status() != pm.TaskRunning || got.Assignee() != "agent:owner" {
		t.Fatalf("rejected reset mutated task: status=%s assignee=%q", got.Status(), got.Assignee())
	}
	if got.RecoveryResetCount() != 0 {
		t.Fatalf("rejected reset incremented count to %d", got.RecoveryResetCount())
	}
}

// (THE-gate) OWNER + confirmedDead resets a task whose lease is STILL LIVE: the owner
// runtime tier-3-confirmed its own executor dead, and it is the one renewing the lease
// (which would therefore never lapse). actor == assignee → bypassLease → the reset
// succeeds on the first call. This is the case the whole fix exists for.
func TestResetTask_OwnerConfirmedDeadBypassesLiveLease(t *testing.T) {
	h, _, _ := autoAssignInject(t)
	pid, tid := runningLeasedPoolTask(t, h, "org-1", "P", "agent:owner")
	addMember(t, h, pid, "agent:pd")
	// Do NOT advance the clock — the lease is still live. The OWNER itself resets.
	if err := h.svc.ResetTask(h.ctx, tid, "agent:owner", true); err != nil {
		t.Fatalf("owner confirmed-dead reset of a live lease must succeed, got %v", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Status() != pm.TaskOpen || got.Assignee() != "" {
		t.Fatalf("owner reset must return to pool: status=%s assignee=%q", got.Status(), got.Assignee())
	}
	if got.RecoveryResetCount() != 1 {
		t.Fatalf("owner reset must increment count, got %d", got.RecoveryResetCount())
	}
}

// (THE-gate guard) A STRANGER asserting confirmedDead on a live-leased task is STILL
// rejected — bypassLease requires actor == assignee, so only the owner can force-reset
// its own confirmed-dead task; a different agent must wait for a genuine lapse. Protects
// a slow-but-alive owner from a stranger's mis-fire.
func TestResetTask_StrangerConfirmedDeadStillRejectedOnLiveLease(t *testing.T) {
	h, _, _ := autoAssignInject(t)
	pid, tid := runningLeasedPoolTask(t, h, "org-1", "P", "agent:owner")
	addMember(t, h, pid, "agent:pd")
	// Live lease; a NON-owner claims confirmed-dead → must not bypass.
	if err := h.svc.ResetTask(h.ctx, tid, "agent:pd", true); err != pm.ErrLeaseStillLive {
		t.Fatalf("stranger confirmed-dead on live lease = %v, want ErrLeaseStillLive", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Status() != pm.TaskRunning || got.Assignee() != "agent:owner" {
		t.Fatalf("rejected stranger reset mutated task: status=%s assignee=%q", got.Status(), got.Assignee())
	}
}

// (2 cap) After MaxRecoveryResets consecutive resets, ResetTask BLOCKS the task for
// triage instead of resetting again — the durable circuit breaker.
func TestResetTask_CapTripsBlock(t *testing.T) {
	h, _, _ := autoAssignInject(t)
	pid, tid := runningLeasedPoolTask(t, h, "org-1", "P", "agent:dead")
	addMember(t, h, pid, "agent:pd")

	// Reset MaxRecoveryResets times, re-driving to running+lapsed each round.
	for i := 0; i < pm.MaxRecoveryResets; i++ {
		h.clk.Advance(DefaultExecutionLeaseTTL + time.Hour)
		if err := h.svc.ResetTask(h.ctx, tid, "agent:pd", false); err != nil {
			t.Fatalf("reset #%d: %v", i+1, err)
		}
		// Re-arm: reassign + start so the next round has a running+leased task.
		a := "agent:dead"
		if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
			t.Fatalf("re-assign #%d: %v", i+1, err)
		}
		if err := h.svc.StartTask(h.ctx, tid, "agent:dead"); err != nil {
			t.Fatalf("re-start #%d: %v", i+1, err)
		}
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.RecoveryResetCount() != pm.MaxRecoveryResets {
		t.Fatalf("precondition: count=%d, want %d", got.RecoveryResetCount(), pm.MaxRecoveryResets)
	}
	// The (cap+1)th reset trips the breaker: BLOCK, not reset.
	h.clk.Advance(DefaultExecutionLeaseTTL + time.Hour)
	if err := h.svc.ResetTask(h.ctx, tid, "agent:pd", false); err != nil {
		t.Fatalf("cap-trip ResetTask: %v", err)
	}
	got, _ = h.svc.GetTask(h.ctx, tid)
	if got.Status() != pm.TaskRunning {
		t.Fatalf("cap trip must keep running (blocked annotation), got %s", got.Status())
	}
	if got.BlockedReason() == "" || got.BlockedReasonType() != pm.BlockReasonObstacle {
		t.Fatalf("cap trip must block-for-triage, got reason=%q type=%q", got.BlockedReason(), got.BlockedReasonType())
	}
	if got.RecoveryResetCount() != pm.MaxRecoveryResets {
		t.Fatalf("cap trip must NOT increment past cap, got %d", got.RecoveryResetCount())
	}
}

// (4) Re-dispatch REALLY happens: an online, capable, project-member agent exists, so
// the synchronous TriggerAutoAssignForProject inside ResetTask claims the reset task
// (ClaimIfUnassigned CAS) and assigns it to that FRESH agent — proven by the new
// assignee, not by a status field.
func TestResetTask_RedispatchesToFreshAgent(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, tid := runningLeasedPoolTask(t, h, "org-1", "P", "agent:dead")
	addMember(t, h, pid, "agent:pd")
	addMember(t, h, pid, "agent:fresh")
	// agent:dead is OFFLINE (gone); agent:fresh is online + auto-assignable + a member.
	dir.byOrg["org-1"] = []AutoAssignCandidate{
		cand("agent:dead", false, true, 1),
		cand("agent:fresh", true, true, 1),
	}
	h.clk.Advance(DefaultExecutionLeaseTTL + time.Hour)

	if err := h.svc.ResetTask(h.ctx, tid, "agent:pd", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Assignee() != "agent:fresh" {
		t.Fatalf("reset task not re-dispatched: assignee=%q, want agent:fresh", got.Assignee())
	}
	// claim→open: the fresh agent start_tasks it when woken (real auto-assign semantics).
	if got.Status() != pm.TaskOpen {
		t.Fatalf("re-dispatched task status=%s, want open (claim→open)", got.Status())
	}
	if got.RecoveryResetCount() != 1 {
		t.Fatalf("recovery_reset_count=%d, want 1", got.RecoveryResetCount())
	}
}

// (2 concurrent) Two concurrent ResetTask calls on the same running task: exactly ONE
// wins (+1), the loser re-reads a now-open task and fails ErrIllegalTransition — the
// count lands at exactly 1 (no double increment).
func TestResetTask_ConcurrentSingleIncrement(t *testing.T) {
	h, _, _ := autoAssignInject(t)
	pid, tid := runningLeasedPoolTask(t, h, "org-1", "P", "agent:dead")
	addMember(t, h, pid, "agent:pd")
	h.clk.Advance(DefaultExecutionLeaseTTL + time.Hour)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = h.svc.ResetTask(h.ctx, tid, "agent:pd", false)
		}(i)
	}
	close(start)
	wg.Wait()

	nilCount, illegalCount := 0, 0
	for _, e := range errs {
		switch e {
		case nil:
			nilCount++
		case pm.ErrIllegalTransition:
			illegalCount++
		default:
			t.Fatalf("unexpected concurrent reset error: %v", e)
		}
	}
	if nilCount != 1 || illegalCount != 1 {
		t.Fatalf("concurrent resets: nil=%d illegal=%d, want 1/1", nilCount, illegalCount)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.RecoveryResetCount() != 1 {
		t.Fatalf("concurrent reset must +1 exactly once, count=%d", got.RecoveryResetCount())
	}
}
