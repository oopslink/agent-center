package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// These tests guard the PRODUCTION outbox-relay wiring (App.outboxProjectors) — the
// single source runWebConsole uses. They exist because of the #266 P1 headline
// blocker: PlanParticipantProjector consumes pm.plan.created → creates the Plan's
// 1:1 conversation + binds conversation_id, but it was wired ONLY in service/handler
// tests (which hand-build their own relay) and NEVER registered in the production
// relay → in the real server a Plan created via HTTP got no conversation →
// `advance` 500 "no conversation to dispatch into" → the whole headline (advance →
// @mention → wake agent) was dead. A hand-wired relay can never catch that; these
// run against App.outboxProjectors.

// productionRelay builds the relay exactly as runWebConsole does (same deps, same
// single-source projector list), returning the outbox repo so the test can emit.
func productionRelay(t *testing.T, app *App) (*outbox.Relay, *outboxsql.OutboxRepo) {
	t.Helper()
	outboxRepo := outboxsql.NewOutboxRepo(app.DB)
	appliedRepo := outboxsql.NewAppliedRepo(app.DB)
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(app.DB), app.IDGen, app.Clock)
	projectors, _ := app.outboxProjectors(outboxRepo, appliedRepo, controlLog)
	return outbox.NewRelay(outboxRepo, appliedRepo, app.Clock, projectors...), outboxRepo
}

// TestOutboxProjectors_RegistersEventConsumers is the deterministic class-guard:
// every outbox event emitted by a Service MUST have its consuming projector
// registered in the production relay list. Dropping any (as #266 dropped the
// plan-participant projector) makes this FAIL — cheaply, in CI, with no async.
func TestOutboxProjectors_RegistersEventConsumers(t *testing.T) {
	app := newTestApp(t)
	outboxRepo := outboxsql.NewOutboxRepo(app.DB)
	appliedRepo := outboxsql.NewAppliedRepo(app.DB)
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(app.DB), app.IDGen, app.Clock)
	projectors, _ := app.outboxProjectors(outboxRepo, appliedRepo, controlLog)

	got := map[string]bool{}
	for _, p := range projectors {
		got[p.Name()] = true
	}
	// The consumers the production relay must register. pm-plan-participant-sync is
	// the #266 regression: it consumes pm.plan.created → binds the Plan conversation.
	for _, required := range []string{
		"pm-plan-participant-sync", // #266 — was missing → headline dead
		"pm-plan-orchestrator",     // P2-1 — the AUTO-ADVANCE core; dropping it = auto-advance silently dead (the #266 lesson)
		"pm-participant-sync",
		"env-agent-control",
		"conv-agent-wake",
	} {
		if !got[required] {
			t.Errorf("App.outboxProjectors() does not register %q — the outbox events it consumes would have no consumer in the production relay (this is exactly how #266 broke the Plan headline)", required)
		}
	}
}

