package projectmanager

import (
	"testing"
	"time"
)

// running returns a fresh task driven to running with the given assignee, so the
// I14 block/lease methods (which require running + an assignee) have a valid base.
func running(t *testing.T, assignee IdentityRef) *Task {
	t.Helper()
	tk := newTask(t)
	if err := tk.Assign(assignee, t0); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := tk.Start(t0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return tk
}

func lastLog(t *testing.T, tk *Task) TaskActionLog {
	t.Helper()
	logs := tk.ActionLogs()
	if len(logs) == 0 {
		t.Fatal("expected at least one action log, got none")
	}
	return logs[len(logs)-1]
}

// --- Block (I14 §2.5) ---

func TestBlock_SetsAnnotationClearsLeaseLogs(t *testing.T) {
	tk := running(t, "agent:c")
	// Give it a live lease so we can prove Block clears it.
	if err := tk.RenewLease(time.Minute, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	at := t0.Add(time.Hour)
	if err := tk.Block("need a decision", BlockReasonInputRequired, "agent:c", at); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if tk.Status() != TaskFailed {
		t.Fatalf("Block must fail the task, got %s", tk.Status())
	}
	if tk.Assignee() != "agent:c" {
		t.Fatalf("Block must keep assignee, got %q", tk.Assignee())
	}
	if tk.BlockedReason() != "" || tk.BlockedReasonType() != "" {
		t.Fatalf("block annotation wrong: %q / %q", tk.BlockedReason(), tk.BlockedReasonType())
	}
	if tk.FailedReason() != "need a decision" {
		t.Fatalf("failed_reason wrong: %q", tk.FailedReason())
	}
	if tk.BlockedComment() != "" {
		t.Fatalf("Block must reset comment, got %q", tk.BlockedComment())
	}
	if tk.ExecutionLeaseExpiresAt() != nil {
		t.Fatal("Block must clear the execution lease")
	}
	lg := lastLog(t, tk)
	if lg.Action != TaskActionFailed || lg.ActorRef != "agent:c" || lg.AgentRef != "agent:c" {
		t.Fatalf("failed log wrong: %+v", lg)
	}
	if lg.Note != "need a decision" {
		t.Fatalf("failed log note wrong: %q", lg.Note)
	}
	if !lg.OccurredAt.Equal(at) {
		t.Fatalf("failed log time wrong: %v", lg.OccurredAt)
	}
}

func TestBlock_RejectsNonRunning(t *testing.T) {
	tk := newTask(t) // open
	if err := tk.Block("x", BlockReasonObstacle, "", t0); err != ErrIllegalTransition {
		t.Fatalf("Block on open must be ErrIllegalTransition, got %v", err)
	}
}

func TestBlock_RejectsNonAssignee(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.Block("x", BlockReasonObstacle, "agent:other", t0); err != ErrNotTaskAssignee {
		t.Fatalf("Block by non-assignee must be ErrNotTaskAssignee, got %v", err)
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("rejected Block must not set a reason, got %q", tk.BlockedReason())
	}
}

func TestBlock_RejectsEmptyReason(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.Block("   ", BlockReasonObstacle, "agent:c", t0); err != ErrBlockReasonRequired {
		t.Fatalf("Block with blank reason must be ErrBlockReasonRequired, got %v", err)
	}
}

// --- Unblock legacy no-op ---

func TestUnblock_DoesNotRecoverFailed(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.Block("need a decision", BlockReasonInputRequired, "agent:c", t0); err != nil {
		t.Fatalf("Block: %v", err)
	}
	at := t0.Add(time.Hour)
	if err := tk.Unblock("go with main", "user:a", at); err != nil {
		t.Fatalf("Unblock: %v", err)
	}
	if tk.Status() != TaskFailed || tk.Assignee() != "agent:c" {
		t.Fatalf("Unblock must not recover failed tasks, got %s/%s", tk.Status(), tk.Assignee())
	}
}

func TestUnblock_NotBlockedIsNoOp(t *testing.T) {
	tk := running(t, "agent:c")
	v := tk.Version()
	if err := tk.Unblock("x", "user:a", t0); err != nil {
		t.Fatalf("Unblock no-op must be nil, got %v", err)
	}
	if tk.Version() != v {
		t.Fatalf("idempotent Unblock must not bump version: %d -> %d", v, tk.Version())
	}
	if len(tk.ActionLogs()) != 0 {
		t.Fatalf("idempotent Unblock must not log, got %d", len(tk.ActionLogs()))
	}
	if tk.BlockedComment() != "" {
		t.Fatalf("idempotent Unblock must not set a comment, got %q", tk.BlockedComment())
	}
}

// --- RenewLease (I14 §2.5) ---

func TestRenewLease_SetsDeadline(t *testing.T) {
	tk := running(t, "agent:c")
	at := t0.Add(time.Hour)
	if err := tk.RenewLease(90*time.Second, at); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	got := tk.ExecutionLeaseExpiresAt()
	if got == nil || !got.Equal(at.Add(90*time.Second)) {
		t.Fatalf("lease deadline wrong: %v", got)
	}
}

func TestRenewLease_RejectsNonRunning(t *testing.T) {
	tk := newTask(t) // open
	if err := tk.RenewLease(time.Minute, t0); err != ErrIllegalTransition {
		t.Fatalf("RenewLease on open must be ErrIllegalTransition, got %v", err)
	}
}

func TestRenewLease_RejectsFailed(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.Block("stuck", BlockReasonObstacle, "agent:c", t0); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if err := tk.RenewLease(time.Minute, t0); err != ErrIllegalTransition {
		t.Fatalf("RenewLease on failed must be ErrIllegalTransition, got %v", err)
	}
}

// --- ExpireLease (I14 §2.5) ---

func TestExpireLease_ReclaimsLapsedRunning(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.RenewLease(time.Minute, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	at := t0.Add(2 * time.Minute) // past the lease
	if err := tk.ExpireLease(at); err != nil {
		t.Fatalf("ExpireLease: %v", err)
	}
	if tk.Status() != TaskOpen {
		t.Fatalf("expired lease must return task to open, got %s", tk.Status())
	}
	if tk.Assignee() != "" {
		t.Fatalf("expired lease must clear assignee, got %q", tk.Assignee())
	}
	if tk.ExecutionLeaseExpiresAt() != nil {
		t.Fatal("expired lease must clear the deadline")
	}
	lg := lastLog(t, tk)
	if lg.Action != TaskActionLeaseExpired || lg.ActorRef != "system" || lg.AgentRef != "agent:c" {
		t.Fatalf("lease_expired log wrong: %+v", lg)
	}
}

func TestExpireLease_NoOpCases(t *testing.T) {
	// not yet lapsed
	tk := running(t, "agent:c")
	_ = tk.RenewLease(time.Hour, t0)
	v := tk.Version()
	if err := tk.ExpireLease(t0.Add(time.Minute)); err != nil {
		t.Fatalf("ExpireLease (not lapsed): %v", err)
	}
	if tk.Status() != TaskRunning || tk.Version() != v {
		t.Fatalf("ExpireLease before deadline must be a no-op, status=%s v=%d", tk.Status(), tk.Version())
	}

	// no lease set
	tk2 := running(t, "agent:c")
	v2 := tk2.Version()
	if err := tk2.ExpireLease(t0.Add(time.Hour)); err != nil || tk2.Version() != v2 {
		t.Fatalf("ExpireLease with no lease must be a no-op, err=%v v=%d", err, tk2.Version())
	}

	// failed → never reclaimed even with a lapsed lease
	tk3 := running(t, "agent:c")
	_ = tk3.RenewLease(time.Minute, t0)
	_ = tk3.Block("stuck", BlockReasonObstacle, "agent:c", t0) // clears lease, but assert block wins regardless
	if err := tk3.ExpireLease(t0.Add(time.Hour)); err != nil {
		t.Fatalf("ExpireLease on blocked: %v", err)
	}
	// The guarantee is unchanged (a legal pause is never lease-reclaimed); ADR-0054 only
	// changes what "blocked" looks like — it must stay PARKED, not be forced back to open.
	if tk3.Status() != TaskFailed {
		t.Fatalf("failed task must not be reclaimed by lease, got %s", tk3.Status())
	}
}

// --- NudgeOnLeaseExpiry (T456 / issue-21ba5b78 I30) ---

func TestNudgeOnLeaseExpiry_RenewsAndNudgesLapsedRunning(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.RenewLease(time.Minute, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	at := t0.Add(2 * time.Minute) // past the lease
	fired, err := tk.NudgeOnLeaseExpiry(time.Hour, at)
	if err != nil {
		t.Fatalf("NudgeOnLeaseExpiry: %v", err)
	}
	if !fired {
		t.Fatal("expected a nudge to fire on a lapsed lease")
	}
	// The anti-orphan invariant: NEVER reclaim — stay running, keep the assignee.
	if tk.Status() != TaskRunning {
		t.Fatalf("a lapsed lease must NOT reclaim the task, got %s", tk.Status())
	}
	if tk.Assignee() != "agent:c" {
		t.Fatalf("a lapsed lease must keep the assignee, got %q", tk.Assignee())
	}
	// The lease is RENEWED (pushed to at+ttl) so the next sweep does not re-nudge.
	exp := tk.ExecutionLeaseExpiresAt()
	if exp == nil || !exp.Equal(at.Add(time.Hour)) {
		t.Fatalf("lease must be renewed to at+ttl, got %v", exp)
	}
	lg := lastLog(t, tk)
	if lg.Action != TaskActionLeaseNudged || lg.ActorRef != "system" || lg.AgentRef != "agent:c" {
		t.Fatalf("lease_nudge log wrong: %+v", lg)
	}
}

func TestNudgeOnLeaseExpiry_NoOpCases(t *testing.T) {
	// not yet lapsed
	tk := running(t, "agent:c")
	_ = tk.RenewLease(time.Hour, t0)
	v := tk.Version()
	if fired, err := tk.NudgeOnLeaseExpiry(time.Hour, t0.Add(time.Minute)); err != nil || fired {
		t.Fatalf("not-lapsed must be (false,nil), got (%v,%v)", fired, err)
	}
	if tk.Status() != TaskRunning || tk.Version() != v {
		t.Fatalf("not-lapsed must not mutate, status=%s v=%d", tk.Status(), tk.Version())
	}

	// no lease set
	tk2 := running(t, "agent:c")
	v2 := tk2.Version()
	if fired, err := tk2.NudgeOnLeaseExpiry(time.Hour, t0.Add(time.Hour)); err != nil || fired || tk2.Version() != v2 {
		t.Fatalf("no-lease must be a no-op, fired=%v err=%v v=%d", fired, err, tk2.Version())
	}

	// failed → never nudged even with a (would-be) lapsed lease
	tk3 := running(t, "agent:c")
	_ = tk3.RenewLease(time.Minute, t0)
	_ = tk3.Block("stuck", BlockReasonObstacle, "agent:c", t0) // clears lease + marks failed
	if fired, err := tk3.NudgeOnLeaseExpiry(time.Hour, t0.Add(time.Hour)); err != nil || fired {
		t.Fatalf("failed must be a no-op, fired=%v err=%v", fired, err)
	}
	if tk3.Status() != TaskFailed {
		t.Fatalf("failed task must stay terminal and un-nudged, got %s", tk3.Status())
	}
}

// --- RecordReassignment (I14 §2.5) ---

func TestRecordReassignment_RetargetsAndClears(t *testing.T) {
	tk := running(t, "agent:c")
	_ = tk.RenewLease(time.Minute, t0)
	at := t0.Add(time.Hour)
	if err := tk.RecordReassignment("agent:d", "user:pm", at); err != nil {
		t.Fatalf("RecordReassignment: %v", err)
	}
	if tk.Assignee() != "agent:d" {
		t.Fatalf("reassign must retarget assignee, got %q", tk.Assignee())
	}
	if tk.BlockedReason() != "" || tk.BlockedReasonType() != "" || tk.ExecutionLeaseExpiresAt() != nil {
		t.Fatal("reassign must clear block + lease for a clean start")
	}
	lg := lastLog(t, tk)
	if lg.Action != TaskActionReassigned || lg.ActorRef != "user:pm" || lg.AgentRef != "agent:d" {
		t.Fatalf("reassigned log wrong: %+v", lg)
	}
}

func TestRecordReassignment_RejectsInvalidAndTerminal(t *testing.T) {
	tk := running(t, "agent:c")
	if err := tk.RecordReassignment("", "user:pm", t0); err == nil {
		t.Fatal("reassign to an invalid assignee must error")
	}
	done := running(t, "agent:c")
	if err := done.Complete("user:a", t0); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := done.RecordReassignment("agent:d", "user:pm", t0); err != ErrIllegalTransition {
		t.Fatalf("reassign of a terminal task must be ErrIllegalTransition, got %v", err)
	}
}

// --- accessors return copies (no aliasing of aggregate internals) ---

func TestActionLogsAndLeaseAreCopies(t *testing.T) {
	tk := running(t, "agent:c")
	_ = tk.RenewLease(time.Minute, t0)
	_ = tk.Block("stuck", BlockReasonObstacle, "agent:c", t0)

	logs := tk.ActionLogs()
	logs[0].Note = "mutated"
	if tk.ActionLogs()[0].Note == "mutated" {
		t.Fatal("ActionLogs must return a copy; caller mutation leaked into the aggregate")
	}

	// Lease copy: use a fresh running (non-blocked) task — RenewLease rejects a
	// blocked one, and Block above cleared tk's lease.
	leased := running(t, "agent:c")
	if err := leased.RenewLease(time.Minute, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	lease := leased.ExecutionLeaseExpiresAt()
	*lease = lease.Add(99 * time.Hour)
	if leased.ExecutionLeaseExpiresAt().Equal(*lease) {
		t.Fatal("ExecutionLeaseExpiresAt must return a copy; caller mutation leaked")
	}
}

// --- rehydrate round-trips the new fields (F2 repo contract) ---

func TestRehydrateTask_RoundTripsI14Fields(t *testing.T) {
	exp := t0.Add(time.Minute)
	in := RehydrateTaskInput{
		ID: "T1", ProjectID: "P1", Title: "do", Status: TaskRunning, Assignee: "agent:c",
		CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 4,
		FailedReason: "need a decision", ExecutionLeaseExpiresAt: &exp,
		ActionLogs: []TaskActionLog{{ID: "L1", OccurredAt: t0, Action: TaskActionBlocked, ActorRef: "agent:c", AgentRef: "agent:c", Note: "[input_required] need a decision"}},
	}
	tk, err := RehydrateTask(in)
	if err != nil {
		t.Fatalf("RehydrateTask: %v", err)
	}
	if tk.FailedReason() != "need a decision" {
		t.Fatalf("rehydrate lost failed reason: %q", tk.FailedReason())
	}
	if got := tk.ExecutionLeaseExpiresAt(); got == nil || !got.Equal(exp) {
		t.Fatalf("rehydrate lost lease: %v", got)
	}
	if logs := tk.ActionLogs(); len(logs) != 1 || logs[0].ID != "L1" || logs[0].Action != TaskActionBlocked {
		t.Fatalf("rehydrate lost action logs: %+v", logs)
	}
}
