package query_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestStats_UnknownScope(t *testing.T) {
	env := newQEnv(t)
	svc := query.NewStatsService(env.deps)
	_, err := svc.Aggregate(context.Background(), "blob", nil)
	if !errors.Is(err, query.ErrStatsScopeUnknown) {
		t.Fatalf("expected ErrStatsScopeUnknown, got %v", err)
	}
}

func TestStats_Tasks_CountsByStatus(t *testing.T) {
	env := newQEnv(t)
	// v2.7 #107 Phase-2: tasks scope repointed to pm_tasks (grouped count).
	env.seedTask(t, "T-1", "p", "a")
	env.seedTask(t, "T-2", "p", "b")
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	// new pm task default status = open; counts reflect pm model.
	if res.Counters["open"] != 2 {
		t.Fatalf("expected 2 open, got %d (all=%v)", res.Counters["open"], res.Counters)
	}
	if res.Totals["total"].(int) != 2 {
		t.Fatalf("expected total 2, got %v", res.Totals["total"])
	}
	// label-correctness (#5): old taskruntime-only status names must not appear.
	for _, old := range []string{"suspended", "abandoned"} {
		if _, ok := res.Counters[old]; ok {
			t.Fatalf("old taskruntime label %q must not appear: %v", old, res.Counters)
		}
	}
}

func TestStats_Workers_Aggregate(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedWorker(t, "W-2", workforce.WorkerOffline)
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters["online"] != 1 || res.Counters["offline"] != 1 {
		t.Fatalf("worker counters: %+v", res.Counters)
	}
	if res.Totals["worker_count"] != 2 {
		t.Fatalf("worker_count: %v", res.Totals["worker_count"])
	}
}

func TestStats_Events_Aggregate(t *testing.T) {
	env := newQEnv(t)
	for i := 0; i < 3; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task.created", Actor: "user:h",
		})
	}
	for i := 0; i < 2; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "issue.opened", Actor: "user:h",
		})
	}
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters["task.created"] != 3 {
		t.Fatalf("task.created: %d", res.Counters["task.created"])
	}
	if res.Counters["issue.opened"] != 2 {
		t.Fatalf("issue.opened: %d", res.Counters["issue.opened"])
	}
	if res.Totals["total"] != 5 {
		t.Fatalf("events total: %v", res.Totals["total"])
	}
}

func TestStats_Executions_CountsActiveAndTerminal(t *testing.T) {
	env := newQEnv(t)
	// v2.7 #107 Phase-2: executions scope repointed to the agent work-item model.
	// Active (live) work items by status via WorkItemProjections.List(live set).
	env.seedWorkItemProjection(t, "WI-1", "agent-1", "active")
	env.seedWorkItemProjection(t, "WI-2", "agent-2", "active")
	env.seedWorkItemProjection(t, "WI-3", "agent-3", "waiting_input")
	env.seedWorkItemProjection(t, "WI-4", "agent-4", "queued")
	// A terminal projection: must NOT be counted as active (live-set excludes it).
	env.seedWorkItemProjection(t, "WI-5", "agent-5", "done")
	// Terminal counts come from agent.work_item.transitioned events (status in payload).
	emitTransition := func(status string) {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "agent.work_item.transitioned", Actor: "system",
			Payload: map[string]any{"status": status},
		})
	}
	emitTransition("done")
	emitTransition("done")
	emitTransition("failed")
	emitTransition("canceled")   // counted: canceled = v1-killed equivalent
	emitTransition("superseded") // must NOT be counted (no v1 analog; reassignment bookkeeping)

	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "executions", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Active by new work-item status labels.
	if res.Counters["active"] != 2 {
		t.Fatalf("active: %d (all=%v)", res.Counters["active"], res.Counters)
	}
	if res.Counters["waiting_input"] != 1 {
		t.Fatalf("waiting_input: %d (all=%v)", res.Counters["waiting_input"], res.Counters)
	}
	if res.Counters["queued"] != 1 {
		t.Fatalf("queued: %d (all=%v)", res.Counters["queued"], res.Counters)
	}
	// Terminal = done + failed + canceled (canceled = v1-killed equivalent;
	// preserves v1 completed/failed/killed without dropping a class). superseded
	// is excluded (no v1 analog; reassignment bookkeeping).
	if res.Counters["done"] != 2 {
		t.Fatalf("done: %d (all=%v)", res.Counters["done"], res.Counters)
	}
	if res.Counters["failed"] != 1 {
		t.Fatalf("failed: %d (all=%v)", res.Counters["failed"], res.Counters)
	}
	if res.Counters["canceled"] != 1 {
		t.Fatalf("canceled (=v1 killed) must count: %d (all=%v)", res.Counters["canceled"], res.Counters)
	}
	if _, ok := res.Counters["superseded"]; ok {
		t.Fatalf("superseded must NOT count in executions terminal: %v", res.Counters)
	}
	// active total = live work items only (the done projection is excluded).
	if res.Totals["active"] != 4 {
		t.Fatalf("active total: %v (want 4 live)", res.Totals["active"])
	}
	// label-correctness (#5): no old taskruntime execution labels.
	for _, old := range []string{"working", "submitted", "completed", "killed", "input_required"} {
		if _, ok := res.Counters[old]; ok {
			t.Fatalf("old execution label %q must not appear: %v", old, res.Counters)
		}
	}
}

func TestStats_Issues_CountsByStatus(t *testing.T) {
	env := newQEnv(t)
	env.seedIssue(t, "I-1", "p", "a")
	env.seedIssue(t, "I-2", "p", "b")
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters["open"] != 2 {
		t.Fatalf("expected 2 open issues, got %d", res.Counters["open"])
	}
}

func TestStats_SinceFilter(t *testing.T) {
	env := newQEnv(t)
	now := env.clk.Now()
	// Seed an issue before the cutoff.
	env.seedIssue(t, "I-1", "p", "a")
	// Advance + seed after.
	env.clk.Set(now.Add(time.Hour))
	env.seedIssue(t, "I-2", "p", "b")
	since := now.Add(30 * time.Minute)
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "issues", &since)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters["open"] != 1 {
		t.Fatalf("expected 1 issue after cutoff, got %d", res.Counters["open"])
	}
}
