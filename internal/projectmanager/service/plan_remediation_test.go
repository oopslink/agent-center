package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

func TestStageReject_AppendsIncrementalStageWithoutReopeningHistory(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "monotonic", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, stageA, _ := seedTwoStagePlan(t, h, projectID, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
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
	oldStage, _ := h.svc.GetStage(ctx, stageA)
	oldGateTaskID := oldStage.Stage.GateTaskID()
	h.setTaskStatus(t, oldGateTaskID, pm.TaskCompleted)

	result, err := h.svc.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID: oldGateTaskID, Outcome: pm.GateVerdictReject,
		Evidence: "integration test failed on retry semantics", ReviewedSHA: "deadbeef",
		IdempotencyKey: "reject-a-1", Actor: "user:a",
	})
	if err != nil {
		t.Fatalf("RecordStageGateVerdict: %v", err)
	}
	if result.StageID == "" || result.StageID == stageA || result.Continuation == nil {
		t.Fatalf("result = %+v, want a new remediation stage and continuation", result)
	}
	for _, id := range []pm.TaskID{a1, a2, oldGateTaskID} {
		task, err := h.tasks.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status() != pm.TaskCompleted {
			t.Fatalf("historical task %s changed to %s; completed facts must be immutable", id, task.Status())
		}
	}
	stages, err := h.svc.stages.ListByPlan(ctx, planID)
	if err != nil || len(stages) != 3 {
		t.Fatalf("stages=%d err=%v, want original two + one remediation", len(stages), err)
	}
	newStage, err := h.svc.stages.FindByID(ctx, result.StageID)
	if err != nil {
		t.Fatal(err)
	}
	if newStage.OriginVerdictID() != result.Verdict.ID || newStage.ContinuationID() != result.Continuation.ID || newStage.Generation() != 1 {
		t.Fatalf("remediation lineage missing: verdict=%s continuation=%s generation=%d", newStage.OriginVerdictID(), newStage.ContinuationID(), newStage.Generation())
	}
	if newStage.SupersedesStageID() != stageA {
		t.Fatalf("remediation supersedes stage=%s, want %s", newStage.SupersedesStageID(), stageA)
	}
	repairTask := remediationBusinessTask(t, h, planID, result.StageID)
	if repairTask.Assignee() == "" || repairTask.Assignee() == "user:a" {
		t.Fatalf("repair assignee=%s, want an implementer independent from reviewer user:a", repairTask.Assignee())
	}
	if repairTask.FollowsTaskID() != oldGateTaskID {
		t.Fatalf("repair follows=%s, want rejected gate %s", repairTask.FollowsTaskID(), oldGateTaskID)
	}
	remediationGate, err := h.tasks.FindByID(ctx, newStage.GateTaskID())
	if err != nil {
		t.Fatal(err)
	}
	if remediationGate.Assignee() != "user:a" || remediationGate.DispatchMode() != pm.DispatchSupervisorInline {
		t.Fatalf("remediation gate assignee/mode=%s/%s, want independent reviewer user:a supervisor_inline", remediationGate.Assignee(), remediationGate.DispatchMode())
	}

	plan, _ := h.plans.FindByID(ctx, planID)
	nodes, _ := orchSvc.ListNodes(ctx, orch.GraphID(plan.GraphID()))
	var b1Node, newGateNode orch.NodeID
	for _, node := range nodes {
		if nodeTaskID(node) == b1 {
			b1Node = node.ID()
		}
		if sid, _ := node.Metadata()["stage_gate"].(string); sid == string(result.StageID) {
			newGateNode = node.ID()
		}
	}
	edges, _ := orchSvc.ListEdges(ctx, orch.GraphID(plan.GraphID()))
	oldGateNode := orch.NodeID(oldStage.Stage.GateNodeID())
	if hasOrchEdge(edges, oldGateNode, b1Node) || !hasOrchEdge(edges, newGateNode, b1Node) {
		t.Fatalf("downstream boundary was not rewired old_gate→remediation→new_gate→downstream")
	}

	dispatched, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 1 || dispatched[0] == b1 {
		t.Fatalf("dispatch after reject=%v; want remediation entry only", dispatched)
	}
	remediationTaskID := dispatched[0]
	remediationTask, _ := h.tasks.FindByID(ctx, remediationTaskID)
	if remediationTask.StageID() != result.StageID || remediationTask.OriginVerdictID() != result.Verdict.ID {
		t.Fatalf("new task lineage stage=%s verdict=%s", remediationTask.StageID(), remediationTask.OriginVerdictID())
	}

	replay, err := h.svc.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID: oldGateTaskID, Outcome: pm.GateVerdictReject,
		Evidence: "integration test failed on retry semantics", ReviewedSHA: "deadbeef",
		IdempotencyKey: "reject-a-1", Actor: "user:a",
	})
	if err != nil || !replay.Duplicate {
		t.Fatalf("idempotent replay result=%+v err=%v", replay, err)
	}
	stages, _ = h.svc.stages.ListByPlan(ctx, planID)
	if len(stages) != 3 {
		t.Fatalf("replay appended another stage: got %d", len(stages))
	}

	h.setTaskStatus(t, remediationTaskID, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	newGateTaskID := newStage.GateTaskID()
	h.setTaskStatus(t, newGateTaskID, pm.TaskCompleted)
	pass, err := h.svc.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID: newGateTaskID, Outcome: pm.GateVerdictPass, Evidence: "fix verified",
		ReviewedSHA: "feedface", IdempotencyKey: "pass-remediation-1", Actor: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pass.Continuation == nil || pass.Continuation.Status != pm.ContinuationClosed {
		t.Fatalf("continuation not closed by pass: %+v", pass.Continuation)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if !dispatchedSet(t, h, planID)[b1] {
		t.Fatal("downstream did not release after remediation gate passed")
	}
}

func TestStageReject_DuplicateOriginalKeyKeepsOriginalRemediationAfterLaterReject(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "multi reject", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, _, stageA, _ := seedTwoStagePlan(t, h, projectID, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
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
	baseGateTaskID := stage.Stage.GateTaskID()
	h.setTaskStatus(t, baseGateTaskID, pm.TaskCompleted)

	baseCmd := RecordStageGateVerdictCommand{
		GateTaskID: baseGateTaskID, Outcome: pm.GateVerdictReject,
		Evidence: "base reject", ReviewedSHA: "1111111",
		IdempotencyKey: "reject-base", Actor: "user:a",
	}
	baseReject, err := h.svc.RecordStageGateVerdict(ctx, baseCmd)
	if err != nil {
		t.Fatalf("base reject: %v", err)
	}
	remediation1 := baseReject.StageID
	repair1 := remediationBusinessTask(t, h, planID, remediation1)
	h.setTaskStatus(t, repair1.ID(), pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	remediationStage1, err := h.svc.stages.FindByID(ctx, remediation1)
	if err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, remediationStage1.GateTaskID(), pm.TaskCompleted)

	secondReject, err := h.svc.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID: remediationStage1.GateTaskID(), Outcome: pm.GateVerdictReject,
		Evidence: "second reject", ReviewedSHA: "2222222",
		IdempotencyKey: "reject-remediation-1", Actor: "user:a",
	})
	if err != nil {
		t.Fatalf("second reject: %v", err)
	}
	if secondReject.StageID == "" || secondReject.StageID == remediation1 {
		t.Fatalf("second reject stage=%s, want a new remediation generation after %s", secondReject.StageID, remediation1)
	}
	stages, err := h.svc.stages.ListByPlan(ctx, planID)
	if err != nil || len(stages) != 4 {
		t.Fatalf("stages after second reject=%d err=%v, want original two + two remediations", len(stages), err)
	}

	replay, err := h.svc.RecordStageGateVerdict(ctx, baseCmd)
	if err != nil || !replay.Duplicate {
		t.Fatalf("base replay after later reject=%+v err=%v", replay, err)
	}
	if replay.StageID != remediation1 {
		t.Fatalf("base replay stage=%s, want original remediation stage %s", replay.StageID, remediation1)
	}
	stages, err = h.svc.stages.ListByPlan(ctx, planID)
	if err != nil || len(stages) != 4 {
		t.Fatalf("base replay appended duplicate topology: stages=%d err=%v", len(stages), err)
	}
}

