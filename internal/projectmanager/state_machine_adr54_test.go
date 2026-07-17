package projectmanager

import (
	"testing"
	"time"
)

// ADR-0054 (amends ADR-0046) Task state-machine class-guards. The state set is exactly
// {open, running, delivered, blocked, completed, discarded, reopened}.
//
// ADR-0046 had cut `blocked` to a mere annotation on a `running` task to kill the T16
// deadlock class. That killed the deadlock but also killed the circuit-breaker: a park
// that does not leave `running` cannot stop dispatch. ADR-0054 restores `blocked` as a
// real state and adds `delivered`, while KEEPING the no-deadlock guarantee — which is why
// TestTaskStates_NoDeadlockableState below is the load-bearing guard, generalized to run
// over EVERY non-terminal state rather than a hand-listed few.
//
// The ADR-0046 guarantees that still hold are re-pinned here, not dropped:
//   - `verified` stays deleted (TestTaskStates_VerifiedStillInvalid);
//   - no non-terminal state is a dead end (TestTaskStates_NoDeadlockableState);
//   - the terminal set is still exactly {completed, discarded} — the 命门 that a parked
//     task must NOT be reaped as concluded work (TestTaskStates_ParkedIsNotTerminal).

// TestTaskStates_NoDeadlockableState is the "no reachable deadlock" class-guard inherited
// from ADR-0046, now derived from the enum instead of a hardcoded list: from EVERY
// non-terminal status there must exist at least one legal forward transition. A
// non-terminal status with no outgoing edge is an unrecoverable deadlock — the exact class
// ADR-0046 existed to remove and that ADR-0054 must not re-introduce while adding states.
func TestTaskStates_NoDeadlockableState(t *testing.T) {
	all := []TaskStatus{TaskOpen, TaskRunning, TaskDelivered, TaskBlocked, TaskCompleted, TaskDiscarded, TaskReopened}
	for _, s := range all {
		if !s.IsValid() {
			t.Fatalf("%s must be a valid status", s)
		}
		if s.IsTerminal() {
			continue // terminal states are intentionally sinks
		}
		if len(taskTransitions[s]) == 0 {
			t.Fatalf("non-terminal status %q has no legal forward transition — deadlockable state", s)
		}
	}
}

// TestTaskStates_ParkedIsNotTerminal is the ADR-0054 命门 guard: `delivered` and `blocked`
// are PARKED but NOT terminal. Making either terminal is the precise failure this ADR
// exists to prevent — a delivered task would become a false green (nothing accepted it),
// and a parked task would be reaped as concluded work, dropping out of every active view
// and recovery sweep that keys on the non-terminal set. It also pins the complement: the
// terminal set stays exactly {completed, discarded}.
func TestTaskStates_ParkedIsNotTerminal(t *testing.T) {
	for _, s := range []TaskStatus{TaskDelivered, TaskBlocked} {
		if !s.IsParked() {
			t.Fatalf("%s must be parked", s)
		}
		if s.IsTerminal() {
			t.Fatalf("%s must NOT be terminal — a parked task is active work, not concluded", s)
		}
		if s.IsDispatchable() {
			t.Fatalf("%s must NOT be dispatchable — park must really stop dispatch", s)
		}
	}
	for _, s := range []TaskStatus{TaskCompleted, TaskDiscarded} {
		if !s.IsTerminal() {
			t.Fatalf("%s must be terminal", s)
		}
	}
	// The live-work states are dispatchable and not parked.
	for _, s := range []TaskStatus{TaskOpen, TaskRunning, TaskReopened} {
		if s.IsParked() {
			t.Fatalf("%s must NOT be parked", s)
		}
		if !s.IsDispatchable() {
			t.Fatalf("%s must be dispatchable", s)
		}
	}
}

// TestTaskStates_VerifiedStillInvalid pins that ADR-0046's OTHER deletion stays deleted —
// re-adding `blocked` must not smuggle `verified` back with it.
func TestTaskStates_VerifiedStillInvalid(t *testing.T) {
	if TaskStatus("verified").IsValid() {
		t.Fatal(`TaskStatus("verified") must NOT be valid (ADR-0046 deleted it; ADR-0054 keeps it deleted)`)
	}
	if TaskStatus("nonsense").IsValid() {
		t.Fatal(`an unknown status must not validate`)
	}
}

