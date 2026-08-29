package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

func TestMigrateOrphanConditions_CLIIsolatedCopyDryRunApplyReapply(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "isolated-copy.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		db.Close()
		t.Fatal(err)
	}
	seedOrphanConditionRows(t, ctx, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	dryRun := runOrphanConditionCLI(t, "--db="+dbPath, "--dry-run")
	if dryRun.Mode != "dry-run" || dryRun.Summary.Scanned != 5 || dryRun.Summary.Resolved != 1 || dryRun.Summary.Superseded != 2 || dryRun.Summary.Incidents != 1 {
		t.Fatalf("dry-run summary=%+v", dryRun)
	}
	t.Logf("isolated copy dry-run: %+v", dryRun.Summary)

	first := runOrphanConditionCLI(t, "--db="+dbPath, "--apply", "--deadline-hours=2")
	if first.Mode != "apply" || first.Summary.Resolved != 1 || first.Summary.Superseded != 2 || first.Summary.Incidents != 1 {
		t.Fatalf("first apply summary=%+v", first)
	}
	t.Logf("isolated copy first apply: %+v", first.Summary)

	second := runOrphanConditionCLI(t, "--db="+dbPath, "--apply", "--deadline-hours=2")
	if second.Summary.AlreadyApplied != 1 || second.Summary.ShadowCurrent != 1 || second.Summary.Resolved != 0 || second.Summary.Superseded != 0 || second.Summary.Incidents != 0 {
		t.Fatalf("second apply summary=%+v", second)
	}
	t.Logf("isolated copy second apply: %+v", second.Summary)

	db, err = persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_orphan_condition_migration_markers`); n != 4 {
		t.Fatalf("markers after reapply=%d want 4", n)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_progress_incidents WHERE kind='migration_gap'`); n != 1 {
		t.Fatalf("incidents after reapply=%d want 1", n)
	}
	if n := scalarInt(t, ctx, db, `SELECT COUNT(*) FROM pm_audit_log WHERE change_type='migration' AND field='orphan_condition'`); n != 4 {
		t.Fatalf("audits after reapply=%d want 4", n)
	}
}

func runOrphanConditionCLI(t *testing.T, args ...string) orphanConditionResult {
	t.Helper()
	router, _, err := BuildRouter("v-test", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	router.Out = &out
	router.Err = &errw
	code := router.Run(context.Background(), append([]string{"migrate", "orphan-conditions"}, args...))
	if code != ExitOK {
		t.Fatalf("CLI args=%v code=%d stderr=%s", args, code, errw.String())
	}
	var result orphanConditionResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode CLI output %q: %v", out.String(), err)
	}
	return result
}

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
