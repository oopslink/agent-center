package api

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// T130 end-to-end — the EXACT bug path the T83 claim guard left open: a backlog
// task assigned DIRECTLY to an agent mints a queued work item; with the real run
// gate wired (as production wires it at the composition root), start_work REJECTS
// activating it, so the task never reaches running. Adding the task to a real
// plan (the documented remedy) makes the SAME work item startable.
func TestStartWork_DirectAssignBacklog_RejectedEndToEnd_T130(t *testing.T) {
	f := newWriteToolsFixture(t)
	// Wire the real T130 gate (production: agentSvc.SetTaskRunGate(NewAgentTaskRunGate(pmSvc))).
	f.deps.AgentSvc.SetTaskRunGate(pmservice.NewAgentTaskRunGate(f.pmSvc))

	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t) // participant projector creates the task Conversation.
	// Direct assign → the WorkItem projector mints the queued work item. This is
	// the path that bypassed claimability (assign, not pool-claim).
	if err := f.pmSvc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1), owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t)

	items, err := f.workItems.ListByTask(ctx, "pm://tasks/"+string(tid))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 queued work item, got %d", len(items))
	}
	wi := items[0]

	// start_work on the BACKLOG task is rejected — the open→running gate.
	if err := f.deps.AgentSvc.StartWork(ctx, wi.AgentID(), wi.ID()); !errors.Is(err, agent.ErrWorkItemTaskNotRunnable) {
		t.Fatalf("start backlog = %v, want ErrWorkItemTaskNotRunnable", err)
	}
	// …and the task is still open (never flipped to running).
	got, err := f.pmSvc.GetTask(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != pm.TaskOpen {
		t.Fatalf("backlog task status = %s, want open (start was rejected)", got.Status())
	}

	// Remedy: add the task to a real (non-builtin) plan AND start the plan → the
	// (dependency-free) node becomes ready → runnable. T329 (issue-9d4b3895 §13.A):
	// being a plan member is no longer sufficient — the plan must be RUNNING and the
	// node's DAG deps satisfied, so start the plan before the SAME work item starts.
	planID, err := f.pmSvc.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "real", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.pmSvc.SelectTaskIntoPlan(ctx, planID, tid, owner); err != nil {
		t.Fatal(err)
	}
	if err := f.pmSvc.StartPlan(ctx, planID, owner); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if err := f.deps.AgentSvc.StartWork(ctx, wi.AgentID(), wi.ID()); err != nil {
		t.Fatalf("start after add-to-plan+start = %v, want nil", err)
	}
}
