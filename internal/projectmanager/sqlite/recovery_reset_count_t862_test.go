package sqlite

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T862 §2B / migration 0097: recovery_reset_count round-trips — DEFAULT 0 on a fresh
// row (migration is additive), incremented by ResetToOpen, persists through Update +
// re-read, and is zeroed by Complete.
func TestTaskRepo_RecoveryResetCount_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	tk, err := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "do", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	// Additive migration: an untouched row reads DEFAULT 0.
	got, _ := tr.FindByID(ctx, "T1")
	if got.RecoveryResetCount() != 0 {
		t.Fatalf("new task recovery_reset_count = %d, want 0", got.RecoveryResetCount())
	}

	// Drive it to running with a lapsed lease, reset → count 1, persist + re-read.
	if err := got.Assign("agent:dead", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := got.Start(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := got.ResetToOpen(t0.Add(2*time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := tr.FindByID(ctx, "T1")
	if re.RecoveryResetCount() != 1 {
		t.Fatalf("after reset recovery_reset_count = %d, want 1", re.RecoveryResetCount())
	}

	// Complete zeroes it and that persists.
	if err := re.Assign("agent:new", t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := re.Start(t0.Add(3 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := re.Complete("user:pm", t0.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, re); err != nil {
		t.Fatal(err)
	}
	after, _ := tr.FindByID(ctx, "T1")
	if after.RecoveryResetCount() != 0 {
		t.Fatalf("after complete recovery_reset_count = %d, want 0", after.RecoveryResetCount())
	}
}
