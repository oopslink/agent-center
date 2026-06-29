package sqlite

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T570 follow-up: the persisted completed_at column round-trips — set on complete,
// NULL (zero) after reopen.
func TestTaskRepo_CompletedAt_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	tk, err := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "do", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	// A fresh task has no completion time.
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, _ := tr.FindByID(ctx, "T1")
	if !got.CompletedAt().IsZero() {
		t.Fatalf("new task completed_at = %v, want zero", got.CompletedAt())
	}

	// run → complete stamps completed_at; it survives Update + re-read.
	if err := got.SetStatus(pm.TaskRunning, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	completeAt := t0.Add(2 * time.Hour)
	if err := got.SetStatus(pm.TaskCompleted, completeAt); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := tr.FindByID(ctx, "T1")
	if !re.CompletedAt().Equal(completeAt) {
		t.Fatalf("completed_at round-trip = %v, want %v", re.CompletedAt(), completeAt)
	}

	// reopen clears it → persists as NULL (zero).
	if err := re.SetStatus(pm.TaskReopened, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, re); err != nil {
		t.Fatal(err)
	}
	after, _ := tr.FindByID(ctx, "T1")
	if !after.CompletedAt().IsZero() {
		t.Fatalf("after reopen completed_at = %v, want zero", after.CompletedAt())
	}
}
