package insight

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

func TestInsightFreshness_ProductionCheckpointFreshStaleAndRebuild(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	seedDims(t, db, "org-1")
	finished := time.Now().UTC().Add(-time.Second)
	insertActivity(t, db, "start-freshness", "agent-1", "task-1", "exec-freshness", map[string]any{"event": "executor.start", "executor_id": "exec-freshness"}, finished.Add(-time.Second))
	insertActivity(t, db, "stop-freshness", "agent-1", "task-1", "exec-freshness", map[string]any{"event": "executor.stop", "executor_id": "exec-freshness", "outcome": "succeeded"}, finished)

	const ttl = 2 * time.Minute
	svc, err := Open(ctx, db, t.TempDir()+"/insight.duckdb", ttl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	var rawRef string
	if err := svc.duck.QueryRowContext(ctx, `SELECT CAST(MAX(refreshed_at) AS VARCHAR) FROM projector_checkpoint WHERE state='fresh'`).Scan(&rawRef); err != nil {
		t.Fatal(err)
	}
	if _, ok := parseTS(rawRef); !ok {
		t.Fatalf("parseTS(%q) = false; DuckDB TIMESTAMPTZ checkpoint text must parse", rawRef)
	}

	now := time.Now().UTC()
	overview, err := svc.Overview(ctx, "org-1", now)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if overview.Freshness.State != "fresh" {
		t.Fatalf("overview freshness after refresh = %+v raw_ref=%q, want fresh", overview.Freshness, rawRef)
	}
	if overview.RefreshedAt == "" {
		t.Fatal("overview refreshed_at empty after refresh")
	}

	refreshedAt, ok := parseTS(overview.RefreshedAt)
	if !ok {
		t.Fatalf("overview refreshed_at did not parse: %q", overview.RefreshedAt)
	}
	atThreshold, err := svc.Execution(ctx, "org-1", "exec-freshness", refreshedAt.Add(ttl))
	if err != nil {
		t.Fatalf("execution at threshold: %v", err)
	}
	if atThreshold.Freshness.State != "fresh" || atThreshold.Freshness.AgeMS != ttl.Milliseconds() {
		t.Fatalf("execution freshness at threshold = %+v, want fresh age=%d", atThreshold.Freshness, ttl.Milliseconds())
	}
	overThreshold, err := svc.Execution(ctx, "org-1", "exec-freshness", refreshedAt.Add(ttl+time.Millisecond))
	if err != nil {
		t.Fatalf("execution over threshold: %v", err)
	}
	if overThreshold.Freshness.State != "stale" || overThreshold.Freshness.AgeMS != (ttl+time.Millisecond).Milliseconds() {
		t.Fatalf("execution freshness over threshold = %+v, want stale age=%d", overThreshold.Freshness, (ttl + time.Millisecond).Milliseconds())
	}

	if err := svc.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuilt, err := svc.Overview(ctx, "org-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("overview after rebuild: %v", err)
	}
	if rebuilt.Freshness.State != "fresh" {
		t.Fatalf("rebuilt overview freshness = %+v, want fresh", rebuilt.Freshness)
	}
	if rebuilt.Summary.CompletedExecutions != 1 {
		t.Fatalf("rebuilt completed executions = %d, want 1", rebuilt.Summary.CompletedExecutions)
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

func TestInsightSlotObservation_AdmissionCapOnlyChangeClosesCapacityInterval(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	repo := NewObservationRepo(db, idgen.NewGenerator(clock.NewFakeClock(asOf)))

	_, err := repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 2, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateIdle},
		{SlotIndex: 1, State: concurrency.StateIdle},
	}}, asOf.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 1, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateIdle},
		{SlotIndex: 1, State: concurrency.StateIdle},
	}}, asOf.Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.duck.QueryContext(ctx, `SELECT admissible, CAST(valid_from AS VARCHAR), CAST(valid_to AS VARCHAR)
		FROM slot_interval_fact WHERE worker_id='worker-1' AND agent_ref='agent:agent-1' AND slot_index=1
		ORDER BY valid_from`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type interval struct {
		admissible bool
		from, to   string
	}
	var got []interval
	for rows.Next() {
		var v interval
		var to sql.NullString
		if err := rows.Scan(&v.admissible, &v.from, &to); err != nil {
			t.Fatal(err)
		}
		v.to = to.String
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].admissible || got[1].admissible || got[0].to == "" {
		t.Fatalf("slot 1 cap-boundary intervals = %+v, want admissible closed interval then inadmissible open interval", got)
	}
}

