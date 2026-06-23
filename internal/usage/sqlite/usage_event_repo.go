package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/usage"
)

// UsageEventRepo is the SQLite-backed usage.UsageEventRepository over the
// usage_events table (migration 0077).
type UsageEventRepo struct {
	db *sql.DB
}

// NewUsageEventRepo constructs the repo.
func NewUsageEventRepo(db *sql.DB) *UsageEventRepo { return &UsageEventRepo{db: db} }

// Append validates and inserts each event under the caller's ambient tx (if any).
func (r *UsageEventRepo) Append(ctx context.Context, events ...usage.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return err
		}
		if _, err := exec.ExecContext(ctx,
			`INSERT INTO usage_events
			   (id, agent_ref, project_id, task_id, model,
			    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			    cost_micros, ts, source)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			e.ID, e.AgentRef, e.ProjectID, nullString(e.TaskID), e.Model,
			e.Tokens.Input, e.Tokens.Output, e.Tokens.CacheRead, e.Tokens.CacheWrite,
			e.CostMicros, ts(e.TS), string(e.Source)); err != nil {
			return err
		}
	}
	return nil
}

// ListByAgent returns agentRef's events with from <= ts < to, ordered by ts. A
// zero `to` means open-ended (no upper bound).
func (r *UsageEventRepo) ListByAgent(ctx context.Context, agentRef string, from, to time.Time) ([]usage.UsageEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	q := `SELECT id, agent_ref, project_id, task_id, model,
	             input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
	             cost_micros, ts, source
	        FROM usage_events
	       WHERE agent_ref = ? AND ts >= ?`
	args := []any{agentRef, ts(from)}
	if !to.IsZero() {
		q += ` AND ts < ?`
		args = append(args, ts(to))
	}
	q += ` ORDER BY ts, id`
	return r.query(ctx, exec, q, args...)
}

// ListByTask returns taskID's events ordered by ts.
func (r *UsageEventRepo) ListByTask(ctx context.Context, taskID string) ([]usage.UsageEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	return r.query(ctx, exec,
		`SELECT id, agent_ref, project_id, task_id, model,
		        input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
		        cost_micros, ts, source
		   FROM usage_events WHERE task_id = ? ORDER BY ts, id`, taskID)
}

func (r *UsageEventRepo) query(ctx context.Context, exec persistence.SQLExecutor, q string, args ...any) ([]usage.UsageEvent, error) {
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.UsageEvent
	for rows.Next() {
		var e usage.UsageEvent
		var taskID sql.NullString
		var tsStr, source string
		if err := rows.Scan(&e.ID, &e.AgentRef, &e.ProjectID, &taskID, &e.Model,
			&e.Tokens.Input, &e.Tokens.Output, &e.Tokens.CacheRead, &e.Tokens.CacheWrite,
			&e.CostMicros, &tsStr, &source); err != nil {
			return nil, err
		}
		e.TaskID = strOrEmpty(taskID)
		e.TS = parseTime(tsStr)
		e.Source = usage.Source(source)
		out = append(out, e)
	}
	return out, rows.Err()
}

var _ usage.UsageEventRepository = (*UsageEventRepo)(nil)
