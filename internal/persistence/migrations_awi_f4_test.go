package persistence

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration0071_AwiDataBackfill proves the I14/F4 data backfill
// (0071_v214_awi_data_backfill) folds legacy agent_work_items state into the
// pm_tasks columns 0070 added, demotes each agent's surplus running tasks so F3's
// 0072 single-active UNIQUE index builds clean, and rebuilds a best-effort
// pm_task_action_logs history — without fabricating beyond what the work-item rows
// imply.
//
// Strategy mirrors the ADR-0046 test: Down to 70 reverts 0071 (its down deletes the
// rebuilt action logs; the pm_tasks columns from 0070 stay) AND, post-F7, runs
// 0073's down which recreates an EMPTY agent_work_items — so we seed legacy rows
// against the real post-0070 schema and re-Up to run the backfill. A second
// Down(70)->reseed->Up proves idempotency (no duplicate logs, a stable end state
// when the migration re-runs over already-migrated data). The reseed is required
// because F7's 0073 DROPs agent_work_items at the top of the chain, so the legacy
// source rows do not survive a full Up→Down cycle (0073.down recreates it empty);
// 0071.down's "re-Up faithfully reproduces it" contract now means "re-Up over the
// re-seeded source".
func TestMigration0071_AwiDataBackfill(t *testing.T) {
	db, err := Open(t.TempDir() + "/awi_f4.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	// Down to 70 reverts 0071 (logs) + 0072 (index) + 0073 (recreates empty awi) +
	// 0074 (activity column rename); pm_tasks keeps 0070's columns so we can seed.
	if err := mig.Down(ctx, 70); err != nil {
		t.Fatalf("Down(70): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 70 {
		t.Fatalf("version after Down(70): got %d want 70", v)
	}

	seedTasks(t, db)
	seedWorkItems(t, db)

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("second Up (apply 0071): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 88 {
		t.Fatalf("version after re-Up: got %d want 88", v)
	}

	assertBackfill(t, db)

	// Idempotency: revert 0071's logs (and 0073 recreates an empty agent_work_items,
	// so re-seed the legacy source rows it dropped), re-run 0071 over the
	// already-backfilled pm_tasks rows, and assert the same end state with no
	// duplicate action logs.
	if err := mig.Down(ctx, 70); err != nil {
		t.Fatalf("Down(70) #2: %v", err)
	}
	seedWorkItems(t, db) // 0073.down recreated agent_work_items empty — re-seed the source
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("third Up (re-apply 0071, idempotency): %v", err)
	}
	assertBackfill(t, db)
}

// seedTasks inserts the pm_tasks rows (the durable side — they survive a Down(70)
// cycle, since 0071.down clears only the backfilled columns/logs, not the rows).
func seedTasks(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	task := func(id, status, assignee, blockedReason, updatedAt, lease string) {
		t.Helper()
		var br any
		if blockedReason == "" {
			br = nil // mirror the repo: an empty blocked_reason is stored NULL.
		} else {
			br = blockedReason
		}
		var ls any
		if lease == "" {
			ls = nil
		} else {
			ls = lease
		}
		_, err := db.ExecContext(ctx, `
			INSERT INTO pm_tasks (id, project_id, title, status, assignee, blocked_reason,
			                      execution_lease_expires_at, created_by, created_at, updated_at)
			VALUES (?, 'P1', 'do', ?, ?, ?, ?, 'user:a', '2026-06-21T00:00:00Z', ?)`,
			id, status, assignee, br, ls, updatedAt)
		if err != nil {
			t.Fatalf("seed task %s: %v", id, err)
		}
	}
	// Block-annotation mappings.
	task("T-wi", "running", "ag-A", "", "2026-06-21T01:00:00Z", "2026-06-21T02:00:00Z")
	task("T-blk", "running", "ag-B", "disk full", "2026-06-21T01:00:00Z", "2026-06-21T02:00:00Z")
	task("T-fail", "running", "ag-C", "", "2026-06-21T01:00:00Z", "")
	// §13.E in-flight paused -> open.
	task("T-paused", "running", "ag-D", "", "2026-06-21T01:00:00Z", "2026-06-21T02:00:00Z")
	// Single-active: ag-E holds three running, non-blocked pm_tasks (the legacy pool
	// cap=3 allowed this) and we keep the newest. These came from pool claims, not a
	// single-active AWI, so they have no agent_work_items rows — the surplus-running
	// demotion (step 3) reads pm_tasks alone. (The AWI table's own idx_awi_agent_active
	// UNIQUE index forbids >1 active|waiting_input work item per agent anyway.)
	task("T-e1", "running", "ag-E", "", "2026-06-21T00:00:01Z", "2026-06-21T00:00:01Z")
	task("T-e2", "running", "ag-E", "", "2026-06-21T00:00:02Z", "2026-06-21T00:00:02Z")
	task("T-e3", "running", "ag-E", "", "2026-06-21T00:00:03Z", "2026-06-21T00:00:03Z")
	// ag-F: a blocked running task is EXEMPT from the active slot, so its one
	// non-blocked running task must survive (blocked does not count toward the cap).
	task("T-f-blk", "running", "ag-F", "", "2026-06-21T03:00:00Z", "2026-06-21T03:00:00Z")
	task("T-f-act", "running", "ag-F", "", "2026-06-21T01:00:00Z", "2026-06-21T01:00:00Z")
	// A completed task with an AWI must NOT be resurrected as blocked.
	task("T-done", "completed", "ag-G", "", "2026-06-21T01:00:00Z", "")
}

// seedWorkItems inserts the legacy agent_work_items rows — the backfill SOURCE
// that 0071 folds into pm_tasks. F7's 0073 DROPs this table (and 0073.down
// recreates it EMPTY), so these source rows do NOT survive a full Up→Down cycle;
// the test re-seeds them before each backfill re-Up. The five ag-E pool-claim
// tasks have no work items (the surplus-running demotion reads pm_tasks alone).
func seedWorkItems(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	awi := func(id, agentID, taskID, status string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO agent_work_items (id, agent_id, task_ref, status, interactions, created_at, updated_at, version)
			VALUES (?, ?, ?, ?, 2, '2026-06-21T00:00:00Z', '2026-06-21T00:00:00Z', 1)`,
			id, agentID, "pm://tasks/"+taskID, status)
		if err != nil {
			t.Fatalf("seed awi %s: %v", id, err)
		}
	}
	awi("wi-A", "ag-A", "T-wi", "waiting_input")
	awi("wi-B", "ag-B", "T-blk", "blocked")
	awi("wi-B-old", "ag-Z", "T-blk", "superseded") // reassignment history
	awi("wi-C", "ag-C", "T-fail", "failed")
	awi("wi-D", "ag-D", "T-paused", "paused")
	awi("wi-F-blk", "ag-F", "T-f-blk", "blocked")
	awi("wi-F-act", "ag-F", "T-f-act", "active")
	awi("wi-G", "ag-G", "T-done", "done")
}

func assertBackfill(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	read := func(id string) (status, brt, br, lease string) {
		t.Helper()
		var s, rt string
		var rr, ls sql.NullString
		err := db.QueryRowContext(ctx, `
			SELECT status, blocked_reason_type, blocked_reason, execution_lease_expires_at
			FROM pm_tasks WHERE id = ?`, id).Scan(&s, &rt, &rr, &ls)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return s, rt, rr.String, ls.String
	}
	expect := func(id, wantStatus, wantBrt, wantBrNonEmpty, wantLeaseEmpty string) {
		t.Helper()
		s, rt, br, ls := read(id)
		if s != wantStatus {
			t.Errorf("%s status: got %q want %q", id, s, wantStatus)
		}
		if rt != wantBrt {
			t.Errorf("%s blocked_reason_type: got %q want %q", id, rt, wantBrt)
		}
		if wantBrNonEmpty == "nonempty" && br == "" {
			t.Errorf("%s blocked_reason: want non-empty, got empty", id)
		}
		if wantBrNonEmpty == "empty" && br != "" {
			t.Errorf("%s blocked_reason: want empty, got %q", id, br)
		}
		if wantLeaseEmpty == "cleared" && ls != "" {
			t.Errorf("%s lease: want cleared, got %q", id, ls)
		}
		if wantLeaseEmpty == "kept" && ls == "" {
			t.Errorf("%s lease: want kept, got empty", id)
		}
	}

	// Block mappings — annotation set, status stays running, lease cleared (§2.3).
	expect("T-wi", "running", "input_required", "nonempty", "cleared")
	expect("T-blk", "running", "obstacle", "nonempty", "cleared")
	if _, _, br, _ := readReason(t, db, "T-blk"); br != "disk full" {
		t.Errorf("T-blk should keep its existing reason, got %q", br)
	}
	expect("T-fail", "running", "obstacle", "nonempty", "cleared")

	// §13.E paused -> open, lease + block cleared.
	expect("T-paused", "open", "", "empty", "cleared")

	// Single-active: ag-E keeps newest (T-e3) running, demotes the other two.
	expect("T-e3", "running", "", "empty", "kept")
	expect("T-e1", "open", "", "empty", "cleared")
	expect("T-e2", "open", "", "empty", "cleared")
	if n := countRunning(t, db, "ag-E"); n != 1 {
		t.Errorf("ag-E running count: got %d want 1", n)
	}

	// ag-F: blocked task exempt (lease cleared) + its single non-blocked running
	// survives with its lease intact.
	expect("T-f-blk", "running", "obstacle", "nonempty", "cleared")
	expect("T-f-act", "running", "", "empty", "kept")
	// At most one running, non-blocked task per agent — the 0072 invariant.
	for _, ag := range []string{"ag-A", "ag-B", "ag-C", "ag-D", "ag-E", "ag-F", "ag-G"} {
		if n := countActiveSlot(t, db, ag); n > 1 {
			t.Errorf("%s occupies %d active slots, want <=1 (0072 would fail)", ag, n)
		}
	}

	// Completed task not resurrected.
	expect("T-done", "completed", "", "empty", "cleared")

	// Best-effort action logs: one per work item whose task exists; superseded ->
	// reassigned. The ag-E pool-claim tasks have no work items, so they produce no
	// logs; the 8 seeded work items (wi-A, wi-B, wi-B-old, wi-C, wi-D, wi-F-blk,
	// wi-F-act, wi-G) each map to an existing task and yield one log.
	if n := countLogs(t, db); n != 8 {
		t.Errorf("pm_task_action_logs rows: got %d want 8 (one per seeded work item)", n)
	}
	if a := logAction(t, db, "awimig-wi-B-old"); a != "reassigned" {
		t.Errorf("superseded work item log action: got %q want reassigned", a)
	}
	if a := logAction(t, db, "awimig-wi-A"); a != "assigned" {
		t.Errorf("live work item log action: got %q want assigned", a)
	}
	if ref := logAgentRef(t, db, "awimig-wi-A"); ref != "agent:ag-A" {
		t.Errorf("log agent_ref: got %q want agent:ag-A", ref)
	}
}

func readReason(t *testing.T, db *sql.DB, id string) (string, string, string, string) {
	t.Helper()
	var s, rt string
	var rr sql.NullString
	if err := db.QueryRowContext(context.Background(),
		`SELECT status, blocked_reason_type, blocked_reason FROM pm_tasks WHERE id=?`, id).
		Scan(&s, &rt, &rr); err != nil {
		t.Fatalf("readReason %s: %v", id, err)
	}
	return s, rt, rr.String, ""
}

func countRunning(t *testing.T, db *sql.DB, agent string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM pm_tasks WHERE assignee=? AND status='running'`, agent).Scan(&n); err != nil {
		t.Fatalf("countRunning %s: %v", agent, err)
	}
	return n
}

// countActiveSlot counts an agent's running, non-blocked tasks — exactly the set
// F3's 0072 UNIQUE partial index (WHERE status='running' AND blocked_reason=”)
// constrains to at most one.
func countActiveSlot(t *testing.T, db *sql.DB, agent string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM pm_tasks
		 WHERE assignee=? AND status='running'
		   AND (blocked_reason IS NULL OR blocked_reason='')`, agent).Scan(&n); err != nil {
		t.Fatalf("countActiveSlot %s: %v", agent, err)
	}
	return n
}

func countLogs(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM pm_task_action_logs WHERE id LIKE 'awimig-%'`).Scan(&n); err != nil {
		t.Fatalf("countLogs: %v", err)
	}
	return n
}

func logAction(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var a string
	if err := db.QueryRowContext(context.Background(),
		`SELECT action FROM pm_task_action_logs WHERE id=?`, id).Scan(&a); err != nil {
		t.Fatalf("logAction %s: %v", id, err)
	}
	return a
}

func logAgentRef(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var a string
	if err := db.QueryRowContext(context.Background(),
		`SELECT agent_ref FROM pm_task_action_logs WHERE id=?`, id).Scan(&a); err != nil {
		t.Fatalf("logAgentRef %s: %v", id, err)
	}
	return a
}
