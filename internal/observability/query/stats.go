package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// StatsScope is the closed enum for `stats --scope=...`.
type StatsScope string

const (
	StatsScopeTasks      StatsScope = "tasks"
	StatsScopeExecutions StatsScope = "executions"
	StatsScopeWorkers    StatsScope = "workers"
	StatsScopeEvents     StatsScope = "events"
	StatsScopeIssues     StatsScope = "issues"
)

// AllStatsScopes is the closed enum list.
var AllStatsScopes = []StatsScope{
	StatsScopeTasks, StatsScopeExecutions, StatsScopeWorkers, StatsScopeEvents, StatsScopeIssues,
}

// ValidStatsScope reports whether a scope name is recognised.
func ValidStatsScope(s string) bool {
	for _, x := range AllStatsScopes {
		if string(x) == s {
			return true
		}
	}
	return false
}

// StatsResult is the unified Stats output envelope.
type StatsResult struct {
	Scope     StatsScope     `json:"scope"`
	Since     string         `json:"since,omitempty"`
	Counters  map[string]int `json:"counters,omitempty"`
	Totals    map[string]any `json:"totals,omitempty"`
	Generated string         `json:"generated_at"`
}

// ErrStatsScopeUnknown is returned for unrecognised scope names.
var ErrStatsScopeUnknown = errors.New("stats: unknown scope")

// StatsService implements the `stats` verb. v1 hits state tables directly
// + walks events for the events scope; no pre-aggregation (plan-4 § 6.5
// risk acknowledged: single-VPS scale, postpone to roadmap).
type StatsService struct {
	deps Deps
}

// NewStatsService wires.
func NewStatsService(deps Deps) *StatsService {
	return &StatsService{deps: deps}
}

// Aggregate dispatches to a scope-specific aggregator.
func (s *StatsService) Aggregate(ctx context.Context, scope string, since *time.Time) (StatsResult, error) {
	if !ValidStatsScope(scope) {
		return StatsResult{}, fmt.Errorf("%w: %q", ErrStatsScopeUnknown, scope)
	}
	res := StatsResult{
		Scope:     StatsScope(scope),
		Generated: time.Now().UTC().Format(time.RFC3339Nano),
		Counters:  map[string]int{},
		Totals:    map[string]any{},
	}
	if since != nil {
		res.Since = since.UTC().Format(time.RFC3339Nano)
	}
	switch StatsScope(scope) {
	case StatsScopeTasks:
		return s.aggregateTasks(ctx, res, since)
	case StatsScopeExecutions:
		return s.aggregateExecutions(ctx, res, since)
	case StatsScopeWorkers:
		return s.aggregateWorkers(ctx, res)
	case StatsScopeEvents:
		return s.aggregateEvents(ctx, res, since)
	case StatsScopeIssues:
		return s.aggregateIssues(ctx, res, since)
	}
	return res, nil
}

func (s *StatsService) aggregateTasks(ctx context.Context, res StatsResult, since *time.Time) (StatsResult, error) {
	if s.deps.PMTasks == nil {
		return res, errors.New("pm tasks repo not wired")
	}
	// v2.7 #107 Phase-2: repointed to pm_tasks (grouped count across ALL
	// projects/orgs — global, like the old taskruntime FindByStatus scan).
	// Counter labels are pm.Task status names (open/assigned/running/blocked/
	// completed/verified/canceled/reopened); the old taskruntime status names
	// are gone (clean swap, no compat — known breaking, recorded in E2 docs).
	counts, err := s.deps.PMTasks.CountByStatus(ctx, since)
	if err != nil {
		return res, err
	}
	total := 0
	for st, n := range counts {
		res.Counters[string(st)] = n
		total += n
	}
	res.Totals["total"] = total
	return res, nil
}

