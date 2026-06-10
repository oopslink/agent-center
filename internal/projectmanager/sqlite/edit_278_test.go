package sqlite

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestTaskRepo_TagsStatusChangedAt_RoundTrip covers the v2.8.1 edit-task #278
// columns: tags (JSON-serialized) and status_changed_at survive Save+FindByID.
func TestTaskRepo_TagsStatusChangedAt_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	tk, err := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "do", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := tk.SetTags([]string{"alpha", "beta"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	statusAt := t0.Add(2 * time.Hour)
	if err := tk.SetStatus(pm.TaskRunning, statusAt); err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := tr.FindByID(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if tags := got.Tags(); len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Fatalf("tags round-trip = %v, want [alpha beta]", tags)
	}
	if !got.StatusChangedAt().Equal(statusAt) {
		t.Fatalf("status_changed_at round-trip = %v, want %v", got.StatusChangedAt(), statusAt)
	}

	// Update path: clear tags + change status, re-read.
	if err := got.SetTags(nil, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	completeAt := t0.Add(4 * time.Hour)
	if err := got.SetStatus(pm.TaskCompleted, completeAt); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := tr.FindByID(ctx, "T1")
	if re.Tags() != nil {
		t.Fatalf("cleared tags should round-trip nil, got %v", re.Tags())
	}
	if !re.StatusChangedAt().Equal(completeAt) {
		t.Fatalf("updated status_changed_at = %v, want %v", re.StatusChangedAt(), completeAt)
	}
}

// TestIssueRepo_TagsStatusChangedAt_RoundTrip mirrors the task test for issues.
func TestIssueRepo_TagsStatusChangedAt_RoundTrip(t *testing.T) {
	ctx, _, _, ir, _, _, _, _ := setup(t)
	is, err := pm.NewIssue(pm.NewIssueInput{ID: "I1", ProjectID: "P1", Title: "bug", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := is.SetTags([]string{"urgent"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	statusAt := t0.Add(2 * time.Hour)
	if err := is.SetStatus(pm.IssueResolved, statusAt); err != nil {
		t.Fatal(err)
	}
	if err := ir.Save(ctx, is); err != nil {
		t.Fatal(err)
	}
	got, err := ir.FindByID(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	if tags := got.Tags(); len(tags) != 1 || tags[0] != "urgent" {
		t.Fatalf("issue tags round-trip = %v, want [urgent]", tags)
	}
	if !got.StatusChangedAt().Equal(statusAt) {
		t.Fatalf("issue status_changed_at = %v, want %v", got.StatusChangedAt(), statusAt)
	}
}
