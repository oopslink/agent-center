package query_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

func TestQuery_InputRequests_TaskIDFilter(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusInputRequired)
	env.seedExecution(t, "E-2", "T-1", "W-1", execution.StatusInputRequired)
	for i, exec := range []string{"E-1", "E-2"} {
		ir, _ := inputrequest.New(inputrequest.NewInput{
			ID: taskruntime.InputRequestID([]string{"IR-1", "IR-2"}[i]),
			TaskExecutionID: taskruntime.TaskExecutionID(exec),
			Question:        "?", Urgency: inputrequest.UrgencyNormal, Now: env.clk.Now(),
		})
		_ = env.deps.InputReqs.Save(context.Background(), ir)
	}
	// Filter by exec id (treated as task id)
	res, _ := env.svc.Query(context.Background(), "input_requests", query.QueryFilter{TaskID: "E-1"})
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 IR for E-1, got %d", len(res.Items))
	}
}

func TestQuery_InputRequests_StatusFilter(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusInputRequired)
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1",
		Question: "?", Urgency: inputrequest.UrgencyNormal, Now: env.clk.Now(),
	})
	_ = env.deps.InputReqs.Save(context.Background(), ir)
	res, _ := env.svc.Query(context.Background(), "input_requests", query.QueryFilter{Status: "pending"})
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(res.Items))
	}
	res, _ = env.svc.Query(context.Background(), "input_requests", query.QueryFilter{Status: "responded"})
	if len(res.Items) != 0 {
		t.Fatalf("expected 0 responded, got %d", len(res.Items))
	}
}
