package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// workDeliverySetup wires the Service + a WorkItemProjector with the D2-c-i work
// delivery deps (ControlLog + agents repo + tasks repo) + a relay, plus the
// agent repo so a test can seed an Agent with a WorkerID. It returns everything
// a test needs to assert both the queued WorkItem and the enqueued agent.work
// command.
func workDeliverySetup(t *testing.T) (svc *Service, agents *agentsql.AgentRepo, wiRepo *agentsql.WorkItemRepo, controlLog *environment.ControlLog, relay *outbox.Relay, ctx context.Context) {
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
	wiRepo = agentsql.NewWorkItemRepo(db)
	agents = agentsql.NewAgentRepo(db)
	tasks := pmsql.NewTaskRepo(db)
	controlLog = environment.NewControlLog(envsql.NewControlEventRepo(db), gen, clk)

	svc = New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, AgentDir: allOrgDir("org-1"), IDGen: gen, Clock: clk,
	})
	partProj := NewParticipantProjector(db, convsql.NewConversationRepo(db), applied, gen, clk)
	wiProj := NewWorkItemProjectorWithDeps(WorkItemProjectorDeps{
		DB: db, WorkItems: wiRepo, Applied: applied, IDGen: gen, Clock: clk,
		ControlLog: controlLog, Agents: agents, Tasks: tasks,
	})
	relay = outbox.NewRelay(ob, applied, clk, partProj, wiProj)
	return svc, agents, wiRepo, controlLog, relay, context.Background()
}

// seedAgentWithWorker inserts a RUNNING Agent bound to workerID directly via the
// repo. Running is the dispatch-relevant state: FINDING-1 PART ①'s lifecycle guard
// only enqueues agent.work for a running agent, so the work-delivery positive
// tests need the assignee running.
func seedAgentWithWorker(t *testing.T, agents *agentsql.AgentRepo, ctx context.Context, id, workerID string) {
	t.Helper()
	seedAgentWithWorkerLifecycle(t, agents, ctx, id, workerID, agentpkg.LifecycleRunning)
}

