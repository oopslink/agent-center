package persistence

import (
	"context"
	"database/sql"
	"testing"
)

func TestMigration_0156_DiscardSupersededOpenTasks(t *testing.T) {
	db, err := Open(t.TempDir() + "/m156.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mig.Down(ctx, 155); err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO pm_plans
			(id, project_id, name, status, creator_ref, created_at, updated_at, version, active_generation_id)
		VALUES
			('plan-active', 'project-1', 'active', 'running', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 2, 'generation-active'),
			('plan-other', 'project-1', 'other', 'running', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, 'generation-other');
		INSERT INTO pm_plan_generations
			(id, plan_id, parent_generation_id, reason, evidence, creator_ref, diff_json, snapshot_json, idempotency_key, request_fingerprint, created_at)
		VALUES
			('generation-parent', 'plan-active', '', 'parent', 'evidence', 'user:a', '{"node_decisions":[{"task_id":"task-old-parent","action":"supersede"}]}', '{}', 'parent-key', 'fp-parent', '2026-09-02T00:10:00Z'),
			('generation-active', 'plan-active', 'generation-parent', 'active', 'evidence', 'user:a', '{"node_decisions":[{"task_id":"task-old-active","action":"supersede"}]}', '{}', 'active-key', 'fp-active', '2026-09-02T00:20:00Z'),
			('generation-other', 'plan-other', '', 'other', 'evidence', 'user:a', '{"node_decisions":[{"task_id":"task-other-old","action":"supersede"}]}', '{}', 'other-key', 'fp-other', '2026-09-02T00:30:00Z');
		INSERT INTO pm_tasks
			(id, project_id, title, status, assignee, created_by, created_at, updated_at, version, plan_id, execution_lease_expires_at, failed_reason)
		VALUES
			('task-old-parent', 'project-1', 'old parent', 'open', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, 'plan-active', '2026-09-02T06:00:00Z', ''),
			('task-old-active', 'project-1', 'old active', 'open', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, 'plan-active', '2026-09-02T06:00:00Z', ''),
			('task-replacement', 'project-1', 'replacement', 'open', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, 'plan-active', '2026-09-02T06:00:00Z', ''),
			('task-running-old', 'project-1', 'running old', 'running', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, 'plan-active', '2026-09-02T06:00:00Z', ''),
			('task-other-old', 'project-1', 'other old', 'open', 'agent:a', 'user:a', '2026-09-02T00:00:00Z', '2026-09-02T01:00:00Z', 1, 'plan-other', '2026-09-02T06:00:00Z', '')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"task-old-parent", "task-old-active", "task-other-old"} {
		var status string
		var lease sql.NullString
		var version int
		if err := db.QueryRowContext(ctx, `SELECT status, execution_lease_expires_at, version FROM pm_tasks WHERE id=?`, id).Scan(&status, &lease, &version); err != nil {
			t.Fatal(err)
		}
		if status != "discarded" || lease.Valid || version != 2 {
			t.Fatalf("%s status/lease/version=%s/%v/%d, want discarded/null/2", id, status, lease, version)
		}
	}
	for _, id := range []string{"task-replacement", "task-running-old"} {
		var status string
		var lease sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT status, execution_lease_expires_at FROM pm_tasks WHERE id=?`, id).Scan(&status, &lease); err != nil {
			t.Fatal(err)
		}
		if (id == "task-replacement" && status != "open") || (id == "task-running-old" && status != "running") || !lease.Valid {
			t.Fatalf("%s status/lease=%s/%v, want unchanged with lease", id, status, lease)
		}
	}
}