func (s *StatsService) aggregateExecutions(ctx context.Context, res StatsResult, since *time.Time) (StatsResult, error) {
	if s.deps.PMTasks == nil {
		return res, errors.New("pm tasks repo not wired")
	}
	// v2.14.0 F7 (issue I14): repointed off the retired AgentWorkItem model onto
	// pm_tasks. Active (live) executions = non-terminal agent-assigned tasks,
	// counted under the mapped execution-status vocab (queued/active/waiting_input)
	// the Web Console still renders — distinct namespace from the pm-task status
	// counters set by aggregateTasks.
	tasks, err := s.deps.PMTasks.ListByStatuses(ctx, activeTaskStatuses)
	if err != nil {
		return res, err
	}
	active := 0
	for _, t := range tasks {
		if agentMemberIDFromAssignee(t.Assignee()) == "" {
			continue // executions are agent work only
		}
		res.Counters[taskExecStatus(t)]++
		active++
	}
	res.Totals["active"] = active
	// Terminal executions: the AgentWorkItem done/failed/canceled split is gone
	// (no failed status; blocked is a running annotation), so map the terminal
	// pm-task outcomes onto the legacy execution-terminal labels — completed→done,
	// discarded→canceled — keeping a distinct namespace from the pm-task counters.
	// CountByStatus is global, optionally since-scoped.
	counts, err := s.deps.PMTasks.CountByStatus(ctx, since)
	if err != nil {
		return res, err
	}
	res.Counters["done"] += counts[pm.TaskCompleted]
	res.Counters["canceled"] += counts[pm.TaskDiscarded]
	return res, nil
}

func (s *StatsService) aggregateWorkers(ctx context.Context, res StatsResult) (StatsResult, error) {
	if s.deps.Workers == nil {
		return res, errors.New("workers repo not wired")
	}
	all, err := s.deps.Workers.FindAll(ctx)
	if err != nil {
		return res, err
	}
	var totalSeconds int64
	for _, w := range all {
		res.Counters[string(w.Status())]++
		totalSeconds += w.WorkingSeconds()
	}
	res.Totals["worker_count"] = len(all)
	res.Totals["working_seconds_total"] = totalSeconds
	if len(all) > 0 {
		res.Totals["working_seconds_avg"] = totalSeconds / int64(len(all))
	}
	_ = workforce.WorkerOnline // touch the import
	return res, nil
}

func (s *StatsService) aggregateEvents(ctx context.Context, res StatsResult, since *time.Time) (StatsResult, error) {
	if s.deps.Events == nil {
		return res, errors.New("events repo not wired")
	}
	f := observability.EventQueryFilter{Limit: 1000}
	if since != nil {
		f.Since = since
	}
	evs, err := s.deps.Events.Find(ctx, f)
	if err != nil {
		return res, err
	}
	for _, e := range evs {
		res.Counters[string(e.Type())]++
	}
	res.Totals["total"] = len(evs)
	return res, nil
}

func (s *StatsService) aggregateIssues(ctx context.Context, res StatsResult, since *time.Time) (StatsResult, error) {
	if s.deps.PMIssues == nil {
		return res, errors.New("pm issues repo not wired")
	}
	// v2.7 #131: repointed off the retired discussion model to pm_issues.
	// Counter labels are pm.IssueStatus names; one FindByStatuses scan covers the
	// full enum (mirrors the aggregateTasks/aggregateExecutions new-model reads in
	// this file). pm.Issue has no OpenedAt → the `since` window uses CreatedAt.
	statuses := []pm.IssueStatus{
		pm.IssueOpen, pm.IssueInProgress, pm.IssueReopened,
		pm.IssueResolved, pm.IssueClosed, pm.IssueDiscarded,
	}
	items, err := s.deps.PMIssues.FindByStatuses(ctx, statuses, 1000)
	if err != nil {
		return res, err
	}
	for _, i := range items {
		if since != nil && i.CreatedAt().Before(*since) {
			continue
		}
		res.Counters[string(i.Status())]++
	}
	total := 0
	for _, v := range res.Counters {
		total += v
	}
	res.Totals["total"] = total
	return res, nil
}

