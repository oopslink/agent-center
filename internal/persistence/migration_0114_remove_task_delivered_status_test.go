package persistence

import (
	"context"
	"database/sql"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestMigration0114_DeliveredTasksBecomeCompletedAndUnblockReview is the
// regression for the Dev→Review DAG stall: a historical Dev row parked at
// status='delivered' was not considered done, so Review stayed blocked forever.
func TestMigration0114_DeliveredTasksBecomeCompletedAndUnblockReview(t *testing.T) {
	db, err := Open(MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := mig.Down(ctx, 113); err != nil {
		t.Fatalf("Down(113): %v", err)
	}

	seed0114Plan(t, db)
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up (apply 0114): %v", err)
	}

	var status, completedAt string
	if err := db.QueryRowContext(ctx,
		`SELECT status, completed_at FROM pm_tasks WHERE id='dev'`).Scan(&status, &completedAt); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("dev status = %q, want completed", status)
	}
	if completedAt != "2026-07-22T01:00:00Z" {
		t.Fatalf("dev completed_at = %q, want status_changed_at", completedAt)
	}

	tasks := []*pm.Task{
		rehydrate0114Task(t, "dev", pm.TaskCompleted),
		rehydrate0114Task(t, "review", pm.TaskOpen),
	}
	view := pm.DerivePlanView(tasks, []pm.Dependency{{
		PlanID: "plan-1", FromTaskID: "review", ToTaskID: "dev", Kind: pm.EdgeSeq,
	}}, nil, nil, nil)
	if len(view.ReadySet) != 1 || view.ReadySet[0] != "review" {
		t.Fatalf("ready set = %v, want [review]", view.ReadySet)
	}
}

func rehydrate0114Task(t *testing.T, id string, status pm.TaskStatus) *pm.Task {
	t.Helper()
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	task, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(id), ProjectID: "project-1", Title: id, Status: status,
		Assignee: "agent:dev", CreatedBy: "user:owner", CreatedAt: now, UpdatedAt: now,
		StatusChangedAt: now, Version: 1, PlanID: "plan-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func seed0114Plan(t *testing.T, db *sql.DB) {
	t.Helper()
	const now = "2026-07-22T00:00:00Z"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO pm_plans (id, project_id, name, status, creator_ref, conversation_id, created_at, updated_at, version)
		 VALUES ('plan-1','project-1','plan','running','user:owner','conv-1',?, ?, 1)`, now, now); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	for _, row := range []struct {
		id              string
		status          string
		statusChangedAt string
	}{
		{"dev", "delivered", "2026-07-22T01:00:00Z"},
		{"review", "open", "2026-07-22T00:00:00Z"},
	} {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO pm_tasks (id, project_id, title, status, assignee, created_by, created_at, updated_at, version, status_changed_at, plan_id)
			 VALUES (?, 'project-1', ?, ?, 'agent:dev', 'user:owner', ?, ?, 1, ?, 'plan-1')`,
			row.id, row.id, row.status, now, now, row.statusChangedAt); err != nil {
			t.Fatalf("seed task %s: %v", row.id, err)
		}
	}
}
