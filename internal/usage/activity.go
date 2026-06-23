package usage

import "context"

// AgentActivityDaily is one (agent_ref, day, project_id) rollup row — the
// pre-aggregated read model behind the per-agent dashboard (v2.15.0 I28/F3). It
// fuses two measures over the same grain: an activity count (events_count, from
// task_action_logs / messages / issues / plans / commits) and the token/cost
// totals (from usage_events).
//
// Day is a UTC calendar date "YYYY-MM-DD". ProjectID is "" for the no-project /
// interaction bucket (never a NULL — the column is NOT NULL and "" is the
// consistent sentinel, mirroring usage_events). CacheTokens is the COMBINED
// cache_read + cache_write count (the rollup does not split them).
type AgentActivityDaily struct {
	AgentRef    string // canonical "agent:<id>" ref
	Day         string // "YYYY-MM-DD" UTC
	ProjectID   string // "" = no-project / interaction bucket
	EventsCount int64
	TokensIn    int64
	TokensOut   int64
	CacheTokens int64
	CostMicros  int64
}

// AgentActivityDailyRepository persists the daily rollup and serves the
// dashboard's per-agent time-series read.
type AgentActivityDailyRepository interface {
	// Upsert inserts or replaces rows keyed by (agent_ref, day, project_id). The
	// rollup job recomputes a bucket from scratch and re-asserts it, so Upsert is
	// idempotent on the key (replace, not accumulate).
	Upsert(ctx context.Context, rows ...AgentActivityDaily) error
	// ListByAgent returns agentRef's rows with fromDay <= day <= toDay (inclusive
	// calendar-date bounds, "YYYY-MM-DD"), ordered by (day, project_id). An empty
	// toDay means open-ended; an empty fromDay means from the beginning.
	ListByAgent(ctx context.Context, agentRef, fromDay, toDay string) ([]AgentActivityDaily, error)
}
