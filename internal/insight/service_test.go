package insight

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestInsightReplay_IdempotentLateEventsBoundariesQuantilesAndRebuild(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")

	// Four executions with queue waits [10,20,30,110] ms exercise quantile_cont
	// interpolation: p50=25, p95=98.
	waits := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, 110 * time.Millisecond}
	for i, wait := range waits {
		queued := asOf.Add(-time.Hour).Add(time.Duration(i) * time.Minute)
		start := queued.Add(wait)
		stop := start.Add(time.Second)
		execID := "exec-q-" + string(rune('a'+i))
		insertQueue(t, db, "cmd-"+execID, "worker-1", "agent-1", "task-1", execID, "started", queued, start)
		insertActivity(t, db, "start-"+execID, "agent-1", "task-1", execID, map[string]any{"event": "executor.start", "executor_id": execID, "cli": "codex", "model": "gpt-5"}, start)
		insertActivity(t, db, "stop-"+execID, "agent-1", "task-1", execID, map[string]any{"event": "executor.stop", "executor_id": execID, "outcome": "succeeded"}, stop)
	}

	// Boundary contract: start inclusive, end exclusive.
	insertQueue(t, db, "cmd-boundary-in", "worker-1", "agent-1", "task-1", "exec-boundary-in", "started", asOf.Add(-25*time.Hour), asOf.Add(-24*time.Hour))
	insertActivity(t, db, "start-boundary-in", "agent-1", "task-1", "exec-boundary-in", map[string]any{"event": "executor.start", "executor_id": "exec-boundary-in"}, asOf.Add(-24*time.Hour-1*time.Second))
	insertActivity(t, db, "stop-boundary-in", "agent-1", "task-1", "exec-boundary-in", map[string]any{"event": "executor.stop", "executor_id": "exec-boundary-in", "outcome": "failed", "reason": "boom"}, asOf.Add(-24*time.Hour))
	insertActivity(t, db, "start-boundary-out", "agent-1", "task-1", "exec-boundary-out", map[string]any{"event": "executor.start", "executor_id": "exec-boundary-out"}, asOf.Add(-time.Minute))
	insertActivity(t, db, "stop-boundary-out", "agent-1", "task-1", "exec-boundary-out", map[string]any{"event": "executor.stop", "executor_id": "exec-boundary-out", "outcome": "failed"}, asOf)

	// Late start after a previously projected stop recomputes duration.
	insertQueue(t, db, "cmd-late", "worker-1", "agent-1", "task-1", "exec-late", "started", asOf.Add(-2*time.Hour), asOf.Add(-2*time.Hour+time.Second))
	insertActivity(t, db, "stop-late", "agent-1", "task-1", "exec-late", map[string]any{"event": "executor.stop", "executor_id": "exec-late", "outcome": "crashed", "reason": "process_gone", "recovered": true}, asOf.Add(-2*time.Hour+10*time.Second))
	insertQueue(t, db, "cmd-pending", "worker-1", "agent-1", "task-1", "", "pending", asOf.Add(-30*time.Minute), asOf.Add(-30*time.Minute))

	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh first: %v", err)
	}
	first, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview first: %v", err)
	}
	second, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview second: %v", err)
	}
	if first.Summary.CompletedExecutions != second.Summary.CompletedExecutions {
		t.Fatalf("duplicate refresh changed completed: first=%d second=%d", first.Summary.CompletedExecutions, second.Summary.CompletedExecutions)
	}
	if got := second.Summary.CompletedExecutions; got != 6 {
		t.Fatalf("completed executions = %d, want 6", got)
	}
	if got := second.Summary.FailedExecutions; got != 2 {
		t.Fatalf("failed executions = %d, want 2", got)
	}
	if second.Summary.QueueWaitMS.P50 == nil || *second.Summary.QueueWaitMS.P50 != 25 {
		t.Fatalf("queue p50 = %v, want 25", second.Summary.QueueWaitMS.P50)
	}
	if second.Summary.QueueWaitMS.P95 == nil || *second.Summary.QueueWaitMS.P95 != 98 {
		var got int64
		if second.Summary.QueueWaitMS.P95 != nil {
			got = *second.Summary.QueueWaitMS.P95
		}
		t.Fatalf("queue p95 = %d, want 98", got)
	}

	insertActivity(t, db, "start-late", "agent-1", "task-1", "exec-late", map[string]any{"event": "executor.start", "executor_id": "exec-late"}, asOf.Add(-2*time.Hour+1*time.Second))
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh late start: %v", err)
	}
	resp, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, AgentRef: "agent:agent-1", Limit: 100})
	if err != nil {
		t.Fatalf("executions after late start: %v", err)
	}
	var late *ExecutionRow
	var pending *ExecutionRow
	for i := range resp.Executions {
		if resp.Executions[i].ExecutionID == "exec-late" {
			late = &resp.Executions[i]
		}
		if resp.Executions[i].ExecutionID == "command:cmd-pending" {
			pending = &resp.Executions[i]
		}
	}
	if late == nil || late.DurationMS == nil || *late.DurationMS != 9000 || !late.Recovered || late.Quality != "valid" {
		t.Fatalf("late execution row = %+v, want duration=9000 recovered valid", late)
	}
	if pending == nil || pending.CommandID == nil || *pending.CommandID != "cmd-pending" || pending.StartedAt != nil || pending.Outcome != nil {
		t.Fatalf("pending command row = %+v, want visible queued command without start/outcome", pending)
	}

	rebuildPath := second
	if err := svc.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview after rebuild: %v", err)
	}
	if after.Summary.CompletedExecutions != rebuildPath.Summary.CompletedExecutions || after.Summary.FailedExecutions != rebuildPath.Summary.FailedExecutions {
		t.Fatalf("rebuild changed summary: before=%+v after=%+v", rebuildPath.Summary, after.Summary)
	}
}

