package sqlite

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestTaskRepo_DispatchMode_RoundTrip covers the I105 per-node fork override column
// (migration 0110): a task marked supervisor_inline survives Save+FindByID, and — the
// part that matters — a task created WITHOUT the field reads back "" (= executor_fork
// = today's routing), because a stray non-empty value here would suppress that node's
// fork on the worker.
func TestTaskRepo_DispatchMode_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)

	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: "TD1", ProjectID: "P1", Title: "deploy node", CreatedBy: "user:a", CreatedAt: t0,
		DispatchMode: pm.DispatchSupervisorInline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := tr.FindByID(ctx, "TD1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DispatchMode() != pm.DispatchSupervisorInline {
		t.Fatalf("dispatch_mode round-trip = %q, want supervisor_inline", got.DispatchMode())
	}
	if !got.DispatchMode().RoutesInline() {
		t.Error("a persisted supervisor_inline task must route inline after rehydrate")
	}

	// The default path: an ordinary task never stamps the field and MUST read back as
	// fork. This is the persistence half of I105 red line #1.
	d, err := pm.NewTask(pm.NewTaskInput{ID: "TD2", ProjectID: "P1", Title: "plain dev task", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	got2, err := tr.FindByID(ctx, "TD2")
	if err != nil {
		t.Fatal(err)
	}
	if got2.DispatchMode() != "" {
		t.Fatalf("default task dispatch_mode = %q, want empty (= executor_fork)", got2.DispatchMode())
	}
	if got2.DispatchMode().RoutesInline() {
		t.Fatal("an unmarked task must NEVER route inline — that would starve every Dev node")
	}
}

// TestTaskRepo_DispatchMode_SurvivesUpdate locks that the column is written on the
// UPDATE path too: Update rewrites the full column list, so omitting dispatch_mode
// there (or misaligning the positional args) would silently drop a node's inline mark
// on the first status change and send it back to forking into an empty workspace.
func TestTaskRepo_DispatchMode_SurvivesUpdate(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)

	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: "TD3", ProjectID: "P1", Title: "verdict node", CreatedBy: "user:a", CreatedAt: t0,
		DispatchMode: pm.DispatchSupervisorInline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	// Any mutation → Update path.
	tk.SetRequiredCapabilities([]string{"deploy"}, t0)
	if err := tr.Update(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := tr.FindByID(ctx, "TD3")
	if err != nil {
		t.Fatal(err)
	}
	if got.DispatchMode() != pm.DispatchSupervisorInline {
		t.Fatalf("dispatch_mode after Update = %q, want supervisor_inline (must not be dropped)", got.DispatchMode())
	}
}

// TestTaskRepo_DispatchMode_UnknownPersistedValueCoercesToFork locks the read-side
// net: a row carrying a value this build does not know (hand-edited, or written by a
// newer center) must rehydrate as "" = FORK, never as an accidental inline route. Read
// paths have to be total; the write path is where bad values are rejected.
func TestTaskRepo_DispatchMode_UnknownPersistedValueCoercesToFork(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)

	tk, err := pm.NewTask(pm.NewTaskInput{ID: "TD4", ProjectID: "P1", Title: "t", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	// Simulate a row this build cannot interpret.
	if _, err := tr.db.ExecContext(ctx, `UPDATE pm_tasks SET dispatch_mode='some_future_mode' WHERE id='TD4'`); err != nil {
		t.Fatal(err)
	}
	got, err := tr.FindByID(ctx, "TD4")
	if err != nil {
		t.Fatalf("an unknown dispatch_mode must not break the read: %v", err)
	}
	if got.DispatchMode() != "" {
		t.Fatalf("unknown persisted dispatch_mode = %q, want coerced to \"\" (fork)", got.DispatchMode())
	}
	if got.DispatchMode().RoutesInline() {
		t.Fatal("an unknown persisted value must never route inline")
	}
}
