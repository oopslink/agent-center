package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	env "github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ControlEventRepo implements env.ControlEventRepository. `offset` is quoted in
// every query because it is a reserved-ish word in some engines.
type ControlEventRepo struct{ db *sql.DB }

// NewControlEventRepo constructs the repo.
func NewControlEventRepo(db *sql.DB) *ControlEventRepo { return &ControlEventRepo{db: db} }

// Append writes one command. A UNIQUE violation on (worker_id, idempotency_key)
// maps to env.ErrDuplicateIdempotencyKey (the lost-race backstop); any other
// UNIQUE violation — i.e. a duplicate (worker_id, offset) — surfaces as-is so
// the caller sees the offset clash.
func (r *ControlEventRepo) Append(ctx context.Context, e *env.WorkerControlEvent) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO worker_control_events
		 (id, worker_id, "offset", idempotency_key, command_type, payload,
		  agent_id, task_id, status, status_reason, status_detail, execution_id, status_updated_at,
		  created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID(), string(e.WorkerID()), e.Offset(), e.IdempotencyKey(),
		e.CommandType(), nullString(e.Payload()),
		nullString(e.AgentID()), nullString(e.TaskID()), nullString(e.Status()),
		nullString(e.StatusReason()), nullString(e.StatusDetail()), nullString(e.ExecutionID()),
		tsZeroNull(e.StatusUpdatedAt()), ts(e.CreatedAt()))
	if persistence.IsUniqueViolation(err) {
		// Distinguish the idempotency-key clash (race backstop) from an
		// offset clash. The constraint name appears in the modernc.org/sqlite
		// error text (e.g. "UNIQUE constraint failed: worker_control_events.worker_id, worker_control_events.idempotency_key").
		if strings.Contains(strings.ToLower(err.Error()), "idempotency_key") {
			return env.ErrDuplicateIdempotencyKey
		}
		return err
	}
	return err
}

func (r *ControlEventRepo) MaxOffset(ctx context.Context, workerID env.WorkerID) (int64, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var maxOff sql.NullInt64
	err := exec.QueryRowContext(ctx,
		// The command stream is retention-GCed after a worker has acked rows, so the
		// table max can fall below the durable ack cursor. The next appended offset must
		// stay above BOTH sources or a reconnecting worker with last_acked_offset=N will
		// never see newly-written offset 1..N commands.
		`SELECT MAX(v) FROM (
			SELECT COALESCE(MAX("offset"), 0) AS v
			  FROM worker_control_events
			 WHERE worker_id = ?
			UNION ALL
			SELECT COALESCE(last_acked_offset, 0) AS v
			  FROM env_workers
			 WHERE id = ?
		)`, string(workerID), string(workerID)).
		Scan(&maxOff)
	if err != nil {
		return 0, err
	}
	if !maxOff.Valid {
		return 0, nil
	}
	return maxOff.Int64, nil
}

func (r *ControlEventRepo) FindByIdempotencyKey(ctx context.Context, workerID env.WorkerID, key string) (*env.WorkerControlEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, eventSelect+` WHERE worker_id = ? AND idempotency_key = ?`,
		string(workerID), key)
	e, err := scanEvent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

func (r *ControlEventRepo) FindByID(ctx context.Context, id string) (*env.WorkerControlEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, eventSelect+` WHERE id = ?`, id)
	e, err := scanEvent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

func (r *ControlEventRepo) LatestNonTerminalByAgentTask(ctx context.Context, workerID env.WorkerID, commandType, agentID, taskID string) (*env.WorkerControlEvent, error) {
	events, err := r.ListByAgentTask(ctx, workerID, commandType, agentID, taskID)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		status := strings.TrimSpace(e.Status())
		if status == "" || status == env.CommandStatusPending || status == env.CommandStatusStarted {
			return e, nil
		}
	}
	return nil, nil
}

