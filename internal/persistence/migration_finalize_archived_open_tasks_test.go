package persistence

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration0076_FinalizeArchivedOpenTasks proves the T339 backfill
// (0076_v214_finalize_archived_open_tasks) finalizes every ARCHIVED + non-terminal
// task to discarded — closing out the pre-existing open+archived leak (the 7 escape
// nodes) — while leaving live tasks and already-terminal archived tasks untouched.
//
// Strategy mirrors the 0071 data-backfill test: full Up, Down to 75 (reverts 0076
// only; pm_tasks keeps 0056's archived_at column), seed the leak rows directly, re-Up
// to run the backfill, assert. A second Down(75)->re-Up proves idempotency.
func TestMigration0076_FinalizeArchivedOpenTasks(t *testing.T) {
	db, err := Open(t.TempDir() + "/arch.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if err := mig.Down(ctx, 75); err != nil {
		t.Fatalf("Down(75): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 75 {
		t.Fatalf("version after Down(75): got %d want 75", v)
	}

	seedArchivedLeakTasks(t, db)

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("second Up (apply 0076): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 95 {
		t.Fatalf("version after re-Up: got %d want 95", v)
	}

	assertArchivedBackfill(t, db)

	// Idempotency: revert 0076 (no-op down) and re-run over already-backfilled rows.
	if err := mig.Down(ctx, 75); err != nil {
		t.Fatalf("Down(75) #2: %v", err)
	}
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("third Up (idempotency): %v", err)
	}
	assertArchivedBackfill(t, db)
}

func seedArchivedLeakTasks(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	const archAt = "2026-06-21T08:00:00Z"
	task := func(id, status, archivedAt, archivedBy, blockedReason string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at,
			                      status_changed_at, archived_at, archived_by, blocked_reason)
			VALUES (?, 'P1', 'do', ?, 'user:a', '2026-06-21T00:00:00Z', '2026-06-21T01:00:00Z',
			        '2026-06-21T00:30:00Z', ?, ?, ?)`,
			id, status, archivedAt, archivedBy, blockedReason)
		if err != nil {
			t.Fatalf("seed task %s: %v", id, err)
		}
	}
	// The leak: archived + non-terminal → must become discarded.
	task("arch-open", "open", archAt, "user:a", "")
	task("arch-running", "running", archAt, "user:a", "")
	task("arch-reopened", "reopened", archAt, "user:a", "")
	task("arch-open-blocked", "open", archAt, "user:a", "stuck on infra")
	// Terminal archived → outcome preserved.
	task("arch-completed", "completed", archAt, "user:a", "")
	task("arch-discarded", "discarded", archAt, "user:a", "")
	// Live (not archived) → never touched.
	task("live-open", "open", "", "", "")
	task("live-running", "running", "", "", "")
}

func assertArchivedBackfill(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	read := func(id string) (status, statusChangedAt, blockedReason string) {
		t.Helper()
		var s, sc string
		var br sql.NullString
		if err := db.QueryRowContext(ctx,
			`SELECT status, status_changed_at, blocked_reason FROM pm_tasks WHERE id=?`, id).
			Scan(&s, &sc, &br); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return s, sc, br.String
	}
	wantStatus := func(id, want string) {
		t.Helper()
		if s, _, _ := read(id); s != want {
			t.Errorf("%s status: got %q want %q", id, s, want)
		}
	}

	// Archived non-terminal → discarded.
	wantStatus("arch-open", "discarded")
	wantStatus("arch-running", "discarded")
	wantStatus("arch-reopened", "discarded")
	wantStatus("arch-open-blocked", "discarded")
	// status_changed_at stamped to archived_at; blocked_reason cleared.
	if _, sc, br := read("arch-open-blocked"); sc != "2026-06-21T08:00:00Z" || br != "" {
		t.Errorf("arch-open-blocked: status_changed_at=%q (want archived_at) blocked_reason=%q (want empty)", sc, br)
	}
	// Terminal archived → preserved.
	wantStatus("arch-completed", "completed")
	wantStatus("arch-discarded", "discarded")
	// Live → untouched.
	wantStatus("live-open", "open")
	wantStatus("live-running", "running")
}
