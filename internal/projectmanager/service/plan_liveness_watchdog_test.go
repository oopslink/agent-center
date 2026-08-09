package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func planLivenessEvents(t *testing.T, h *planAdvanceHarness) []planLivenessWatchdogPayload {
	t.Helper()
	ob := outboxsql.NewOutboxRepo(h.svc.db)
	events, err := ob.FetchUnprocessed(h.ctx, 1000)
	if err != nil {
		t.Fatalf("FetchUnprocessed: %v", err)
	}
	var out []planLivenessWatchdogPayload
	for _, e := range events {
		if e.EventType != EvtPlanLivenessWatchdog {
			continue
		}
		var payload planLivenessWatchdogPayload
		if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil {
			t.Fatalf("watchdog payload: %v", err)
		}
		out = append(out, payload)
	}
	return out
}

func unprocessedEventTypes(t *testing.T, h *planAdvanceHarness) []string {
	t.Helper()
	ob := outboxsql.NewOutboxRepo(h.svc.db)
	events, err := ob.FetchUnprocessed(h.ctx, 1000)
	if err != nil {
		t.Fatalf("FetchUnprocessed: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.EventType)
	}
	return out
}

func countEventType(events []string, eventType string) int {
	count := 0
	for _, got := range events {
		if got == eventType {
			count++
		}
	}
	return count
}

func restartPlanService(h *planAdvanceHarness) *Service {
	return New(Deps{
		DB: h.svc.db, Projects: h.svc.projects, Members: h.svc.members,
		Issues: h.svc.issues, Tasks: h.svc.tasks, TaskSubs: h.svc.taskSubs,
		IssueSubs: h.svc.issueSubs, CodeRepoRefs: h.svc.codeRepoRefs,
		Plans: h.svc.plans, Outbox: h.svc.outbox, IDGen: h.svc.idgen, Clock: h.svc.clock,
		AgentDir: h.svc.agentDir, CodeRepoResolver: h.svc.codeRepoResolver, OrgSeq: h.svc.orgSeq,
		PlanDispatcher: h.svc.planDispatcher, Findings: h.svc.findings,
		PausedTasks: h.svc.pausedTasks, NodeResumer: h.svc.nodeResumer,
		PoolClaimLimit: h.svc.poolClaimLimit, TaskActionLogs: h.svc.actionLogs,
		Audit: h.svc.audit, AutoAssignDir: h.svc.autoAssignDir,
		AutoAssignSettings: h.svc.autoAssignSettings, Orch: h.svc.orch,
		Stages: h.svc.stages, AssignmentPools: h.svc.pools,
		Remediation: h.svc.remediation, DeadlinePolicy: h.svc.deadlinePolicy,
		TimeoutSink: h.svc.timeoutSink, LiveExecutors: h.svc.liveExecutors,
	})
}

func TestPlanLivenessWatchdog_ReplaysPausedRejectAfterResumeAndRestart(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "watchdog paused reject", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, _, stageA, _ := seedTwoStagePlan(t, h, projectID, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	stage, _ := h.svc.GetStage(ctx, stageA)
	gateTaskID := stage.Stage.GateTaskID()
	h.setTaskStatus(t, gateTaskID, pm.TaskCompleted)
	if err := h.svc.PausePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	cmd := RecordStageGateVerdictCommand{
		GateTaskID: gateTaskID, Outcome: pm.GateVerdictReject,
		Evidence: "paused review found an edge case", ReviewedSHA: "deadbeef",
		IdempotencyKey: "watchdog-paused-reject-1", Actor: "user:a",
	}
	paused, err := h.svc.RecordStageGateVerdict(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if paused.StageID != "" || paused.Continuation == nil || paused.Continuation.Status != pm.ContinuationAwaitingRemediation {
		t.Fatalf("paused reject result=%+v, want awaiting continuation and no topology", paused)
	}
	if err := h.svc.ResumePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	stages, _ := h.svc.stages.ListByPlan(ctx, planID)
	if len(stages) != 2 {
		t.Fatalf("watchdog fired before threshold: stages=%d want 2", len(stages))
	}

	h.clk.Advance(PlanLivenessDeadEndAfter + time.Second)
	h.svc = restartPlanService(h)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	stages, _ = h.svc.stages.ListByPlan(ctx, planID)
	if len(stages) != 3 {
		t.Fatalf("watchdog did not append remediation stage after restart: stages=%d want 3", len(stages))
	}
	events := planLivenessEvents(t, h)
	if len(events) != 1 {
		t.Fatalf("watchdog events=%d want 1 (unprocessed=%v)", len(events), unprocessedEventTypes(t, h))
	}
	if events[0].Reason != planLivenessReasonUnhandledReject || events[0].Action != planLivenessActionReplayRejectVerdict {
		t.Fatalf("watchdog event reason/action=%s/%s", events[0].Reason, events[0].Action)
	}
	if countEventType(unprocessedEventTypes(t, h), EvtRemediationStageAppended) != 1 {
		t.Fatal("watchdog recovery did not emit remediation-stage-appended")
	}
}

func TestPlanLivenessWatchdog_DoesNotFlagNormalUpstreamWait(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "normal upstream", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	h.clk.Advance(PlanLivenessDeadEndAfter + time.Second)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if events := planLivenessEvents(t, h); len(events) != 0 {
		t.Fatalf("normal upstream/dispatched frontier produced watchdog events: %+v", events)
	}
}

func TestPlanLivenessWatchdog_EscalatesAcceptanceWaitAfterThreshold(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "acceptance wait", CreatedBy: "user:a"})
	h.drain(t)
	dev := h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	dec := h.seedAssignedTask(t, pid, planID, "Decision", "user:pd")
	merge := h.seedAssignedTask(t, pid, planID, "merge to main", "user:int")
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: merge, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: merge, ToTaskID: dev, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	baseMsgs := h.planConvMsgCount(t, planID)

	h.clk.Advance(PlanLivenessDeadEndAfter + time.Second)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	events := planLivenessEvents(t, h)
	if len(events) != 1 {
		t.Fatalf("watchdog events=%d want 1", len(events))
	}
	if events[0].Reason != planLivenessReasonAcceptanceWait || events[0].Action != planLivenessActionEscalate {
		t.Fatalf("watchdog event reason/action=%s/%s", events[0].Reason, events[0].Action)
	}
	if got := h.planConvMsgCount(t, planID) - baseMsgs; got != 1 {
		t.Fatalf("watchdog escalation messages=%d want 1", got)
	}
}

func TestPlanLivenessWatchdog_DetectsLeaseOnlyRunningAndTriggersRecovery(t *testing.T) {
	h, _ := planGraphSetup(t)
	store := concurrency.NewInMemoryStore()
	h.svc.liveExecutors = store
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "lease only", CreatedBy: "user:a"})
	h.drain(t)
	taskID := h.seedAssignedTask(t, pid, planID, "A", "agent:bot")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, taskID, pm.TaskRunning)
	store.Put("bot", concurrency.AgentSnapshot{Active: 0, Executors: nil}, h.clk.Now())
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	h.clk.Advance(PlanLivenessDeadEndAfter + time.Second)
	store.Put("bot", concurrency.AgentSnapshot{Active: 0, Executors: nil}, h.clk.Now())
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	events := planLivenessEvents(t, h)
	if len(events) != 1 {
		t.Fatalf("watchdog events=%d want 1", len(events))
	}
	if events[0].Reason != planLivenessReasonLeaseOnlyRunning || events[0].Action != planLivenessActionTriggerStuckRecovery {
		t.Fatalf("watchdog event reason/action=%s/%s", events[0].Reason, events[0].Action)
	}
	if !h.svc.isStuckTracked(taskID) {
		t.Fatal("lease-only watchdog did not enter stuck-node recovery tracking")
	}

	// A repeated sweep within the alert window keeps recovery tracking but does not spam
	// another liveness event.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	events = planLivenessEvents(t, h)
	if len(events) != 1 {
		t.Fatalf("repeated lease-only sweep emitted %d watchdog events, want 1", len(events))
	}
}
