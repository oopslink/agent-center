package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

type reportExecutionMirrorReq struct {
	AgentID  string                             `json:"agent_id"`
	WorkerID string                             `json:"worker_id"`
	Snapshot concurrency.ExecutionStateSnapshot `json:"snapshot"`
}

type executionMirrorRow struct {
	AgentID            string         `json:"agent_id"`
	TaskID             string         `json:"task_id"`
	WorkerID           string         `json:"worker_id"`
	ExecutionMode      string         `json:"execution_mode"`
	ExecutorID         string         `json:"executor_id,omitempty"`
	ExecutorState      string         `json:"executor_state,omitempty"`
	DeliveryState      string         `json:"delivery_state,omitempty"`
	RequiredNextAction string         `json:"required_next_action,omitempty"`
	Branch             string         `json:"branch,omitempty"`
	HeadSHA            string         `json:"head_sha,omitempty"`
	Worktree           string         `json:"worktree,omitempty"`
	ObservedAt         string         `json:"observed_at"`
	UpdatedAt          string         `json:"updated_at"`
	Row                map[string]any `json:"row,omitempty"`
}

func (s *Server) reportExecutionMirrorHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req reportExecutionMirrorReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.DB == nil {
		writeError(w, http.StatusNotImplemented, "db_not_wired", "")
		return
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		workerID = a.WorkerID()
	}
	if workerID != a.WorkerID() {
		writeError(w, http.StatusForbidden, "agent_not_bound_to_worker", "mirror worker_id must match the authenticated worker")
		return
	}
	snap := req.Snapshot
	if strings.TrimSpace(snap.AgentID) == "" {
		snap.AgentID = req.AgentID
	}
	if snap.AgentID != req.AgentID {
		writeError(w, http.StatusBadRequest, "agent_mismatch", "snapshot.agent_id must match agent_id")
		return
	}
	if snap.UpdatedAt.IsZero() {
		snap.UpdatedAt = time.Now().UTC()
	}
	if err := upsertExecutionMirror(r.Context(), d.DB, workerID, snap); err != nil {
		writeError(w, http.StatusInternalServerError, "execution_mirror_write_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": req.AgentID, "active_tasks": len(snap.ActiveTasks)})
}

func upsertExecutionMirror(ctx context.Context, db *sql.DB, workerID string, snap concurrency.ExecutionStateSnapshot) error {
	snapshotBytes, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	observedAt := snap.UpdatedAt.UTC().Format(time.RFC3339Nano)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_execution_mirrors WHERE agent_id=?`, snap.AgentID); err != nil {
		if ignoreMissingMirrorTable(err) == nil {
			return nil
		}
		return err
	}
	for _, row := range snap.ActiveTasks {
		if strings.TrimSpace(row.TaskID) == "" {
			continue
		}
		rowBytes, err := json.Marshal(row)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_execution_mirrors
			(agent_id, task_id, worker_id, execution_mode, executor_id, executor_state, delivery_state, required_next_action, branch, head_sha, worktree, row_json, snapshot_json, observed_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(agent_id, task_id) DO UPDATE SET
				worker_id=excluded.worker_id,
				execution_mode=excluded.execution_mode,
				executor_id=excluded.executor_id,
				executor_state=excluded.executor_state,
				delivery_state=excluded.delivery_state,
				required_next_action=excluded.required_next_action,
				branch=excluded.branch,
				head_sha=excluded.head_sha,
				worktree=excluded.worktree,
				row_json=excluded.row_json,
				snapshot_json=excluded.snapshot_json,
				observed_at=excluded.observed_at,
				updated_at=excluded.updated_at`,
			snap.AgentID, row.TaskID, workerID, row.ExecutionMode, row.ExecutorID, row.ExecutorState, row.DeliveryState,
			row.RequiredNextAction, row.Branch, row.HeadSHA, row.Worktree, string(rowBytes), string(snapshotBytes), observedAt, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return ignoreMissingMirrorTable(err)
		}
	}
	return tx.Commit()
}

func executionMirrorForAgentTask(ctx context.Context, db *sql.DB, agentID, taskID string) (*executionMirrorRow, error) {
	if db == nil || strings.TrimSpace(agentID) == "" || strings.TrimSpace(taskID) == "" {
		return nil, nil
	}
	var row executionMirrorRow
	var rowJSON string
	err := db.QueryRowContext(ctx, `SELECT agent_id, task_id, worker_id, execution_mode, executor_id, executor_state, delivery_state,
		required_next_action, branch, head_sha, worktree, row_json, observed_at, updated_at
		FROM agent_execution_mirrors WHERE agent_id=? AND task_id=?`, agentID, taskID).Scan(
		&row.AgentID, &row.TaskID, &row.WorkerID, &row.ExecutionMode, &row.ExecutorID, &row.ExecutorState, &row.DeliveryState,
		&row.RequiredNextAction, &row.Branch, &row.HeadSHA, &row.Worktree, &rowJSON, &row.ObservedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if ignored := ignoreMissingMirrorTable(err); ignored == nil {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(rowJSON) != "" {
		_ = json.Unmarshal([]byte(rowJSON), &row.Row)
	}
	return &row, nil
}

func ignoreMissingMirrorTable(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "no such table: agent_execution_mirrors") {
		return nil
	}
	return err
}

func executionMirrorMap(row *executionMirrorRow) map[string]any {
	if row == nil {
		return nil
	}
	m := map[string]any{
		"agent_id":             row.AgentID,
		"task_id":              row.TaskID,
		"worker_id":            row.WorkerID,
		"execution_mode":       row.ExecutionMode,
		"executor_id":          row.ExecutorID,
		"executor_state":       row.ExecutorState,
		"delivery_state":       row.DeliveryState,
		"required_next_action": row.RequiredNextAction,
		"branch":               row.Branch,
		"head_sha":             row.HeadSHA,
		"worktree":             row.Worktree,
		"observed_at":          row.ObservedAt,
		"updated_at":           row.UpdatedAt,
	}
	if row.Row != nil {
		m["row"] = row.Row
	}
	return m
}
