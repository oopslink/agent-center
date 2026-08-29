package cli

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

func TestMigrateOrphanConditions_DryRunClassifiesWithoutMutation(t *testing.T) {
	ctx, db := orphanConditionDB(t)
	seedOrphanConditionRows(t, ctx, db)

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	res, err := runOrphanConditionMigration(ctx, db, orphanConditionOptions{Now: now})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if res.Mode != "dry-run" {
		t.Fatalf("mode=%q want dry-run", res.Mode)
	}
	if res.Summary.Scanned != 5 || res.Summary.ShadowCurrent != 1 || res.Summary.Resolved != 1 || res.Summary.Superseded != 2 || res.Summary.Incidents != 1 {
		t.Fatalf("summary=%+v", res.Summary)
	}
	if tableExistsCLI(ctx, db, "pm_orphan_condition_migration_markers") {
		t.Fatal("dry-run created marker table")
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_plan_blocked_on`); n != 5 {
		t.Fatalf("dry-run mutated blocked_on rows: got %d", n)
	}
}

func TestMigrateOrphanConditions_ApplyIsIdempotentAndAudited(t *testing.T) {
	ctx, db := orphanConditionDB(t)
	seedOrphanConditionRows(t, ctx, db)

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	res, err := runOrphanConditionMigration(ctx, db, orphanConditionOptions{Apply: true, Now: now, DeadlineAfter: 2 * time.Hour})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Summary.Resolved != 1 || res.Summary.Superseded != 2 || res.Summary.Incidents != 1 {
		t.Fatalf("apply summary=%+v", res.Summary)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_plan_blocked_on`); n != 2 {
		t.Fatalf("blocked_on rows after apply=%d want 2 (shadow + incident retained)", n)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_orphan_condition_migration_markers`); n != 4 {
		t.Fatalf("markers=%d want 4", n)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_audit_log WHERE change_type='migration' AND field='orphan_condition'`); n != 4 {
		t.Fatalf("audit rows=%d want 4", n)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_progress_incidents WHERE kind='migration_gap' AND owner_ref='user:owner' AND deadline_at='2026-08-29T14:00:00Z' AND status='open'`); n != 1 {
		t.Fatalf("migration_gap incidents=%d want 1 with owner/deadline/status", n)
	}

	second, err := runOrphanConditionMigration(ctx, db, orphanConditionOptions{Apply: true, Now: now.Add(time.Hour), DeadlineAfter: 2 * time.Hour})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if second.Summary.AlreadyApplied != 1 || second.Summary.ShadowCurrent != 1 || second.Summary.Resolved != 0 || second.Summary.Superseded != 0 || second.Summary.Incidents != 0 {
		t.Fatalf("second summary=%+v", second.Summary)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_orphan_condition_migration_markers`); n != 4 {
		t.Fatalf("second apply changed markers=%d want 4", n)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_progress_incidents WHERE kind='migration_gap'`); n != 1 {
		t.Fatalf("second apply duplicated incident rows=%d", n)
	}
}

func TestMigrateOrphanConditions_All52LegacyConditions(t *testing.T) {
	ctx, db := orphanConditionDB(t)
	seed52LegacyOrphanConditionRows(t, ctx, db)

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	dry, err := runOrphanConditionMigration(ctx, db, orphanConditionOptions{Now: now})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if dry.Summary.Scanned != 52 || dry.Summary.Resolved != 13 || dry.Summary.Superseded != 26 || dry.Summary.Incidents != 13 || dry.Summary.ShadowCurrent != 0 {
		t.Fatalf("dry-run summary=%+v, want 52 legacy conditions classified as 13 resolve / 26 supersede / 13 incident", dry.Summary)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_plan_blocked_on`); n != 52 {
		t.Fatalf("dry-run mutated blocked_on rows: got %d", n)
	}

	applied, err := runOrphanConditionMigration(ctx, db, orphanConditionOptions{Apply: true, Now: now, DeadlineAfter: time.Hour})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Summary.Scanned != 52 || applied.Summary.Resolved != 13 || applied.Summary.Superseded != 26 || applied.Summary.Incidents != 13 {
		t.Fatalf("apply summary=%+v", applied.Summary)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_orphan_condition_migration_markers`); n != 52 {
		t.Fatalf("markers=%d want 52", n)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_plan_blocked_on`); n != 13 {
		t.Fatalf("blocked_on rows after apply=%d want 13 retained incidents", n)
	}

	reapplied, err := runOrphanConditionMigration(ctx, db, orphanConditionOptions{Apply: true, Now: now.Add(time.Hour), DeadlineAfter: time.Hour})
	if err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if reapplied.Summary.Scanned != 13 || reapplied.Summary.AlreadyApplied != 13 || reapplied.Summary.Resolved != 0 || reapplied.Summary.Superseded != 0 || reapplied.Summary.Incidents != 0 {
		t.Fatalf("reapply summary=%+v, want retained incident rows recognized as already applied only", reapplied.Summary)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_orphan_condition_migration_markers`); n != 52 {
		t.Fatalf("reapply changed markers=%d want 52", n)
	}
}

func orphanConditionDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background(), db
}

func seedOrphanConditionRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	ts := "2026-08-29T10:00:00Z"
	execSQL(t, ctx, db, `INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at, version) VALUES ('proj-1','org-1','P','', 'active','user:owner',?,?,1)`, ts, ts)
	execSQL(t, ctx, db, `INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, conversation_id, target_date, is_builtin, org_number, created_at, updated_at, version, graph_id, active_generation_id, archived_at, archived_by) VALUES ('plan-1','proj-1','Plan','', 'running','user:owner','','',0,1,?,?,1,'graph-1','','','')`, ts, ts)
	execSQL(t, ctx, db, `INSERT INTO pm_graphs (id, plan_id, status, start_node, end_node, created_at, updated_at, version) VALUES ('graph-1','plan-1','running','start','end',?,?,1)`, ts, ts)
	execSQL(t, ctx, db, `INSERT INTO pm_graph_nodes (id, graph_id, category, control_kind, title, status, outcome, metadata, action_logs, created_at, updated_at, version) VALUES ('node-current','graph-1','task','','Current','open','','{}','[]',?,?,1)`, ts, ts)

	insertTask := `INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at, version, plan_id, node_id, archived_at, blocked_reason) VALUES (?,?,?,?, 'user:owner',?,?,1,?,?,?, '')`
	execSQL(t, ctx, db, insertTask, "task-current", "proj-1", "current", "open", ts, ts, "plan-1", "node-current", "")
	execSQL(t, ctx, db, insertTask, "task-done", "proj-1", "done", "completed", ts, ts, "plan-1", "node-current", "")
	execSQL(t, ctx, db, insertTask, "task-other-plan", "proj-1", "other plan", "open", ts, ts, "plan-2", "node-current", "")
	execSQL(t, ctx, db, insertTask, "task-other-node", "proj-1", "other node", "open", ts, ts, "plan-1", "node-new", "")
	execSQL(t, ctx, db, insertTask, "task-missing-node", "proj-1", "missing node", "open", ts, ts, "plan-1", "node-missing", "")

	insertBlocked := `INSERT INTO pm_plan_blocked_on (plan_id, task_id, node_id, wait_type, wait_keys, trigger_condition, waited_since) VALUES ('plan-1',?,?,?,?,?,?)`
	execSQL(t, ctx, db, insertBlocked, "task-current", "node-current", "upstream_completion", `["x"]`, "wait", ts)
	execSQL(t, ctx, db, insertBlocked, "task-done", "node-current", "upstream_completion", `["x"]`, "wait", ts)
	execSQL(t, ctx, db, insertBlocked, "task-other-plan", "node-current", "upstream_completion", `["x"]`, "wait", ts)
	execSQL(t, ctx, db, insertBlocked, "task-other-node", "node-current", "upstream_completion", `["x"]`, "wait", ts)
	execSQL(t, ctx, db, insertBlocked, "task-missing-node", "node-missing", "upstream_completion", `["x"]`, "wait", ts)
}

func seed52LegacyOrphanConditionRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	ts := "2026-08-29T10:00:00Z"
	execSQL(t, ctx, db, `INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at, version) VALUES ('proj-52','org-1','P52','', 'active','user:owner',?,?,1)`, ts, ts)
	execSQL(t, ctx, db, `INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, conversation_id, target_date, is_builtin, org_number, created_at, updated_at, version, graph_id, active_generation_id, archived_at, archived_by) VALUES ('plan-52','proj-52','Plan 52','', 'running','user:owner','','',0,1,?,?,1,'graph-52','','','')`, ts, ts)
	execSQL(t, ctx, db, `INSERT INTO pm_graphs (id, plan_id, status, start_node, end_node, created_at, updated_at, version) VALUES ('graph-52','plan-52','running','start','end',?,?,1)`, ts, ts)
	execSQL(t, ctx, db, `INSERT INTO pm_graph_nodes (id, graph_id, category, control_kind, title, status, outcome, metadata, action_logs, created_at, updated_at, version) VALUES ('node-52','graph-52','task','','Current','open','','{}','[]',?,?,1)`, ts, ts)

	insertTask := `INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at, version, plan_id, node_id, archived_at, blocked_reason) VALUES (?,?,?,?, 'user:owner',?,?,1,?,?,?, '')`
	insertBlocked := `INSERT INTO pm_plan_blocked_on (plan_id, task_id, node_id, wait_type, wait_keys, trigger_condition, waited_since) VALUES ('plan-52',?,?,?,?,?,?)`
	for i := 0; i < 52; i++ {
		taskID := fmt.Sprintf("legacy-task-%02d", i)
		status := "open"
		planID := "plan-52"
		taskNodeID := "node-52"
		conditionNodeID := "node-52"
		insertExistingTask := true
		if i < 13 {
			status = "completed"
		} else if i < 26 {
			planID = "other-plan"
		} else if i < 39 {
			taskNodeID = fmt.Sprintf("legacy-replacement-node-%02d", i)
		} else {
			conditionNodeID = fmt.Sprintf("legacy-missing-node-%02d", i)
			insertExistingTask = false
		}
		if insertExistingTask {
			execSQL(t, ctx, db, insertTask, taskID, "proj-52", taskID, status, ts, ts, planID, taskNodeID, "")
		}
		execSQL(t, ctx, db, insertBlocked, taskID, conditionNodeID, "upstream_completion", `["x"]`, "wait", ts)
	}
}

func execSQL(t *testing.T, ctx context.Context, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func scalarInt(t *testing.T, ctx context.Context, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		t.Fatalf("scalar %q: %v", q, err)
	}
	return n
}
