package persistence

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration0088_TaskCompletedAt verifies the additive completed_at column and
// its one-shot backfill: a row in 'completed' inherits completed_at =
// status_changed_at; a non-completed row stays NULL. Migrate to head, Down to 87
// (dropping the column), seed pre-0088 rows, then Up.
func TestMigration0088_TaskCompletedAt(t *testing.T) {
	db, err := Open(MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mig := NewMigrator(db)
	ctx := context.Background()
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := mig.Down(ctx, 87); err != nil {
		t.Fatalf("Down(87): %v", err)
	}
	if colExists0088(t, db, "pm_tasks", "completed_at") {
		t.Fatal("completed_at should be absent at v87")
	}

	// Seed a completed task (status_changed_at = the completion moment) and an open
	// task — both without the new column.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at, version, status_changed_at)
		 VALUES ('done1','p1','x','completed','user:a','2026-06-29T00:00:00Z','2026-06-29T00:00:00Z',1,'2026-06-29T05:00:00Z')`); err != nil {
		t.Fatalf("seed completed task: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at, version, status_changed_at)
		 VALUES ('open1','p1','y','open','user:a','2026-06-29T00:00:00Z','2026-06-29T00:00:00Z',1,'2026-06-29T01:00:00Z')`); err != nil {
		t.Fatalf("seed open task: %v", err)
	}

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up (apply 0088): %v", err)
	}

	// completed row: backfilled from status_changed_at.
	var done sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT completed_at FROM pm_tasks WHERE id='done1'`).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if !done.Valid || done.String != "2026-06-29T05:00:00Z" {
		t.Fatalf("completed task completed_at = %v, want 2026-06-29T05:00:00Z", done)
	}
	// open row: stays NULL.
	var open sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT completed_at FROM pm_tasks WHERE id='open1'`).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if open.Valid {
		t.Fatalf("open task completed_at = %q, want NULL", open.String)
	}

	// Down(87) cleanly drops the column again.
	if err := mig.Down(ctx, 87); err != nil {
		t.Fatalf("Down(87) after apply: %v", err)
	}
	if colExists0088(t, db, "pm_tasks", "completed_at") {
		t.Fatal("completed_at should be gone after Down(87)")
	}
}

func colExists0088(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == col {
			return true
		}
	}
	return false
}
