package persistence

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration0106_PmStages verifies the Plan Stage additions: the new pm_stages
// table + the additive pm_tasks.stage_id column with its ” default (§8 back-compat).
// Migrate to head, Down to 105 (dropping both), seed a pre-0106 task, then Up.
func TestMigration0106_PmStages(t *testing.T) {
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
	// pm_stages + stage_id present at head.
	if !tableExists0106(t, db, "pm_stages") {
		t.Fatal("pm_stages should exist at head")
	}
	if !colExists0106(t, db, "pm_tasks", "stage_id") {
		t.Fatal("pm_tasks.stage_id should exist at head")
	}

	if err := mig.Down(ctx, 105); err != nil {
		t.Fatalf("Down(105): %v", err)
	}
	if tableExists0106(t, db, "pm_stages") {
		t.Fatal("pm_stages should be gone at v105")
	}
	if colExists0106(t, db, "pm_tasks", "stage_id") {
		t.Fatal("pm_tasks.stage_id should be gone at v105")
	}

	// Seed a pre-0106 task (no stage_id column).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at, version, status_changed_at)
		 VALUES ('t1','p1','x','open','user:a','2026-07-01T00:00:00Z','2026-07-01T00:00:00Z',1,'2026-07-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up (apply 0106): %v", err)
	}
	// Pre-existing row backfills stage_id to '' (the NOT NULL DEFAULT) → zero-regression.
	var stage string
	if err := db.QueryRowContext(ctx, `SELECT stage_id FROM pm_tasks WHERE id='t1'`).Scan(&stage); err != nil {
		t.Fatal(err)
	}
	if stage != "" {
		t.Fatalf("backfilled stage_id = %q, want '' (§8)", stage)
	}
	// pm_stages accepts a row.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pm_stages (id, plan_id, name, created_at, updated_at) VALUES ('s1','p1','stage','2026-07-01T00:00:00Z','2026-07-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert stage: %v", err)
	}
	var deps, gate string
	var maxRounds, version int
	if err := db.QueryRowContext(ctx,
		`SELECT depends_on_stages, gate_node_id, max_rounds, version FROM pm_stages WHERE id='s1'`).Scan(&deps, &gate, &maxRounds, &version); err != nil {
		t.Fatal(err)
	}
	if deps != "[]" || gate != "" || maxRounds != 0 || version != 1 {
		t.Fatalf("stage defaults = (%q,%q,%d,%d), want ([],'',0,1)", deps, gate, maxRounds, version)
	}

	// Down(105) cleanly drops both again.
	if err := mig.Down(ctx, 105); err != nil {
		t.Fatalf("Down(105) after apply: %v", err)
	}
	if tableExists0106(t, db, "pm_stages") || colExists0106(t, db, "pm_tasks", "stage_id") {
		t.Fatal("pm_stages / stage_id should be gone after Down(105)")
	}
}

func colExists0106(t *testing.T, db *sql.DB, table, col string) bool {
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

func tableExists0106(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return name == table
}
