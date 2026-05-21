package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// ProjectionRepo is the SQLite-backed implementation of
// observability/projection.Repository, owning the task_execution_projections
// table (conventions § 9.z BC physical isolation + 02-persistence § 8.2.1).
type ProjectionRepo struct {
	db *sql.DB
}

// NewProjectionRepo constructs a repo over the given *sql.DB.
func NewProjectionRepo(db *sql.DB) *ProjectionRepo {
	return &ProjectionRepo{db: db}
}

// FindByID returns the projection row for the given task execution.
func (r *ProjectionRepo) FindByID(ctx context.Context, id taskruntime.TaskExecutionID) (*projection.TaskExecutionProjection, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const stmt = `SELECT task_execution_id, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_push_at
        FROM task_execution_projections
        WHERE task_execution_id = ?`
	row := exec.QueryRowContext(ctx, stmt, string(id))
	p, err := scanProjection(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projection.ErrProjectionNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// FindByIDs returns projections for the given task executions in any order.
// IDs without a row are simply absent from the result map.
func (r *ProjectionRepo) FindByIDs(ctx context.Context, ids []taskruntime.TaskExecutionID) (map[taskruntime.TaskExecutionID]*projection.TaskExecutionProjection, error) {
	out := map[taskruntime.TaskExecutionID]*projection.TaskExecutionProjection{}
	if len(ids) == 0 {
		return out, nil
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = string(id)
	}
	stmt := fmt.Sprintf(`SELECT task_execution_id, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_push_at
        FROM task_execution_projections
        WHERE task_execution_id IN (%s)`, strings.Join(placeholders, ","))
	rows, err := exec.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProjection(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[p.TaskExecutionID] = p
	}
	return out, rows.Err()
}

// UpsertIfFresh writes the row only if no stored row exists OR the stored
// row's last_push_at is strictly older than update.LastPushAt. When the
// stored row is fresher or equal it returns ErrProjectionStale and the
// caller is expected to emit the observability.projection_stale_drop event.
func (r *ProjectionRepo) UpsertIfFresh(ctx context.Context, id taskruntime.TaskExecutionID, update projection.ProjectionUpdate) (projection.TaskExecutionProjection, bool, error) {
	if err := update.Validate(); err != nil {
		return projection.TaskExecutionProjection{}, false, err
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return projection.TaskExecutionProjection{}, false, err
	}
	// SELECT existing → if newer, drop.
	const sel = `SELECT current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_push_at
        FROM task_execution_projections WHERE task_execution_id = ?`
	row := exec.QueryRowContext(ctx, sel, string(id))
	var (
		curAct    sql.NullString
		curActAt  sql.NullString
		toolCalls int64
		toksIn    int64
		toksOut   int64
		workSec   int64
		lastPush  string
	)
	err = row.Scan(&curAct, &curActAt, &toolCalls, &toksIn, &toksOut, &workSec, &lastPush)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return projection.TaskExecutionProjection{}, false, err
	}
	existing := projection.TaskExecutionProjection{TaskExecutionID: id}
	hasExisting := err == nil
	if hasExisting {
		if curAct.Valid {
			existing.CurrentActivity = curAct.String
		}
		if curActAt.Valid {
			if t, perr := time.Parse(time.RFC3339Nano, curActAt.String); perr == nil {
				existing.CurrentActivityAt = t
			}
		}
		existing.TotalToolCalls = toolCalls
		existing.TotalTokensInput = toksIn
		existing.TotalTokensOutput = toksOut
		existing.WorkingSecondsAccumulated = workSec
		if t, perr := time.Parse(time.RFC3339Nano, lastPush); perr == nil {
			existing.LastPushAt = t
		}
		if !existing.LastPushAt.Before(update.LastPushAt) {
			return existing, false, projection.ErrProjectionStale
		}
	}
	// UPSERT
	const ups = `INSERT INTO task_execution_projections (
        task_execution_id, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_push_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(task_execution_id) DO UPDATE SET
        current_activity = excluded.current_activity,
        current_activity_at = excluded.current_activity_at,
        total_tool_calls = excluded.total_tool_calls,
        total_tokens_input = excluded.total_tokens_input,
        total_tokens_output = excluded.total_tokens_output,
        working_seconds_accumulated = excluded.working_seconds_accumulated,
        last_push_at = excluded.last_push_at`
	var actAtArg any
	if !update.CurrentActivityAt.IsZero() {
		actAtArg = update.CurrentActivityAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = exec.ExecContext(ctx, ups,
		string(id),
		nullableString(update.CurrentActivity),
		actAtArg,
		update.TotalToolCalls,
		update.TotalTokensInput,
		update.TotalTokensOutput,
		update.WorkingSecondsAccumulated,
		update.LastPushAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return projection.TaskExecutionProjection{}, false, err
	}
	fresh := projection.TaskExecutionProjection{
		TaskExecutionID:           id,
		CurrentActivity:           update.CurrentActivity,
		CurrentActivityAt:         update.CurrentActivityAt,
		TotalToolCalls:            update.TotalToolCalls,
		TotalTokensInput:          update.TotalTokensInput,
		TotalTokensOutput:         update.TotalTokensOutput,
		WorkingSecondsAccumulated: update.WorkingSecondsAccumulated,
		LastPushAt:                update.LastPushAt,
	}
	return fresh, true, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanProjection(scan scanFn) (*projection.TaskExecutionProjection, error) {
	var (
		id        string
		curAct    sql.NullString
		curActAt  sql.NullString
		toolCalls int64
		toksIn    int64
		toksOut   int64
		workSec   int64
		lastPush  string
	)
	if err := scan(&id, &curAct, &curActAt, &toolCalls, &toksIn, &toksOut, &workSec, &lastPush); err != nil {
		return nil, err
	}
	p := &projection.TaskExecutionProjection{
		TaskExecutionID:           taskruntime.TaskExecutionID(id),
		TotalToolCalls:            toolCalls,
		TotalTokensInput:          toksIn,
		TotalTokensOutput:         toksOut,
		WorkingSecondsAccumulated: workSec,
	}
	if curAct.Valid {
		p.CurrentActivity = curAct.String
	}
	if curActAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, curActAt.String); err == nil {
			p.CurrentActivityAt = t
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, lastPush); err == nil {
		p.LastPushAt = t
	}
	return p, nil
}