// TestProductionRelay_PlanCreated_BindsConversation is the behavioral guard: a
// pm.plan.created event drained through the PRODUCTION relay must create the
// Plan's 1:1 conversation and bind conversation_id back onto the Plan. Before the
// #266 fix this stayed "" (no consumer); after, it binds.
func TestProductionRelay_PlanCreated_BindsConversation(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	planRepo := pmsql.NewPlanRepo(app.DB)

	plan, err := pm.NewPlan(pm.NewPlanInput{
		ID:         pm.PlanID("plan-reltest"),
		ProjectID:  pm.ProjectID("proj-reltest"),
		Name:       "v3.0",
		CreatorRef: pm.IdentityRef("user:reltester"),
		CreatedAt:  app.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	if err := planRepo.Save(ctx, plan); err != nil {
		t.Fatalf("Save plan: %v", err)
	}
	if plan.ConversationID() != "" {
		t.Fatalf("precondition: new Plan should have empty conversation_id, got %q", plan.ConversationID())
	}

	payload, _ := json.Marshal(map[string]any{
		"plan_id":         "plan-reltest",
		"project_id":      "proj-reltest",
		"organization_id": "org-reltest",
		"owner_ref":       "pm://plans/plan-reltest",
		"creator_ref":     "user:reltester",
		"participants":    []string{"user:reltester"},
	})

	relay, outboxRepo := productionRelay(t, app)
	if err := outboxRepo.Append(ctx, outbox.Event{
		ID:        app.IDGen.NewULID(),
		EventType: "pm.plan.created",
		Refs:      `{"plan_id":"plan-reltest","project_id":"proj-reltest"}`,
		Payload:   string(payload),
	}); err != nil {
		t.Fatalf("append pm.plan.created: %v", err)
	}

	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatalf("relay.RunOnce: %v", err)
	}

	bound, err := planRepo.FindByID(ctx, pm.PlanID("plan-reltest"))
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if bound.ConversationID() == "" {
		t.Fatalf("production relay did not bind conversation_id after pm.plan.created — the PlanParticipantProjector is not consuming it in the production list (#266 regression)")
	}
}

// drainRelay runs the production relay until the outbox is empty (events emitted
// by projectors can cascade, so loop until quiescent).
func drainRelay(t *testing.T, relay *outbox.Relay) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		n, err := relay.RunOnce(ctx, 100)
		if err != nil {
			t.Fatalf("relay.RunOnce: %v", err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("relay did not quiesce after 50 passes")
}

// TestProductionRelay_AutoAdvance_OnTaskDone is the P2-1 #266 behavioral guard:
// a RUNNING plan A→B, A completed → pm.task.state_changed drained through the
// PRODUCTION relay must AUTO-DISPATCH B (dispatch record written) — no manual
// AdvancePlan. If the PlanOrchestratorProjector is dropped from
// App.outboxProjectors, the event has no consumer and B is never dispatched →
// this FAILS. It also asserts pm.plan.started auto-dispatches the initial node A.
func TestProductionRelay_AutoAdvance_OnTaskDone(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	svc := app.PMService
	planRepo := pmsql.NewPlanRepo(app.DB)

	pid, err := svc.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: "org-rel", Name: "P", CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	relay, _ := productionRelay(t, app)
	planID, err := svc.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "auto", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	// Drain so the Plan's 1:1 conversation is created + bound (else start/dispatch
	// has nowhere to @mention).
	drainRelay(t, relay)

	// Seed A and B (assigned, selected into the plan), B depends_on A.
	mkTask := func(title, assignee string) pm.TaskID {
		tid, terr := svc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:a"})
		if terr != nil {
			t.Fatalf("CreateTask(%s): %v", title, terr)
		}
		a := assignee
		if uerr := svc.BatchUpdateTask(ctx, tid, pmservice.BatchTaskPatch{Assignee: &a}, "user:a"); uerr != nil {
			t.Fatalf("assign(%s): %v", title, uerr)
		}
		if serr := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); serr != nil {
			t.Fatalf("select(%s): %v", title, serr)
		}
		drainRelay(t, relay)
		return tid
	}
	a := mkTask("A", "user:x")
	b := mkTask("B", "user:y")
	if err := svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil { // B depends_on A
		t.Fatalf("AddPlanDependency: %v", err)
	}

	// Start the plan → emits pm.plan.started → orchestrator dispatches initial node A.
	if err := svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	drainRelay(t, relay)

	recs, err := planRepo.ListDispatchRecords(ctx, planID)
	if err != nil {
		t.Fatalf("ListDispatchRecords: %v", err)
	}
	if !hasDispatch(recs, a) {
		t.Fatalf("pm.plan.started did NOT auto-dispatch initial node A via the production relay (orchestrator not consuming?) — records=%v", recs)
	}
	if hasDispatch(recs, b) {
		t.Fatal("B dispatched before A done — should be blocked")
	}

	// A done → pm.task.state_changed → orchestrator re-dispatches the now-ready B.
	if err := svc.SetTaskStatus(ctx, a, pm.TaskCompleted, "user:a"); err != nil {
		t.Fatalf("SetTaskStatus(A done): %v", err)
	}
	drainRelay(t, relay)

	recs, err = planRepo.ListDispatchRecords(ctx, planID)
	if err != nil {
		t.Fatalf("ListDispatchRecords: %v", err)
	}
	if !hasDispatch(recs, b) {
		t.Fatalf("AUTO-ADVANCE failed: A done did NOT dispatch downstream B through the production relay (PlanOrchestratorProjector missing/unregistered?) — records=%v", recs)
	}
	// §9.3 replay idempotency: re-draining (no new events) keeps EXACTLY one record
	// each for A and B (no double-wake).
	drainRelay(t, relay)
	recs, _ = planRepo.ListDispatchRecords(ctx, planID)
	if countDispatch(recs, a) != 1 || countDispatch(recs, b) != 1 {
		t.Fatalf("idempotency broken: A=%d B=%d dispatch records, want 1 each (§9.3)", countDispatch(recs, a), countDispatch(recs, b))
	}
}

// productionRelayWithControlLog mirrors productionRelay but returns the SAME
// ControlLog the relay's projectors enqueue onto, so a test can assert the
// agent.work_available wake the WorkItemProjector delivers through the real set.
func productionRelayWithControlLog(t *testing.T, app *App) (*outbox.Relay, *outboxsql.OutboxRepo, *environment.ControlLog) {
	t.Helper()
	outboxRepo := outboxsql.NewOutboxRepo(app.DB)
	appliedRepo := outboxsql.NewAppliedRepo(app.DB)
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(app.DB), app.IDGen, app.Clock)
	projectors, _ := app.outboxProjectors(outboxRepo, appliedRepo, controlLog)
	return outbox.NewRelay(outboxRepo, appliedRepo, app.Clock, projectors...), outboxRepo, controlLog
}