func TestInsightSlotObservation_HeartbeatTTLExcludesUnknownTail(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	repo := NewObservationRepo(db, idgen.NewGenerator(clock.NewFakeClock(asOf)))
	_, err := repo.Append(ctx, "worker-1", "agent-1", concurrency.AgentSnapshot{AdmissionCap: 1, Slots: []concurrency.SlotSnapshot{
		{SlotIndex: 0, State: concurrency.StateRunning, ExecutorID: "exec-stale", TaskID: "task-1"},
	}}, asOf.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	const ttl = time.Minute
	svc, err := Open(ctx, db, t.TempDir()+"/insight.duckdb", ttl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	o, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if o.Summary.SlotUtilization == nil || *o.Summary.SlotUtilization != 1 {
		t.Fatalf("slot utilization = %v, want 1 over the known TTL-covered minute", o.Summary.SlotUtilization)
	}
	if o.Summary.SlotCoverageRatio == nil {
		t.Fatal("slot coverage = nil, want one TTL-covered minute over the 24h window")
	}
	want := float64(ttl) / float64(24*time.Hour)
	if delta := *o.Summary.SlotCoverageRatio - want; delta < -1e-9 || delta > 1e-9 {
		t.Fatalf("slot coverage = %.9f, want %.9f; time after heartbeat TTL must be unknown", *o.Summary.SlotCoverageRatio, want)
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

func TestInsightExecutionSemanticsMessagesAndDetailWindow(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")

	queued := asOf.Add(-time.Hour)
	started := queued.Add(time.Second)
	finished := started.Add(time.Minute)
	execSQL(t, db, `INSERT INTO worker_control_events
		(id, worker_id, "offset", idempotency_key, command_type, payload, agent_id, task_id, status, status_reason, status_detail, execution_id, status_updated_at, created_at)
		VALUES ('cmd-failed','worker-1',1,'cmd-failed-idem','agent.fork_executor','{}','agent-1','task-1','started','worker_ack','worker accepted','exec-failed',?,?)`,
		started.Format(time.RFC3339Nano), queued.Format(time.RFC3339Nano))
	insertActivity(t, db, "start-failed", "agent-1", "task-1", "exec-failed", map[string]any{"event": "executor.start", "executor_id": "exec-failed"}, started)
	insertActivity(t, db, "stop-failed", "agent-1", "task-1", "exec-failed", map[string]any{"event": "executor.stop", "executor_id": "exec-failed", "outcome": "failed", "reason": "nonzero_exit", "detail": "command exited 2"}, finished)

	execSQL(t, db, `INSERT INTO worker_control_events
		(id, worker_id, "offset", idempotency_key, command_type, payload, agent_id, task_id, status, status_reason, status_detail, status_updated_at, created_at)
		VALUES ('cmd-rejected','worker-1',2,'cmd-rejected-idem','agent.fork_executor','{}','agent-1','task-1','rejected','repo_source_unavailable','repository missing',?,?)`,
		asOf.Add(-30*time.Minute).Format(time.RFC3339Nano), asOf.Add(-30*time.Minute).Format(time.RFC3339Nano))

	insertActivity(t, db, "start-unknown", "agent-1", "task-1", "exec-unknown", map[string]any{"event": "executor.start", "executor_id": "exec-unknown"}, asOf.Add(-20*time.Minute))
	insertActivity(t, db, "stop-unknown", "agent-1", "task-1", "exec-unknown", map[string]any{"event": "executor.stop", "executor_id": "exec-unknown", "outcome": "future_terminal"}, asOf.Add(-19*time.Minute))

	insertActivity(t, db, "start-old", "agent-1", "task-1", "exec-old", map[string]any{"event": "executor.start", "executor_id": "exec-old"}, asOf.Add(-26*time.Hour))
	insertActivity(t, db, "stop-old", "agent-1", "task-1", "exec-old", map[string]any{"event": "executor.stop", "executor_id": "exec-old", "outcome": "succeeded"}, asOf.Add(-25*time.Hour))

	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	rows, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, Limit: 100})
	if err != nil {
		t.Fatalf("executions: %v", err)
	}
	byID := map[string]ExecutionRow{}
	for _, row := range rows.Executions {
		byID[row.ExecutionID] = row
	}
	failed := byID["exec-failed"]
	if failed.FailureReason == nil || *failed.FailureReason != "nonzero_exit" || failed.FailureMessage == nil || *failed.FailureMessage != "command exited 2" {
		t.Fatalf("failed reason/message = reason:%v message:%v", failed.FailureReason, failed.FailureMessage)
	}
	if failed.CommandStatus == nil || *failed.CommandStatus != "started" || failed.StatusReason == nil || *failed.StatusReason != "worker_ack" || failed.StatusMessage == nil || *failed.StatusMessage != "worker accepted" {
		t.Fatalf("failed command semantics fields = %+v", failed)
	}
	if failed.StatusSemantic.State != "failed" || !failed.StatusSemantic.CountsAsFailure || failed.StatusSemantic.AuditOutcome != "failed" {
		t.Fatalf("failed status semantic = %+v", failed.StatusSemantic)
	}
	rejected := byID["command:cmd-rejected"]
	if rejected.StatusSemantic.State != "did_not_start" || rejected.StatusMessage == nil || *rejected.StatusMessage != "repository missing" {
		t.Fatalf("rejected command row = %+v", rejected)
	}
	unknown := byID["exec-unknown"]
	if unknown.StatusSemantic.State != "outcome_unavailable" || unknown.StatusSemantic.CountsAsFailure {
		t.Fatalf("unknown outcome semantic = %+v", unknown.StatusSemantic)
	}
	if _, ok := byID["exec-old"]; ok {
		t.Fatalf("old execution leaked into 24h list: %+v", byID["exec-old"])
	}
	if _, err := svc.Execution(ctx, "org-1", "exec-old", asOf); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("old execution detail err = %v, want ErrExecutionNotFound", err)
	}
	overview, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Summary.CompletedExecutions != 1 || overview.Summary.FailedExecutions != 1 {
		t.Fatalf("summary = %+v, want only known terminal failed counted", overview.Summary)
	}
	if len(overview.Agents) != 1 || overview.Agents[0].Summary.CompletedExecutions != 1 {
		t.Fatalf("leaderboard = %+v, want unknown outcome excluded from completed sort/count", overview.Agents)
	}
	if overview.Summary.Semantics.FailureRate.Status != "ok" || overview.Summary.Semantics.QueueWaitP50MS.SampleCount == nil || *overview.Summary.Semantics.QueueWaitP50MS.SampleCount != 1 {
		t.Fatalf("summary semantics = %+v", overview.Summary.Semantics)
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

func TestInsightPreCommitCrashReplaysAndAppliesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	insertActivity(t, db, "start-crash", "agent-1", "task-1", "exec-crash", map[string]any{"event": "executor.start", "executor_id": "exec-crash"}, asOf.Add(-time.Hour))
	insertActivity(t, db, "stop-crash", "agent-1", "task-1", "exec-crash", map[string]any{"event": "executor.stop", "executor_id": "exec-crash", "outcome": "failed"}, asOf.Add(-time.Hour+time.Second))
	svc := openInsight(t, db)

	crash := errors.New("test pre-commit crash")
	var hookCalls int
	svc.projectorFaultHook = func(_ context.Context, kind, sourceID string, stage insightProjectorCommitStage) error {
		if kind == SourceActivity && sourceID == "activity:stop-crash" && stage == insightProjectorBeforeCommit {
			hookCalls++
			return crash
		}
		return nil
	}

	if err := svc.Refresh(ctx); !errors.Is(err, crash) {
		t.Fatalf("refresh pre-commit crash = %v, want %v", err, crash)
	}
	if hookCalls != 1 {
		t.Fatalf("pre-commit hook calls = %d, want 1", hookCalls)
	}
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM projected_event WHERE source_event_id='activity:stop-crash'`, 0)
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM projector_checkpoint WHERE source_kind=? AND source_cursor='stop-crash' AND state='fresh'`, 0, SourceActivity)
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM execution_fact WHERE execution_id='exec-crash' AND finished_at IS NOT NULL`, 0)

	svc.projectorFaultHook = nil
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh after pre-commit crash: %v", err)
	}
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM execution_fact WHERE execution_id='exec-crash'`, 1)
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM projected_event WHERE source_event_id='activity:stop-crash'`, 1)
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM projected_event WHERE source_kind=?`, 2, SourceActivity)

	var cursor, state string
	if err := svc.duck.QueryRowContext(ctx, `SELECT source_cursor, state FROM projector_checkpoint WHERE source_kind=?`, SourceActivity).Scan(&cursor, &state); err != nil {
		t.Fatal(err)
	}
	if cursor != "stop-crash" || state != "fresh" {
		t.Fatalf("checkpoint after replay = (%q,%q), want (stop-crash,fresh)", cursor, state)
	}
	overview, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Summary.CompletedExecutions != 1 || overview.Summary.FailedExecutions != 1 {
		t.Fatalf("summary after replay = %+v, want one failed completion", overview.Summary)
	}
}

func TestInsightPostCommitCrashRestartDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	insertActivity(t, db, "start-post", "agent-1", "task-1", "exec-post", map[string]any{"event": "executor.start", "executor_id": "exec-post"}, asOf.Add(-time.Hour))
	insertActivity(t, db, "stop-post", "agent-1", "task-1", "exec-post", map[string]any{"event": "executor.stop", "executor_id": "exec-post", "outcome": "succeeded"}, asOf.Add(-time.Hour+time.Second))
	svc := openInsight(t, db)

	crash := errors.New("test post-commit crash")
	var hookCalls int
	svc.projectorFaultHook = func(_ context.Context, kind, sourceID string, stage insightProjectorCommitStage) error {
		if kind == SourceActivity && sourceID == "activity:stop-post" && stage == insightProjectorAfterCommit {
			hookCalls++
			return crash
		}
		return nil
	}

	if err := svc.projectActivity(ctx); !errors.Is(err, crash) {
		t.Fatalf("projectActivity post-commit crash = %v, want %v", err, crash)
	}
	if hookCalls != 1 {
		t.Fatalf("post-commit hook calls = %d, want 1", hookCalls)
	}
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM execution_fact WHERE execution_id='exec-post'`, 1)
	assertDuckCount(t, svc.duck, `SELECT COUNT(*) FROM projected_event WHERE source_event_id='activity:stop-post'`, 1)
	var cursor string
	if err := svc.duck.QueryRowContext(ctx, `SELECT source_cursor FROM projector_checkpoint WHERE source_kind=?`, SourceActivity).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if cursor != "stop-post" {
		t.Fatalf("checkpoint cursor after post-commit crash = %q, want stop-post", cursor)
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
	assertDuckCount(t, reopened.duck, `SELECT COUNT(*) FROM execution_fact WHERE execution_id='exec-post'`, 1)
	assertDuckCount(t, reopened.duck, `SELECT COUNT(*) FROM projected_event WHERE source_event_id='activity:stop-post'`, 1)
	assertDuckCount(t, reopened.duck, `SELECT COUNT(*) FROM projected_event WHERE source_kind=?`, 2, SourceActivity)

	overview, err := reopened.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Summary.CompletedExecutions != 1 || overview.Summary.FailedExecutions != 0 {
		t.Fatalf("summary after restart = %+v, want one successful completion", overview.Summary)
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

func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %s: %v", q, err)
	}
}

func assertDuckCount(t *testing.T, db *sql.DB, q string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(context.Background(), q, args...).Scan(&got); err != nil {
		t.Fatalf("query count %s: %v", q, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", q, got, want)
	}
}
