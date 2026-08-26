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
	aged, err := svc.Overview(ctx, "org-1", asOf.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("overview after window advances: %v", err)
	}
	if aged.Summary.CompletedExecutions != 0 || aged.Summary.FailureRate != nil {
		t.Fatalf("aged-out summary = %+v, want zero completed and null failure rate", aged.Summary)
	}
}

func TestInsightCheckpointRestartDoesNotDuplicateFacts(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	insertQueue(t, db, "cmd-restart", "worker-1", "agent-1", "task-1", "exec-restart", "started", asOf.Add(-time.Hour), asOf.Add(-time.Hour+time.Second))
	insertActivity(t, db, "start-restart", "agent-1", "task-1", "exec-restart", map[string]any{"event": "executor.start", "executor_id": "exec-restart"}, asOf.Add(-time.Hour+time.Second))
	insertActivity(t, db, "stop-restart", "agent-1", "task-1", "exec-restart", map[string]any{"event": "executor.stop", "executor_id": "exec-restart", "outcome": "succeeded"}, asOf.Add(-30*time.Minute))

	path := t.TempDir() + "/insight.duckdb"
	first, err := Open(ctx, db, path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Overview(ctx, "org-1", asOf); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, db, path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := second.Overview(ctx, "org-1", asOf); err != nil {
		t.Fatal(err)
	}
	var facts, events int
	if err := second.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_fact WHERE execution_id='exec-restart'`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if err := second.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM projected_event WHERE source_event_id IN ('queue:cmd-restart:started:2026-08-26T11:00:01Z','activity:start-restart','activity:stop-restart')`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if facts != 1 || events != 3 {
		t.Fatalf("restart counts facts=%d events=%d, want 1 fact and 3 projected source events", facts, events)
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

	// Slot 1 remains idle while the admission cap falls from two to one. The
	// state is identical, but its denominator eligibility changes, so this must
	// create a new interval rather than coalescing across the cap boundary.
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
	o, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if o.Diagnostics.InvalidFacts != 1 {
		t.Fatalf("invalid facts = %d, want 1", o.Diagnostics.InvalidFacts)
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
