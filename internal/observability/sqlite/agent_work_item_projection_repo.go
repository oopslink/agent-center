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
)

// AgentWorkItemProjectionRepo is the SQLite-backed implementation of
// observability/projection.AgentWorkItemProjectionRepository, owning the
// agent_work_item_projections table (mig 0046; conventions § 9.z BC physical
// isolation). The new-model equivalent of ProjectionRepo.
type AgentWorkItemProjectionRepo struct {
	db *sql.DB
}

// NewAgentWorkItemProjectionRepo constructs a repo over the given *sql.DB.
func NewAgentWorkItemProjectionRepo(db *sql.DB) *AgentWorkItemProjectionRepo {
	return &AgentWorkItemProjectionRepo{db: db}
}

var _ projection.AgentWorkItemProjectionRepository = (*AgentWorkItemProjectionRepo)(nil)

// FindByID returns the projection row for the given work item.
func (r *AgentWorkItemProjectionRepo) FindByID(ctx context.Context, workItemID string) (*projection.AgentWorkItemProjection, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const stmt = `SELECT work_item_id, agent_id, status, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_activity_at
        FROM agent_work_item_projections
        WHERE work_item_id = ?`
	row := exec.QueryRowContext(ctx, stmt, workItemID)
	p, err := scanAgentWorkItemProjection(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projection.ErrProjectionNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// FindByIDs returns projections for the given work items in any order.
// IDs without a row are simply absent from the result map.
func (r *AgentWorkItemProjectionRepo) FindByIDs(ctx context.Context, workItemIDs []string) (map[string]*projection.AgentWorkItemProjection, error) {
	out := map[string]*projection.AgentWorkItemProjection{}
	if len(workItemIDs) == 0 {
		return out, nil
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	placeholders := make([]string, len(workItemIDs))
	args := make([]any, len(workItemIDs))
	for i, id := range workItemIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	stmt := fmt.Sprintf(`SELECT work_item_id, agent_id, status, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_activity_at
        FROM agent_work_item_projections
        WHERE work_item_id IN (%s)`, strings.Join(placeholders, ","))
	rows, err := exec.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanAgentWorkItemProjection(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[p.WorkItemID] = p
	}
	return out, rows.Err()
}

// UpsertIfFresh writes the row only if no stored row exists OR the stored
// row's last_activity_at is strictly older than update.LastActivityAt. When
// the stored row is fresher or equal it returns ErrProjectionStale and the
// caller is expected to drop the write.
func (r *AgentWorkItemProjectionRepo) UpsertIfFresh(ctx context.Context, workItemID string, update projection.AgentWorkItemProjectionUpdate) (projection.AgentWorkItemProjection, bool, error) {
	if err := update.Validate(); err != nil {
		return projection.AgentWorkItemProjection{}, false, err
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return projection.AgentWorkItemProjection{}, false, err
	}
	// SELECT existing → if newer, drop.
	const sel = `SELECT agent_id, status, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_activity_at
        FROM agent_work_item_projections WHERE work_item_id = ?`
	row := exec.QueryRowContext(ctx, sel, workItemID)
	var (
		agentID   string
		status    string
		curAct    sql.NullString
		curActAt  sql.NullString
		toolCalls int64
		toksIn    int64
		toksOut   int64
		workSec   int64
		lastAct   string
	)
	err = row.Scan(&agentID, &status, &curAct, &curActAt, &toolCalls, &toksIn, &toksOut, &workSec, &lastAct)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return projection.AgentWorkItemProjection{}, false, err
	}
	existing := projection.AgentWorkItemProjection{WorkItemID: workItemID}
	hasExisting := err == nil
	if hasExisting {
		existing.AgentID = agentID
		existing.Status = status
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
		if t, perr := time.Parse(time.RFC3339Nano, lastAct); perr == nil {
			existing.LastActivityAt = t
		}
		if !existing.LastActivityAt.Before(update.LastActivityAt) {
			return existing, false, projection.ErrProjectionStale
		}
	}
	// UPSERT
	const ups = `INSERT INTO agent_work_item_projections (
        work_item_id, agent_id, status, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_activity_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(work_item_id) DO UPDATE SET
        agent_id = excluded.agent_id,
        status = excluded.status,
        current_activity = excluded.current_activity,
        current_activity_at = excluded.current_activity_at,
        total_tool_calls = excluded.total_tool_calls,
        total_tokens_input = excluded.total_tokens_input,
        total_tokens_output = excluded.total_tokens_output,
        working_seconds_accumulated = excluded.working_seconds_accumulated,
        last_activity_at = excluded.last_activity_at`
	var actAtArg any
	if !update.CurrentActivityAt.IsZero() {
		actAtArg = update.CurrentActivityAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = exec.ExecContext(ctx, ups,
		workItemID,
		update.AgentID,
		update.Status,
		nullableString(update.CurrentActivity),
		actAtArg,
		update.TotalToolCalls,
		update.TotalTokensInput,
		update.TotalTokensOutput,
		update.WorkingSecondsAccumulated,
		update.LastActivityAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return projection.AgentWorkItemProjection{}, false, err
	}
	fresh := projection.AgentWorkItemProjection{
		WorkItemID:                workItemID,
		AgentID:                   update.AgentID,
		Status:                    update.Status,
		CurrentActivity:           update.CurrentActivity,
		CurrentActivityAt:         update.CurrentActivityAt,
		TotalToolCalls:            update.TotalToolCalls,
		TotalTokensInput:          update.TotalTokensInput,
		TotalTokensOutput:         update.TotalTokensOutput,
		WorkingSecondsAccumulated: update.WorkingSecondsAccumulated,
		LastActivityAt:            update.LastActivityAt,
	}
	return fresh, true, nil
}

// List returns projection rows matching filter, ORDER BY last_activity_at DESC
// (index-backed by idx_awip_last_active). Status/agent filters are index-backed
// (idx_awip_status / idx_awip_agent). Empty filter returns all rows.
func (r *AgentWorkItemProjectionRepo) List(ctx context.Context, filter projection.AgentWorkItemProjectionFilter) ([]*projection.AgentWorkItemProjection, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	var where []string
	var args []any
	if len(filter.Statuses) > 0 {
		ph := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			ph[i] = "?"
			args = append(args, s)
		}
		where = append(where, "status IN ("+strings.Join(ph, ",")+")")
	}
	if filter.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	stmt := `SELECT work_item_id, agent_id, status, current_activity, current_activity_at,
        total_tool_calls, total_tokens_input, total_tokens_output,
        working_seconds_accumulated, last_activity_at
        FROM agent_work_item_projections`
	if len(where) > 0 {
		stmt += " WHERE " + strings.Join(where, " AND ")
	}
	stmt += " ORDER BY last_activity_at DESC"
	rows, err := exec.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*projection.AgentWorkItemProjection
	for rows.Next() {
		p, err := scanAgentWorkItemProjection(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanAgentWorkItemProjection(scan scanFn) (*projection.AgentWorkItemProjection, error) {
	var (
		workItemID string
		agentID    string
		status     string
		curAct     sql.NullString
		curActAt   sql.NullString
		toolCalls  int64
		toksIn     int64
		toksOut    int64
		workSec    int64
		lastAct    string
	)
	if err := scan(&workItemID, &agentID, &status, &curAct, &curActAt, &toolCalls, &toksIn, &toksOut, &workSec, &lastAct); err != nil {
		return nil, err
	}
	p := &projection.AgentWorkItemProjection{
		WorkItemID:                workItemID,
		AgentID:                   agentID,
		Status:                    status,
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
	if t, err := time.Parse(time.RFC3339Nano, lastAct); err == nil {
		p.LastActivityAt = t
	}
	return p, nil
}