// TestADR54_BlockIsAStateNotAnAnnotation is the I107 ② core: Block must really move the
// status to `blocked` (that is what stops dispatch), while STILL writing the
// blocked_reason annotation every reason-keyed consumer reads. Under ADR-0046 the status
// stayed `running`, so a block marked rather than interrupted — and a re-drive forked a
// fresh empty-context executor onto the task.
func TestADR54_BlockIsAStateNotAnAnnotation(t *testing.T) {
	now := t0

	tk := newTask(t)
	_ = tk.Assign("agent:c", now)
	if err := tk.Start(now); err != nil {
		t.Fatal(err)
	}

	if err := tk.Block("stuck", BlockReasonObstacle, "agent:c", now); err != nil {
		t.Fatalf("Block on running task: %v", err)
	}
	if tk.Status() != TaskBlocked {
		t.Fatalf("Block must PARK the task (status=blocked), got %s — a block that stays running cannot stop dispatch", tk.Status())
	}
	if tk.Status().IsDispatchable() {
		t.Fatal("a blocked task must not be dispatchable")
	}
	// The annotation is still written: every reason-keyed consumer (overdue reminder,
	// taskCancelEvidence, the UI) depends on it.
	if tk.BlockedReason() != "stuck" {
		t.Fatalf("Block must still set the reason annotation, got %q", tk.BlockedReason())
	}
	if tk.BlockedReasonType() != BlockReasonObstacle {
		t.Fatalf("Block must set the reason type, got %q", tk.BlockedReasonType())
	}
	// The assignee is KEPT — a block pauses the task, it does not hand it to anyone else.
	if tk.Assignee() != "agent:c" {
		t.Fatalf("Block must keep the assignee, got %q", tk.Assignee())
	}

	// Unblock un-parks it back to running and clears the reason.
	if err := tk.Unblock("resolved", "agent:c", now); err != nil {
		t.Fatalf("Unblock: %v", err)
	}
	if tk.Status() != TaskRunning {
		t.Fatalf("Unblock must un-park to running, got %s", tk.Status())
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("Unblock must clear the reason, got %q", tk.BlockedReason())
	}
	if tk.BlockedComment() != "resolved" {
		t.Fatalf("Unblock must keep the comment for the agent to read on resume, got %q", tk.BlockedComment())
	}

	// Re-block then Complete: an owner may conclude a parked task directly (the
	// pre-ADR-0054 "complete a blocked task" path stays open — blocked→completed is legal).
	if err := tk.Block("stuck again", BlockReasonObstacle, "agent:c", now); err != nil {
		t.Fatal(err)
	}
	if err := tk.Complete("agent:c", now); err != nil {
		t.Fatalf("Complete on a blocked task: %v", err)
	}
	if tk.Status() != TaskCompleted {
		t.Fatalf("Complete must move to completed, got %s", tk.Status())
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("Complete must clear the reason annotation, got %q", tk.BlockedReason())
	}
}

// TestADR54_LegacyRunningPlusReasonStillUnblocks pins BACKWARD COMPATIBILITY with rows
// parked the ADR-0046 way (status=running + a blocked_reason) that predate this change and
// are NOT migrated. Unblock keys its not-blocked test on the REASON, not the status, so it
// still recovers such a row: it clears the annotation and leaves the already-running status
// alone. Without this, every task parked before the upgrade would be unrecoverable.
func TestADR54_LegacyRunningPlusReasonStillUnblocks(t *testing.T) {
	now := t0
	tk := newTask(t)
	_ = tk.Assign("agent:c", now)
	if err := tk.Start(now); err != nil {
		t.Fatal(err)
	}
	// Forge the legacy shape: running + a reason, WITHOUT the ADR-0054 status change.
	tk.blockedReason = "legacy stuck"
	tk.blockedReasonType = BlockReasonObstacle

	if err := tk.Unblock("fixed", "user:pd", now); err != nil {
		t.Fatalf("Unblock on a legacy running+reason row: %v", err)
	}
	if tk.Status() != TaskRunning {
		t.Fatalf("legacy unblock must leave status running, got %s", tk.Status())
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("legacy unblock must clear the reason, got %q", tk.BlockedReason())
	}
}

// TestADR54_UnblockIdempotent: unblocking a not-blocked task is a silent no-op (no log, no
// version bump) — unchanged from ADR-0046.
func TestADR54_UnblockIdempotent(t *testing.T) {
	now := t0
	tk := newTask(t)
	_ = tk.Assign("agent:c", now)
	_ = tk.Start(now)
	before := tk.Version()
	if err := tk.Unblock("nothing to do", "user:pd", now); err != nil {
		t.Fatalf("Unblock on a non-blocked task must no-op, got %v", err)
	}
	if tk.Version() != before {
		t.Fatalf("idempotent Unblock must not bump the version: %d → %d", before, tk.Version())
	}
	if tk.Status() != TaskRunning {
		t.Fatalf("idempotent Unblock must not change status, got %s", tk.Status())
	}
}

