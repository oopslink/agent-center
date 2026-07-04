package projectmanager

import (
	"testing"
	"time"
)

// --- ResetToOpen (T862 tier-3 recovery) ---

// A running task with a LAPSED lease resets to open: assignee/block/lease cleared,
// recovery_reset_count incremented, a reset log appended.
func TestResetToOpen_ClearsAndReturnsToPool(t *testing.T) {
	tk := running(t, "agent:dead")
	// A lease that has already lapsed by reset time (the confirmed-dead precondition).
	if err := tk.RenewLease(time.Minute, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	at := t0.Add(time.Hour) // well past the 1-minute lease
	if err := tk.ResetToOpen(at); err != nil {
		t.Fatalf("ResetToOpen: %v", err)
	}
	if tk.Status() != TaskOpen {
		t.Fatalf("reset must move running→open, got %s", tk.Status())
	}
	if tk.Assignee() != "" {
		t.Fatalf("reset must clear assignee, got %q", tk.Assignee())
	}
	if tk.ExecutionLeaseExpiresAt() != nil {
		t.Fatal("reset must clear the execution lease")
	}
	if tk.BlockedReason() != "" || tk.BlockedReasonType() != "" {
		t.Fatalf("reset must clear block annotation, got %q/%q", tk.BlockedReason(), tk.BlockedReasonType())
	}
	if tk.RecoveryResetCount() != 1 {
		t.Fatalf("reset must increment recovery_reset_count to 1, got %d", tk.RecoveryResetCount())
	}
	if !tk.StatusChangedAt().Equal(at.UTC()) {
		t.Fatalf("reset must move statusChangedAt to %v, got %v", at, tk.StatusChangedAt())
	}
	lg := lastLog(t, tk)
	if lg.Action != TaskActionReset || lg.ActorRef != "system" || lg.AgentRef != "agent:dead" {
		t.Fatalf("reset log wrong: %+v", lg)
	}
}

// A nil lease (no live run) also passes the guard — reset succeeds.
func TestResetToOpen_NilLeasePasses(t *testing.T) {
	tk := running(t, "agent:dead") // no RenewLease → nil lease
	if err := tk.ResetToOpen(t0.Add(time.Minute)); err != nil {
		t.Fatalf("ResetToOpen with nil lease must succeed, got %v", err)
	}
	if tk.Status() != TaskOpen || tk.RecoveryResetCount() != 1 {
		t.Fatalf("reset state wrong: status=%s count=%d", tk.Status(), tk.RecoveryResetCount())
	}
}

// GUARD (a) / non-interference with T456 lease-nudge: a still-LIVE lease is rejected
// with ErrLeaseStillLive — a live lease belongs to the nudge path, never reset.
func TestResetToOpen_LiveLeaseRejected(t *testing.T) {
	tk := running(t, "agent:maybe-alive")
	if err := tk.RenewLease(time.Hour, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	// Reset "now" is BEFORE the lease deadline → still live.
	if err := tk.ResetToOpen(t0.Add(time.Minute)); err != ErrLeaseStillLive {
		t.Fatalf("live-lease reset must be ErrLeaseStillLive, got %v", err)
	}
	if tk.Status() != TaskRunning || tk.Assignee() != "agent:maybe-alive" {
		t.Fatalf("rejected reset must not mutate the task: status=%s assignee=%q", tk.Status(), tk.Assignee())
	}
	if tk.RecoveryResetCount() != 0 {
		t.Fatalf("rejected reset must not increment the count, got %d", tk.RecoveryResetCount())
	}
}

func TestResetToOpen_RejectsNonRunning(t *testing.T) {
	tk := newTask(t) // open
	if err := tk.ResetToOpen(t0); err != ErrIllegalTransition {
		t.Fatalf("reset on open must be ErrIllegalTransition, got %v", err)
	}
}

func TestResetToOpen_RejectsBlocked(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.Block("stuck", BlockReasonObstacle, "agent:c", t0); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if err := tk.ResetToOpen(t0.Add(time.Hour)); err != ErrTaskBlocked {
		t.Fatalf("reset on a blocked (paused) task must be ErrTaskBlocked, got %v", err)
	}
}

// Complete zeroes the recovery tally (consecutive-since-last-success semantics).
func TestComplete_ZeroesRecoveryResetCount(t *testing.T) {
	tk := running(t, "agent:dead")
	if err := tk.ResetToOpen(t0.Add(time.Minute)); err != nil {
		t.Fatalf("ResetToOpen: %v", err)
	}
	if tk.RecoveryResetCount() != 1 {
		t.Fatalf("precondition: count should be 1, got %d", tk.RecoveryResetCount())
	}
	// Drive it back to running then complete.
	if err := tk.Assign("agent:new", t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := tk.Start(t0.Add(3 * time.Minute)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tk.Complete("user:pm", t0.Add(4*time.Minute)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if tk.RecoveryResetCount() != 0 {
		t.Fatalf("Complete must zero recovery_reset_count, got %d", tk.RecoveryResetCount())
	}
}

// --- BlockForResetExhaustion (T862 §2B circuit breaker) ---

func TestBlockForResetExhaustion_BlocksWithDistinctLog(t *testing.T) {
	tk := running(t, "agent:dead")
	if err := tk.RenewLease(time.Minute, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	at := t0.Add(time.Hour)
	if err := tk.BlockForResetExhaustion("reset×3 needs triage", at); err != nil {
		t.Fatalf("BlockForResetExhaustion: %v", err)
	}
	if tk.Status() != TaskRunning {
		t.Fatalf("exhaustion block keeps status running (annotation), got %s", tk.Status())
	}
	if tk.BlockedReason() != "reset×3 needs triage" || tk.BlockedReasonType() != BlockReasonObstacle {
		t.Fatalf("block annotation wrong: %q/%q", tk.BlockedReason(), tk.BlockedReasonType())
	}
	if tk.ExecutionLeaseExpiresAt() != nil {
		t.Fatal("exhaustion block must clear the lease")
	}
	lg := lastLog(t, tk)
	if lg.Action != TaskActionResetExhausted || lg.ActorRef != "system" {
		t.Fatalf("exhaustion log wrong: %+v", lg)
	}
}

func TestBlockForResetExhaustion_RejectsNonRunning(t *testing.T) {
	tk := newTask(t) // open
	if err := tk.BlockForResetExhaustion("x", t0); err != ErrIllegalTransition {
		t.Fatalf("exhaustion block on open must be ErrIllegalTransition, got %v", err)
	}
}