func TestStageReject_BudgetExhaustionEscalatesAndKeepsPlanOpen(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "budget", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, _, stageA, _ := seedTwoStagePlan(t, h, projectID, planID, 1)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
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
	baseGateTaskID := stage.Stage.GateTaskID()
	h.setTaskStatus(t, baseGateTaskID, pm.TaskCompleted)
	baseReject, err := h.svc.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID: baseGateTaskID, Outcome: pm.GateVerdictReject,
		Evidence: "first reject consumes the only repair round", ReviewedSHA: "1111111",
		IdempotencyKey: "budget-reject-base", Actor: "user:a",
	})
	if err != nil {
		t.Fatalf("base reject: %v", err)
	}
	if baseReject.Continuation == nil || baseReject.Continuation.Status != pm.ContinuationExecuting || baseReject.Continuation.RemainingBudget != 0 {
		t.Fatalf("base continuation=%+v, want executing with zero remaining budget after one allowed repair", baseReject.Continuation)
	}

	repair := remediationBusinessTask(t, h, planID, baseReject.StageID)
	h.setTaskStatus(t, repair.ID(), pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	remediationStage, err := h.svc.stages.FindByID(ctx, baseReject.StageID)
	if err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, remediationStage.GateTaskID(), pm.TaskCompleted)
	msgsBefore := h.planConvMsgCount(t, planID)
	exhaustedCmd := RecordStageGateVerdictCommand{
		GateTaskID: remediationStage.GateTaskID(), Outcome: pm.GateVerdictReject,
		Evidence: "repair still fails and the budget is exhausted", ReviewedSHA: "2222222",
		IdempotencyKey: "budget-reject-remediation", Actor: "user:a",
	}
	exhausted, err := h.svc.RecordStageGateVerdict(ctx, exhaustedCmd)
	if err != nil {
		t.Fatalf("exhausting reject: %v", err)
	}
	if exhausted.Continuation == nil || exhausted.Continuation.Status != pm.ContinuationBudgetExhausted || !exhausted.Escalated {
		t.Fatalf("exhausted result=%+v, want budget_exhausted escalation", exhausted)
	}
	if exhausted.StageID != "" || exhausted.Proposal != nil {
		t.Fatalf("exhausted result appended stage/proposal: stage=%s proposal=%+v", exhausted.StageID, exhausted.Proposal)
	}
	if delta := h.planConvMsgCount(t, planID) - msgsBefore; delta != 1 {
		t.Fatalf("budget exhaustion posted %d messages, want exactly one escalation @mention", delta)
	}
	stages, err := h.svc.stages.ListByPlan(ctx, planID)
	if err != nil || len(stages) != 3 {
		t.Fatalf("stages after exhaustion=%d err=%v, want original two + first remediation only", len(stages), err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if plan, err := h.plans.FindByID(ctx, planID); err != nil {
		t.Fatal(err)
	} else if plan.Status() == pm.PlanDone {
		t.Fatal("plan marked done after completed/rejected exhausted stage; want human escalation to keep it open")
	}

	msgsAfter := h.planConvMsgCount(t, planID)
	replay, err := h.svc.RecordStageGateVerdict(ctx, exhaustedCmd)
	if err != nil || !replay.Duplicate {
		t.Fatalf("exhaustion replay=%+v err=%v", replay, err)
	}
	if h.planConvMsgCount(t, planID) != msgsAfter {
		t.Fatal("duplicate exhausted reject re-posted escalation")
	}
	stages, err = h.svc.stages.ListByPlan(ctx, planID)
	if err != nil || len(stages) != 3 {
		t.Fatalf("exhaustion replay appended topology: stages=%d err=%v", len(stages), err)
	}
}

func TestPlainPlanRejectDoesNotUseStageRemediationRoute(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "plain graph", CreatedBy: "user:a"})
	h.drain(t)
	dev, rev, dec, _ := buildGraphCycle(t, h, projectID, planID)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)

	if _, err := h.svc.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID: dec, Outcome: pm.GateVerdictReject, Evidence: "plain reject", ReviewedSHA: "3333333",
		IdempotencyKey: "plain-reject", Actor: "user:a",
	}); !errors.Is(err, ErrNotStageGate) {
		t.Fatalf("plain RecordStageGateVerdict err=%v, want ErrNotStageGate", err)
	}
	if err := h.svc.RecordDecisionOutcome(ctx, dec, "reject", "user:a"); err != nil {
		t.Fatalf("plain RecordDecisionOutcome reject: %v", err)
	}
	dispatched, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) == 0 {
		t.Fatalf("plain reject dispatch=%v, want graph loopback to create a new retry task", dispatched)
	}
	continuations, err := h.svc.remediation.ListContinuationsByPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(continuations) != 0 {
		t.Fatalf("plain plan created remediation continuations: %+v", continuations)
	}
	stages, err := h.svc.stages.ListByPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 0 {
		t.Fatalf("plain plan created staged remediation entries: %+v", stages)
	}
}

