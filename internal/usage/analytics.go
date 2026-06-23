package usage

import (
	"context"
	"time"
)

// This file is the F4 (issue-a7ff560e, v2.15.0) read side of the per-agent
// analytics dashboard: the view-model types the HTTP layer serializes and the
// AnalyticsService contract the dashboard reads through.
//
// READ-MODEL SPLIT (PD-pinned, F3 grain). agent_activity_daily is keyed by
// (agent_ref, day, project_id) and has NO model column, so:
//   · heatmap / overview cards / PROJECT-dimension trend  → agent_activity_daily
//     (the pre-aggregated fast path).
//   · MODEL-dimension trend + Top-Cost-Tasks drill-down    → usage_events, the
//     raw table that carries model + task_id, aggregated on read.
// Completed-task counts come from pm_task_action_logs (action='completed'),
// since pm_tasks records completed_by but no completion timestamp.

// HeatmapCell is one UTC day's activity summed across all of an agent's projects
// — the contribution-graph source. Every measure is carried so the FE can switch
// the heatmap 口径 (activity-count / tokens / cost) without a refetch.
type HeatmapCell struct {
	Day         string // "YYYY-MM-DD" UTC
	Events      int64
	TokensIn    int64
	TokensOut   int64
	CacheTokens int64
	CostMicros  int64
}

// WindowStat is the token/cost/completed-task totals over one time window — the
// per-card figure for today / week / month.
type WindowStat struct {
	TokensIn       int64
	TokensOut      int64
	CacheTokens    int64
	CostMicros     int64
	CompletedTasks int64
}

// Overview is the overview-card payload: rolling today / last-7-days /
// last-30-days windows plus the activity-streak figures over the 12-month
// window, all derived from the daily rollup (+ completion counts).
type Overview struct {
	Today      WindowStat // the single current UTC day
	Week       WindowStat // rolling last 7 days, inclusive of today
	Month      WindowStat // rolling last 30 days, inclusive of today
	ActiveDays int        // distinct days with any activity over the 12-month window
	Streak     int        // consecutive days with activity ending at today (0 if today is idle)
}

// ProjectTrendPoint is one (day, project) stacked-trend point — read from the
// rollup, where the project dimension lives.
type ProjectTrendPoint struct {
	Day         string
	ProjectID   string
	Events      int64
	TokensIn    int64
	TokensOut   int64
	CacheTokens int64
	CostMicros  int64
}

// ModelTrendPoint is one (day, model) stacked-trend point — computed from
// usage_events (the model dimension is deliberately absent from the rollup).
type ModelTrendPoint struct {
	Day         string
	Model       string
	TokensIn    int64
	TokensOut   int64
	CacheTokens int64
	CostMicros  int64
}

// TaskCost is one task's usage totals — a Top-Cost-Tasks ranking row computed
// from usage_events grouped by task_id. Events is the number of usage events
// (turns) charged to the task; drill into the raw events via TaskDrilldown.
type TaskCost struct {
	TaskID      string
	Events      int64
	TokensIn    int64
	TokensOut   int64
	CacheTokens int64
	CostMicros  int64
}

// AnalyticsService serves the per-agent dashboard reads (F4). agentRef is the
// canonical "agent:<member-id>" form (matching usage_events / the rollup). Date
// bounds are inclusive UTC "YYYY-MM-DD" calendar dates on the rollup grain.
type AnalyticsService interface {
	// Heatmap returns one cell per active UTC day in [fromDay, toDay], summed
	// across projects. Days with no activity are omitted (the FE renders the full
	// 53×7 grid and fills gaps with zero).
	Heatmap(ctx context.Context, agentRef, fromDay, toDay string) ([]HeatmapCell, error)
	// Overview computes the today/week/month cards + active-days/streak relative
	// to now (UTC calendar date of now is "today").
	Overview(ctx context.Context, agentRef string, now time.Time) (Overview, error)
	// ProjectTrend returns per-(day, project) points in [fromDay, toDay] from the
	// rollup, ordered by (day, project_id).
	ProjectTrend(ctx context.Context, agentRef, fromDay, toDay string) ([]ProjectTrendPoint, error)
	// ModelTrend returns per-(day, model) points in [fromDay, toDay] from
	// usage_events, ordered by (day, model).
	ModelTrend(ctx context.Context, agentRef, fromDay, toDay string) ([]ModelTrendPoint, error)
	// TopTasks returns the agent's task-scoped usage rolled up by task_id and
	// ranked by cost desc (then task_id), capped at limit (limit<=0 → a default).
	TopTasks(ctx context.Context, agentRef, fromDay, toDay string, limit int) ([]TaskCost, error)
	// TaskDrilldown returns the raw usage events for a task, ordered by ts — the
	// detail behind a Top-Cost-Tasks row.
	TaskDrilldown(ctx context.Context, taskID string) ([]UsageEvent, error)
}