func (r *ControlEventRepo) ListAfter(ctx context.Context, workerID env.WorkerID, offset int64) ([]*env.WorkerControlEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		eventSelect+` WHERE worker_id = ? AND "offset" > ? ORDER BY "offset"`,
		string(workerID), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*env.WorkerControlEvent
	for rows.Next() {
		e, err := scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *ControlEventRepo) ListByAgentTask(ctx context.Context, workerID env.WorkerID, commandType, agentID, taskID string) ([]*env.WorkerControlEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		eventSelect+` WHERE worker_id = ? AND command_type = ?
			AND ((agent_id = ? AND task_id = ?)
			  OR COALESCE(agent_id, '') = ''
			  OR COALESCE(task_id, '') = '')
			ORDER BY "offset" DESC`,
		string(workerID), commandType, agentID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*env.WorkerControlEvent
	for rows.Next() {
		e, err := scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		if controlEventMatchesAgentTask(e, agentID, taskID) {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

func (r *ControlEventRepo) UpdateStatus(ctx context.Context, in env.UpdateCommandStatusInput) (*env.WorkerControlEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE worker_control_events
		   SET agent_id = CASE WHEN COALESCE(agent_id, '') = '' THEN NULLIF(?, '') ELSE agent_id END,
		       task_id = CASE WHEN COALESCE(task_id, '') = '' THEN NULLIF(?, '') ELSE task_id END,
		       status = ?,
		       status_reason = ?,
		       status_detail = ?,
		       execution_id = CASE WHEN ? = '' THEN execution_id ELSE ? END,
		       status_updated_at = ?
		 WHERE id = ?
		   AND worker_id = ?
		   AND (? = '' OR COALESCE(agent_id, '') = '' OR agent_id = ?)
		   AND (? = '' OR COALESCE(task_id, '') = '' OR task_id = ?)`,
		in.AgentID, in.TaskID,
		nullString(in.Status), nullString(in.StatusReason), nullString(in.StatusDetail),
		in.ExecutionID, in.ExecutionID, ts(in.StatusUpdatedAt.UTC()),
		in.CommandID, string(in.WorkerID),
		in.AgentID, in.AgentID,
		in.TaskID, in.TaskID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, env.ErrWorkerNotFound
	}
	return r.FindByID(ctx, in.CommandID)
}

func controlEventMatchesAgentTask(e *env.WorkerControlEvent, agentID, taskID string) bool {
	if e == nil {
		return false
	}
	gotAgentID := strings.TrimSpace(e.AgentID())
	gotTaskID := strings.TrimSpace(e.TaskID())
	if gotAgentID == agentID && gotTaskID == taskID {
		return true
	}
	payloadAgentID, payloadTaskID := controlEventPayloadAgentTask(e.Payload())
	if gotAgentID == "" {
		gotAgentID = payloadAgentID
	}
	if gotTaskID == "" {
		gotTaskID = payloadTaskID
	}
	return gotAgentID == agentID && gotTaskID == taskID
}

func controlEventPayloadAgentTask(payloadJSON string) (string, string) {
	var payload struct {
		AgentID string `json:"agent_id"`
		TaskID  string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", ""
	}
	return strings.TrimSpace(payload.AgentID), strings.TrimSpace(payload.TaskID)
}

// DeleteAckedBefore prunes up to `limit` command-stream rows that are SAFE to GC
// (T340, issue-b71ee81f): rows created strictly before `cutoff` whose offset the
// OWNING worker has already acked (e."offset" <= env_workers.last_acked_offset), plus
// orphan rows whose worker no longer exists (COALESCE to MaxInt64 → matched by time
// alone). It NEVER deletes an un-acked row (offset > last_acked_offset) — that is the
// safety guard guaranteeing a worker offline past the retention window still replays
// every undelivered command (CommandsAfter = offset > last_acked) on reconnect; the
// desired lifecycle/work state is re-derived on reconnect (ResumeState boot-reconcile
// + the server work_available sweep) so already-acked rows are dead weight.
//
// Batched (id IN (SELECT ... LIMIT ?)) so a large backlog never locks the table in one
// big transaction — the caller loops until it returns < limit. Times are stored as
// RFC3339Nano UTC strings (clock.Now is always UTC), so the lexicographic '<' is a
// correct time comparison — the same convention the files-GC ListCollectable relies on.
// Returns the number of rows deleted.
func (r *ControlEventRepo) DeleteAckedBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `DELETE FROM worker_control_events
		WHERE id IN (
			SELECT e.id FROM worker_control_events e
			LEFT JOIN env_workers w ON w.id = e.worker_id
			WHERE e.created_at < ?
			  AND e."offset" <= COALESCE(w.last_acked_offset, 9223372036854775807)
			ORDER BY e.created_at
			LIMIT ?
		)`
	res, err := exec.ExecContext(ctx, stmt, ts(cutoff.UTC()), limit)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const eventSelect = `SELECT id, worker_id, "offset", idempotency_key, command_type, payload,
	COALESCE(agent_id, ''), COALESCE(task_id, ''), COALESCE(status, ''),
	COALESCE(status_reason, ''), COALESCE(status_detail, ''), COALESCE(execution_id, ''),
	COALESCE(status_updated_at, ''), created_at FROM worker_control_events`

func scanEvent(scan func(...any) error) (*env.WorkerControlEvent, error) {
	var (
		id, workerID, idempotencyKey, commandType, createdAt string
		agentID, taskID, status, statusReason, statusDetail  string
		executionID, statusUpdatedAt                         string
		offset                                               int64
		payload                                              sql.NullString
	)
	if err := scan(&id, &workerID, &offset, &idempotencyKey, &commandType, &payload,
		&agentID, &taskID, &status, &statusReason, &statusDetail, &executionID,
		&statusUpdatedAt, &createdAt); err != nil {
		return nil, err
	}
	if agentID == "" || taskID == "" {
		payloadAgentID, payloadTaskID := controlEventPayloadAgentTask(payload.String)
		if agentID == "" {
			agentID = payloadAgentID
		}
		if taskID == "" {
			taskID = payloadTaskID
		}
	}
	return env.NewWorkerControlEvent(env.NewWorkerControlEventInput{
		ID: id, WorkerID: env.WorkerID(workerID), Offset: offset,
		IdempotencyKey: idempotencyKey, CommandType: commandType,
		Payload: payload.String, AgentID: agentID, TaskID: taskID, Status: status,
		StatusReason: statusReason, StatusDetail: statusDetail, ExecutionID: executionID,
		StatusUpdatedAt: parseTime(statusUpdatedAt), CreatedAt: parseTime(createdAt),
	})
}

var _ env.ControlEventRepository = (*ControlEventRepo)(nil)
