package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/usage"
)

// appendUsageTask appends a task-scoped usage event (TaskID set) so the
// Top-Cost-Tasks + model-trend reads have raw rows to aggregate.
func appendUsageTask(t *testing.T, env rollupEnv, id, agentRef, project, taskID, model, at string, in, out, cr, cw, cost int64) {
	t.Helper()
	ev := usage.UsageEvent{
		ID: id, AgentRef: agentRef, ProjectID: project, TaskID: taskID, Model: model,
		Tokens:     usage.TokenCounts{Input: in, Output: out, CacheRead: cr, CacheWrite: cw},
		CostMicros: cost, TS: tm(at), Source: usage.SourceReport,
	}
	if err := env.events.Append(env.ctx, ev); err != nil {
		t.Fatal(err)
	}
}

// completedLog writes a pm_task_action_logs row with action='completed' — the
// timestamped source for the overview "completed tasks" figure (pm_tasks has
// completed_by but no completion timestamp).
func completedLog(t *testing.T, env rollupEnv, id, taskID, agentRef, at string) {
	t.Helper()
	mustExec(t, env.db, `INSERT INTO pm_task_action_logs (id, task_id, occurred_at, action, actor_ref, agent_ref, note)
	                     VALUES (?,?,?,?,?,?,'')`, id, taskID, at, "completed", agentRef, agentRef)
}

