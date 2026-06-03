package query_test

// Branch-coverage supplements pinned to specific uncovered statements in
// query/service.go and stats.go. Each test references the precise line/block
// (per `go tool cover -func`) it exists to exercise — these are real product
// paths (status filters, limit clamps, CorrelationID/DecisionID event filters,
// since-window task counter), not throwaway assertions.

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Covers queryTasks branch where filter has both ProjectID and Status — the
// inner `if f.Status != ""` block (service.go:369-372) sets the underlying
// task filter.Status.
func TestQuery_Tasks_ByProject_WithStatus(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "proj", "a")
	env.seedTask(t, "T-2", "proj", "b")
	res, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{
		ProjectID: "proj",
		Status:    "open",
		Limit:     50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 open tasks, got %d", len(res.Items))
	}
}

// Covers queryIssues branch where filter has both ProjectID and Status —
// inner `if f.Status != ""` block (service.go:493-496).
func TestQuery_Issues_ByProject_WithStatus(t *testing.T) {
	env := newQEnv(t)
	env.seedPMIssue(t, "I-1", "proj", "a", pm.IssueOpen)
	env.seedPMIssue(t, "I-2", "proj", "b", pm.IssueOpen)
	res, err := env.svc.Query(context.Background(), "issues", query.QueryFilter{
		ProjectID: "proj",
		Status:    string(pm.IssueOpen),
		Limit:     50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 open issues, got %d", len(res.Items))
	}
}

// Covers applyDefaultLimit branches "limit > Max" (clamp to Max,
// service.go:131-133) and "limit in range" (pass through, service.go:134) via
// the queryTasks dispatch path that calls applyDefaultLimit.
func TestQuery_Tasks_LimitClampAndPassThrough(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	// limit > Max → clamps without error (applyDefaultLimit returns Max).
	if _, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{
		ProjectID: "p",
		Limit:     observability.MaxEventQueryLimit + 1000,
	}); err != nil {
		t.Fatalf("clamp path: %v", err)
	}
	// limit in (0, Max] → pass through.
	if _, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{
		ProjectID: "p",
		Limit:     5,
	}); err != nil {
		t.Fatalf("pass-through path: %v", err)
	}
}

// Covers events Refs filter branches (service.go:621-635) — ExecutionID,
// WorkerID, IssueID, Cursor. TaskID is already covered by
// TestQuery_Events_RefsFilter; the other three filter dimensions weren't.
func TestQuery_Events_AllRefFlagsAndCursor(t *testing.T) {
	env := newQEnv(t)
	ctx := context.Background()
	// Emit one event per ref dimension we want to filter on.
	_, _ = env.sink.Emit(ctx, observability.EmitCommand{
		EventType: "task_execution.started",
		Refs:      observability.EventRefs{ExecutionID: "E-99"},
		Actor:     "user:h",
	})
	_, _ = env.sink.Emit(ctx, observability.EmitCommand{
		EventType: "worker.online",
		Refs:      observability.EventRefs{WorkerID: "W-99"},
		Actor:     "user:h",
	})
	_, _ = env.sink.Emit(ctx, observability.EmitCommand{
		EventType: "issue.opened",
		Refs:      observability.EventRefs{IssueID: "I-99"},
		Actor:     "user:h",
	})
	cases := []struct {
		name   string
		filter query.QueryFilter
	}{
		{"execution", query.QueryFilter{ExecutionID: "E-99"}},
		{"worker", query.QueryFilter{WorkerID: "W-99"}},
		{"issue", query.QueryFilter{IssueID: "I-99"}},
	}
	for _, tc := range cases {
		res, err := env.svc.Query(ctx, "events", tc.filter)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(res.Items) != 1 {
			t.Fatalf("%s: got %d events, want 1", tc.name, len(res.Items))
		}
	}
	// Cursor branch (service.go:633-635) — pass a cursor; results should
	// still drain without error, even if zero matches.
	if _, err := env.svc.Query(ctx, "events", query.QueryFilter{Cursor: "evt_zzz"}); err != nil {
		t.Fatalf("cursor: %v", err)
	}
}

// Covers events filter branches `if f.CorrelationID != ""` (service.go:607-610)
// and `if f.DecisionID != ""` (service.go:611-614) — the dispatcher must
// translate these flag fields into the underlying observability event filter.
func TestQuery_Events_CorrelationAndDecisionIDFilter(t *testing.T) {
	env := newQEnv(t)
	// Seed a few events with distinct correlation / decision ids.
	for i, corr := range []string{"corr-A", "corr-A", "corr-B"} {
		_, err := env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType:     "task.created",
			Actor:         observability.Actor("user:h"),
			CorrelationID: corr,
			DecisionID:    "dec-" + corr,
			Payload:       map[string]any{"i": i},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// CorrelationID filter — only corr-A matches (2 events).
	res, err := env.svc.Query(context.Background(), "events", query.QueryFilter{
		CorrelationID: "corr-A",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("correlation filter: got %d, want 2", len(res.Items))
	}
	// DecisionID filter — exactly one matches.
	res, err = env.svc.Query(context.Background(), "events", query.QueryFilter{
		DecisionID: "dec-corr-B",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("decision filter: got %d, want 1", len(res.Items))
	}
}

// Covers the stats tasks since-filter path (v2.7 #107 Phase-2: pm CountByStatus
// with a non-nil since cutoff filters by pm_tasks.created_at).
func TestStats_Tasks_SinceFilter(t *testing.T) {
	env := newQEnv(t)
	now := env.clk.Now()
	env.seedTask(t, "T-1", "p", "old")
	env.clk.Set(now.Add(time.Hour))
	env.seedTask(t, "T-2", "p", "new")
	since := now.Add(30 * time.Minute)
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "tasks", &since)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters["open"] != 1 {
		t.Fatalf("expected 1 task after cutoff, got %d", res.Counters["open"])
	}
}

// Covers projections.go:205-212 — the body of projectEventSummaryList, which
// only runs when an Inspect target actually has correlated events. Existing
// Inspect tests seed the AR but no events refer to it, so the loop body
// stays cold.
func TestInspect_Task_PopulatesRecentEvents(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-evt", "proj", "with-events")
	// Emit two events referencing the task so projectEventSummaryList has
	// items to project.
	for i := 0; i < 2; i++ {
		_, err := env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task.created",
			Refs:      observability.EventRefs{TaskID: "T-evt"},
			Actor:     observability.Actor("user:h"),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	res, err := env.svc.Inspect(context.Background(), "task", "T-evt")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	evs, ok := data["recent_events"].([]any)
	if !ok || len(evs) == 0 {
		t.Fatalf("expected recent_events populated, got %+v", data["recent_events"])
	}
	first := evs[0].(map[string]any)
	if first["event_type"] != "task.created" {
		t.Fatalf("event_type roundtrip: %v", first["event_type"])
	}
}

// Covers stats.go:134-136 (`if since != nil { f.Since = since }`) for the
// executions aggregation, and stats.go:176-178 for events — both branches
// only fire when the caller passes a non-nil cutoff to Aggregate.
func TestStats_Executions_And_Events_SinceFilter(t *testing.T) {
	env := newQEnv(t)
	since := env.clk.Now().Add(-time.Hour)
	svc := query.NewStatsService(env.deps)
	if _, err := svc.Aggregate(context.Background(), "executions", &since); err != nil {
		t.Fatalf("executions+since: %v", err)
	}
	if _, err := svc.Aggregate(context.Background(), "events", &since); err != nil {
		t.Fatalf("events+since: %v", err)
	}
}
