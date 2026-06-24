package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/usage"
)

// Analytics is the SQLite-backed usage.AnalyticsService — the F4 read side of the
// per-agent dashboard (issue-a7ff560e, v2.15.0). It aggregates on read across
// three tables per the PD-pinned read-model split (see usage/analytics.go):
//
//	· agent_activity_daily — heatmap, overview cards, project-dimension trend
//	  (the pre-aggregated fast path; summed across projects where needed).
//	· usage_events         — model-dimension trend + Top-Cost-Tasks (the raw
//	  table carries the model + task_id the rollup intentionally drops).
//	· pm_task_action_logs  — completed-task counts (action='completed'); read
//	  directly here exactly as rollup.go reads the PM tables for activity counts.
//
// Day bounds are inclusive UTC "YYYY-MM-DD" strings compared lexicographically
// (valid for the fixed ISO format) against day/substr(ts,1,10), mirroring the
// rollup's UTC-normalized-RFC3339 convention.
type Analytics struct {
	db *sql.DB
}

// NewAnalytics constructs the read service over db.
func NewAnalytics(db *sql.DB) *Analytics { return &Analytics{db: db} }

const defaultTopTasksLimit = 20

// the action-log action that marks a task completion (pm.TaskActionCompleted);
// duplicated as a literal to avoid a usage→projectmanager import for one string.
const actionCompleted = "completed"