// TestADR54_BlockOnNonRunningRejected: only work that is RUNNING (or already delivered —
// its acceptance can itself get stuck) can be parked. Block on an `open` task is
// ErrIllegalTransition and must leave no annotation behind.
func TestADR54_BlockOnNonRunningRejected(t *testing.T) {
	tk := newTask(t) // open
	if err := tk.Block("stuck", BlockReasonObstacle, "", t0); err != ErrIllegalTransition {
		t.Fatalf("Block on an open (non-running) task must be ErrIllegalTransition, got %v", err)
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("rejected Block must not set a reason, got %q", tk.BlockedReason())
	}
	if tk.Status() != TaskOpen {
		t.Fatalf("rejected Block must not change status, got %s", tk.Status())
	}
}

// TestADR54_Deliver is the I107 ① core: running→delivered is neither a completion (the
// false green) nor a block (the false alarm). It parks, keeps the assignee, records the
// summary on the action log, and writes NO blocked_reason.
func TestADR54_Deliver(t *testing.T) {
	now := t0
	tk := newTask(t)
	_ = tk.Assign("agent:c", now)
	_ = tk.Start(now)

	if err := tk.Deliver("pushed branch feat/x @ abc123", "agent:c", now); err != nil {
		t.Fatalf("Deliver on a running task: %v", err)
	}
	if tk.Status() != TaskDelivered {
		t.Fatalf("Deliver must move to delivered, got %s", tk.Status())
	}
	if tk.Status().IsTerminal() {
		t.Fatal("delivered must NOT be terminal — nobody has accepted it yet (false green)")
	}
	if tk.Status().IsDispatchable() {
		t.Fatal("delivered must NOT be dispatchable — the work is already handed over")
	}
	// NOT a block: no reason, so the overdue-block escalation must not treat a healthy
	// delivery as an incident needing rescue.
	if tk.BlockedReason() != "" {
		t.Fatalf("Deliver must not write a blocked_reason (it is not an alarm), got %q", tk.BlockedReason())
	}
	if tk.Assignee() != "agent:c" {
		t.Fatalf("Deliver must keep the assignee (a reject comes back to them), got %q", tk.Assignee())
	}
	if tk.ExecutionLeaseExpiresAt() != nil {
		t.Fatal("Deliver must clear the execution lease — there is no executor left to heartbeat")
	}
	// The summary lives on the action log (its only storage — no schema change needed).
	var found bool
	for _, l := range tk.ActionLogs() {
		if l.Action == TaskActionDelivered && l.Note == "pushed branch feat/x @ abc123" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Deliver must log the summary as a %q action, logs=%+v", TaskActionDelivered, tk.ActionLogs())
	}
}

// TestADR54_DeliverGuards: a delivery must be by the assignee, from `running`, and must
// say what was delivered — an unexplained delivery cannot be judged by the acceptance it
// is waiting on, which would re-create the un-actionable state ① exists to abolish.
func TestADR54_DeliverGuards(t *testing.T) {
	now := t0

	// Not running.
	open := newTask(t)
	_ = open.Assign("agent:c", now)
	if err := open.Deliver("s", "agent:c", now); err != ErrIllegalTransition {
		t.Fatalf("Deliver on an open task must be ErrIllegalTransition, got %v", err)
	}

	// Not the assignee.
	tk := newTask(t)
	_ = tk.Assign("agent:c", now)
	_ = tk.Start(now)
	if err := tk.Deliver("s", "agent:other", now); err != ErrNotTaskAssignee {
		t.Fatalf("Deliver by a non-assignee must be ErrNotTaskAssignee, got %v", err)
	}

	// Empty summary (whitespace-only counts as empty).
	if err := tk.Deliver("   ", "agent:c", now); err != ErrDeliverySummaryRequired {
		t.Fatalf("Deliver without a summary must be ErrDeliverySummaryRequired, got %v", err)
	}
	if tk.Status() != TaskRunning {
		t.Fatalf("a rejected Deliver must not change status, got %s", tk.Status())
	}
}

