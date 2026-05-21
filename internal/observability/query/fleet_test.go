package query_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/workforce"
	wforce "github.com/oopslink/agent-center/internal/workforce"
)

func TestFleetSnapshot_FourSegments_HappyPath(t *testing.T) {
	env := newQEnv(t)
	// Seed: 1 task + 1 working execution + 1 worker online + 1 IR + 1 issue
	env.seedTask(t, "T-1", "proj", "title")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusInputRequired)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedIssue(t, "I-1", "proj", "discuss")
	// projection
	if _, _, err := env.deps.Projection.UpsertIfFresh(context.Background(), "E-1", projection.ProjectionUpdate{LastPushAt: env.clk.Now(), CurrentActivity: "edit"}); err != nil {
		t.Fatal(err)
	}
	// pending IR
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID: taskruntime.InputRequestID("IR-1"), TaskExecutionID: "E-1",
		Question: "?", Urgency: inputrequest.UrgencyNormal, Now: env.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.InputReqs.Save(context.Background(), ir); err != nil {
		t.Fatal(err)
	}
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Warnings) != 0 {
		t.Fatalf("warnings: %v", snap.Warnings)
	}
	if len(snap.Executions) != 1 {
		t.Fatalf("executions: %d", len(snap.Executions))
	}
	if snap.Executions[0].CurrentActivity != "edit" {
		t.Fatalf("activity not joined: %+v", snap.Executions[0])
	}
	if len(snap.Workers) != 1 {
		t.Fatalf("workers: %d", len(snap.Workers))
	}
	if snap.Workers[0].ActiveCount != 1 {
		t.Fatalf("active_count: %d", snap.Workers[0].ActiveCount)
	}
	if len(snap.OpenInputRequests) != 1 {
		t.Fatalf("open_input_requests: %d", len(snap.OpenInputRequests))
	}
	if len(snap.PendingIssues) != 1 {
		t.Fatalf("pending_issues: %d", len(snap.PendingIssues))
	}
}

func TestFleetSnapshot_ProjectFilter(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "proj-a", "a")
	env.seedTask(t, "T-2", "proj-b", "b")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	env.seedExecution(t, "E-2", "T-2", "W-2", execution.StatusWorking)
	env.seedIssue(t, "I-A", "proj-a", "x")
	env.seedIssue(t, "I-B", "proj-b", "y")
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{ProjectID: "proj-a"})
	if len(snap.Executions) != 1 || snap.Executions[0].TaskID != "T-1" {
		t.Fatalf("execution filter: %+v", snap.Executions)
	}
	if len(snap.PendingIssues) != 1 || snap.PendingIssues[0].IssueID != "I-A" {
		t.Fatalf("pending issues filter: %+v", snap.PendingIssues)
	}
}

func TestFleetSnapshot_PartialFailure_EmitsWarnings(t *testing.T) {
	env := newQEnv(t)
	// Drop the Tasks repo to simulate one segment failing.
	deps := env.deps
	deps.Issues = nil
	deps.InputReqs = nil
	svc := query.NewFleetSnapshotService(deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d (%v)", len(snap.Warnings), snap.Warnings)
	}
}

func TestFleetSnapshot_Empty_NoCrash(t *testing.T) {
	env := newQEnv(t)
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Executions) != 0 || len(snap.Workers) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
	// Suppress unused warnings about untyped use of imports under build tags.
	_ = errors.New
	_ = discussion.StatusOpen
	_ = conversation.ConversationKindTask
	_ = wforce.WorkerOnline
}