// TestProductionRelay_AgentCreatorFailureWake is the v2.9 P3 #266-class behavioral
// guard: when a plan task FAILS and the plan's CREATOR is an AGENT, the orchestrator
// emits pm.plan.creator_failure_wake → the (production-registered) WakeProjector
// must consume it and enqueue an agent.converse on the agent-creator's worker
// control stream, driven through the REAL App.outboxProjectors() set. This proves
// the WakeProjector HANDLES the new event in PRODUCTION (registered ≠ handles — the
// #282 lesson): a system @mention can never wake an agent (#220 / #185), so without
// this direct wake the agent-creator would never self-handle the failure. If the new
// case were missing from the WakeProjector switch, no converse fires → this FAILS.
func TestProductionRelay_AgentCreatorFailureWake(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	svc := app.PMService

	// Seed a RUNNING agent-creator bound to worker W1 (the wake must deliver here).
	agents := agentsql.NewAgentRepo(app.DB)
	at := app.Clock.Now()
	ag, err := agentpkg.RehydrateAgent(agentpkg.RehydrateAgentInput{
		ID: agentpkg.AgentID("CREATORBOT"), OrganizationID: "org-rel",
		Profile: agentpkg.Profile{Name: "CREATORBOT"}, WorkerID: "W1",
		Lifecycle: agentpkg.LifecycleRunning, CreatedBy: "system",
		CreatedAt: at, UpdatedAt: at, Version: 1,
	})
	if err != nil {
		t.Fatalf("RehydrateAgent: %v", err)
	}
	if err := agents.Save(ctx, ag); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	pid, err := svc.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: "org-rel", Name: "P", CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// The agent-creator must be a project member to pass CreatePlan's write-gate.
	if _, err := svc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
		ProjectID: pid, IdentityID: "agent:CREATORBOT", Role: pm.RoleMember, Actor: "user:a",
	}); err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}

	relay, _, controlLog := productionRelayWithControlLog(t, app)
	// Plan CREATED BY THE AGENT (creator_ref = agent:CREATORBOT).
	planID, err := svc.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "agentplan", CreatedBy: "agent:CREATORBOT"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	drainRelay(t, relay) // bind the plan conversation

	// A task assigned to a HUMAN (so the failure does not also deliver work to the
	// agent-creator via the work path — the converse below is the P3 wake, isolated).
	tid, err := svc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "do the thing", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	assignee := "user:x"
	if err := svc.BatchUpdateTask(ctx, tid, pmservice.BatchTaskPatch{Assignee: &assignee}, "user:a"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if err := svc.StartPlan(ctx, planID, "agent:CREATORBOT"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	drainRelay(t, relay)

	planRepo := pmsql.NewPlanRepo(app.DB)
	plan, err := planRepo.FindByID(ctx, planID)
	if err != nil {
		t.Fatalf("FindByID plan: %v", err)
	}
	convID := plan.ConversationID()
	if convID == "" {
		t.Fatal("plan conversation not bound")
	}

	baseCmds, _ := controlLog.CommandsAfter(ctx, environment.WorkerID("W1"), 0)

	// The task FAILS → orchestrator @mentions the agent-creator AND emits
	// pm.plan.creator_failure_wake → WakeProjector enqueues agent.converse on W1.
	if err := svc.SetTaskStatus(ctx, tid, pm.TaskDiscarded, "user:a"); err != nil {
		t.Fatalf("SetTaskStatus(fail): %v", err)
	}
	drainRelay(t, relay)

	cmds, err := controlLog.CommandsAfter(ctx, environment.WorkerID("W1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	var converse *environment.WorkerControlEvent
	for i := len(cmds) - 1; i >= len(baseCmds); i-- {
		if cmds[i].CommandType() == "agent.converse" {
			converse = cmds[i]
			break
		}
	}
	if converse == nil {
		t.Fatalf("production relay did NOT enqueue agent.converse on agent-creator plan-task failure — the WakeProjector is not handling pm.plan.creator_failure_wake in production (registered ≠ handles, #282)")
	}
	// The converse targets the plan conversation + the agent-creator's entity id.
	wantPrefix := "agent.converse:" + convID + ":"
	if !strings.HasPrefix(converse.IdempotencyKey(), wantPrefix) || !strings.HasSuffix(converse.IdempotencyKey(), ":CREATORBOT") {
		t.Fatalf("converse idempotency_key = %q, want %s<failureMsgID>:CREATORBOT", converse.IdempotencyKey(), wantPrefix)
	}
	if !strings.Contains(converse.Payload(), `"agent_id":"CREATORBOT"`) {
		t.Fatalf("converse payload missing agent-creator id: %s", converse.Payload())
	}

	// Replay idempotency through the production relay: re-draining (no new events)
	// enqueues no second converse for the same failure.
	drainRelay(t, relay)
	cmds2, _ := controlLog.CommandsAfter(ctx, environment.WorkerID("W1"), 0)
	converseCount := 0
	for _, c := range cmds2 {
		if c.CommandType() == "agent.converse" {
			converseCount++
		}
	}
	if converseCount != 1 {
		t.Fatalf("idempotency broken: %d agent.converse for the agent-creator, want 1 (P3)", converseCount)
	}
}

func hasDispatch(recs []pm.DispatchRecord, tid pm.TaskID) bool {
	return countDispatch(recs, tid) > 0
}

func countDispatch(recs []pm.DispatchRecord, tid pm.TaskID) int {
	n := 0
	for _, r := range recs {
		if r.TaskID == tid {
			n++
		}
	}
	return n
}
