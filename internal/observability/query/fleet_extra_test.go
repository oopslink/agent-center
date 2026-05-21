package query_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

func TestFleetSnapshot_IRProjectFilter_ViaExecChain(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-a", "proj-a", "x")
	env.seedTask(t, "T-b", "proj-b", "y")
	env.seedExecution(t, "E-a", "T-a", "W-a", execution.StatusInputRequired)
	env.seedExecution(t, "E-b", "T-b", "W-b", execution.StatusInputRequired)
	for _, p := range []struct {
		ir, exec string
	}{{"IR-a", "E-a"}, {"IR-b", "E-b"}} {
		ir, _ := inputrequest.New(inputrequest.NewInput{
			ID:              taskruntime.InputRequestID(p.ir),
			TaskExecutionID: taskruntime.TaskExecutionID(p.exec),
			Question:        "?", Urgency: inputrequest.UrgencyNormal, Now: env.clk.Now(),
		})
		_ = env.deps.InputReqs.Save(context.Background(), ir)
	}
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{ProjectID: "proj-a"})
	if len(snap.OpenInputRequests) != 1 || snap.OpenInputRequests[0].InputRequestID != "IR-a" {
		t.Fatalf("project filter IR: %+v", snap.OpenInputRequests)
	}
}
