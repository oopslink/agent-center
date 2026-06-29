package projectmanager

import (
	"testing"
	"time"
)

// T570 follow-up: completed_at is the authoritative completion timestamp — set
// when a task ENTERS completed, CLEARED on any transition out of it (reopen), and
// re-stamped on a later re-complete. Distinct from status_changed_at (which moves
// on every status change).
func TestTask_CompletedAt_SetOnComplete_ClearedOnReopen(t *testing.T) {
	tk := newTask(t) // open; never completed
	if !tk.CompletedAt().IsZero() {
		t.Fatalf("new task completed_at = %v, want zero", tk.CompletedAt())
	}

	// open → running: still not completed.
	if err := tk.SetStatus(TaskRunning, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !tk.CompletedAt().IsZero() {
		t.Fatalf("running completed_at = %v, want zero", tk.CompletedAt())
	}

	// running → completed: stamps completed_at.
	completeAt := t0.Add(2 * time.Hour)
	if err := tk.SetStatus(TaskCompleted, completeAt); err != nil {
		t.Fatal(err)
	}
	if !tk.CompletedAt().Equal(completeAt.UTC()) {
		t.Fatalf("completed_at = %v, want %v", tk.CompletedAt(), completeAt.UTC())
	}

	// completed → reopened: RESET (a reopened task no longer advertises completion).
	if err := tk.SetStatus(TaskReopened, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !tk.CompletedAt().IsZero() {
		t.Fatalf("after reopen completed_at = %v, want zero (reset)", tk.CompletedAt())
	}

	// re-complete: a fresh timestamp, not the old one.
	completeAt2 := t0.Add(5 * time.Hour)
	if err := tk.SetStatus(TaskCompleted, completeAt2); err != nil {
		t.Fatal(err)
	}
	if !tk.CompletedAt().Equal(completeAt2.UTC()) {
		t.Fatalf("re-complete completed_at = %v, want %v", tk.CompletedAt(), completeAt2.UTC())
	}
}

// A non-status metadata edit must NOT change completed_at (it tracks completion,
// not arbitrary touches) — parallels the status_changed_at discipline.
func TestTask_CompletedAt_UnchangedByMetadataEdit(t *testing.T) {
	tk := newTask(t)
	if err := tk.SetStatus(TaskRunning, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	completeAt := t0.Add(2 * time.Hour)
	if err := tk.SetStatus(TaskCompleted, completeAt); err != nil {
		t.Fatal(err)
	}
	// rename / retag well after completion.
	if err := tk.SetTags([]string{"x"}, t0.Add(9*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !tk.CompletedAt().Equal(completeAt.UTC()) {
		t.Fatalf("completed_at moved after a tag edit: %v, want %v", tk.CompletedAt(), completeAt.UTC())
	}
}
