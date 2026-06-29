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

// TestTaskRepo_RequiredCapabilities_RoundTrip covers v2.18.3 BE-1: required_capabilities
// (canonical JSON array) survives Save + FindByID + Update; default is unrestricted (nil).
func TestTaskRepo_RequiredCapabilities_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)

	// Default: created without caps → '[]' in DB → nil on read (unrestricted).
	plain, _ := pm.NewTask(pm.NewTaskInput{ID: "TC0", ProjectID: "P1", Title: "x", CreatedBy: "user:a", CreatedAt: t0})
	if err := tr.Save(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if got, _ := tr.FindByID(ctx, "TC0"); got.RequiredCapabilities() != nil {
		t.Fatalf("default required_capabilities = %v, want nil", got.RequiredCapabilities())
	}

	// Canonicalized set round-trips.
	tk, _ := pm.NewTask(pm.NewTaskInput{
		ID: "TC1", ProjectID: "P1", Title: "x", CreatedBy: "user:a", CreatedAt: t0,
		RequiredCapabilities: []string{" Go ", "go", "RUST"},
	})
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := tr.FindByID(ctx, "TC1")
	if err != nil {
		t.Fatal(err)
	}
	if c := got.RequiredCapabilities(); len(c) != 2 || c[0] != "go" || c[1] != "rust" {
		t.Fatalf("caps round-trip = %v, want [go rust]", c)
	}

	// Update path: replace then clear.
	if err := got.SetRequiredCapabilities([]string{"python"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if c, _ := tr.FindByID(ctx, "TC1"); len(c.RequiredCapabilities()) != 1 || c.RequiredCapabilities()[0] != "python" {
		t.Fatalf("after update = %v, want [python]", c.RequiredCapabilities())
	}
	got2, _ := tr.FindByID(ctx, "TC1")
	_ = got2.SetRequiredCapabilities(nil, t0.Add(2*time.Hour))
	if err := tr.Update(ctx, got2); err != nil {
		t.Fatal(err)
	}
	if c, _ := tr.FindByID(ctx, "TC1"); c.RequiredCapabilities() != nil {
		t.Fatalf("after clear = %v, want nil", c.RequiredCapabilities())
	}
}

// TestCapsMarshalUnmarshal covers the JSON helpers directly, incl. the
// invalid/empty fallbacks (v2.18.3 BE-1).
func TestCapsMarshalUnmarshal(t *testing.T) {
	if marshalCaps(nil) != "[]" {
		t.Fatalf("marshalCaps(nil) = %q, want []", marshalCaps(nil))
	}
	if got := marshalCaps([]string{"go", "rust"}); got != `["go","rust"]` {
		t.Fatalf("marshalCaps = %q", got)
	}
	if unmarshalCaps("") != nil {
		t.Fatal("unmarshalCaps(\"\") should be nil")
	}
	if unmarshalCaps("not json") != nil {
		t.Fatal("unmarshalCaps(invalid) should be nil")
	}
	got := unmarshalCaps(`["go","rust"]`)
	if len(got) != 2 || got[0] != "go" || got[1] != "rust" {
		t.Fatalf("unmarshalCaps = %v", got)
	}
}
