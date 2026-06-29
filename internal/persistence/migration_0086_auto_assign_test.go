package persistence

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration0086_AutoAssignSchema verifies the v2.18.3 BE-1 additive columns and
// their defaults on EXISTING rows: pm_tasks.required_capabilities defaults to '[]'
// (unrestricted) and agents.auto_assignable defaults to 1 (assignable). We migrate
// to head, Down to 85 (dropping both columns), seed pre-0086 rows, then Up.
func TestMigration0086_AutoAssignSchema(t *testing.T) {
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
	if err := mig.Down(ctx, 85); err != nil {
		t.Fatalf("Down(85): %v", err)
	}
	if colExists0086(t, db, "pm_tasks", "required_capabilities") || colExists0086(t, db, "agents", "auto_assignable") {
		t.Fatal("columns should be absent at v85")
	}

	// Seed a pre-0086 task + agent (without the new columns).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at, version)
		 VALUES ('tk1','p1','x','open','user:a','2026-06-29T00:00:00Z','2026-06-29T00:00:00Z',1)`); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO agents (id, organization_id, name, worker_id, lifecycle, created_by, created_at, updated_at)
		 VALUES ('ag1','org1','coder','W1','stopped','user:a','2026-06-29T00:00:00Z','2026-06-29T00:00:00Z')`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up (apply 0086): %v", err)
	}

	// Existing rows get the column defaults.
	var caps string
	if err := db.QueryRowContext(ctx, `SELECT required_capabilities FROM pm_tasks WHERE id='tk1'`).Scan(&caps); err != nil {
		t.Fatal(err)
	}
	if caps != "[]" {
		t.Fatalf("task required_capabilities default = %q, want '[]'", caps)
	}
	var auto int
	if err := db.QueryRowContext(ctx, `SELECT auto_assignable FROM agents WHERE id='ag1'`).Scan(&auto); err != nil {
		t.Fatal(err)
	}
	if auto != 1 {
		t.Fatalf("agent auto_assignable default = %d, want 1", auto)
	}

	// Down(85) cleanly drops both again.
	if err := mig.Down(ctx, 85); err != nil {
		t.Fatalf("Down(85) after apply: %v", err)
	}
	if colExists0086(t, db, "pm_tasks", "required_capabilities") || colExists0086(t, db, "agents", "auto_assignable") {
		t.Fatal("columns should be gone after Down(85)")
	}
}

func colExists0086(t *testing.T, db *sql.DB, table, col string) bool {
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
