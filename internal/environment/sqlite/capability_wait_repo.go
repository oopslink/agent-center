package sqlite

import (
	"context"
	"database/sql"
	"time"

	env "github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/persistence"
)

type CapabilityWaitRepo struct{ db *sql.DB }

func NewCapabilityWaitRepo(db *sql.DB) *CapabilityWaitRepo { return &CapabilityWaitRepo{db: db} }

func (r *CapabilityWaitRepo) UpsertWaiting(ctx context.Context, wait env.CapabilityWait) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	if wait.Status == "" {
		wait.Status = env.CapabilityWaitWaiting
	}
	const stmt = `INSERT INTO capability_waits
		(task_id, agent_id, assignee_ref, worker_id, required_cli, reason, status, created_at, updated_at, expires_at, redrive_count, last_redrive_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, NULL)
		ON CONFLICT(task_id, agent_id) DO UPDATE SET
		  assignee_ref = excluded.assignee_ref,
		  worker_id = excluded.worker_id,
		  required_cli = excluded.required_cli,
		  reason = excluded.reason,
		  status = excluded.status,
		  updated_at = excluded.updated_at,
		  expires_at = excluded.expires_at`
	_, err := exec.ExecContext(ctx, stmt,
		wait.TaskID, wait.AgentID, wait.AssigneeRef, wait.WorkerID, wait.RequiredCLI,
		wait.Reason, string(wait.Status), ts(wait.CreatedAt), ts(wait.UpdatedAt), nullableTime(wait.ExpiresAt))
	return err
}

func (r *CapabilityWaitRepo) Resolve(ctx context.Context, taskID, agentID string, at time.Time) error {
	return r.setStatus(ctx, taskID, agentID, env.CapabilityWaitResolved, "resolved", at)
}

func (r *CapabilityWaitRepo) CancelByTask(ctx context.Context, taskID, reason string, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE capability_waits
		    SET status = ?, reason = ?, updated_at = ?
		  WHERE task_id = ? AND status = ?`,
		string(env.CapabilityWaitCanceled), reason, ts(at), taskID, string(env.CapabilityWaitWaiting))
	return err
}

func (r *CapabilityWaitRepo) MarkTimedOut(ctx context.Context, taskID, agentID, reason string, at time.Time) error {
	return r.setStatus(ctx, taskID, agentID, env.CapabilityWaitTimedOut, reason, at)
}

func (r *CapabilityWaitRepo) RecordRedrive(ctx context.Context, taskID, agentID string, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE capability_waits
		    SET redrive_count = redrive_count + 1, last_redrive_at = ?, updated_at = ?
		  WHERE task_id = ? AND agent_id = ? AND status = ?`,
		ts(at), ts(at), taskID, agentID, string(env.CapabilityWaitWaiting))
	return err
}

func (r *CapabilityWaitRepo) ListWaitingByWorker(ctx context.Context, workerID string) ([]env.CapabilityWait, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, waitSelect+` WHERE status = ? AND worker_id = ? ORDER BY created_at, task_id`,
		string(env.CapabilityWaitWaiting), workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWaitRows(rows)
}

func (r *CapabilityWaitRepo) ListWaiting(ctx context.Context) ([]env.CapabilityWait, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, waitSelect+` WHERE status = ? ORDER BY created_at, task_id`,
		string(env.CapabilityWaitWaiting))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWaitRows(rows)
}

func (r *CapabilityWaitRepo) ListExpiredWaiting(ctx context.Context, at time.Time, limit int) ([]env.CapabilityWait, error) {
	if limit <= 0 {
		limit = 100
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, waitSelect+` WHERE status = ? AND expires_at IS NOT NULL AND expires_at <= ? ORDER BY expires_at LIMIT ?`,
		string(env.CapabilityWaitWaiting), ts(at), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWaitRows(rows)
}

func (r *CapabilityWaitRepo) setStatus(ctx context.Context, taskID, agentID string, status env.CapabilityWaitStatus, reason string, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE capability_waits
		    SET status = ?, reason = ?, updated_at = ?
		  WHERE task_id = ? AND agent_id = ? AND status = ?`,
		string(status), reason, ts(at), taskID, agentID, string(env.CapabilityWaitWaiting))
	return err
}

const waitSelect = `SELECT task_id, agent_id, assignee_ref, worker_id, required_cli, reason, status, created_at, updated_at, expires_at, redrive_count, last_redrive_at FROM capability_waits`

func scanWaitRows(rows *sql.Rows) ([]env.CapabilityWait, error) {
	var out []env.CapabilityWait
	for rows.Next() {
		w, err := scanWait(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanWait(scan func(...any) error) (env.CapabilityWait, error) {
	var (
		w                        env.CapabilityWait
		status                   string
		createdAt, updatedAt     string
		expiresAt, lastRedriveAt sql.NullString
	)
	if err := scan(&w.TaskID, &w.AgentID, &w.AssigneeRef, &w.WorkerID, &w.RequiredCLI,
		&w.Reason, &status, &createdAt, &updatedAt, &expiresAt, &w.RedriveCount, &lastRedriveAt); err != nil {
		return env.CapabilityWait{}, err
	}
	w.Status = env.CapabilityWaitStatus(status)
	w.CreatedAt = parseTime(createdAt)
	w.UpdatedAt = parseTime(updatedAt)
	if expiresAt.Valid {
		w.ExpiresAt = parseTime(expiresAt.String)
	}
	if lastRedriveAt.Valid {
		w.LastRedriveAt = parseTime(lastRedriveAt.String)
	}
	return w, nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return ts(t)
}

var _ env.CapabilityWaitRepository = (*CapabilityWaitRepo)(nil)