func TestStageReject_WhilePausedRecordsFactsThenAppendsOnceAfterResume(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "paused remediation", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, _, stageA, _ := seedTwoStagePlan(t, h, projectID, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
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
		IdempotencyKey: "paused-reject-1", Actor: "user:a",
	}
	paused, err := h.svc.RecordStageGateVerdict(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Continuation == nil || paused.Continuation.Status != pm.ContinuationAwaitingRemediation || paused.StageID != "" {
		t.Fatalf("paused result=%+v; want recorded awaiting continuation without topology mutation", paused)
	}
	stages, _ := h.svc.stages.ListByPlan(ctx, planID)
	if len(stages) != 2 {
		t.Fatalf("paused reject appended topology: stages=%d want 2", len(stages))
	}

	if err := h.svc.ResumePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	resumed, err := h.svc.RecordStageGateVerdict(ctx, cmd)
	if err != nil || !resumed.Duplicate || resumed.StageID == "" {
		t.Fatalf("resume replay=%+v err=%v; want one appended remediation stage", resumed, err)
	}
	stages, _ = h.svc.stages.ListByPlan(ctx, planID)
	if len(stages) != 3 {
		t.Fatalf("resume appended stages=%d want exactly 3", len(stages))
	}
	replay, err := h.svc.RecordStageGateVerdict(ctx, cmd)
	if err != nil || !replay.Duplicate || replay.StageID != resumed.StageID {
		t.Fatalf("second replay=%+v err=%v", replay, err)
	}
	stages, _ = h.svc.stages.ListByPlan(ctx, planID)
	if len(stages) != 3 {
		t.Fatalf("second replay duplicated topology: stages=%d", len(stages))
	}

	conflict := cmd
	conflict.Evidence = "different evidence with the same key"
	if _, err := h.svc.RecordStageGateVerdict(ctx, conflict); !errors.Is(err, pm.ErrIdempotencyConflict) {
		t.Fatalf("same key/different payload err=%v want ErrIdempotencyConflict", err)
	}
}

func remediationBusinessTask(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, stageID pm.StageID) *pm.Task {
	t.Helper()
	stage, err := h.svc.stages.FindByID(h.ctx, stageID)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := h.tasks.ListByPlan(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.StageID() == stageID && task.ID() != stage.GateTaskID() {
			return task
		}
	}
	t.Fatalf("stage %s has no remediation business task", stageID)
	return nil
}

func hasOrchEdge(edges []orch.Edge, from, to orch.NodeID) bool {
	for _, edge := range edges {
		if edge.FromNodeID == from && edge.ToNodeID == to {
			return true
		}
	}
	return false
}
