package insight

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestIndependentAcceptance_AggregateDrilldownReconcilesTaskExecutionsAndOrgIsolation(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	seedIndependentDims(t, db, "org-2", "project-2", "task-2", "worker-2", "agent-2")

	insertQueue(t, db, "cmd-o1-a", "worker-1", "agent-1", "task-1", "exec-o1-a", "started", asOf.Add(-2*time.Hour), asOf.Add(-2*time.Hour+10*time.Millisecond))
	insertActivity(t, db, "start-o1-a", "agent-1", "task-1", "exec-o1-a", map[string]any{"event": "executor.start", "executor_id": "exec-o1-a"}, asOf.Add(-2*time.Hour+10*time.Millisecond))
	insertActivity(t, db, "stop-o1-a", "agent-1", "task-1", "exec-o1-a", map[string]any{"event": "executor.stop", "executor_id": "exec-o1-a", "outcome": "succeeded"}, asOf.Add(-2*time.Hour+110*time.Millisecond))

	insertQueue(t, db, "cmd-o1-b", "worker-1", "agent-1", "task-1", "exec-o1-b", "started", asOf.Add(-time.Hour), asOf.Add(-time.Hour+30*time.Millisecond))
	insertActivity(t, db, "start-o1-b", "agent-1", "task-1", "exec-o1-b", map[string]any{"event": "executor.start", "executor_id": "exec-o1-b"}, asOf.Add(-time.Hour+30*time.Millisecond))
	insertActivity(t, db, "stop-o1-b", "agent-1", "task-1", "exec-o1-b", map[string]any{"event": "executor.stop", "executor_id": "exec-o1-b", "outcome": "failed", "reason": "nonzero_exit", "detail": "exit 1"}, asOf.Add(-time.Hour+230*time.Millisecond))

	insertQueue(t, db, "cmd-o2-a", "worker-2", "agent-2", "task-2", "exec-o2-a", "started", asOf.Add(-30*time.Minute), asOf.Add(-30*time.Minute+50*time.Millisecond))
	insertActivity(t, db, "start-o2-a", "agent-2", "task-2", "exec-o2-a", map[string]any{"event": "executor.start", "executor_id": "exec-o2-a"}, asOf.Add(-30*time.Minute+50*time.Millisecond))
	insertActivity(t, db, "stop-o2-a", "agent-2", "task-2", "exec-o2-a", map[string]any{"event": "executor.stop", "executor_id": "exec-o2-a", "outcome": "succeeded"}, asOf.Add(-30*time.Minute+100*time.Millisecond))

	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	org1, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	org2, err := svc.Overview(ctx, "org-2", asOf)
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	raw := readIndependentFacts(t, svc.duck)
	t.Logf("api_overview_org1=%s", mustJSON(org1.Summary))
	t.Logf("api_overview_org2=%s", mustJSON(org2.Summary))
	t.Logf("api_executions_org1=%s", mustJSON(list.Executions))
	t.Logf("duck_execution_fact_raw=%s", mustJSON(raw))

	if org1.Summary.CompletedExecutions != 2 || org1.Summary.FailedExecutions != 1 || org1.Summary.FailureRate == nil || *org1.Summary.FailureRate != 0.5 {
		t.Errorf("org1 summary mismatch: got=%s want completed=2 failed=1 failure_rate=0.5", mustJSON(org1.Summary))
	}
	if org1.Summary.QueueWaitMS.P50 == nil || *org1.Summary.QueueWaitMS.P50 != 20 || org1.Summary.QueueWaitMS.P95 == nil || *org1.Summary.QueueWaitMS.P95 != 29 {
		t.Errorf("org1 quantiles mismatch: got queue=%s want p50=20 p95=29 by quantile_cont over [10,30]", mustJSON(org1.Summary.QueueWaitMS))
	}
	if org1.Summary.ExecutionDurationMS.P50 == nil || *org1.Summary.ExecutionDurationMS.P50 != 150 || org1.Summary.ExecutionDurationMS.P95 == nil || *org1.Summary.ExecutionDurationMS.P95 != 195 {
		t.Errorf("org1 duration quantiles mismatch: got duration=%s want p50=150 p95=195 by quantile_cont over [100,200]", mustJSON(org1.Summary.ExecutionDurationMS))
	}
	if len(list.Executions) != int(org1.Summary.CompletedExecutions) {
		t.Errorf("aggregate to drilldown mismatch: summary completed=%d list_len=%d list=%s", org1.Summary.CompletedExecutions, len(list.Executions), mustJSON(list.Executions))
	}
	for _, row := range list.Executions {
		if row.ExecutionID == "exec-o2-a" || row.ProjectID == nil || *row.ProjectID != "project-1" {
			t.Errorf("org isolation failed in org1 list: row=%s", mustJSON(row))
		}
	}
	if org2.Summary.CompletedExecutions != 1 || org2.Summary.FailedExecutions != 0 {
		t.Errorf("org2 summary mismatch: got=%s want completed=1 failed=0", mustJSON(org2.Summary))
	}
	if _, err := svc.Execution(ctx, "org-1", "exec-o2-a", asOf); !errors.Is(err, ErrExecutionNotFound) {
		t.Errorf("org isolation detail mismatch: org1 reading exec-o2-a err=%v want ErrExecutionNotFound", err)
	}
}

