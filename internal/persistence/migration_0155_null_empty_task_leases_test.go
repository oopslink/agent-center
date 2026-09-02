package persistence

import (
	"context"
	"database/sql"
	"testing"
)

func TestMigration_0155_NullEmptyTaskLeases(t *testing.T) {
	db, err := Open(t.TempDir() + "/m155.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mig.Down(ctx, 154); err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO pm_tasks
			(id, project_id, title, status, assignee, created_by, created_at, updated_at, version, execution_lease_expires_at, failed_reason)
		VALUES
			('task-empty', 'project-1', 'empty', 'completed', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, '', ''),
			('task-real', 'project-1', 'real', 'running', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, '2026-09-02T06:00:00Z', '')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}

	var empty sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT execution_lease_expires_at FROM pm_tasks WHERE id='task-empty'`).Scan(&empty); err != nil {
		t.Fatal(err)
	}
	if empty.Valid {
		t.Fatalf("empty lease was not normalized: %q", empty.String)
	}
	var real sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT execution_lease_expires_at FROM pm_tasks WHERE id='task-real'`).Scan(&real); err != nil {
		t.Fatal(err)
	}
	if !real.Valid || real.String != "2026-09-02T06:00:00Z" {
		t.Fatalf("real lease = %v, want preserved", real)
	}
}