// Heatmap sums each UTC day's rollup rows across projects, in [fromDay, toDay],
// and folds in that day's task-completion count (Completed) so the dashboard can
// derive every overview card + delta from this single per-day series. A day with
// activity but no completions reports Completed=0; a completion always coincides
// with an activity row (the completion IS a task_action_log the rollup counts),
// but a completed-only day is still surfaced defensively.
func (a *Analytics) Heatmap(ctx context.Context, agentRef, fromDay, toDay string) ([]usage.HeatmapCell, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, a.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT day,
		        COALESCE(SUM(events_count),0), COALESCE(SUM(tokens_in),0), COALESCE(SUM(tokens_out),0),
		        COALESCE(SUM(cache_tokens),0), COALESCE(SUM(cost_micros),0)
		   FROM agent_activity_daily
		  WHERE agent_ref = ? AND day >= ? AND day <= ?
		  GROUP BY day ORDER BY day`, agentRef, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]*usage.HeatmapCell{}
	order := []string{}
	for rows.Next() {
		var c usage.HeatmapCell
		if err := rows.Scan(&c.Day, &c.Events, &c.TokensIn, &c.TokensOut, &c.CacheTokens, &c.CostMicros); err != nil {
			return nil, err
		}
		cc := c
		byDay[c.Day] = &cc
		order = append(order, c.Day)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	completed, err := a.completedByDay(ctx, exec, agentRef, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	for day, n := range completed {
		if cell := byDay[day]; cell != nil {
			cell.Completed = n
		} else {
			byDay[day] = &usage.HeatmapCell{Day: day, Completed: n}
			order = append(order, day)
		}
	}
	sort.Strings(order)

	out := make([]usage.HeatmapCell, 0, len(order))
	for _, day := range order {
		out = append(out, *byDay[day])
	}
	return out, nil
}

// Overview computes today / last-7-days / last-30-days windows + 12-month
// active-days/streak relative to now's UTC calendar date. It reads the per-day
// rollup once over the year window and the completed-task days once, then folds
// both in Go.
func (a *Analytics) Overview(ctx context.Context, agentRef string, now time.Time) (usage.Overview, error) {
	today := now.UTC().Format("2006-01-02")
	weekStart := dayString(now, -6)   // 7 days inclusive of today
	monthStart := dayString(now, -29) // 30 days inclusive of today
	yearStart := dayString(now, -364) // 365 days inclusive of today

	cells, err := a.Heatmap(ctx, agentRef, yearStart, today)
	if err != nil {
		return usage.Overview{}, err
	}
	completedExec, _ := persistence.ExecutorFromCtx(ctx, a.db)
	completed, err := a.completedByDay(ctx, completedExec, agentRef, yearStart, today)
	if err != nil {
		return usage.Overview{}, err
	}

	var ov usage.Overview
	active := map[string]bool{}
	for _, c := range cells {
		if c.Events > 0 || c.TokensIn > 0 || c.TokensOut > 0 || c.CacheTokens > 0 || c.CostMicros > 0 {
			active[c.Day] = true
		}
		addWindow(&ov.Today, c, completed[c.Day], c.Day == today)
		addWindow(&ov.Week, c, completed[c.Day], c.Day >= weekStart)
		addWindow(&ov.Month, c, completed[c.Day], c.Day >= monthStart)
	}
	// completed-task days with no usage row still count toward the windows.
	for day, n := range completed {
		hasCell := false
		for _, c := range cells {
			if c.Day == day {
				hasCell = true
				break
			}
		}
		if hasCell {
			continue
		}
		active[day] = true
		if day == today {
			ov.Today.CompletedTasks += n
		}
		if day >= weekStart {
			ov.Week.CompletedTasks += n
		}
		if day >= monthStart {
			ov.Month.CompletedTasks += n
		}
	}
	ov.ActiveDays = len(active)
	ov.Streak = streak(active, now)
	return ov, nil
}

// addWindow folds one day's measures into a window stat when inWindow.
func addWindow(w *usage.WindowStat, c usage.HeatmapCell, completed int64, inWindow bool) {
	if !inWindow {
		return
	}
	w.TokensIn += c.TokensIn
	w.TokensOut += c.TokensOut
	w.CacheTokens += c.CacheTokens
	w.CostMicros += c.CostMicros
	w.CompletedTasks += completed
}

// streak counts consecutive active days ending at now's UTC date (0 if today is
// idle).
func streak(active map[string]bool, now time.Time) int {
	n := 0
	for d := now.UTC(); ; d = d.AddDate(0, 0, -1) {
		if !active[d.Format("2006-01-02")] {
			break
		}
		n++
	}
	return n
}

// completedByDay returns per-UTC-day counts of this agent's task completions
// (pm_task_action_logs action='completed') in [fromDay, toDay].
func (a *Analytics) completedByDay(ctx context.Context, exec persistence.SQLExecutor, agentRef, fromDay, toDay string) (map[string]int64, error) {
	rows, err := exec.QueryContext(ctx,
		`SELECT substr(occurred_at,1,10) AS day, COUNT(*)
		   FROM pm_task_action_logs
		  WHERE agent_ref = ? AND action = ?
		    AND substr(occurred_at,1,10) >= ? AND substr(occurred_at,1,10) <= ?
		  GROUP BY day`, agentRef, actionCompleted, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var day string
		var n int64
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}
		out[day] = n
	}
	return out, rows.Err()
}

// ProjectTrend returns per-(day, project) rollup points in [fromDay, toDay].
func (a *Analytics) ProjectTrend(ctx context.Context, agentRef, fromDay, toDay string) ([]usage.ProjectTrendPoint, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, a.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT day, project_id, events_count, tokens_in, tokens_out, cache_tokens, cost_micros
		   FROM agent_activity_daily
		  WHERE agent_ref = ? AND day >= ? AND day <= ?
		  ORDER BY day, project_id`, agentRef, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.ProjectTrendPoint
	for rows.Next() {
		var p usage.ProjectTrendPoint
		if err := rows.Scan(&p.Day, &p.ProjectID, &p.Events, &p.TokensIn, &p.TokensOut, &p.CacheTokens, &p.CostMicros); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ModelTrend returns per-(day, model) points from usage_events in [fromDay, toDay]
// — the model dimension the rollup omits, aggregated on read.
func (a *Analytics) ModelTrend(ctx context.Context, agentRef, fromDay, toDay string) ([]usage.ModelTrendPoint, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, a.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT substr(ts,1,10) AS day, model,
		        COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(cache_read_tokens + cache_write_tokens),0), COALESCE(SUM(cost_micros),0)
		   FROM usage_events
		  WHERE agent_ref = ? AND substr(ts,1,10) >= ? AND substr(ts,1,10) <= ?
		  GROUP BY day, model ORDER BY day, model`, agentRef, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.ModelTrendPoint
	for rows.Next() {
		var p usage.ModelTrendPoint
		if err := rows.Scan(&p.Day, &p.Model, &p.TokensIn, &p.TokensOut, &p.CacheTokens, &p.CostMicros); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// TopTasks ranks the agent's task-scoped usage by cost desc (then task_id) over
// [fromDay, toDay], capped at limit (<=0 → defaultTopTasksLimit).
func (a *Analytics) TopTasks(ctx context.Context, agentRef, fromDay, toDay string, limit int) ([]usage.TaskCost, error) {
	if limit <= 0 {
		limit = defaultTopTasksLimit
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, a.db)
	// Title + org_number via LEFT JOIN pm_tasks: a deleted / cross-project-unresolved
	// task keeps its usage row but yields no title/number → "" / 0 (the UI falls back
	// to the task_id). MAX(...) collapses the per-task-constant columns under GROUP BY.
	rows, err := exec.QueryContext(ctx,
		`SELECT ue.task_id, COALESCE(MAX(t.title),''), COALESCE(MAX(t.org_number),0), COUNT(*),
		        COALESCE(SUM(ue.input_tokens),0), COALESCE(SUM(ue.output_tokens),0),
		        COALESCE(SUM(ue.cache_read_tokens + ue.cache_write_tokens),0), COALESCE(SUM(ue.cost_micros),0)
		   FROM usage_events ue LEFT JOIN pm_tasks t ON ue.task_id = t.id
		  WHERE ue.agent_ref = ? AND ue.task_id IS NOT NULL AND ue.task_id != ''
		    AND substr(ue.ts,1,10) >= ? AND substr(ue.ts,1,10) <= ?
		  GROUP BY ue.task_id
		  ORDER BY SUM(ue.cost_micros) DESC, ue.task_id
		  LIMIT ?`, agentRef, fromDay, toDay, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.TaskCost
	for rows.Next() {
		var tc usage.TaskCost
		var orgNumber int64
		if err := rows.Scan(&tc.TaskID, &tc.Title, &orgNumber, &tc.Events, &tc.TokensIn, &tc.TokensOut, &tc.CacheTokens, &tc.CostMicros); err != nil {
			return nil, err
		}
		if orgNumber > 0 { // v2.7.1 #245: "T<n>" human ref; omitted when unallocated
			tc.OrgRef = "T" + strconv.FormatInt(orgNumber, 10)
		}
		out = append(out, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Dominant model per task: the model carrying the most cost on that task.
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].TaskID
	}
	dom, err := a.dominantModels(ctx, exec, agentRef, fromDay, toDay, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].DominantModel = dom[out[i].TaskID]
	}
	return out, nil
}

// dominantModels returns, for each task_id in ids, the model accounting for the
// most cost_micros on that task within [fromDay, toDay] (ties broken by the
// max-cost row encountered). Empty ids → empty map.
func (a *Analytics) dominantModels(ctx context.Context, exec persistence.SQLExecutor, agentRef, fromDay, toDay string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids)+3)
	args = append(args, agentRef)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, fromDay, toDay)
	q := `SELECT task_id, model, COALESCE(SUM(cost_micros),0)
	        FROM usage_events
	       WHERE agent_ref = ? AND task_id IN (` + placeholders(len(ids)) + `)
	         AND substr(ts,1,10) >= ? AND substr(ts,1,10) <= ?
	       GROUP BY task_id, model`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	best := map[string]int64{}
	for rows.Next() {
		var taskID, model string
		var cost int64
		if err := rows.Scan(&taskID, &model, &cost); err != nil {
			return nil, err
		}
		if _, seen := best[taskID]; !seen || cost > best[taskID] {
			best[taskID] = cost
			out[taskID] = model
		}
	}
	return out, rows.Err()
}

// placeholders returns "?,?,...," with n question marks for an IN clause.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// TaskDrilldown returns the raw usage events for a task ordered by ts — delegates
// to the usage_events repo's ListByTask.
func (a *Analytics) TaskDrilldown(ctx context.Context, taskID string) ([]usage.UsageEvent, error) {
	return NewUsageEventRepo(a.db).ListByTask(ctx, taskID)
}

// dayString returns the UTC calendar date of now shifted by delta days.
func dayString(now time.Time, delta int) string {
	return now.UTC().AddDate(0, 0, delta).Format("2006-01-02")
}

var _ usage.AnalyticsService = (*Analytics)(nil)
