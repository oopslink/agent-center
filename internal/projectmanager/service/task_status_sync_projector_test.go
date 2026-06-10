package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// taskSyncSetup wires a Service + the pm-task-status-sync projector over a fresh
// in-memory DB.
func taskSyncSetup(t *testing.T) (*Service, *TaskStatusSyncProjector, outbox.AppliedStore, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	ob := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, AgentDir: allOrgDir("org-1"), IDGen: gen, Clock: clk,
	})
	proj := NewTaskStatusSyncProjector(db, svc, applied, clk)
	return svc, proj, applied, context.Background()
}

func assignedTask(t *testing.T, svc *Service, ctx context.Context) pm.TaskID {
	t.Helper()
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	return tid
}

func transitionEvent(t *testing.T, eventID string, tid pm.TaskID, status string) outbox.Event {
	t.Helper()
	pl, _ := json.Marshal(agentsvc.WorkItemTransitionPayload{
		WorkItemID: "WI-1", AgentID: "AG1", TaskRef: "pm://tasks/" + string(tid),
		PrevStatus: "queued", Status: status, Version: 2, OccurredAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	return outbox.Event{ID: eventID, EventType: agentsvc.EvtAgentWorkItemTransitioned, Payload: string(pl)}
}

func taskStatus(t *testing.T, svc *Service, ctx context.Context, tid pm.TaskID) pm.TaskStatus {
	t.Helper()
	tk, err := svc.tasks.FindByID(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status()
}

func TestTaskStatusSync_ActiveStartsAssignedTask(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx)
	if taskStatus(t, svc, ctx, tid) != pm.TaskOpen {
		t.Fatalf("precondition: task should be open (assignee is metadata), got %s", taskStatus(t, svc, ctx, tid))
	}
	if err := proj.Project(ctx, transitionEvent(t, "E-1", tid, "active")); err != nil {
		t.Fatal(err)
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskRunning {
		t.Fatalf("active transition must move open task → running, got %s", got)
	}
}

func TestTaskStatusSync_Idempotent(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx)
	ev := transitionEvent(t, "E-1", tid, "active")
	if err := proj.Project(ctx, ev); err != nil {
		t.Fatal(err)
	}
	// Re-deliver the SAME event id: AppliedStore must short-circuit → still running, no error.
	if err := proj.Project(ctx, ev); err != nil {
		t.Fatalf("re-delivery must be a no-op, got %v", err)
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskRunning {
		t.Fatalf("want running after idempotent re-delivery, got %s", got)
	}
}

func TestTaskStatusSync_WakeAlreadyRunningNoop(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx)
	if err := proj.Project(ctx, transitionEvent(t, "E-1", tid, "active")); err != nil {
		t.Fatal(err)
	}
	// A second active transition (wake: waiting_input→active) on an already-running
	// task must be a no-op, not an illegal-transition error.
	if err := proj.Project(ctx, transitionEvent(t, "E-2", tid, "active")); err != nil {
		t.Fatalf("active on already-running task must be a no-op, got %v", err)
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskRunning {
		t.Fatalf("want running, got %s", got)
	}
}

func TestTaskStatusSync_NonActiveIgnored(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx)
	// done/failed/etc are not the active→Start trigger → task stays open (#2
	// active→Start scope; done is owned by complete_task, B3 handled separately).
	for _, st := range []string{"done", "failed", "queued", "canceled", "superseded", "waiting_input"} {
		if err := proj.Project(ctx, transitionEvent(t, "E-"+st, tid, st)); err != nil {
			t.Fatalf("status %s: %v", st, err)
		}
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskOpen {
		t.Fatalf("non-active transitions must not change task status; want open, got %s", got)
	}
}

func TestTaskStatusSync_NonMatchingEventIgnored(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx)
	if err := proj.Project(ctx, outbox.Event{ID: "E-x", EventType: "pm.task.assigned", Payload: "{}"}); err != nil {
		t.Fatal(err)
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskOpen {
		t.Fatalf("unrelated event must be ignored, got %s", got)
	}
}

func transitionEventCause(t *testing.T, eventID string, tid pm.TaskID, status, cause string) outbox.Event {
	t.Helper()
	pl, _ := json.Marshal(agentsvc.WorkItemTransitionPayload{
		WorkItemID: "WI-1", AgentID: "AG1", TaskRef: "pm://tasks/" + string(tid),
		PrevStatus: "active", Status: status, Version: 3, OccurredAt: time.Unix(1_700_000_000, 0).UTC(), Cause: cause,
	})
	return outbox.Event{ID: eventID, EventType: agentsvc.EvtAgentWorkItemTransitioned, Payload: string(pl)}
}

func TestTaskStatusSync_B3AgentDeathBlocksRunningTask(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx)
	if err := proj.Project(ctx, transitionEvent(t, "E-1", tid, "active")); err != nil { // assigned→running
		t.Fatal(err)
	}
	if err := proj.Project(ctx, transitionEventCause(t, "E-2", tid, "failed", agentpkg.WorkItemCauseAgentDeath)); err != nil {
		t.Fatal(err)
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskBlocked {
		t.Fatalf("B3 agent-death failure must block the running task, got %s", got)
	}
}

// v2.8.1 #278 (b) fix: an L2 single-turn failure (failed WITHOUT cause, e.g.
// rate_limit) on a RUNNING task now ALSO blocks it — closing the limbo gap where
// the task stayed running while its WorkItem was failed (UI lied "running"). Both
// fail sources (agent_death AND no-cause) → Task.Block, since the projector keys
// on the WI failed transition, not a specific cause.
func TestTaskStatusSync_L2FailedBlocksRunningTask(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx)
	_ = proj.Project(ctx, transitionEvent(t, "E-1", tid, "active")) // →running
	// L2 single-turn failure = failed WITHOUT cause → must block (no more limbo).
	if err := proj.Project(ctx, transitionEventCause(t, "E-2", tid, "failed", "")); err != nil {
		t.Fatal(err)
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskBlocked {
		t.Fatalf("L2 single-turn failure on a running task must block (no limbo), got %s", got)
	}
}

func TestTaskStatusSync_B3OnOpenTaskLeavesOpen(t *testing.T) {
	svc, proj, _, ctx := taskSyncSetup(t)
	tid := assignedTask(t, svc, ctx) // never activated → task still open
	// Agent dies before activation: open→blocked is illegal → leave open
	// (edge per 口径; agent-level failed already surfaces, user reassigns).
	if err := proj.Project(ctx, transitionEventCause(t, "E-1", tid, "failed", agentpkg.WorkItemCauseAgentDeath)); err != nil {
		t.Fatal(err)
	}
	if got := taskStatus(t, svc, ctx, tid); got != pm.TaskOpen {
		t.Fatalf("B3 on a not-yet-running task must leave it open (no illegal block), got %s", got)
	}
}
