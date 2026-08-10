package service

import (
	"errors"
	"slices"
	"strings"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func (h *planAdvanceHarness) seedAssignedTaskFollowing(t *testing.T, pid pm.ProjectID, planID pm.PlanID, title, assignee string, follows pm.TaskID) pm.TaskID {
	t.Helper()
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{
		ProjectID:     pid,
		Title:         title,
		CreatedBy:     "user:a",
		FollowsTaskID: follows,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := assignee
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	return tid
}

func TestPlanCompletion_RemediatedHistoricalFailuresAutoCompletePlan5a432139Shape(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "plan-5a432139 shape", CreatedBy: "user:a"})
	h.drain(t)

	oldT1286 := h.seedAssignedTask(t, pid, planID, "T1286 early failed implementation", "user:dev1")
	oldT1287 := h.seedAssignedTask(t, pid, planID, "T1287 early failed verification", "user:qa1")
	remediate1286 := h.seedAssignedTaskFollowing(t, pid, planID, "recovery/remediation for T1286", "user:dev2", oldT1286)
	remediate1287 := h.seedAssignedTaskFollowing(t, pid, planID, "recovery/remediation for T1287", "user:qa2", oldT1287)
	reverify := h.seedAssignedTask(t, pid, planID, "independent reverify", "user:qa3")
	integrate := h.seedAssignedTask(t, pid, planID, "integration", "user:int")
	mainAcceptance := h.seedAssignedTask(t, pid, planID, "main final acceptance", "user:pd")
	baseline := make([]pm.TaskID, 0, 7)
	for i := 0; i < 7; i++ {
		baseline = append(baseline, h.seedAssignedTask(t, pid, planID, "completed prerequisite", "user:dev"))
	}

	deps := []struct {
		from pm.TaskID
		to   pm.TaskID
	}{
		{reverify, oldT1286},
		{reverify, oldT1287},
		{reverify, remediate1286},
		{reverify, remediate1287},
		{integrate, reverify},
		{mainAcceptance, integrate},
		{mainAcceptance, oldT1286},
		{mainAcceptance, oldT1287},
		{integrate, baseline[len(baseline)-1]},
	}
	for i := 1; i < len(baseline); i++ {
		deps = append(deps, struct {
			from pm.TaskID
			to   pm.TaskID
		}{from: baseline[i], to: baseline[i-1]})
	}
	for _, dep := range deps {
		if err := h.svc.AddPlanDependency(ctx, planID, dep.from, dep.to, "user:a"); err != nil {
			t.Fatalf("AddPlanDependency(%s,%s): %v", dep.from, dep.to, err)
		}
	}

	h.setTaskStatus(t, oldT1286, pm.TaskDiscarded)
	h.setTaskStatus(t, oldT1287, pm.TaskDiscarded)
	completedBeforeFinalAcceptance := append([]pm.TaskID{remediate1286, remediate1287, reverify, integrate}, baseline...)
	for _, id := range completedBeforeFinalAcceptance {
		h.setTaskStatus(t, id, pm.TaskCompleted)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if p, err := h.plans.FindByID(ctx, planID); err != nil {
		t.Fatal(err)
	} else if p.Status() != pm.PlanRunning {
		t.Fatalf("plan status before final acceptance=%s want running", p.Status())
	}

	// Completing the final effective node must trigger the same evaluator used by
	// CompletePlan; no manual advance/settle call is needed.
	h.setTaskStatus(t, mainAcceptance, pm.TaskCompleted)
	p, err := h.plans.FindByID(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != pm.PlanDone {
		t.Fatalf("plan status=%s want done", p.Status())
	}
	if err := h.svc.CompletePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("CompletePlan idempotent on done plan: %v", err)
	}

	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.View.Nodes) != 14 {
		t.Fatalf("nodes=%d want plan-5a432139-shaped 14", len(detail.View.Nodes))
	}
	if detail.View.Progress.Done != 12 || detail.View.Progress.Total != 12 {
		t.Fatalf("progress=%+v want current effective 12/12", detail.View.Progress)
	}
	if len(detail.View.ReadySet) != 0 {
		t.Fatalf("ready_set=%v want empty", detail.View.ReadySet)
	}
	if !detail.View.HasFailed {
		t.Fatal("has_failed=false; historical failures must remain observable")
	}
	if len(detail.View.HistoricalFailures) != 2 || len(detail.View.ActiveFailures) != 0 {
		t.Fatalf("historical_failures=%v active_failures=%v want 2 historical and 0 active", detail.View.HistoricalFailures, detail.View.ActiveFailures)
	}
	nodes := nodesByID(detail.View.Nodes)
	for _, id := range []pm.TaskID{oldT1286, oldT1287} {
		node := nodes[id]
		if node.NodeStatus != pm.NodeFailed || node.Effective {
			t.Fatalf("historical node %s=%+v want failed effective=false", id, node)
		}
		if len(node.SupersededBy) == 0 {
			t.Fatalf("historical node %s missing superseded_by", id)
		}
		task, err := h.tasks.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status() != pm.TaskDiscarded {
			t.Fatalf("historical task %s status=%s want discarded", id, task.Status())
		}
	}
}

