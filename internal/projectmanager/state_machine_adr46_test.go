package projectmanager

import "testing"

// ADR-0046 (Task state machine 7→5) class-guards. The state set is exactly
// {open, running, completed, discarded, reopened}; "blocked" and "verified" are
// DELETED. "blocked" is no longer a STATE — it is a blocked_reason annotation on
// a RUNNING task, so a stuck task can never deadlock.

// TestADR46_NoDeadlockableState is the "no reachable deadlock" class-guard: from
// EVERY non-terminal status there must exist at least one legal forward
// transition (taskTransitions[s] non-empty). A non-terminal status with no
// outgoing edge would be an unrecoverable deadlock.
func TestADR46_NoDeadlockableState(t *testing.T) {
	nonTerminal := []TaskStatus{TaskOpen, TaskRunning, TaskReopened}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Fatalf("%s must be non-terminal", s)
		}
		if len(taskTransitions[s]) == 0 {
			t.Fatalf("non-terminal status %q has no legal forward transition — deadlockable state", s)
		}
	}
	// Terminal states are intentionally sinks; the set is exactly {completed, discarded}.
	for _, s := range []TaskStatus{TaskCompleted, TaskDiscarded} {
		if !s.IsTerminal() {
			t.Fatalf("%s must be terminal", s)
		}
	}
}

// TestADR46_DeletedStatusesInvalid pins that the removed enum values no longer
// validate — guards against a stray "blocked"/"verified" sneaking back in.
func TestADR46_DeletedStatusesInvalid(t *testing.T) {
	if TaskStatus("blocked").IsValid() {
		t.Fatal(`TaskStatus("blocked") must NOT be valid (ADR-0046 deleted it)`)
	}
	if TaskStatus("verified").IsValid() {
		t.Fatal(`TaskStatus("verified") must NOT be valid (ADR-0046 deleted it)`)
	}
}

// TestADR46_BlockIsAnnotationNotState proves the new block semantics: Block on a
// RUNNING task sets a reason and KEEPS the status running; Unblock clears the
// reason and KEEPS it running; Complete on a running+blocked task clears the
// reason and moves to completed.
func TestADR46_BlockIsAnnotationNotState(t *testing.T) {
	now := t0

	tk := newTask(t)
	_ = tk.Assign("agent:c", now)
	if err := tk.Start(now); err != nil {
		t.Fatal(err)
	}

	if err := tk.Block("stuck", BlockReasonObstacle, "agent:c", now); err != nil {
		t.Fatalf("Block on running task: %v", err)
	}
	if tk.Status() != TaskRunning {
		t.Fatalf("Block must keep status running, got %s", tk.Status())
	}
	if tk.BlockedReason() != "stuck" {
		t.Fatalf("Block must set the reason annotation, got %q", tk.BlockedReason())
	}

	if err := tk.Unblock("", "agent:c", now); err != nil {
		t.Fatalf("Unblock: %v", err)
	}
	if tk.Status() != TaskRunning {
		t.Fatalf("Unblock must keep status running, got %s", tk.Status())
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("Unblock must clear the reason, got %q", tk.BlockedReason())
	}

	// Re-block then Complete: completion clears the reason and moves to completed.
	if err := tk.Block("stuck again", BlockReasonObstacle, "agent:c", now); err != nil {
		t.Fatal(err)
	}
	if err := tk.Complete("agent:c", now); err != nil {
		t.Fatalf("Complete on a running+blocked task: %v", err)
	}
	if tk.Status() != TaskCompleted {
		t.Fatalf("Complete must move to completed, got %s", tk.Status())
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("Complete must clear the reason annotation, got %q", tk.BlockedReason())
	}
}

// TestADR46_BlockOnNonRunningRejected: only RUNNING work can be "stuck". Block on
// a non-running (e.g. open) task is ErrIllegalTransition.
func TestADR46_BlockOnNonRunningRejected(t *testing.T) {
	tk := newTask(t) // open
	if err := tk.Block("stuck", BlockReasonObstacle, "", t0); err != ErrIllegalTransition {
		t.Fatalf("Block on an open (non-running) task must be ErrIllegalTransition, got %v", err)
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("rejected Block must not set a reason, got %q", tk.BlockedReason())
	}
}
