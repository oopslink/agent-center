package persistence

import (
	"context"
	"database/sql"
	"testing"
)

func TestMigration_0154_ClearTerminalTaskLeases(t *testing.T) {
	db, err := Open(t.TempDir() + "/m154.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mig.Down(ctx, 153); err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO pm_tasks
			(id, project_id, title, status, assignee, created_by, created_at, updated_at, version, execution_lease_expires_at, failed_reason)
		VALUES
			('task-completed', 'project-1', 'completed', 'completed', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, '2026-09-02T06:00:00Z', ''),
			('task-failed', 'project-1', 'failed', 'failed', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, '2026-09-02T06:00:00Z', 'failed'),
			('task-discarded', 'project-1', 'discarded', 'discarded', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, '2026-09-02T06:00:00Z', ''),
			('task-open', 'project-1', 'open', 'open', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, '2026-09-02T06:00:00Z', ''),
			('task-running', 'project-1', 'running', 'running', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, '2026-09-02T06:00:00Z', '')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"task-completed", "task-failed", "task-discarded"} {
		var lease sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT execution_lease_expires_at FROM pm_tasks WHERE id=?`, id).Scan(&lease); err != nil {
			t.Fatal(err)
		}
		if lease.Valid {
			t.Fatalf("%s kept terminal lease %q", id, lease.String)
		}
	}
	for _, id := range []string{"task-open", "task-running"} {
		var lease sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT execution_lease_expires_at FROM pm_tasks WHERE id=?`, id).Scan(&lease); err != nil {
			t.Fatal(err)
		}
		if !lease.Valid || lease.String != "2026-09-02T06:00:00Z" {
			t.Fatalf("%s lease = %v, want preserved", id, lease)
		}
	}
}