// TestADR54_DeliveredExits pins the acceptance verdict's TWO exits: Complete (accept) and
// Rework (reject). Together they are why `delivered` is not a deadlock.
func TestADR54_DeliveredExits(t *testing.T) {
	now := t0

	// Accept: delivered → completed.
	acc := newTask(t)
	_ = acc.Assign("agent:c", now)
	_ = acc.Start(now)
	_ = acc.Deliver("done", "agent:c", now)
	if err := acc.Complete("user:pd", now); err != nil {
		t.Fatalf("Complete on a delivered task (the ACCEPT exit): %v", err)
	}
	if acc.Status() != TaskCompleted {
		t.Fatalf("accept must complete the task, got %s", acc.Status())
	}

	// Reject: delivered → running, with the note handed back to the assignee.
	rej := newTask(t)
	_ = rej.Assign("agent:c", now)
	_ = rej.Start(now)
	_ = rej.Deliver("done", "agent:c", now)
	if err := rej.Rework("tests fail on CI", "user:pd", now); err != nil {
		t.Fatalf("Rework on a delivered task (the REJECT exit): %v", err)
	}
	if rej.Status() != TaskRunning {
		t.Fatalf("reject must return the task to running, got %s", rej.Status())
	}
	if rej.Assignee() != "agent:c" {
		t.Fatalf("reject must keep the assignee (the work returns to THEM), got %q", rej.Assignee())
	}
	if rej.BlockedComment() != "tests fail on CI" {
		t.Fatalf("reject must hand the note back via blocked_comment, got %q", rej.BlockedComment())
	}

	// Rework on a non-delivered task is meaningless.
	if err := rej.Rework("again", "user:pd", now); err != ErrIllegalTransition {
		t.Fatalf("Rework on a running (non-delivered) task must be ErrIllegalTransition, got %v", err)
	}
}

// TestADR54_StartRejectsParked is the anti-resurrection guard. `blocked` legitimately has
// a → running edge (that is Unblock's exit, and the no-deadlock guarantee), so adjacency
// ALONE would let Start walk it — quietly un-parking a task and forking a fresh
// empty-context executor onto it, which is exactly the re-drive I107 ② exists to stop.
// Start must refuse both parked states explicitly.
func TestADR54_StartRejectsParked(t *testing.T) {
	now := t0

	blocked := newTask(t)
	_ = blocked.Assign("agent:c", now)
	_ = blocked.Start(now)
	_ = blocked.Block("stuck", BlockReasonObstacle, "agent:c", now)
	if err := blocked.Start(now); err != ErrTaskParked {
		t.Fatalf("Start on a blocked task must be ErrTaskParked, got %v", err)
	}
	if blocked.Status() != TaskBlocked {
		t.Fatalf("a refused Start must leave the task parked, got %s", blocked.Status())
	}
	if blocked.BlockedReason() != "stuck" {
		t.Fatalf("a refused Start must not clear the block reason, got %q", blocked.BlockedReason())
	}

	delivered := newTask(t)
	_ = delivered.Assign("agent:c", now)
	_ = delivered.Start(now)
	_ = delivered.Deliver("done", "agent:c", now)
	if err := delivered.Start(now); err != ErrTaskParked {
		t.Fatalf("Start on a delivered task must be ErrTaskParked, got %v", err)
	}
	if delivered.Status() != TaskDelivered {
		t.Fatalf("a refused Start must leave the task delivered, got %s", delivered.Status())
	}
}

// TestADR54_ParkedRejectsLeaseAndReset: a parked task has no live execution, so it can
// neither renew a lease nor be tier-3 reset. Both must answer the PRECISE ErrTaskBlocked
// the callers already handle ("recovered by unblock") rather than degrading to the generic
// ErrIllegalTransition now that a park leaves `running`.
func TestADR54_ParkedRejectsLeaseAndReset(t *testing.T) {
	now := t0
	for _, tc := range []struct {
		name string
		park func(tk *Task)
	}{
		{"blocked", func(tk *Task) { _ = tk.Block("stuck", BlockReasonObstacle, "agent:c", now) }},
		{"delivered", func(tk *Task) { _ = tk.Deliver("done", "agent:c", now) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tk := newTask(t)
			_ = tk.Assign("agent:c", now)
			_ = tk.Start(now)
			tc.park(tk)

			if err := tk.RenewLease(time.Minute, now); err != ErrTaskBlocked {
				t.Fatalf("RenewLease on a %s task must be ErrTaskBlocked, got %v", tc.name, err)
			}
			if err := tk.ResetToOpen(now, true); err != ErrTaskBlocked {
				t.Fatalf("ResetToOpen on a %s task must be ErrTaskBlocked, got %v", tc.name, err)
			}
			if tk.Status().IsDispatchable() {
				t.Fatalf("a %s task must stay parked after refused lease/reset, got %s", tc.name, tk.Status())
			}
		})
	}
}