func TestInsightSlotObservation_DuplicateHeartbeatAdmissionAndStaleGap(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	repo := NewObservationRepo(db, idgen.NewGenerator(clock.NewFakeClock(asOf)))
	_, _ = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 2, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateRunning, ExecutorID: "exec-a", TaskID: "task-1"},
		{SlotIndex: 1, State: concurrency.StateIdle},
		{SlotIndex: 2, State: concurrency.StateDraining},
	}}, asOf.Add(-time.Hour))
	_, _ = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 2, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateRunning, ExecutorID: "exec-a", TaskID: "task-1"},
		{SlotIndex: 1, State: concurrency.StateIdle},
		{SlotIndex: 2, State: concurrency.StateDraining},
	}}, asOf.Add(-30*time.Minute))
	_, _ = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 1, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateIdle},
		{SlotIndex: 1, State: concurrency.StateDraining},
	}}, asOf)
	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	o, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if o.Summary.SlotUtilization == nil || *o.Summary.SlotUtilization <= 0 {
		t.Fatalf("slot utilization = %v, want positive", o.Summary.SlotUtilization)
	}
	var openIntervals int
	if err := svc.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM slot_interval_fact WHERE worker_id='worker-1' AND agent_ref='agent:agent-1' AND slot_index=0`).Scan(&openIntervals); err != nil {
		t.Fatal(err)
	}
	if openIntervals != 2 {
		t.Fatalf("slot intervals for duplicate+change = %d, want 2", openIntervals)
	}
}

func TestInsightS2CProjectionReplayRecoveryAndTaskExecutionReconciliation(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	repo := NewObservationRepo(db, idgen.NewGenerator(clock.NewFakeClock(asOf)))

	insertQueue(t, db, "cmd-reconcile", "worker-1", "agent-1", "task-1", "exec-reconcile", "started", asOf.Add(-90*time.Minute), asOf.Add(-89*time.Minute))
	insertActivity(t, db, "stop-reconcile", "agent-1", "task-1", "exec-reconcile", map[string]any{
		"event": "executor.stop", "executor_id": "exec-reconcile", "outcome": "succeeded",
	}, asOf.Add(-80*time.Minute))
	insertActivity(t, db, "start-reconcile", "agent-1", "task-1", "exec-reconcile", map[string]any{
		"event": "executor.start", "executor_id": "exec-reconcile", "cli": "codex", "model": "gpt-5",
	}, asOf.Add(-85*time.Minute))
	insertActivity(t, db, "start-before-stop", "agent-1", "task-1", "exec-bad-order", map[string]any{
		"event": "executor.start", "executor_id": "exec-bad-order",
	}, asOf.Add(-30*time.Minute))
	insertActivity(t, db, "stop-before-start", "agent-1", "task-1", "exec-bad-order", map[string]any{
		"event": "executor.stop", "executor_id": "exec-bad-order", "outcome": "failed",
	}, asOf.Add(-31*time.Minute))
	insertActivity(t, db, "start-old", "agent-1", "task-1", "exec-old", map[string]any{
		"event": "executor.start", "executor_id": "exec-old",
	}, asOf.Add(-25*time.Hour))
	insertActivity(t, db, "stop-old", "agent-1", "task-1", "exec-old", map[string]any{
		"event": "executor.stop", "executor_id": "exec-old", "outcome": "failed",
	}, asOf.Add(-24*time.Hour-time.Nanosecond))
	insertActivity(t, db, "start-end-exclusive", "agent-1", "task-1", "exec-end-exclusive", map[string]any{
		"event": "executor.start", "executor_id": "exec-end-exclusive",
	}, asOf.Add(-time.Minute))
	insertActivity(t, db, "stop-end-exclusive", "agent-1", "task-1", "exec-end-exclusive", map[string]any{
		"event": "executor.stop", "executor_id": "exec-end-exclusive", "outcome": "failed",
	}, asOf)

	_, _ = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 2, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateRunning, ExecutorID: "exec-a", TaskID: "task-1"},
		{SlotIndex: 1, State: concurrency.StateIdle},
		{SlotIndex: 2, State: concurrency.StateDraining},
	}}, asOf.Add(-3*time.Hour))
	_, _ = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 1, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateIdle},
		{SlotIndex: 1, State: concurrency.StateDraining},
	}}, asOf.Add(-2*time.Hour))
	_, _ = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 0, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateDraining},
	}}, asOf.Add(-time.Hour))
	// Late, out-of-order source insert: projection must rebuild intervals by observed_at,
	// not close a newer open interval with this older timestamp.
	_, _ = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 2, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateRunning, ExecutorID: "exec-late-slot", TaskID: "task-1"},
		{SlotIndex: 1, State: concurrency.StateIdle},
	}}, asOf.Add(-150*time.Minute))

	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh replay: %v", err)
	}
	overview, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if overview.Summary.CompletedExecutions != 2 {
		t.Fatalf("completed executions = %d, want 2", overview.Summary.CompletedExecutions)
	}
	if overview.Summary.FailedExecutions != 1 {
		t.Fatalf("failed executions = %d, want 1", overview.Summary.FailedExecutions)
	}
	if overview.Summary.SlotUtilization == nil || *overview.Summary.SlotUtilization <= 0 || *overview.Summary.SlotUtilization >= 1 {
		t.Fatalf("slot utilization = %v, want non-zero fraction below 1", overview.Summary.SlotUtilization)
	}
	if overview.Summary.SlotCoverageRatio == nil || *overview.Summary.SlotCoverageRatio <= 0 || *overview.Summary.SlotCoverageRatio >= 1 {
		t.Fatalf("slot coverage = %v, want partial 24h denominator coverage", overview.Summary.SlotCoverageRatio)
	}
	if overview.Diagnostics.InvalidFacts != 1 {
		t.Fatalf("invalid facts = %d, want stop-before-start fact only", overview.Diagnostics.InvalidFacts)
	}
	assertNoNegativeSlotIntervals(t, svc)

	execs, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, AgentRef: "agent:agent-1", Limit: 20})
	if err != nil {
		t.Fatalf("executions: %v", err)
	}
	rec := findExecution(execs.Executions, "exec-reconcile")
	if rec == nil || rec.CommandID == nil || *rec.CommandID != "cmd-reconcile" || rec.TaskID == nil || *rec.TaskID != "task-1" || rec.QueueWaitMS == nil || *rec.QueueWaitMS != 300000 {
		t.Fatalf("reconciled execution = %+v, want queue/task details", rec)
	}
	if old := findExecution(execs.Executions, "exec-old"); old != nil {
		t.Fatalf("aged-out execution still present: %+v", old)
	}
	if end := findExecution(execs.Executions, "exec-end-exclusive"); end != nil {
		t.Fatalf("end-exclusive execution still present: %+v", end)
	}

	before := overview.Summary
	if err := svc.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuilt, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview rebuilt: %v", err)
	}
	if rebuilt.Summary.CompletedExecutions != before.CompletedExecutions || rebuilt.Summary.FailedExecutions != before.FailedExecutions {
		t.Fatalf("rebuild summary changed: before=%+v after=%+v", before, rebuilt.Summary)
	}
	assertNoNegativeSlotIntervals(t, svc)

	path := svc.path
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, db, path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Refresh(ctx); err != nil {
		t.Fatalf("refresh after checkpoint restart: %v", err)
	}
	var facts int
	if err := reopened.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_fact WHERE execution_id='exec-reconcile'`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if facts != 1 {
		t.Fatalf("execution fact duplicates after restart = %d, want 1", facts)
	}
	assertNoNegativeSlotIntervals(t, reopened)
}

