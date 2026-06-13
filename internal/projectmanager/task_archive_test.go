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
		"Block":          func(tk *Task) error { return tk.Block("why", at) },
		"Complete":       func(tk *Task) error { return tk.Complete("user:a", at) },
		"Unblock":        func(tk *Task) error { return tk.Unblock(at) },
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
