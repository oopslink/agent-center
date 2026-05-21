package query_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestQuery_Executions_ByTaskID(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{TaskID: "T-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Items))
	}
}

func TestQuery_Executions_DefaultActive(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 active, got %d", len(res.Items))
	}
}

func TestQuery_Workers_HasMappingTrueFalse(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedWorker(t, "W-2", workforce.WorkerOnline)
	tr := true
	res, _ := env.svc.Query(context.Background(), "workers", query.QueryFilter{HasMapping: &tr})
	if len(res.Items) != 0 {
		t.Fatalf("expected 0 with mappings, got %d", len(res.Items))
	}
	fa := false
	res, _ = env.svc.Query(context.Background(), "workers", query.QueryFilter{HasMapping: &fa})
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 w/o mappings, got %d", len(res.Items))
	}
}

func TestQuery_Issues_ByOpener(t *testing.T) {
	env := newQEnv(t)
	env.seedIssue(t, "I-1", "p", "x")
	res, err := env.svc.Query(context.Background(), "issues", query.QueryFilter{Opener: "user:test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Items))
	}
}

func TestQuery_Tasks_PriorityFilter(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	res, _ := env.svc.Query(context.Background(), "tasks", query.QueryFilter{ProjectID: "p", Priority: "medium"})
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 medium-priority task, got %d", len(res.Items))
	}
	res, _ = env.svc.Query(context.Background(), "tasks", query.QueryFilter{ProjectID: "p", Priority: "high"})
	if len(res.Items) != 0 {
		t.Fatalf("expected 0 high, got %d", len(res.Items))
	}
}

func TestQuery_Tasks_BlockedBy(t *testing.T) {
	env := newQEnv(t)
	// Empty blocker list — call still succeeds
	res, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{BlockedBy: "T-blocker"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("expected 0, got %d", len(res.Items))
	}
}

func TestQuery_Events_FullFlagSurface(t *testing.T) {
	env := newQEnv(t)
	for i := 0; i < 3; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task.created", Actor: "user:h",
		})
	}
	since := env.clk.Now().Add(-time.Hour)
	until := env.clk.Now().Add(time.Hour)
	res, err := env.svc.Query(context.Background(), "events", query.QueryFilter{
		Since: &since, Until: &until, Actor: "user:h",
		EventType: "task.created",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("expected 3, got %d", len(res.Items))
	}
}
