package projectmanager

import (
	"testing"
	"time"
)

// TestTask_Archive_Happy: a fresh task archives, recording archivedAt/archivedBy,
// bumping version, and flipping IsArchived — WITHOUT changing status (orthogonal).
func TestTask_Archive_Happy(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(time.Hour)
	v0 := tk.Version()
	if tk.IsArchived() {
		t.Fatal("fresh task must not be archived")
	}
	if err := tk.Archive(at, "user:admin"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !tk.IsArchived() {
		t.Fatal("task must be archived after Archive")
	}
	if tk.ArchivedAt() == nil || !tk.ArchivedAt().Equal(at.UTC()) {
		t.Fatalf("archivedAt = %v want %v", tk.ArchivedAt(), at.UTC())
	}
	if tk.ArchivedBy() != "user:admin" {
		t.Fatalf("archivedBy = %q want user:admin", tk.ArchivedBy())
	}
	if tk.Version() != v0+1 {
		t.Fatalf("version = %d want %d", tk.Version(), v0+1)
	}
	// Orthogonal: status preserved (still open).
	if tk.Status() != TaskOpen {
		t.Fatalf("status changed to %q on archive — must be orthogonal", tk.Status())
	}
}

// TestTask_Archive_StatusPreservedThroughArchive: archiving a task in a non-open
// status leaves that status intact (orthogonal archive).
func TestTask_Archive_StatusPreservedThroughArchive(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(time.Hour)
	if err := tk.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := tk.Complete("user:a", at); err != nil {
		t.Fatal(err)
	}
	if tk.Status() != TaskCompleted {
		t.Fatalf("precondition: status = %q want completed", tk.Status())
	}
	if err := tk.Archive(at, "user:admin"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if tk.Status() != TaskCompleted {
		t.Fatalf("status = %q after archive — must stay completed (orthogonal)", tk.Status())
	}
	if !tk.IsArchived() {
		t.Fatal("must be archived")
	}
}

// TestTask_Archive_DoubleRejected: re-archiving an already-archived task returns
// ErrTaskArchived (mirrors Conversation.Archive idempotency contract).
func TestTask_Archive_DoubleRejected(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(time.Hour)
	if err := tk.Archive(at, "user:admin"); err != nil {
		t.Fatal(err)
	}
	if err := tk.Archive(at, "user:admin"); err != ErrTaskArchived {
		t.Fatalf("double archive = %v want ErrTaskArchived", err)
	}
}

// TestTask_Archive_InvalidByRejected: archived_by must validate.
func TestTask_Archive_InvalidByRejected(t *testing.T) {
	tk := newTask(t)
	if err := tk.Archive(t0, "bogus"); err == nil {
		t.Fatal("Archive with invalid by must error")
	}
	if tk.IsArchived() {
		t.Fatal("task must not be archived after a failed Archive")
	}
}

// TestTask_Archived_RejectsMutations: an archived task is read-only — every
// mutator returns ErrTaskArchived (Rename / SetDescription / status transitions /
// Assign / Unassign / SetStatus / SetTags / SetPlan / ClearPlan).
func TestTask_Archived_RejectsMutations(t *testing.T) {
	at := t0.Add(time.Hour)

	mutators := map[string]func(tk *Task) error{
		"Rename":         func(tk *Task) error { return tk.Rename("x", at) },
		"SetDescription": func(tk *Task) error { return tk.SetDescription("x", at) },
		"Start":          func(tk *Task) error { return tk.Start(at) },
		"Discard":        func(tk *Task) error { return tk.Discard(at) },
		"Block":          func(tk *Task) error { return tk.Block("why", BlockReasonObstacle, "user:a", at) },
		"Complete":       func(tk *Task) error { return tk.Complete("user:a", at) },
		"Unblock":        func(tk *Task) error { return tk.Unblock("", "user:a", at) },
		"RenewLease":     func(tk *Task) error { return tk.RenewLease(time.Minute, at) },
		"RecordReassign": func(tk *Task) error { return tk.RecordReassignment("agent:x", "user:a", at) },
		"SetStatus":      func(tk *Task) error { return tk.SetStatus(TaskRunning, at) },
		"Assign":         func(tk *Task) error { return tk.Assign("user:a", at) },
		"Unassign":       func(tk *Task) error { return tk.Unassign(at) },
		"SetTags":        func(tk *Task) error { return tk.SetTags([]string{"x"}, at) },
		"SetPlan":        func(tk *Task) error { return tk.SetPlan("PL1", at) },
		"ClearPlan":      func(tk *Task) error { return tk.ClearPlan(at) },
		"Reopen":         func(tk *Task) error { return tk.Reopen(at) },
	}
	for name, mut := range mutators {
		tk := newTask(t)
		if err := tk.Archive(at, "user:admin"); err != nil {
			t.Fatal(err)
		}
		vAfterArchive := tk.Version()
		if err := mut(tk); err != ErrTaskArchived {
			t.Fatalf("%s on archived task = %v want ErrTaskArchived", name, err)
		}
		// A rejected mutation must not bump version.
		if tk.Version() != vAfterArchive {
			t.Fatalf("%s bumped version on an archived task (%d→%d)", name, vAfterArchive, tk.Version())
		}
	}
}

// TestTask_FinalizeForArchive_Open: an OPEN task (the escape/skipped-node case)
// finalizes to discarded — so an archive cascade never leaves it open+archived.
func TestTask_FinalizeForArchive_Open(t *testing.T) {
	tk := newTask(t) // open
	at := t0.Add(time.Hour)
	if err := tk.FinalizeForArchive(at); err != nil {
		t.Fatalf("FinalizeForArchive(open): %v", err)
	}
	if tk.Status() != TaskDiscarded {
		t.Fatalf("status = %q want discarded", tk.Status())
	}
	if tk.IsArchived() {
		t.Fatal("FinalizeForArchive must NOT archive — that is Archive()'s job (called next in the cascade)")
	}
}

// TestTask_FinalizeForArchive_Reopened: a reopened task (which Discard() rejects as a
// non-adjacent transition) still finalizes — an archive abandons whatever is in
// flight regardless of the exact non-terminal state.
func TestTask_FinalizeForArchive_Reopened(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(time.Hour)
	if err := tk.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := tk.Complete("user:a", at); err != nil {
		t.Fatal(err)
	}
	if err := tk.Reopen(at); err != nil {
		t.Fatal(err)
	}
	if tk.Status() != TaskReopened {
		t.Fatalf("precondition: status = %q want reopened", tk.Status())
	}
	// Discard() would reject reopened (non-adjacent); FinalizeForArchive must not.
	if err := tk.Discard(at); err != ErrIllegalTransition {
		t.Fatalf("precondition: Discard(reopened) = %v want ErrIllegalTransition", err)
	}
	if err := tk.FinalizeForArchive(at); err != nil {
		t.Fatalf("FinalizeForArchive(reopened): %v", err)
	}
	if tk.Status() != TaskDiscarded {
		t.Fatalf("status = %q want discarded", tk.Status())
	}
}

// TestTask_FinalizeForArchive_TerminalNoop: a completed/discarded task keeps its real
// outcome (no-op) — the cascade must not rewrite a finished node to discarded.
func TestTask_FinalizeForArchive_TerminalNoop(t *testing.T) {
	at := t0.Add(time.Hour)
	for _, tc := range []struct {
		name  string
		build func() *Task
		want  TaskStatus
	}{
		{"completed", func() *Task {
			tk := newTask(t)
			_ = tk.Start(at)
			_ = tk.Complete("user:a", at)
			return tk
		}, TaskCompleted},
		{"discarded", func() *Task {
			tk := newTask(t)
			_ = tk.Discard(at)
			return tk
		}, TaskDiscarded},
	} {
		tk := tc.build()
		v0 := tk.Version()
		if err := tk.FinalizeForArchive(at); err != nil {
			t.Fatalf("%s: FinalizeForArchive: %v", tc.name, err)
		}
		if tk.Status() != tc.want {
			t.Fatalf("%s: status = %q want %q (terminal preserved)", tc.name, tk.Status(), tc.want)
		}
		if tk.Version() != v0 {
			t.Fatalf("%s: no-op must not bump version (%d→%d)", tc.name, v0, tk.Version())
		}
	}
}

// TestTask_FinalizeForArchive_ClearsBlockedReason: a stuck (blocked-annotated) task
// finalizes to discarded and clears the annotation (a discarded task is not stuck).
func TestTask_FinalizeForArchive_ClearsBlockedReason(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(time.Hour)
	if err := tk.Assign("user:a", at); err != nil {
		t.Fatal(err)
	}
	if err := tk.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := tk.Block("waiting on infra", BlockReasonObstacle, "user:a", at); err != nil {
		t.Fatal(err)
	}
	if err := tk.FinalizeForArchive(at); err != nil {
		t.Fatalf("FinalizeForArchive: %v", err)
	}
	if tk.Status() != TaskDiscarded {
		t.Fatalf("status = %q want discarded", tk.Status())
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("blocked_reason = %q want cleared", tk.BlockedReason())
	}
}

// TestTask_FinalizeForArchive_AlreadyArchivedRejected: it must run BEFORE Archive();
// on an already-archived task it returns ErrTaskArchived (the lock it exists to beat).
func TestTask_FinalizeForArchive_AlreadyArchivedRejected(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(time.Hour)
	if err := tk.Archive(at, "user:admin"); err != nil {
		t.Fatal(err)
	}
	if err := tk.FinalizeForArchive(at); err != ErrTaskArchived {
		t.Fatalf("FinalizeForArchive(archived) = %v want ErrTaskArchived", err)
	}
}

// TestTask_FinalizeArchived_Escape: the escape hatch concludes an already-archived
// open task to discarded — the one permitted write on an archived task (every other
// mutator, incl. Discard, stays locked with ErrTaskArchived).
func TestTask_FinalizeArchived_Escape(t *testing.T) {
	tk := newTask(t) // open
	at := t0.Add(time.Hour)
	if err := tk.Archive(at, "user:admin"); err != nil {
		t.Fatal(err)
	}
	// Precondition: it is a dead state — Discard() (and every mutator) is locked.
	if err := tk.Discard(at); err != ErrTaskArchived {
		t.Fatalf("precondition: Discard(archived) = %v want ErrTaskArchived", err)
	}
	if err := tk.FinalizeArchived(at); err != nil {
		t.Fatalf("FinalizeArchived(archived open): %v", err)
	}
	if tk.Status() != TaskDiscarded {
		t.Fatalf("status = %q want discarded", tk.Status())
	}
	if !tk.IsArchived() {
		t.Fatal("FinalizeArchived must keep the task archived (it concludes, not un-archives)")
	}
}

// TestTask_FinalizeArchived_LiveRejected: a live task must use the normal Discard
// path; the escape hatch refuses it (ErrTaskNotArchived).
func TestTask_FinalizeArchived_LiveRejected(t *testing.T) {
	tk := newTask(t)
	if err := tk.FinalizeArchived(t0.Add(time.Hour)); err != ErrTaskNotArchived {
		t.Fatalf("FinalizeArchived(live) = %v want ErrTaskNotArchived", err)
	}
}

// TestTask_FinalizeArchived_TerminalNoop: an archived task already terminal keeps its
// outcome (no-op) — the escape hatch never rewrites a real completed/discarded result.
func TestTask_FinalizeArchived_TerminalNoop(t *testing.T) {
	at := t0.Add(time.Hour)
	tk := newTask(t)
	if err := tk.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := tk.Complete("user:a", at); err != nil {
		t.Fatal(err)
	}
	if err := tk.Archive(at, "user:admin"); err != nil {
		t.Fatal(err)
	}
	v0 := tk.Version()
	if err := tk.FinalizeArchived(at); err != nil {
		t.Fatalf("FinalizeArchived(archived completed): %v", err)
	}
	if tk.Status() != TaskCompleted {
		t.Fatalf("status = %q want completed (preserved)", tk.Status())
	}
	if tk.Version() != v0 {
		t.Fatalf("no-op must not bump version (%d→%d)", v0, tk.Version())
	}
}

// TestTask_Rehydrate_ArchiveRoundTrip: archived fields survive a rehydrate.
func TestTask_Rehydrate_ArchiveRoundTrip(t *testing.T) {
	at := t0.Add(2 * time.Hour)
	tk, err := RehydrateTask(RehydrateTaskInput{
		ID: "T1", ProjectID: "P1", Title: "do", Status: TaskCompleted,
		CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: at, Version: 5,
		ArchivedAt: &at, ArchivedBy: "user:admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tk.IsArchived() {
		t.Fatal("rehydrated task must be archived")
	}
	if tk.ArchivedBy() != "user:admin" || tk.ArchivedAt() == nil || !tk.ArchivedAt().Equal(at.UTC()) {
		t.Fatalf("archive fields lost: by=%q at=%v", tk.ArchivedBy(), tk.ArchivedAt())
	}
	if tk.Status() != TaskCompleted {
		t.Fatalf("status = %q want completed (orthogonal preserved)", tk.Status())
	}
}
