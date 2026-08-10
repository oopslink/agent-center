package service

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestGetPlanGenerations_ProjectsActiveHistoryProgressAndDiff(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "generations", CreatedBy: "user:a"})
	h.drain(t)

	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	stage0, err := h.svc.CreateStage(ctx, CreateStageCommand{PlanID: planID, Name: "Base", Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage base: %v", err)
	}
	if err := h.svc.AssignTaskToStage(ctx, planID, a, stage0, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage A: %v", err)
	}
	if err := h.svc.AssignTaskToStage(ctx, planID, b, stage0, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage B: %v", err)
	}
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency B->A: %v", err)
	}
	h.setTaskStatus(t, a, pm.TaskRunning)
	h.setTaskStatus(t, a, pm.TaskCompleted)

	now := time.Unix(1_700_000_100, 0).UTC()
	verdict, err := pm.NewGateVerdict(pm.GateVerdict{
		ID: "verdict-1", ProjectID: pid, PlanID: planID, StageID: stage0, GateTaskID: b,
		Outcome: pm.GateVerdictReject, Evidence: "missing verification evidence",
		ReviewedSHA: "abc123", ActorRef: "user:a", IdempotencyKey: "idem-verdict-1", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewGateVerdict: %v", err)
	}
	if err := h.svc.remediation.SaveVerdict(ctx, verdict); err != nil {
		t.Fatalf("SaveVerdict: %v", err)
	}
	continuation := &pm.PlanContinuation{
		ID: "continuation-1", ProjectID: pid, PlanID: planID, RootStageID: stage0, CurrentStageID: "stage-rem",
		TriggerVerdictID: verdict.ID, Status: pm.ContinuationExecuting, Generation: 1,
		RemainingBudget: 2, BoundaryFingerprint: "boundary", CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if err := h.svc.remediation.SaveContinuation(ctx, continuation); err != nil {
		t.Fatalf("SaveContinuation: %v", err)
	}
	stage1, err := pm.NewStage(pm.NewStageInput{
		ID: "stage-rem", PlanID: planID, Name: "Remediation", DependsOnStages: []pm.StageID{stage0},
		Generation: 1, OriginVerdictID: verdict.ID, ContinuationID: continuation.ID,
		AcceptanceContract: "verify evidence", TopologyFingerprint: "boundary", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewStage rem: %v", err)
	}
	if err := h.svc.stages.Save(ctx, stage1); err != nil {
		t.Fatalf("Save remediation stage: %v", err)
	}
	c := h.seedBacklogAssignedTask(t, pid, "C", "user:c1")
	tc, err := h.tasks.FindByID(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := tc.SetPlan(planID, now); err != nil {
		t.Fatalf("SetPlan C: %v", err)
	}
	if err := h.tasks.Update(ctx, tc); err != nil {
		t.Fatalf("Update C: %v", err)
	}
	if err := h.svc.AssignTaskToStage(ctx, planID, c, stage1.ID(), "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage C: %v", err)
	}
	if err := h.plans.AddDependency(ctx, pm.Dependency{PlanID: planID, FromTaskID: c, ToTaskID: b}); err != nil {
		t.Fatalf("AddDependency C->B: %v", err)
	}

	read, err := h.svc.GetPlanGenerations(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanGenerations: %v", err)
	}
	if read.ActiveGeneration != 1 {
		t.Fatalf("active generation=%d want 1", read.ActiveGeneration)
	}
	if len(read.Generations) != 2 {
		t.Fatalf("generations=%d want 2: %+v", len(read.Generations), read.Generations)
	}
	base := read.Generations[0]
	if base.Generation != 0 || base.Progress.Done != 1 || base.Progress.Total != 3 {
		t.Fatalf("base generation progress=%+v generation=%d want 1/3 gen0", base.Progress, base.Generation)
	}
	rem := read.Generations[1]
	if !rem.Active || rem.Generation != 1 {
		t.Fatalf("remediation generation active=%v generation=%d want active gen1", rem.Active, rem.Generation)
	}
	if rem.Evidence != "missing verification evidence" || rem.IdempotencyKey != "idem-verdict-1" {
		t.Fatalf("remediation provenance evidence=%q key=%q", rem.Evidence, rem.IdempotencyKey)
	}
	if len(rem.Diff.AddedNodes) != 1 || rem.Diff.AddedNodes[0] != c {
		t.Fatalf("remediation diff added nodes=%v want [%s]", rem.Diff.AddedNodes, c)
	}
	if len(rem.Diff.AddedEdges) != 1 || rem.Diff.AddedEdges[0].FromTaskID != c || rem.Diff.AddedEdges[0].ToTaskID != b {
		t.Fatalf("remediation diff edges=%v want C->B", rem.Diff.AddedEdges)
	}
	foundC := false
	for _, node := range read.Nodes {
		if node.TaskID == c {
			foundC = true
			if node.Generation != 1 || node.StageID != stage1.ID() || node.Revision != 2 {
				t.Fatalf("node C generation projection=%+v", node)
			}
		}
	}
	if !foundC {
		t.Fatal("node C missing from generation ownership projection")
	}
}