func TestPlanCompletion_LegacyPlan5a432139CompletesWithoutLineageBackfill(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	const planID pm.PlanID = "plan-5a432139"
	p, err := pm.NewPlan(pm.NewPlanInput{
		ID: planID, ProjectID: pid, Name: "legacy remediation Plan", CreatorRef: "user:a", CreatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.plans.Save(ctx, p); err != nil {
		t.Fatal(err)
	}

	prep := make([]pm.TaskID, 0, 8)
	for _, title := range []string{
		"T1278 baseline prep",
		"T1279 executor slot acceptance",
		"T1280 read model audit",
		"T1281 deployment prep",
		"T1282 integration evidence",
		"T1283 regression sweep",
		"T1284 release notes",
		"T1285 branch hygiene",
	} {
		prep = append(prep, h.seedAssignedTask(t, pid, planID, title, "user:dev"))
	}
	oldT1286 := h.seedAssignedTask(t, pid, planID, "T1286 legacy failed remediation attempt", "user:dev1")
	oldT1287 := h.seedAssignedTask(t, pid, planID, "T1287 legacy failed verification attempt", "user:qa1")
	recovery := h.seedAssignedTask(t, pid, planID, "T1288 recovery for T1286/T1287", "user:dev2")
	remediation := h.seedAssignedTask(t, pid, planID, "T1289 remediation for T1286/T1287", "user:dev3")
	ship := h.seedAssignedTask(t, pid, planID, "T1290 ship: remediation branch -> main", "user:ship")
	finalAcceptance := h.seedAssignedTask(t, pid, planID, "T1291 final acceptance", "user:pd")

	for i := 1; i < len(prep); i++ {
		if err := h.svc.AddPlanDependency(ctx, planID, prep[i], prep[i-1], "user:a"); err != nil {
			t.Fatalf("AddPlanDependency(prep %d): %v", i, err)
		}
	}
	for _, dep := range []struct {
		from pm.TaskID
		to   pm.TaskID
	}{
		{oldT1286, prep[2]},
		{oldT1287, prep[3]},
		{recovery, prep[len(prep)-1]},
		{remediation, recovery},
		{ship, remediation},
		{finalAcceptance, ship},
	} {
		if err := h.svc.AddPlanDependency(ctx, planID, dep.from, dep.to, "user:a"); err != nil {
			t.Fatalf("AddPlanDependency(%s,%s): %v", dep.from, dep.to, err)
		}
	}

	for _, id := range prep {
		h.setTaskStatus(t, id, pm.TaskCompleted)
	}
	h.setTaskStatus(t, oldT1286, pm.TaskDiscarded)
	h.setTaskStatus(t, oldT1287, pm.TaskDiscarded)
	h.setTaskStatus(t, recovery, pm.TaskCompleted)
	h.setTaskStatus(t, remediation, pm.TaskCompleted)
	h.setTaskStatus(t, ship, pm.TaskCompleted)
	h.setTaskStatus(t, finalAcceptance, pm.TaskCompleted)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	rawTasks, err := h.tasks.ListByPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	rawEdges, err := h.plans.ListDependencies(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	rawView := pm.DerivePlanView(rawTasks, rawEdges, nil, nil, nil)
	if !slices.Contains(rawView.ActiveFailures, oldT1286) || !slices.Contains(rawView.ActiveFailures, oldT1287) {
		t.Fatalf("raw active_failures=%v want T1286/T1287 legacy failures", rawView.ActiveFailures)
	}
	rawIncompleteLeaves := incompleteEffectiveLeaves(rawView, rawEdges)
	if !slices.Contains(rawIncompleteLeaves, oldT1286) || !slices.Contains(rawIncompleteLeaves, oldT1287) {
		t.Fatalf("raw incomplete_effective_leaf=%v want T1286/T1287", rawIncompleteLeaves)
	}

	if err := h.svc.CompletePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("CompletePlan legacy plan-5a432139 shape: %v", err)
	}
	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Plan.Status() != pm.PlanDone {
		t.Fatalf("plan status=%s want done", detail.Plan.Status())
	}
	if len(detail.View.Nodes) != 14 {
		t.Fatalf("nodes=%d want real legacy 14-node shape", len(detail.View.Nodes))
	}
	if detail.View.Progress.Done != 12 || detail.View.Progress.Total != 12 {
		t.Fatalf("progress=%+v want current effective 12/12", detail.View.Progress)
	}
	if len(detail.View.ActiveFailures) != 0 || len(detail.View.HistoricalFailures) != 2 {
		t.Fatalf("active_failures=%v historical_failures=%v want 0 active, 2 historical", detail.View.ActiveFailures, detail.View.HistoricalFailures)
	}
	nodes := nodesByID(detail.View.Nodes)
	for _, id := range []pm.TaskID{oldT1286, oldT1287} {
		node := nodes[id]
		if node.NodeStatus != pm.NodeFailed || node.Effective || node.SupersededReason != "legacy_completed_remediation" {
			t.Fatalf("legacy node %s=%+v want failed historical legacy remediation", id, node)
		}
		task, err := h.tasks.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status() != pm.TaskDiscarded || task.FollowsTaskID() != "" || task.OriginVerdictID() != "" {
			t.Fatalf("legacy task %s mutated: status=%s follows=%s origin=%s", id, task.Status(), task.FollowsTaskID(), task.OriginVerdictID())
		}
	}
	edgesAfter, err := h.plans.ListDependencies(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edgesAfter) != len(rawEdges) {
		t.Fatalf("dependencies changed: before=%d after=%d", len(rawEdges), len(edgesAfter))
	}
}

func TestPlanCompletion_ManualCompleteAcceptsRemediatedHistoricalFailure(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "manual remediation completion", CreatedBy: "user:a"})
	h.drain(t)
	old := h.seedAssignedTask(t, pid, planID, "old failed node", "user:dev1")
	fix := h.seedAssignedTaskFollowing(t, pid, planID, "replacement node", "user:dev2", old)
	if err := h.svc.AddPlanDependency(ctx, planID, fix, old, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, old, pm.TaskDiscarded)
	h.setTaskStatus(t, fix, pm.TaskCompleted)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.CompletePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("CompletePlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() != pm.PlanDone {
		t.Fatalf("plan status=%s want done", p.Status())
	}
}

