package sqlite

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/usage"
)

// AgentActivityDailyRepo is the SQLite-backed usage.AgentActivityDailyRepository
// over the agent_activity_daily rollup table (migration 0078).
type AgentActivityDailyRepo struct {
	db *sql.DB
}

// NewAgentActivityDailyRepo constructs the repo.
func NewAgentActivityDailyRepo(db *sql.DB) *AgentActivityDailyRepo {
	return &AgentActivityDailyRepo{db: db}
}

// Upsert inserts or replaces each rollup row keyed by (agent_ref, day,
// project_id). ON CONFLICT replaces every measure — the job recomputes a bucket
// in full, so this is a replace (not an accumulate). Runs under the caller's
// ambient tx when one is set.
func (r *AgentActivityDailyRepo) Upsert(ctx context.Context, rows ...usage.AgentActivityDaily) error {
	if len(rows) == 0 {
		return nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	for _, row := range rows {
		if _, err := exec.ExecContext(ctx,
			`INSERT INTO agent_activity_daily
			   (agent_ref, day, project_id, events_count, tokens_in, tokens_out, cache_tokens, cost_micros)
			 VALUES (?,?,?,?,?,?,?,?)
			 ON CONFLICT(agent_ref, day, project_id) DO UPDATE SET
			   events_count = excluded.events_count,
			   tokens_in    = excluded.tokens_in,
			   tokens_out   = excluded.tokens_out,
			   cache_tokens = excluded.cache_tokens,
			   cost_micros  = excluded.cost_micros`,
			row.AgentRef, row.Day, row.ProjectID,
			row.EventsCount, row.TokensIn, row.TokensOut, row.CacheTokens, row.CostMicros); err != nil {
			return err
		}
	}
	return nil
}

// ListByAgent returns agentRef's rows with fromDay <= day <= toDay (inclusive),
// ordered by (day, project_id). Empty bounds are open-ended on that side. Day
// bounds are "YYYY-MM-DD" strings compared lexicographically (valid for the fixed
// ISO date format).
func (r *AgentActivityDailyRepo) ListByAgent(ctx context.Context, agentRef, fromDay, toDay string) ([]usage.AgentActivityDaily, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	q := `SELECT agent_ref, day, project_id, events_count, tokens_in, tokens_out, cache_tokens, cost_micros
	        FROM agent_activity_daily
	       WHERE agent_ref = ?`
	args := []any{agentRef}
	if fromDay != "" {
		q += ` AND day >= ?`
		args = append(args, fromDay)
	}
	if toDay != "" {
		q += ` AND day <= ?`
		args = append(args, toDay)
	}
	q += ` ORDER BY day, project_id`

	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.AgentActivityDaily
	for rows.Next() {
		var a usage.AgentActivityDaily
		if err := rows.Scan(&a.AgentRef, &a.Day, &a.ProjectID,
			&a.EventsCount, &a.TokensIn, &a.TokensOut, &a.CacheTokens, &a.CostMicros); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

var _ usage.AgentActivityDailyRepository = (*AgentActivityDailyRepo)(nil)