func TestIndependentAcceptance_ZeroUnknownSemanticsAreNotCoercedToZero(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")
	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	overview, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("zero_unknown_api_summary=%s", mustJSON(overview.Summary))
	if overview.Summary.CompletedExecutions != 0 || overview.Summary.FailedExecutions != 0 {
		t.Errorf("zero counts mismatch: got=%s want completed=0 failed=0", mustJSON(overview.Summary))
	}
	if overview.Summary.FailureRate != nil {
		t.Errorf("failure_rate should be null with no completed executions: got=%s", mustJSON(overview.Summary.FailureRate))
	}
	if overview.Summary.QueueWaitMS.P50 != nil || overview.Summary.QueueWaitMS.P95 != nil || overview.Summary.QueueWaitMS.Samples != 0 {
		t.Errorf("queue percentiles should be null with zero samples: got=%s", mustJSON(overview.Summary.QueueWaitMS))
	}
	if overview.Summary.SlotUtilization != nil || overview.Summary.SlotCoverageRatio != nil {
		t.Errorf("slot utilization/coverage should be unknown nil without observations: got util=%s coverage=%s", mustJSON(overview.Summary.SlotUtilization), mustJSON(overview.Summary.SlotCoverageRatio))
	}
}

func TestIndependentAcceptance_PreStartQueueWithExecutionIDRemainsDrilldownVisible(t *testing.T) {
	ctx := context.Background()
	db := migratedSQLite(t)
	asOf := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	seedDims(t, db, "org-1")

	insertQueue(t, db, "cmd-prestart", "worker-1", "agent-1", "task-1", "exec-prestart", "pending", asOf.Add(-10*time.Minute), asOf.Add(-10*time.Minute))

	svc := openInsight(t, db)
	if err := svc.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	overview, err := svc.Overview(ctx, "org-1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.Executions(ctx, "org-1", ExecutionFilter{AsOf: asOf, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	rawQueue := readIndependentQueueFacts(t, svc.duck)
	rawExec := readIndependentFacts(t, svc.duck)
	t.Logf("prestart_api_summary=%s", mustJSON(overview.Summary))
	t.Logf("prestart_api_executions=%s", mustJSON(list.Executions))
	t.Logf("prestart_duck_queue_interval_fact=%s", mustJSON(rawQueue))
	t.Logf("prestart_duck_execution_fact=%s", mustJSON(rawExec))

	if overview.Summary.CompletedExecutions != 0 || overview.Summary.QueueWaitMS.Samples != 0 {
		t.Errorf("pre-start command must not affect completed or queue percentile aggregates: got=%s", mustJSON(overview.Summary))
	}
	var found bool
	for _, row := range list.Executions {
		if row.CommandID != nil && *row.CommandID == "cmd-prestart" {
			found = true
			if row.StartedAt != nil || row.Outcome != nil || row.ExecutionID != "command:cmd-prestart" {
				t.Errorf("pre-start drilldown row semantics mismatch: got=%s want command pseudo row without start/outcome", mustJSON(row))
			}
		}
	}
	if !found {
		t.Fatalf("pre-start queue command with execution_id is missing from drilldown: api_executions=%s raw_queue=%s raw_execution_fact=%s want command:cmd-prestart row", mustJSON(list.Executions), mustJSON(rawQueue), mustJSON(rawExec))
	}
}

func seedIndependentDims(t *testing.T, db *sql.DB, orgID, projectID, taskID, workerID, agentID string) {
	t.Helper()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execSQL(t, db, `INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'active', 'user:test', ?, ?)`, projectID, orgID, projectID+" name", now, now)
	execSQL(t, db, `INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'running', ?, 'user:test', ?, ?)`, taskID, projectID, taskID+" title", "agent:"+agentID, now, now)
	execSQL(t, db, `INSERT INTO workers (id, organization_id, status, capabilities_json, enrolled_at, created_at, updated_at) VALUES (?, ?,'online','[]',?,?,?)`, workerID, orgID, now, now, now)
	execSQL(t, db, `INSERT INTO agents (id, organization_id, name, env_vars, worker_id, lifecycle, created_by, created_at, updated_at) VALUES (?, ?, ?, '{}', ?, 'running', 'user:test', ?, ?)`, agentID, orgID, agentID+" name", workerID, now, now)
}

func readIndependentFacts(t *testing.T, db *sql.DB) []map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT execution_id, command_id, organization_id, project_id, task_id, agent_ref, outcome, command_status, CAST(queued_at AS VARCHAR), CAST(started_at AS VARCHAR), CAST(finished_at AS VARCHAR), quality FROM execution_fact ORDER BY organization_id, execution_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var executionID, agentRef, quality string
		var commandID, orgID, projectID, taskID, outcome, commandStatus, queuedAt, startedAt, finishedAt sql.NullString
		if err := rows.Scan(&executionID, &commandID, &orgID, &projectID, &taskID, &agentRef, &outcome, &commandStatus, &queuedAt, &startedAt, &finishedAt, &quality); err != nil {
			t.Fatal(err)
		}
		out = append(out, map[string]any{
			"execution_id": executionID, "command_id": nullableLog(commandID), "organization_id": nullableLog(orgID),
			"project_id": nullableLog(projectID), "task_id": nullableLog(taskID), "agent_ref": agentRef,
			"outcome": nullableLog(outcome), "command_status": nullableLog(commandStatus), "queued_at": nullableLog(queuedAt),
			"started_at": nullableLog(startedAt), "finished_at": nullableLog(finishedAt), "quality": quality,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func readIndependentQueueFacts(t *testing.T, db *sql.DB) []map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT command_id, execution_id, organization_id, project_id, task_id, agent_ref, command_status, CAST(queued_at AS VARCHAR), CAST(started_at AS VARCHAR), quality FROM queue_interval_fact ORDER BY command_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var commandID, agentRef, commandStatus, queuedAt, quality string
		var executionID, orgID, projectID, taskID, startedAt sql.NullString
		if err := rows.Scan(&commandID, &executionID, &orgID, &projectID, &taskID, &agentRef, &commandStatus, &queuedAt, &startedAt, &quality); err != nil {
			t.Fatal(err)
		}
		out = append(out, map[string]any{
			"command_id": commandID, "execution_id": nullableLog(executionID), "organization_id": nullableLog(orgID),
			"project_id": nullableLog(projectID), "task_id": nullableLog(taskID), "agent_ref": agentRef,
			"command_status": commandStatus, "queued_at": queuedAt, "started_at": nullableLog(startedAt), "quality": quality,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func nullableLog(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<json error: " + err.Error() + ">"
	}
	return string(b)
}