func TestInsightS2CZeroDenominators(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-zero")
	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	o, err := svc.Overview(ctx, "org-zero", asOf)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if o.Summary.FailureRate != nil || o.Summary.SlotUtilization != nil || o.Summary.SlotCoverageRatio != nil {
		t.Fatalf("zero denominators must stay nil, got failure=%v util=%v coverage=%v", o.Summary.FailureRate, o.Summary.SlotUtilization, o.Summary.SlotCoverageRatio)
	}
}

func TestInsightInvalidTimeOrder(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	insertActivity(t, db, "start-bad", "agent-1", "task-1", "exec-bad", map[string]any{"event": "executor.start", "executor_id": "exec-bad"}, asOf.Add(-time.Hour))
	insertActivity(t, db, "stop-bad", "agent-1", "task-1", "exec-bad", map[string]any{"event": "executor.stop", "executor_id": "exec-bad", "outcome": "failed"}, asOf.Add(-2*time.Hour))
	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	o, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if o.Diagnostics.InvalidFacts != 1 {
		t.Fatalf("invalid facts = %d, want 1", o.Diagnostics.InvalidFacts)
	}
}

func TestInsightExecutionsCursorDoesNotSkipLimitPlusOneRow(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	for _, execID := range []string{"exec-c", "exec-b", "exec-a"} {
		insertActivity(t, db, "start-"+execID, "agent-1", "task-1", execID, map[string]any{"event": "executor.start", "executor_id": execID}, asOf.Add(-time.Hour))
		insertActivity(t, db, "stop-"+execID, "agent-1", "task-1", execID, map[string]any{"event": "executor.stop", "executor_id": execID, "outcome": "succeeded"}, asOf.Add(-time.Minute))
	}
	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	first, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, Limit: 1})
	if err != nil {
		t.Fatalf("executions page 1: %v", err)
	}
	if len(first.Executions) != 1 || first.Executions[0].ExecutionID != "exec-c" || first.NextCursor == "" {
		t.Fatalf("page 1 = %+v, want exec-c with next cursor", first)
	}
	second, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("executions page 2: %v", err)
	}
	if len(second.Executions) != 1 || second.Executions[0].ExecutionID != "exec-b" || second.NextCursor == "" {
		t.Fatalf("page 2 = %+v, want exec-b with next cursor", second)
	}
	third, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, Limit: 1, Cursor: second.NextCursor})
	if err != nil {
		t.Fatalf("executions page 3: %v", err)
	}
	if len(third.Executions) != 1 || third.Executions[0].ExecutionID != "exec-a" || third.NextCursor != "" {
		t.Fatalf("page 3 = %+v, want final exec-a without cursor", third)
	}
}

