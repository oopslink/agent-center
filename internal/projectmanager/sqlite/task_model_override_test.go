package sqlite

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestTaskRepo_ModelOverride_RoundTrip covers the F3 model-routing per-task
// executor model override column (design §5 & §10): a task created with a Model
// survives Save+FindByID, and a default (unset) task reads back "".
func TestTaskRepo_ModelOverride_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)

	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: "TM1", ProjectID: "P1", Title: "pinned model", CreatedBy: "user:a", CreatedAt: t0,
		Model: "claude-opus",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := tr.FindByID(ctx, "TM1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model() != "claude-opus" {
		t.Fatalf("model override round-trip = %q, want claude-opus", got.Model())
	}

	// A default (never-stamped) task reads back "".
	d, err := pm.NewTask(pm.NewTaskInput{ID: "TM2", ProjectID: "P1", Title: "plain", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	got2, _ := tr.FindByID(ctx, "TM2")
	if got2.Model() != "" {
		t.Fatalf("default task model = %q, want empty", got2.Model())
	}
}