// runRollup folds raw rows into agent_activity_daily so the rollup-backed reads
// (heatmap / cards / project-trend) have data.
func runRollup(t *testing.T, env rollupEnv) {
	t.Helper()
	if _, err := env.roll.RunIncremental(env.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyticsHeatmap(t *testing.T) {
	env := setupRollup(t)
	an := NewAnalytics(env.db)
	const ag = "agent:agent-hm"
	// Two projects on the same day must SUM into one heatmap cell.
	appendUsage(t, env, "u1", ag, "p1", "claude-opus-4-8", "2026-06-20T10:00:00Z", 100, 50, 10, 5)
	appendUsage(t, env, "u2", ag, "p2", "claude-opus-4-8", "2026-06-20T11:00:00Z", 200, 80, 0, 0)
	appendUsage(t, env, "u3", ag, "p1", "claude-opus-4-8", "2026-06-21T09:00:00Z", 30, 10, 0, 0)
	// two task completions on 06-20 → that cell's Completed = 2 (folded from
	// pm_task_action_logs); 06-21 has none → Completed 0.
	insertTask(t, env.db, "task-c", "p1", ag, "2026-06-20T08:00:00Z")
	completedLog(t, env, "cl-a", "task-c", ag, "2026-06-20T10:30:00Z")
	completedLog(t, env, "cl-b", "task-c", ag, "2026-06-20T12:00:00Z")
	runRollup(t, env)

	cells, err := an.Heatmap(env.ctx, ag, "2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 2 {
		t.Fatalf("cells = %d, want 2 (06-20, 06-21): %+v", len(cells), cells)
	}
	if cells[0].Day != "2026-06-20" || cells[0].TokensIn != 300 || cells[0].TokensOut != 130 || cells[0].CacheTokens != 15 {
		t.Fatalf("06-20 cell wrong (want in=300 out=130 cache=15): %+v", cells[0])
	}
	if cells[0].Completed != 2 {
		t.Fatalf("06-20 Completed = %d, want 2 (two completion logs folded in)", cells[0].Completed)
	}
	// 06-21 has usage but no completions → tokens sum, Completed 0.
	if cells[1].Day != "2026-06-21" || cells[1].TokensIn != 30 || cells[1].Completed != 0 {
		t.Fatalf("06-21 cell wrong (want in=30 completed=0): %+v", cells[1])
	}
	// Range filter excludes out-of-window days.
	none, err := an.Heatmap(env.ctx, ag, "2026-07-01", "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("out-of-range want 0 cells, got %+v", none)
	}
}

func TestAnalyticsOverview(t *testing.T) {
	env := setupRollup(t)
	an := NewAnalytics(env.db)
	const ag = "agent:agent-ov"
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) // "today" = 2026-06-22

	// today (06-22): usage + 1 completed task.
	insertTask(t, env.db, "task-t", "p1", ag, "2026-06-22T08:00:00Z")
	appendUsage(t, env, "u-today", ag, "p1", "claude-opus-4-8", "2026-06-22T10:00:00Z", 100, 0, 0, 0)
	completedLog(t, env, "cl-1", "task-t", ag, "2026-06-22T11:00:00Z")
	// in week, not today (06-19): usage + 1 completed.
	appendUsage(t, env, "u-wk", ag, "p1", "claude-opus-4-8", "2026-06-19T10:00:00Z", 50, 0, 0, 0)
	completedLog(t, env, "cl-2", "task-t", ag, "2026-06-19T10:30:00Z")
	// in month, not week (06-05): usage only.
	appendUsage(t, env, "u-mo", ag, "p1", "claude-opus-4-8", "2026-06-05T10:00:00Z", 25, 0, 0, 0)
	// in year, not month (2026-04-01): usage + 1 completed.
	appendUsage(t, env, "u-yr", ag, "p1", "claude-opus-4-8", "2026-04-01T10:00:00Z", 7, 0, 0, 0)
	completedLog(t, env, "cl-3", "task-t", ag, "2026-04-01T10:00:00Z")
	runRollup(t, env)

	ov, err := an.Overview(env.ctx, ag, now)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Today.TokensIn != 100 || ov.Today.CompletedTasks != 1 {
		t.Fatalf("today wrong (want in=100 completed=1): %+v", ov.Today)
	}
	// week = last 7 days incl today: 06-22 (100) + 06-19 (50) = 150; completed = 2.
	if ov.Week.TokensIn != 150 || ov.Week.CompletedTasks != 2 {
		t.Fatalf("week wrong (want in=150 completed=2): %+v", ov.Week)
	}
	// month = last 30 days incl today: 100 + 50 + 25 = 175; completed = 2 (04-01 excluded).
	if ov.Month.TokensIn != 175 || ov.Month.CompletedTasks != 2 {
		t.Fatalf("month wrong (want in=175 completed=2): %+v", ov.Month)
	}
	// active days over the 12-month window: 06-22, 06-19, 06-05, 04-01 = 4.
	if ov.ActiveDays != 4 {
		t.Fatalf("active days = %d, want 4", ov.ActiveDays)
	}
	// streak: today active, 06-21 idle → streak = 1 (only today).
	if ov.Streak != 1 {
		t.Fatalf("streak = %d, want 1", ov.Streak)
	}
}

func TestAnalyticsOverviewStreakMultiDay(t *testing.T) {
	env := setupRollup(t)
	an := NewAnalytics(env.db)
	const ag = "agent:agent-streak"
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	// three consecutive days ending today.
	for i, day := range []string{"2026-06-20", "2026-06-21", "2026-06-22"} {
		appendUsage(t, env, "s"+string(rune('a'+i)), ag, "p1", "claude-opus-4-8", day+"T10:00:00Z", 10, 0, 0, 0)
	}
	runRollup(t, env)
	ov, err := an.Overview(env.ctx, ag, now)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Streak != 3 {
		t.Fatalf("streak = %d, want 3", ov.Streak)
	}
}

func TestAnalyticsProjectTrend(t *testing.T) {
	env := setupRollup(t)
	an := NewAnalytics(env.db)
	const ag = "agent:agent-pt"
	appendUsage(t, env, "u1", ag, "p1", "claude-opus-4-8", "2026-06-20T10:00:00Z", 100, 0, 0, 0)
	appendUsage(t, env, "u2", ag, "p2", "claude-opus-4-8", "2026-06-20T11:00:00Z", 200, 0, 0, 0)
	runRollup(t, env)

	pts, err := an.ProjectTrend(env.ctx, ag, "2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("points = %d, want 2 (p1, p2 on 06-20): %+v", len(pts), pts)
	}
	if pts[0].ProjectID != "p1" || pts[0].TokensIn != 100 || pts[1].ProjectID != "p2" || pts[1].TokensIn != 200 {
		t.Fatalf("project trend ordering/values wrong: %+v", pts)
	}
}

func TestAnalyticsModelTrend(t *testing.T) {
	env := setupRollup(t)
	an := NewAnalytics(env.db)
	const ag = "agent:agent-mt"
	// two models on the same day → two model-trend points (rollup has no model dim).
	appendUsageTask(t, env, "u1", ag, "p1", "task-1", "claude-opus-4-8", "2026-06-20T10:00:00Z", 100, 50, 0, 0, 1000)
	appendUsageTask(t, env, "u2", ag, "p1", "task-1", "claude-opus-4-8", "2026-06-20T11:00:00Z", 100, 50, 0, 0, 1000)
	appendUsageTask(t, env, "u3", ag, "p1", "task-1", "claude-sonnet-4-6", "2026-06-20T12:00:00Z", 10, 5, 0, 0, 100)
	// no rollup needed — model trend reads usage_events directly.

	pts, err := an.ModelTrend(env.ctx, ag, "2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("model points = %d, want 2 (opus, sonnet): %+v", len(pts), pts)
	}
	// ordered by (day, model): sonnet < opus? "claude-opus" < "claude-sonnet" lexically.
	if pts[0].Model != "claude-opus-4-8" || pts[0].TokensIn != 200 || pts[0].TokensOut != 100 {
		t.Fatalf("opus point wrong (want in=200 out=100): %+v", pts[0])
	}
	if pts[1].Model != "claude-sonnet-4-6" || pts[1].TokensIn != 10 {
		t.Fatalf("sonnet point wrong: %+v", pts[1])
	}
}

func TestAnalyticsTopTasks(t *testing.T) {
	env := setupRollup(t)
	an := NewAnalytics(env.db)
	const ag = "agent:agent-tt"
	// task-A: cheap, NO pm_tasks row → title falls back to "" (UI shows task_id).
	appendUsageTask(t, env, "a1", ag, "p1", "task-A", "claude-opus-4-8", "2026-06-20T10:00:00Z", 10, 5, 0, 0, 100)
	// task-B: expensive, HAS a pm_tasks row (title resolves) + mixed models so the
	// dominant model = the one with the most cost (opus 1800 >> sonnet 50).
	insertTask(t, env.db, "task-B", "p1", ag, "2026-06-20T08:00:00Z") // title = "t" (helper default)
	appendUsageTask(t, env, "b1", ag, "p1", "task-B", "claude-opus-4-8", "2026-06-20T10:00:00Z", 100, 50, 0, 0, 900)
	appendUsageTask(t, env, "b2", ag, "p1", "task-B", "claude-opus-4-8", "2026-06-21T10:00:00Z", 100, 50, 0, 0, 900)
	appendUsageTask(t, env, "b3", ag, "p1", "task-B", "claude-sonnet-4-6", "2026-06-21T11:00:00Z", 10, 5, 0, 0, 50)
	appendUsage(t, env, "no-task", ag, "p1", "claude-opus-4-8", "2026-06-20T10:00:00Z", 999, 0, 0, 0) // TaskID="" → excluded

	tasks, err := an.TopTasks(env.ctx, ag, "2026-06-01", "2026-06-30", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2 (A, B; no-task excluded): %+v", len(tasks), tasks)
	}
	// rank#1 = task-B (cost 900+900+50 = 1850, 3 events), title resolved, opus dominant.
	if tasks[0].TaskID != "task-B" || tasks[0].CostMicros != 1850 || tasks[0].Events != 3 {
		t.Fatalf("rank#1 should be task-B (cost=1850, events=3): %+v", tasks[0])
	}
	if tasks[0].Title != "t" {
		t.Fatalf("task-B title should resolve from pm_tasks: %q", tasks[0].Title)
	}
	if tasks[0].DominantModel != "claude-opus-4-8" {
		t.Fatalf("task-B dominant model should be opus (most cost): %q", tasks[0].DominantModel)
	}
	// rank#2 = task-A: no pm_tasks row → Title falls back to "" (never blank-by-error).
	if tasks[1].TaskID != "task-A" || tasks[1].CostMicros != 100 {
		t.Fatalf("rank#2 should be task-A (cost=100): %+v", tasks[1])
	}
	if tasks[1].Title != "" {
		t.Fatalf("task-A title should fall back to empty (no pm_tasks row): %q", tasks[1].Title)
	}
	if tasks[1].DominantModel != "claude-opus-4-8" {
		t.Fatalf("task-A dominant model should be opus: %q", tasks[1].DominantModel)
	}
}

func TestAnalyticsTaskDrilldown(t *testing.T) {
	env := setupRollup(t)
	an := NewAnalytics(env.db)
	const ag = "agent:agent-dd"
	appendUsageTask(t, env, "e1", ag, "p1", "task-X", "claude-opus-4-8", "2026-06-20T10:00:00Z", 10, 5, 0, 0, 100)
	appendUsageTask(t, env, "e2", ag, "p1", "task-X", "claude-opus-4-8", "2026-06-20T12:00:00Z", 20, 10, 0, 0, 200)
	appendUsageTask(t, env, "e3", ag, "p1", "task-Y", "claude-opus-4-8", "2026-06-20T10:00:00Z", 1, 1, 0, 0, 1)

	evs, err := an.TaskDrilldown(env.ctx, "task-X")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("drilldown = %d, want 2 task-X events: %+v", len(evs), evs)
	}
	if evs[0].ID != "e1" || evs[1].ID != "e2" {
		t.Fatalf("drilldown order wrong (want e1,e2 by ts): %+v", evs)
	}
}

// ensure the concrete type satisfies the domain interface.
var _ usage.AnalyticsService = (*Analytics)(nil)

var _ = context.Background
