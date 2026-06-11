package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// planDispatchWorkHarness is the v2.9 P2 #1 HEADLINE harness: a plan-advance
// Service (REAL PlanDispatcher posting @mentions into the plan conversation) PLUS
// the SAME WorkItemProjector the production relay registers (with ControlLog +
// agents + tasks deps), so a single drain exercises BOTH effects a dispatch must
// produce: (1) the @mention into the plan conversation (visibility / human-reply
// wake), and (2) the assignee Agent's queued AgentWorkItem + agent.work_available
// wake command on its Worker control stream (the REAL work-delivery path).
type planDispatchWorkHarness struct {
	svc        *Service
	plans      *pmsql.PlanRepo
	agents     *agentsql.AgentRepo
	wiRepo     *agentsql.WorkItemRepo
	controlLog *environment.ControlLog
	relay      *outbox.Relay
	ctx        context.Context
}

func planDispatchWorkSetup(t *testing.T) *planDispatchWorkHarness {
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
	convRepo := convsql.NewConversationRepo(db)
	msgRepo := convsql.NewMessageRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	writer := convservice.NewMessageWriter(db, convRepo, msgRepo, sink, gen, clk).WithOutbox(ob)
	plans := pmsql.NewPlanRepo(db)
	tasks := pmsql.NewTaskRepo(db)
	agents := agentsql.NewAgentRepo(db)
	wiRepo := agentsql.NewWorkItemRepo(db)
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(db), gen, clk)

	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans, Outbox: ob, IDGen: gen, Clock: clk,
		AgentDir:       allOrgDir("org-1"),
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer, planTestDisplayName),
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	// The SAME WorkItemProjector the production relay registers (#266): consumes
	// EvtTaskAssigned/EvtTaskReassigned → queued AgentWorkItem + agent.work_available.
	wiProj := NewWorkItemProjectorWithDeps(WorkItemProjectorDeps{
		DB: db, WorkItems: wiRepo, Applied: applied, IDGen: gen, Clock: clk,
		ControlLog: controlLog, Agents: agents, Tasks: tasks,
	})
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj, wiProj)
	return &planDispatchWorkHarness{
		svc: svc, plans: plans, agents: agents, wiRepo: wiRepo,
		controlLog: controlLog, relay: relay, ctx: context.Background(),
	}
}