// seedAgentWithWorkerLifecycle inserts an Agent bound to workerID in the given
// lifecycle (rehydrated directly so any state is reachable for guard tests).
func seedAgentWithWorkerLifecycle(t *testing.T, agents *agentsql.AgentRepo, ctx context.Context, id, workerID string, lc agentpkg.AgentLifecycle) {
	t.Helper()
	at := time.Unix(1_700_000_000, 0).UTC()
	a, err := agentpkg.RehydrateAgent(agentpkg.RehydrateAgentInput{
		ID:             agentpkg.AgentID(id),
		OrganizationID: "org-1",
		Profile:        agentpkg.Profile{Name: id},
		WorkerID:       workerID,
		Lifecycle:      lc,
		CreatedBy:      "system",
		CreatedAt:      at,
		UpdatedAt:      at,
		Version:        1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agents.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
}

// TestWorkDelivery_EnqueuesAgentWork is the D2-c-i Part A acceptance: assigning a
// Task to an agent whose Agent AR has a WorkerID creates a queued AgentWorkItem
// AND enqueues an agent.work command on that worker's ControlLog (command_type
// "agent.work", idempotency_key "agent.work:<workItemID>", payload carrying
// agent_id/work_item_id/task_ref/brief). Re-projecting the same event does NOT
// double-enqueue.
func TestWorkDelivery_EnqueuesAgentWork(t *testing.T) {
	svc, agents, wiRepo, controlLog, relay, ctx := workDeliverySetup(t)
	seedAgentWithWorker(t, agents, ctx, "AG1", "W1")

	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{
		ProjectID: pid, Title: "Fix the bug", Description: "It crashes on boot.", CreatedBy: "user:a",
	})
	taskRef := "pm://tasks/" + string(tid)

	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}

	// A queued WorkItem exists.
	items, _ := wiRepo.ListByTask(ctx, taskRef)
	if len(items) != 1 || items[0].Status() != agentpkg.WorkItemQueued {
		t.Fatalf("want 1 queued work item, got %+v", items)
	}
	wiID := items[0].ID()

	// Two commands were enqueued on W1 (in append order): the (old) agent.work
	// push + the (v2.8.1 #278 PR2, additive) agent.work_available wake.
	cmds, err := controlLog.CommandsAfter(ctx, environment.WorkerID("W1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("want 2 control commands on W1 (agent.work + agent.work_available), got %d", len(cmds))
	}
	cmd := cmds[0]
	if cmd.CommandType() != "agent.work" {
		t.Fatalf("command_type = %q, want agent.work", cmd.CommandType())
	}
	if cmd.IdempotencyKey() != "agent.work:"+wiID {
		t.Fatalf("idempotency_key = %q, want agent.work:%s", cmd.IdempotencyKey(), wiID)
	}
	var pl struct {
		AgentID    string `json:"agent_id"`
		WorkItemID string `json:"work_item_id"`
		TaskRef    string `json:"task_ref"`
		Brief      string `json:"brief"`
	}
	if err := json.Unmarshal([]byte(cmd.Payload()), &pl); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if pl.AgentID != "AG1" || pl.WorkItemID != wiID || pl.TaskRef != taskRef {
		t.Fatalf("payload mismatch: %+v (wiID=%s, taskRef=%s)", pl, wiID, taskRef)
	}
	if pl.Brief != "Fix the bug\n\nIt crashes on boot." {
		t.Fatalf("brief = %q, want title+description", pl.Brief)
	}

	// v2.8.1 #278 PR2: the additive agent.work_available WAKE command (per-agent
	// "pull your queue"), idempotency_key "agent.work_available:<wiID>", payload
	// agent_id/work_item_id.
	wake := cmds[1]
	if wake.CommandType() != "agent.work_available" {
		t.Fatalf("command_type[1] = %q, want agent.work_available", wake.CommandType())
	}
	if wake.IdempotencyKey() != "agent.work_available:"+wiID {
		t.Fatalf("wake idempotency_key = %q, want agent.work_available:%s", wake.IdempotencyKey(), wiID)
	}
	var wpl struct {
		AgentID    string `json:"agent_id"`
		WorkItemID string `json:"work_item_id"`
	}
	if err := json.Unmarshal([]byte(wake.Payload()), &wpl); err != nil {
		t.Fatalf("wake payload not JSON: %v", err)
	}
	if wpl.AgentID != "AG1" || wpl.WorkItemID != wiID {
		t.Fatalf("wake payload mismatch: %+v (wiID=%s)", wpl, wiID)
	}

	// Re-projecting the SAME event is idempotent: no extra WorkItem, no extra
	// commands (both idempotency keys collapse the re-enqueue).
	if n, _ := relay.RunOnce(ctx, 100); n != 0 {
		t.Fatalf("replay processed %d events, want 0", n)
	}
	items2, _ := wiRepo.ListByTask(ctx, taskRef)
	if len(items2) != 1 {
		t.Fatalf("replay created extra work items: %d", len(items2))
	}
	cmds2, _ := controlLog.CommandsAfter(ctx, environment.WorkerID("W1"), 0)
	if len(cmds2) != 2 {
		t.Fatalf("replay double-enqueued commands: got %d, want 2 (agent.work + agent.work_available)", len(cmds2))
	}
}

// TestWorkDelivery_AgentNotRunning_NoEnqueue is FINDING-1 PART ① (task #115): a
// session-less (non-running) assignee Agent still gets a queued WorkItem created,
// but NO agent.work command is enqueued (the lifecycle guard graceful-skips —
// delivery is deferred to AgentControlProjector's re-emit on →running). This is
// what prevents the daemon work() "no running session" infinite-retry HOL deadlock.
func TestWorkDelivery_AgentNotRunning_NoEnqueue(t *testing.T) {
	svc, agents, wiRepo, controlLog, relay, ctx := workDeliverySetup(t)
	// Seed the agent STOPPED (the default NewAgent state) — has a worker binding but
	// no running session.
	seedAgentWithWorkerLifecycle(t, agents, ctx, "AG1", "W1", agentpkg.LifecycleStopped)

	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "T", CreatedBy: "user:a"})
	taskRef := "pm://tasks/" + string(tid)

	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatalf("projection must not fail on non-running agent: %v", err)
	}

	// The WorkItem is still created/active (delivery deferred, not dropped).
	items, _ := wiRepo.ListByTask(ctx, taskRef)
	if len(items) != 1 || items[0].Status() != agentpkg.WorkItemQueued {
		t.Fatalf("want 1 queued work item for non-running agent, got %+v", items)
	}
	// But NO agent.work command was enqueued on W1.
	cmds, err := controlLog.CommandsAfter(ctx, environment.WorkerID("W1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Fatalf("non-running agent must NOT enqueue agent.work, got %d commands", len(cmds))
	}
}

// TestWorkDelivery_AgentWithNoWorker_NoEnqueue proves the nil-worker guardrail:
// an assignee Agent that is unresolvable (no Agent AR seeded → no worker binding)
// still creates the queued WorkItem but enqueues NO command and does NOT fail.
func TestWorkDelivery_AgentWithNoWorker_NoEnqueue(t *testing.T) {
	svc, _, wiRepo, controlLog, relay, ctx := workDeliverySetup(t)
	// Note: we do NOT seed an Agent AR for AG_NOWORKER, so the agents repo lookup
	// fails → enqueue is skipped (logged) without failing the projection.

	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "T", CreatedBy: "user:a"})
	taskRef := "pm://tasks/" + string(tid)

	if err := svc.AssignTask(ctx, tid, "agent:AG_NOWORKER", "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatalf("projection must not fail on unresolvable agent: %v", err)
	}

	items, _ := wiRepo.ListByTask(ctx, taskRef)
	if len(items) != 1 || items[0].Status() != agentpkg.WorkItemQueued {
		t.Fatalf("want 1 queued work item even with no worker, got %+v", items)
	}
	// No command enqueued on any worker — check the (only) candidate worker id is
	// empty; assert nothing landed under the agent-derived worker. Since the
	// agent was never seeded, there is no worker; assert the WorkItem's agent has
	// no enqueue by checking the empty-worker stream is empty.
	cmds, err := controlLog.CommandsAfter(ctx, environment.WorkerID(""), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Fatalf("expected no agent.work enqueue for unresolvable agent, got %d", len(cmds))
	}
}
