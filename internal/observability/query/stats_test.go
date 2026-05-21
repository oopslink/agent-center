package query_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
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
	env.seedTask(t, "T-1", "p", "a")
	env.seedTask(t, "T-2", "p", "b")
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters["open"] != 2 {
		t.Fatalf("expected 2 open, got %d", res.Counters["open"])
	}
	if res.Totals["total"].(int) != 2 {
		t.Fatalf("expected total 2, got %v", res.Totals["total"])
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
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	// Emit terminal events
	for i := 0; i < 2; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task_execution.completed", Actor: "system",
		})
	}
	_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "task_execution.failed", Actor: "system",
	})
	svc := query.NewStatsService(env.deps)
	res, err := svc.Aggregate(context.Background(), "executions", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters["working"] != 1 {
		t.Fatalf("working: %d", res.Counters["working"])
	}
	if res.Counters["completed"] != 2 {
		t.Fatalf("completed: %d", res.Counters["completed"])
	}
	if res.Counters["failed"] != 1 {
		t.Fatalf("failed: %d", res.Counters["failed"])
	}
	if res.Totals["active"] != 1 {
		t.Fatalf("active: %v", res.Totals["active"])
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
