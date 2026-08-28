package persistence

import (
	"context"
	"testing"
)

func TestMigration_0147_RemovesOnlyPlannedPoolDuplicates(t *testing.T) {
	db, err := Open(t.TempDir() + "/m147.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mig.Down(ctx, 146); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO pm_tasks
			(id, project_id, title, status, created_by, created_at, updated_at, plan_id)
		VALUES
			('task-planned', 'project-1', 'planned', 'open', 'user:a', '2026-08-27T00:00:00Z', '2026-08-27T00:00:00Z', 'plan-1'),
			('task-pool', 'project-1', 'pool', 'open', 'user:a', '2026-08-27T00:00:00Z', '2026-08-27T00:00:00Z', '');
		INSERT INTO pm_assignment_pools
			(id, project_id, scheduling_class, auto_assign_enabled, holding_cap, created_at, updated_at, version)
		VALUES ('pool-1', 'project-1', 'background', 1, 3, '2026-08-27T00:00:00Z', '2026-08-27T00:00:00Z', 1);
		INSERT INTO pm_assignment_pool_tasks
			(pool_id, task_id, priority, added_by, added_at, claimed_by, claimed_at, claim_expires_at, version)
		VALUES
			('pool-1', 'task-planned', 0, 'user:a', '2026-08-27T00:00:00Z', '', '', '', 1),
			('pool-1', 'task-pool', 0, 'user:a', '2026-08-27T00:00:00Z', '', '', '', 1);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var planned, pool int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pm_assignment_pool_tasks WHERE task_id='task-planned'`).Scan(&planned); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pm_assignment_pool_tasks WHERE task_id='task-pool'`).Scan(&pool); err != nil {
		t.Fatal(err)
	}
	if planned != 0 || pool != 1 {
		t.Fatalf("membership counts planned=%d pool=%d, want 0/1", planned, pool)
	}
}