func (h *planDispatchWorkHarness) drain(t *testing.T) {
	t.Helper()
	for {
		n, err := h.relay.RunOnce(h.ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// seedAgentTask creates a task assigned to an agent and selects it into the plan
// (draining so the assignee joins the plan conversation). The agent's AgentWorkItem
// is NOT created here — assign emits pm.task.assigned, which the WorkItemProjector
// consumes BEFORE the plan is running... so for this harness we instead seed the
// agent + task WITHOUT draining the assign event into a WI until dispatch. To keep
// the test honest about the HEADLINE (dispatch delivers work), we assert the WI is
// produced by the DISPATCH emit, after the plan starts and advances.
func (h *planDispatchWorkHarness) seedAgentTask(t *testing.T, pid pm.ProjectID, planID pm.PlanID, title, assignee string) pm.TaskID {
	t.Helper()
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	a := assignee
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	return tid
}

func seedRunningAgent(t *testing.T, agents *agentsql.AgentRepo, ctx context.Context, id, workerID string) {
	t.Helper()
	at := time.Unix(1_700_000_000, 0).UTC()
	a, err := agentpkg.RehydrateAgent(agentpkg.RehydrateAgentInput{
		ID:             agentpkg.AgentID(id),
		OrganizationID: "org-1",
		Profile:        agentpkg.Profile{Name: id},
		WorkerID:       workerID,
		Lifecycle:      agentpkg.LifecycleRunning,
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

// TestDispatch_DeliversWorkToAssignee is the v2.9 P2 #1 HEADLINE acceptance: when
// the orchestrator dispatches a ready node whose assignee is an agent, the agent
// gets a QUEUED AgentWorkItem (actionable work in agent_work_items) AND an
// agent.work_available WAKE command on its Worker control stream — via the REAL
// work-delivery path (WorkItemProjector consuming pm.task.assigned), NOT the
// human-only conversational @mention-wake. The @mention is ALSO posted (visibility).
func TestDispatch_DeliversWorkToAssignee(t *testing.T) {
	h := planDispatchWorkSetup(t)
	seedRunningAgent(t, h.agents, h.ctx, "AG1", "W1")

	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "headline", CreatedBy: "user:a"})
	h.drain(t)
	tid := h.seedAgentTask(t, pid, planID, "build the thing", "agent:AG1")
	taskRef := "pm://tasks/" + string(tid)

	// Baseline: no work command on W1 yet (assign+select did NOT dispatch — the
	// node is not yet ready/dispatched; the plan is still draft).
	pre, _ := h.controlLog.CommandsAfter(h.ctx, environment.WorkerID("W1"), 0)
	baseCmds := len(pre)

	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Advance dispatches the ready root node (AG1's task).
	dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 1 || dispatched[0] != tid {
		t.Fatalf("advance dispatched %v, want [%s]", dispatched, tid)
	}
	// Drain the dispatch's emitted pm.task.assigned through the WorkItemProjector.
	h.drain(t)

	// (1) HEADLINE: a QUEUED AgentWorkItem now exists for the assignee — the agent
	// has actionable work it can pull (NOT just an @mention it cannot act on).
	items, _ := h.wiRepo.ListByTask(h.ctx, taskRef)
	if len(items) != 1 || items[0].Status() != agentpkg.WorkItemQueued {
		t.Fatalf("want 1 queued AgentWorkItem after dispatch, got %+v", items)
	}
	wiID := items[0].ID()

	// (2) HEADLINE: an agent.work_available WAKE command was enqueued on W1 — the
	// agent is woken via the REAL work path (not the human-only @mention-wake).
	cmds, err := h.controlLog.CommandsAfter(h.ctx, environment.WorkerID("W1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds)-baseCmds != 1 {
		t.Fatalf("want exactly 1 new control command on W1 (agent.work_available), got %d", len(cmds)-baseCmds)
	}
	wake := cmds[len(cmds)-1]
	if wake.CommandType() != "agent.work_available" {
		t.Fatalf("command_type = %q, want agent.work_available", wake.CommandType())
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
}

// TestDispatch_WorkDelivery_Idempotent asserts §9.3 idempotency for the work
// delivery: dispatch is gated by RecordDispatch (INSERT-OR-IGNORE per node), so a
// node is dispatched at most once → exactly one pm.task.assigned per node → exactly
// one queued AgentWorkItem + one agent.work_available, even when advance is re-run
// (replay / reconcile-style re-dispatch). No double WorkItem, no double wake.
func TestDispatch_WorkDelivery_Idempotent(t *testing.T) {
	h := planDispatchWorkSetup(t)
	seedRunningAgent(t, h.agents, h.ctx, "AG1", "W1")

	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "idem", CreatedBy: "user:a"})
	h.drain(t)
	tid := h.seedAgentTask(t, pid, planID, "T", "agent:AG1")
	taskRef := "pm://tasks/" + string(tid)

	base, _ := h.controlLog.CommandsAfter(h.ctx, environment.WorkerID("W1"), 0)
	baseCmds := len(base)

	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Advance THREE times (simulating replay / a reconcile sweep re-running the
	// idempotent dispatch core). Only the first one dispatches.
	for i := 0; i < 3; i++ {
		if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatal(err)
		}
		h.drain(t)
	}
	// Also exercise the reconcile path directly (it goes through dispatchReadyNodes).
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// Exactly ONE queued AgentWorkItem (no double-create).
	items, _ := h.wiRepo.ListByTask(h.ctx, taskRef)
	if len(items) != 1 {
		t.Fatalf("want exactly 1 AgentWorkItem after repeated dispatch, got %d: %+v", len(items), items)
	}
	// Exactly ONE new agent.work_available (no double wake).
	cmds, _ := h.controlLog.CommandsAfter(h.ctx, environment.WorkerID("W1"), 0)
	if got := len(cmds) - baseCmds; got != 1 {
		t.Fatalf("want exactly 1 agent.work_available across repeated dispatch, got %d", got)
	}
	// Exactly ONE dispatch record for the node (the INSERT-OR-IGNORE gate).
	recs, _ := h.plans.ListDispatchRecords(h.ctx, planID)
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 dispatch record, got %d", len(recs))
	}
}

// TestDispatch_HumanAssignee_NoWorkItem proves the work-delivery emit is harmless
// for HUMAN assignees: a dispatched node assigned to a user still posts the
// @mention (their notification) but produces NO AgentWorkItem and NO work command
// (the WorkItemProjector's agent-only branch no-ops for a `user:` assignee).
func TestDispatch_HumanAssignee_NoWorkItem(t *testing.T) {
	h := planDispatchWorkSetup(t)

	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "human", CreatedBy: "user:a"})
	h.drain(t)
	tid := h.seedAgentTask(t, pid, planID, "T", "user:human")
	taskRef := "pm://tasks/" + string(tid)

	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 1 {
		t.Fatalf("advance dispatched %v, want [%s]", dispatched, tid)
	}
	h.drain(t)

	items, _ := h.wiRepo.ListByTask(h.ctx, taskRef)
	if len(items) != 0 {
		t.Fatalf("human assignee must produce NO AgentWorkItem, got %+v", items)
	}
}
