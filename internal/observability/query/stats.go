package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
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
	if s.deps.Tasks == nil {
		return res, errors.New("tasks repo not wired")
	}
	statuses := []task.Status{task.StatusOpen, task.StatusSuspended, task.StatusDone, task.StatusAbandoned}
	for _, st := range statuses {
		items, err := s.deps.Tasks.FindByStatus(ctx, st, task.Filter{Limit: 1000})
		if err != nil {
			return res, err
		}
		res.Counters[string(st)] = countSince(items, taskCreatedAt, since)
	}
	total := 0
	for _, v := range res.Counters {
		total += v
	}
	res.Totals["total"] = total
	return res, nil
}

func (s *StatsService) aggregateExecutions(ctx context.Context, res StatsResult, since *time.Time) (StatsResult, error) {
	if s.deps.Executions == nil {
		return res, errors.New("executions repo not wired")
	}
	// Active by status
	active, err := s.deps.Executions.FindActive(ctx)
	if err != nil {
		return res, err
	}
	for _, e := range active {
		res.Counters[string(e.Status())]++
	}
	// Terminal: walk recent executions per-status via events table. v1
	// shortcut: count emitted task_execution.completed / .failed /
	// .killed events (since events are 1:1 with terminal transitions).
	if s.deps.Events != nil {
		for _, kind := range []string{"task_execution.completed", "task_execution.failed", "task_execution.killed"} {
			et := observability.EventType(kind)
			f := observability.EventQueryFilter{EventType: &et, Limit: 1000}
			if since != nil {
				f.Since = since
			}
			evs, err := s.deps.Events.Find(ctx, f)
			if err != nil {
				return res, err
			}
			label := kind[len("task_execution."):]
			res.Counters[label] += len(evs)
		}
	}
	res.Totals["active"] = len(active)
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
	if s.deps.Issues == nil {
		return res, errors.New("issues repo not wired")
	}
	statuses := []discussion.Status{
		discussion.StatusOpen, discussion.StatusUnderDiscussion,
		discussion.StatusConcluded, discussion.StatusClosedNoAction,
		discussion.StatusClosedWithTasks, discussion.StatusWithdrawn,
	}
	for _, st := range statuses {
		items, err := s.deps.Issues.FindByStatus(ctx, st, discussion.IssueFilter{Limit: 1000})
		if err != nil {
			return res, err
		}
		res.Counters[string(st)] = countSince(items, issueOpenedAt, since)
	}
	total := 0
	for _, v := range res.Counters {
		total += v
	}
	res.Totals["total"] = total
	return res, nil
}

// countSince counts items whose timestamp (via extractor) is >= since;
// since==nil counts everything.
func countSince[T any](items []T, extract func(T) time.Time, since *time.Time) int {
	if since == nil {
		return len(items)
	}
	n := 0
	for _, it := range items {
		if !extract(it).Before(*since) {
			n++
		}
	}
	return n
}

func taskCreatedAt(t *task.Task) time.Time   { return t.CreatedAt() }
func issueOpenedAt(i *discussion.Issue) time.Time { return i.OpenedAt() }

// Touch the execution package import (used implicitly for Status enums).
var _ = execution.StatusSubmitted
