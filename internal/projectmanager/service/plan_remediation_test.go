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
		Evidence: "integration test failed on retry semantics", ReviewedSHA: immutableTestSHA,
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
		Evidence: "integration test failed on retry semantics", ReviewedSHA: immutableTestSHA,
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
		ReviewedSHA: "fedcba98765432100123456789abcdef01234567", IdempotencyKey: "pass-remediation-1", Actor: "user:a",
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
		Evidence: "paused review found an edge case", ReviewedSHA: immutableTestSHA,
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

func hasOrchEdge(edges []orch.Edge, from, to orch.NodeID) bool {
	for _, edge := range edges {
		if edge.FromNodeID == from && edge.ToNodeID == to {
			return true
		}
	}
	return false
}