func TestInsightCrashRecoveryTransactionAndCheckpointRestart(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	insertActivity(t, db, "start-crash", "agent-1", "task-1", "exec-crash", map[string]any{"event": "executor.start", "executor_id": "exec-crash"}, asOf.Add(-time.Hour))
	insertActivity(t, db, "stop-crash", "agent-1", "task-1", "exec-crash", map[string]any{"event": "executor.stop", "executor_id": "exec-crash", "outcome": "failed"}, asOf.Add(-time.Hour+time.Second))
	svc := openInsight(t, db)

	tx, err := svc.duck.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO execution_fact
		(execution_id, organization_id, agent_ref, started_at, finished_at, outcome, recovered, quality, first_event_id, last_event_id, observed_at)
		VALUES ('exec-crash','org-1','agent:agent-1',?,?,?,false,'valid','activity:start-crash','activity:stop-crash',?)`,
		fmtTS(asOf.Add(-time.Hour)), fmtTS(asOf.Add(-time.Hour+time.Second)), "failed", fmtTS(asOf)); err != nil {
		t.Fatal(err)
	}
	if err := markProjected(ctx, tx, "activity:stop-crash", SourceActivity, "stop-crash", asOf); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var rolledBack int
	if err := svc.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM projected_event WHERE source_event_id='activity:stop-crash'`).Scan(&rolledBack); err != nil {
		t.Fatal(err)
	}
	if rolledBack != 0 {
		t.Fatalf("rolled back projected_event count = %d, want 0", rolledBack)
	}

	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh after rollback: %v", err)
	}
	var cursor string
	if err := svc.duck.QueryRowContext(ctx, `SELECT source_cursor FROM projector_checkpoint WHERE source_kind=?`, SourceActivity).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if cursor != "stop-crash" {
		t.Fatalf("checkpoint cursor = %q, want stop-crash", cursor)
	}
	first, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if first.Summary.CompletedExecutions != 1 {
		t.Fatalf("completed before restart = %d, want 1", first.Summary.CompletedExecutions)
	}
	path := svc.path
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, db, path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Refresh(ctx); err != nil {
		t.Fatalf("refresh after restart: %v", err)
	}
	var factCount, projectedCount int
	if err := reopened.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_fact WHERE execution_id='exec-crash'`).Scan(&factCount); err != nil {
		t.Fatal(err)
	}
	if err := reopened.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM projected_event WHERE source_kind=?`, SourceActivity).Scan(&projectedCount); err != nil {
		t.Fatal(err)
	}
	if factCount != 1 || projectedCount != 2 {
		t.Fatalf("after restart factCount=%d projectedCount=%d, want 1 and 2", factCount, projectedCount)
	}
}

func migratedSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/center.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func openInsight(t *testing.T, db *sql.DB) *Service {
	t.Helper()
	svc, err := Open(context.Background(), db, t.TempDir()+"/insight.duckdb", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func seedDims(t *testing.T, db *sql.DB, orgID string) {
	t.Helper()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execSQL(t, db, `INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES ('project-1', ?, 'Project One', '', 'active', 'user:test', ?, ?)`, orgID, now, now)
	execSQL(t, db, `INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, created_by, created_at, updated_at) VALUES ('task-1', 'project-1', 'Task One', '', 'running', 'agent:agent-1', 'user:test', ?, ?)`, now, now)
	execSQL(t, db, `INSERT INTO workers (id, organization_id, status, capabilities_json, enrolled_at, created_at, updated_at) VALUES ('worker-1', ?,'online','[]',?,?,?)`, orgID, now, now, now)
	execSQL(t, db, `INSERT INTO agents (id, organization_id, name, env_vars, worker_id, lifecycle, created_by, created_at, updated_at) VALUES ('agent-1', ?, 'Agent One', '{}', 'worker-1', 'running', 'user:test', ?, ?)`, orgID, now, now)
}

func insertQueue(t *testing.T, db *sql.DB, id, workerID, agentID, taskID, execID, status string, queued, statusAt time.Time) {
	t.Helper()
	execSQL(t, db, `INSERT INTO worker_control_events (id, worker_id, "offset", idempotency_key, command_type, payload, agent_id, task_id, status, execution_id, status_updated_at, created_at)
		VALUES (?,?,?,?, 'agent.fork_executor', '{}', ?, ?, ?, ?, ?, ?)`,
		id, workerID, time.Now().UnixNano(), id+"-idem", agentID, taskID, status, execID, statusAt.Format(time.RFC3339Nano), queued.Format(time.RFC3339Nano))
}

func insertActivity(t *testing.T, db *sql.DB, id, agentID, taskID, execID string, payload map[string]any, occurred time.Time) {
	t.Helper()
	if payload["executor_id"] == nil {
		payload["executor_id"] = execID
	}
	b, _ := json.Marshal(payload)
	execSQL(t, db, `INSERT INTO agent_activity_events (id, agent_id, task_ref, interaction_ref, event_type, payload, occurred_at)
		VALUES (?, ?, ?, ?, 'lifecycle', ?, ?)`, id, agentID, taskID, "executor:"+execID, string(b), occurred.Format(time.RFC3339Nano))
}

func findExecution(rows []ExecutionRow, id string) *ExecutionRow {
	for i := range rows {
		if rows[i].ExecutionID == id {
			return &rows[i]
		}
	}
	return nil
}

func assertNoNegativeSlotIntervals(t *testing.T, svc *Service) {
	t.Helper()
	var bad int
	if err := svc.duck.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM slot_interval_fact WHERE valid_to IS NOT NULL AND valid_to < valid_from`).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("negative slot intervals = %d, want 0", bad)
	}
}

func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %s: %v", q, err)
	}
}