func TestPlanCompletion_LegacyContinuationStagesProvideReplacementLineage(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "legacy continuation lineage", CreatedBy: "user:a"})
	h.drain(t)
	old := h.seedAssignedTask(t, pid, planID, "old failed stage task", "user:dev1")
	replacement := h.seedAssignedTask(t, pid, planID, "replacement without follows_task_id", "user:dev2")
	h.setTaskStatus(t, old, pm.TaskDiscarded)
	h.setTaskStatus(t, replacement, pm.TaskCompleted)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}

	const (
		rootStageID    pm.StageID        = "stage-root"
		currentStageID pm.StageID        = "stage-remediation"
		continuationID pm.ContinuationID = "continuation-legacy"
	)
	for taskID, stageID := range map[pm.TaskID]pm.StageID{old: rootStageID, replacement: currentStageID} {
		task, err := h.tasks.FindByID(ctx, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if err := task.SetStage(stageID, h.clk.Now()); err != nil {
			t.Fatal(err)
		}
		if err := h.tasks.Update(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	rootStage, err := pm.NewStage(pm.NewStageInput{
		ID: rootStageID, PlanID: planID, Name: "legacy root", CreatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	currentStage, err := pm.NewStage(pm.NewStageInput{
		ID: currentStageID, PlanID: planID, Name: "legacy remediation",
		ContinuationID: continuationID, Generation: 1, CreatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.stages.Save(ctx, rootStage); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.stages.Save(ctx, currentStage); err != nil {
		t.Fatal(err)
	}
	continuation, err := pm.NewPlanContinuation(continuationID, pm.GateVerdict{
		ID: "verdict-reject", ProjectID: pid, PlanID: planID, StageID: rootStageID, Outcome: pm.GateVerdictReject,
	}, "root-boundary", 2, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := continuation.AttachStage(currentStageID, "proposal-legacy", "current-boundary", h.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.remediation.SaveContinuation(ctx, continuation); err != nil {
		t.Fatal(err)
	}

	if err := h.svc.CompletePlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanNotComplete) {
		t.Fatalf("CompletePlan with open continuation err=%v want ErrPlanNotComplete", err)
	}
	expectedVersion := continuation.Version
	if err := continuation.Close("verdict-pass", h.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if updated, err := h.svc.remediation.UpdateContinuation(ctx, continuation, expectedVersion); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatal("closing legacy continuation lost version race")
	}
	if err := h.svc.CompletePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("CompletePlan after legacy continuation closed: %v", err)
	}

	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	node := nodesByID(detail.View.Nodes)[old]
	if node.Effective || node.SupersededReason != "remediation_continuation" {
		t.Fatalf("legacy historical node=%+v", node)
	}
	if replacementTask, err := h.tasks.FindByID(ctx, replacement); err != nil {
		t.Fatal(err)
	} else if replacementTask.FollowsTaskID() != "" {
		t.Fatalf("fixture unexpectedly has follows_task_id=%s", replacementTask.FollowsTaskID())
	}
	if detail.View.Progress.Done != 1 || detail.View.Progress.Total != 1 {
		t.Fatalf("legacy effective progress=%+v want 1/1", detail.View.Progress)
	}
}

func TestPlanCompletion_UnremediatedFailureBlocksAutoAndManualCompletion(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "unrecovered failure", CreatedBy: "user:a"})
	h.drain(t)
	failed := h.seedAssignedTask(t, pid, planID, "failed leaf", "user:dev")
	accept := h.seedAssignedTask(t, pid, planID, "acceptance", "user:pd")
	if err := h.svc.AddPlanDependency(ctx, planID, accept, failed, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, failed, pm.TaskDiscarded)
	h.setTaskStatus(t, accept, pm.TaskCompleted)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() != pm.PlanRunning {
		t.Fatalf("plan status=%s want running while failed node is unreplaced", p.Status())
	}
	if err := h.svc.CompletePlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanNotComplete) {
		t.Fatalf("CompletePlan err=%v want ErrPlanNotComplete", err)
	}
	detail, _ := h.svc.GetPlanDetail(ctx, planID)
	if len(detail.View.ActiveFailures) != 1 || detail.View.ActiveFailures[0] != failed {
		t.Fatalf("active_failures=%v want [%s]", detail.View.ActiveFailures, failed)
	}
	if len(detail.View.HistoricalFailures) != 0 {
		t.Fatalf("historical_failures=%v want none for unreplaced failure", detail.View.HistoricalFailures)
	}
}

func TestPlanCompletion_LegacyFailedLeafWithoutRecoveryChainStillBlocks(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "legacy failed leaf without remediation", CreatedBy: "user:a"})
	h.drain(t)
	failed := h.seedAssignedTask(t, pid, planID, "T1286 failed implementation", "user:dev")
	finalAcceptance := h.seedAssignedTask(t, pid, planID, "final acceptance", "user:pd")
	h.setTaskStatus(t, failed, pm.TaskDiscarded)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, finalAcceptance, pm.TaskCompleted)
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() != pm.PlanRunning {
		t.Fatalf("plan status=%s want running; failed leaf has no recovery chain", p.Status())
	}
	err := h.svc.CompletePlan(ctx, planID, "user:a")
	if !errors.Is(err, pm.ErrPlanNotComplete) {
		t.Fatalf("CompletePlan err=%v want ErrPlanNotComplete", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "unreplaced_failed:") || !strings.Contains(msg, "incomplete_effective_leaf:") {
		t.Fatalf("CompletePlan err=%q want unreplaced_failed and incomplete_effective_leaf", msg)
	}
	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.View.ActiveFailures) != 1 || detail.View.ActiveFailures[0] != failed {
		t.Fatalf("active_failures=%v want [%s]", detail.View.ActiveFailures, failed)
	}
}

