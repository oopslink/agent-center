package persistence

import (
	"context"
	"testing"
)

// TestMigration0058_TaskStateSimplify proves the ADR-0046 data migration
// (0058_v291_task_state_simplify — renumbered from 0057 on the v2.9.1 merge, since
// thread's 0057_v291_message_thread_refs already holds slot 0057) rewrites the two
// deleted task states:
//   - status='blocked'  → 'running', KEEPING blocked_reason (stuck is now a
//     running-task annotation, not a state — removes the deadlock class).
//   - status='verified' → 'completed'.
//
// Strategy: full Up lands at 58; Down to 56 reverts 0057+0058 (the task-state down
// is a data-only no-op; pm_tasks.status stays a free TEXT column). We then seed
// legacy 'blocked'/'verified' rows and run Up again, which re-applies 0057+0058,
// and assert the rows were rewritten while untouched statuses survive verbatim.
func TestMigration0058_TaskStateSimplify(t *testing.T) {
	db, err := Open(t.TempDir() + "/adr46.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	// Down to 56 reverts 0057(thread)+0058(task-state); the task-state down is a data-only no-op.
	if err := mig.Down(ctx, 56); err != nil {
		t.Fatalf("Down(56): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 56 {
		t.Fatalf("version after Down(56): got %d want 56", v)
	}

	insert := func(id, status, reason string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO pm_tasks (id, project_id, title, status, blocked_reason, created_by, created_at, updated_at)
			VALUES (?, 'P1', 'do', ?, ?, 'user:a', '2026-06-13T00:00:00Z', '2026-06-13T00:00:00Z')`,
			id, status, reason)
		if err != nil {
			t.Fatalf("insert %s(%s): %v", id, status, err)
		}
	}
	insert("T-blocked", "blocked", "waiting on creds")
	insert("T-verified", "verified", "")
	insert("T-running", "running", "")
	insert("T-open", "open", "")

	// Re-apply 0057(thread)+0058(task-state)+0059(builtin).
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("second Up (apply 0057+0058+0059): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 97 {
		t.Fatalf("version after re-Up: got %d want 97", v)
	}

	status := func(id string) (string, string) {
		t.Helper()
		var st, reason string
		if err := db.QueryRowContext(ctx, `SELECT status, blocked_reason FROM pm_tasks WHERE id = ?`, id).
			Scan(&st, &reason); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return st, reason
	}

	// blocked → running, reason KEPT.
	if st, reason := status("T-blocked"); st != "running" || reason != "waiting on creds" {
		t.Fatalf("T-blocked → (%q, reason=%q), want (running, waiting on creds)", st, reason)
	}
	// verified → completed.
	if st, _ := status("T-verified"); st != "completed" {
		t.Fatalf("T-verified → %q, want completed", st)
	}
	// untouched statuses survive verbatim.
	if st, _ := status("T-running"); st != "running" {
		t.Fatalf("T-running → %q, want running (untouched)", st)
	}
	if st, _ := status("T-open"); st != "open" {
		t.Fatalf("T-open → %q, want open (untouched)", st)
	}
}
