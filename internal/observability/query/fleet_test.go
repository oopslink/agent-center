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
	// v2.7 #107: the fleet "executions" segment now reads live work-item projections.
	env.seedLiveWorkItem(t, "WI-1", "AG-1", "T-1", "proj", "org-1", "active")
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Warnings) != 0 {
		t.Fatalf("warnings: %v", snap.Warnings)
	}
	if len(snap.WorkItems) != 1 {
		t.Fatalf("work_items: %d", len(snap.WorkItems))
	}
	if snap.WorkItems[0].WorkItemID != "WI-1" || snap.WorkItems[0].CurrentActivity != "edit" || snap.WorkItems[0].TotalToolCalls != 2 {
		t.Fatalf("work item fields not joined: %+v", snap.WorkItems[0])
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
	env.seedLiveWorkItem(t, "WI-A", "AG-A", "T-1", "proj-a", "org-1", "active")
	env.seedLiveWorkItem(t, "WI-B", "AG-B", "T-2", "proj-b", "org-1", "active")
	env.seedIssue(t, "I-A", "proj-a", "x")
	env.seedIssue(t, "I-B", "proj-b", "y")
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{ProjectID: "proj-a"})
	if len(snap.WorkItems) != 1 || snap.WorkItems[0].TaskID != "T-1" {
		t.Fatalf("work item project filter: %+v", snap.WorkItems)
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
	if len(snap.WorkItems) != 0 || len(snap.Workers) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
	// Suppress unused warnings about untyped use of imports under build tags.
	_ = errors.New
	_ = discussion.StatusOpen
	_ = conversation.ConversationKindTask
	_ = wforce.WorkerOnline
}

// TestFleetSnapshot_OrgScoping_NoCrossOrgLeak is the v2.7 #107 hard §-1 gate:
// org-scoped fleet must NOT leak work items from other orgs. Scoping is resolved
// per work-item via task_ref → pm task → project → org (equivalent to the old
// Tasks.FindByID(org)); fail-closed if a project can't be resolved.
func TestFleetSnapshot_OrgScoping_NoCrossOrgLeak(t *testing.T) {
	env := newQEnv(t)
	// org-A work item + org-B work item.
	env.seedLiveWorkItem(t, "WI-A", "AG-A", "TA", "proj-a", "org-A", "active")
	env.seedLiveWorkItem(t, "WI-B", "AG-B", "TB", "proj-b", "org-B", "active")
	svc := query.NewFleetSnapshotService(env.deps)

	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	if len(snap.WorkItems) != 1 || snap.WorkItems[0].WorkItemID != "WI-A" {
		t.Fatalf("org scope leaked / wrong: want only WI-A for org-A, got %+v", snap.WorkItems)
	}

	// A work item whose project/org can't be resolved (no task_ref hop) must be
	// EXCLUDED under org scope (fail-closed, never leak).
	env.seedWorkItemProjection(t, "WI-orphan", "AG-X", "active") // no work_item row → task_ref unresolvable
	snap = svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	for _, wi := range snap.WorkItems {
		if wi.WorkItemID == "WI-orphan" {
			t.Fatalf("fail-closed violated: unresolvable-project work item leaked under org scope: %+v", snap.WorkItems)
		}
	}
	if len(snap.WorkItems) != 1 {
		t.Fatalf("org-A should still see exactly WI-A, got %+v", snap.WorkItems)
	}
}