func TestPlanCompletion_ReadyDispatchedAndRunningWorkBlockManualCompletion(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, taskID pm.TaskID)
	}{
		{name: "ready"},
		{name: "dispatched", setup: func(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, taskID pm.TaskID) {
			t.Helper()
			if dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
				t.Fatal(err)
			} else if len(dispatched) != 1 || dispatched[0] != taskID {
				t.Fatalf("dispatched=%v want [%s]", dispatched, taskID)
			}
		}},
		{name: "running", setup: func(t *testing.T, h *planAdvanceHarness, _ pm.PlanID, taskID pm.TaskID) {
			t.Helper()
			h.setTaskStatus(t, taskID, pm.TaskRunning)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := planAdvanceSetup(t)
			ctx := h.ctx
			pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
			planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "active " + tc.name, CreatedBy: "user:a"})
			h.drain(t)
			taskID := h.seedAssignedTask(t, pid, planID, "current work", "user:dev")
			if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
				t.Fatal(err)
			}
			if tc.setup != nil {
				tc.setup(t, h, planID, taskID)
			}
			if err := h.svc.CompletePlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanNotComplete) {
				t.Fatalf("CompletePlan err=%v want ErrPlanNotComplete", err)
			}
			p, _ := h.plans.FindByID(ctx, planID)
			if p.Status() != pm.PlanRunning {
				t.Fatalf("plan status=%s want running", p.Status())
			}
		})
	}
}

func nodesByID(nodes []pm.PlanNodeView) map[pm.TaskID]pm.PlanNodeView {
	out := make(map[pm.TaskID]pm.PlanNodeView, len(nodes))
	for _, node := range nodes {
		out[node.TaskID] = node
	}
	return out
}
