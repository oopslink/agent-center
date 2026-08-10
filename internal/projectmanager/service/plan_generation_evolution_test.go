package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func generationTaskByTitle(t *testing.T, g *pm.PlanGeneration, title string) pm.PlanGenerationTaskSnapshot {
	t.Helper()
	for _, snap := range g.Snapshot.Tasks {
		if snap.Title == title {
			return snap
		}
	}
	t.Fatalf("generation %s has no task snapshot titled %q: %+v", g.ID, title, g.Snapshot.Tasks)
	return pm.PlanGenerationTaskSnapshot{}
}

func generationStageByName(t *testing.T, g *pm.PlanGeneration, name string) pm.PlanGenerationStageSnapshot {
	t.Helper()
	for _, snap := range g.Snapshot.Stages {
		if snap.Name == name {
			return snap
		}
	}
	t.Fatalf("generation %s has no stage snapshot named %q: %+v", g.ID, name, g.Snapshot.Stages)
	return pm.PlanGenerationStageSnapshot{}
}

func activeGenerationID(t *testing.T, h *planAdvanceHarness, planID pm.PlanID) pm.PlanGenerationID {
	t.Helper()
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActiveGenerationID() == "" {
		t.Fatalf("plan %s has empty active_generation_id", planID)
	}
	return p.ActiveGenerationID()
}

func clearActiveGenerationForLegacyTest(t *testing.T, h *planAdvanceHarness, planID pm.PlanID) int {
	t.Helper()
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := pm.RehydratePlan(pm.RehydratePlanInput{
		ID: p.ID(), ProjectID: p.ProjectID(), Name: p.Name(), Description: p.Description(),
		Status: p.Status(), CreatorRef: p.CreatorRef(), ConversationID: p.ConversationID(),
		TargetDate: p.TargetDate(), Builtin: p.IsBuiltin(), OrgNumber: p.OrgNumber(),
		GraphID: p.GraphID(), ActiveGenerationID: "", ArchivedAt: p.ArchivedAt(), ArchivedBy: p.ArchivedBy(),
		CreatedAt: p.CreatedAt(), UpdatedAt: p.UpdatedAt(), Version: p.Version(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.plans.Update(h.ctx, legacy); err != nil {
		t.Fatal(err)
	}
	return legacy.Version()
}

func planHasTaskTitle(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, title string) bool {
	t.Helper()
	tasks, err := h.tasks.ListByPlan(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Title() == title {
			return true
		}
	}
	return false
}

func TestStartPlan_ActivatesInitialGenerationSnapshot(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "initial", CreatedBy: "user:a"})
	h.drain(t)
	stageID, err := h.svc.CreateStage(h.ctx, CreateStageCommand{PlanID: planID, Name: "Build", Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	taskID := h.seedAssignedTask(t, pid, planID, "Implement", "user:dev")
	if err := h.svc.AssignTaskToStage(h.ctx, planID, taskID, stageID, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage: %v", err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActiveGenerationID() == "" {
		t.Fatal("StartPlan left active_generation_id empty")
	}
	g, err := h.plans.FindGenerationByID(h.ctx, p.ActiveGenerationID())
	if err != nil {
		t.Fatal(err)
	}
	if g.ParentGenerationID != "" {
		t.Fatalf("initial parent=%s, want empty", g.ParentGenerationID)
	}
	if g.Snapshot.PlanVersion != p.Version() || g.Snapshot.ActiveGenerationID != p.ActiveGenerationID() {
		t.Fatalf("initial snapshot version/active=%d/%s, want %d/%s", g.Snapshot.PlanVersion, g.Snapshot.ActiveGenerationID, p.Version(), p.ActiveGenerationID())
	}
	if len(g.Snapshot.DispatchRecords) != 0 {
		t.Fatalf("initial generation captured dispatch records before plan.started dispatch: %+v", g.Snapshot.DispatchRecords)
	}
	st := generationStageByName(t, g, "Build")
	if st.StageID != stageID || st.GateTaskID == "" || st.GateNodeID == "" {
		t.Fatalf("initial stage snapshot = %+v, want stage/gate task/gate node", st)
	}
	taskSnap := generationTaskByTitle(t, g, "Implement")
	if taskSnap.StageID != stageID || taskSnap.NodeID == "" {
		t.Fatalf("initial task snapshot = %+v, want stage and node baseline", taskSnap)
	}
}

func TestEvolvePlanGeneration_RunningAtomicDispatchIdempotencyAndSnapshot(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "evolve", CreatedBy: "user:a"})
	h.drain(t)
	a, b := h.startRunningPlanAB(t, pid, planID)
	h.setTaskStatus(t, a, pm.TaskRunning)
	oldA, err := h.tasks.FindByID(h.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	oldANode := oldA.NodeID()
	oldAPlan := oldA.PlanID()
	base := h.planVersion(t, planID)
	parent := activeGenerationID(t, h, planID)

	cmd := EvolvePlanGenerationCommand{
		PlanID:             planID,
		ParentGenerationID: parent,
		BaseVersion:        base,
		IdempotencyKey:     "evo-running-1",
		Reason:             "new independent work is required",
		Evidence:           "review found an uncovered case",
		Creator:            "user:a",
		Diff: pm.PlanGenerationDiff{
			NodeDecisions: []pm.PlanGenerationNodeDecision{
				{TaskID: a, Action: pm.EvolutionPreserve, Reason: "already running"},
				{TaskID: b, Action: pm.EvolutionPreserve, Reason: "still blocked"},
			},
			Tasks: []pm.PlanGenerationTaskDraft{{
				Ref: "c", Title: "C", Description: "new root", AssigneeRef: "user:c1",
			}},
		},
	}
	res, err := h.svc.EvolvePlanGeneration(h.ctx, cmd)
	if err != nil {
		t.Fatalf("EvolvePlanGeneration: %v", err)
	}
	if res.Duplicate || res.Generation == nil {
		t.Fatalf("result duplicate=%v generation=%v", res.Duplicate, res.Generation)
	}
	cSnap := generationTaskByTitle(t, res.Generation, "C")
	if len(res.Dispatched) != 1 || res.Dispatched[0] != cSnap.TaskID {
		t.Fatalf("dispatched=%v, want new C task %s", res.Dispatched, cSnap.TaskID)
	}
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActiveGenerationID() != res.Generation.ID || p.Version() != base+1 {
		t.Fatalf("plan active/version = %s/%d, want %s/%d", p.ActiveGenerationID(), p.Version(), res.Generation.ID, base+1)
	}
	if !dispatchedSet(t, h, planID)[cSnap.TaskID] {
		t.Fatalf("new task %s was not dispatched in the evolution commit", cSnap.TaskID)
	}
	stored, err := h.plans.FindGenerationByID(h.ctx, res.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedC := generationTaskByTitle(t, stored, "C")
	if storedC.NodeID == "" {
		t.Fatalf("new task snapshot has empty node id: %+v", storedC)
	}
	freshA, err := h.tasks.FindByID(h.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if freshA.PlanID() != oldAPlan || freshA.NodeID() != oldANode {
		t.Fatalf("preserved running task attribution drifted: plan/node %s/%s -> %s/%s", oldAPlan, oldANode, freshA.PlanID(), freshA.NodeID())
	}

	liveC, err := h.tasks.FindByID(h.ctx, cSnap.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := liveC.Rename("C mutated after snapshot", h.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.tasks.Update(h.ctx, liveC); err != nil {
		t.Fatal(err)
	}
	stored, err = h.plans.FindGenerationByID(h.ctx, res.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := generationTaskByTitle(t, stored, "C").Title; got != "C" {
		t.Fatalf("generation snapshot title drifted to %q", got)
	}

	recordCount := len(dispatchedSet(t, h, planID))
	dup, err := h.svc.EvolvePlanGeneration(h.ctx, cmd)
	if err != nil {
		t.Fatalf("duplicate EvolvePlanGeneration: %v", err)
	}
	if !dup.Duplicate || dup.Generation.ID != res.Generation.ID {
		t.Fatalf("duplicate result = duplicate:%v generation:%s, want same generation %s", dup.Duplicate, dup.Generation.ID, res.Generation.ID)
	}
	if len(dup.Dispatched) != 1 || dup.Dispatched[0] != cSnap.TaskID {
		t.Fatalf("duplicate dispatched=%v, want original dispatched task %s", dup.Dispatched, cSnap.TaskID)
	}
	if got := len(dispatchedSet(t, h, planID)); got != recordCount {
		t.Fatalf("duplicate changed dispatch record count: got %d want %d", got, recordCount)
	}
	p, _ = h.plans.FindByID(h.ctx, planID)
	if p.Version() != base+1 {
		t.Fatalf("duplicate changed plan version to %d, want %d", p.Version(), base+1)
	}

	cmd.IdempotencyKey = "evo-stale-base"
	if _, err := h.svc.EvolvePlanGeneration(h.ctx, cmd); !errors.Is(err, pm.ErrPlanVersionConflict) {
		t.Fatalf("stale base err=%v, want ErrPlanVersionConflict", err)
	}
	cmd.IdempotencyKey = "evo-running-1"
	cmd.Evidence = "same key but different evidence"
	if _, err := h.svc.EvolvePlanGeneration(h.ctx, cmd); !errors.Is(err, pm.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict err=%v, want ErrIdempotencyConflict", err)
	}
}

func TestEvolvePlanGeneration_InFlightConflictDecisions(t *testing.T) {
	t.Run("supersede running node rejected", func(t *testing.T) {
		h := planAdvanceSetup(t)
		pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "supersede", CreatedBy: "user:a"})
		h.drain(t)
		a, _ := h.startRunningPlanAB(t, pid, planID)
		h.setTaskStatus(t, a, pm.TaskRunning)
		base := h.planVersion(t, planID)
		_, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
			PlanID: planID, ParentGenerationID: activeGenerationID(t, h, planID), BaseVersion: base, IdempotencyKey: "evo-supersede-running",
			Reason: "replace work", Evidence: "new evidence", Creator: "user:a",
			Diff: pm.PlanGenerationDiff{NodeDecisions: []pm.PlanGenerationNodeDecision{{TaskID: a, Action: pm.EvolutionSupersede}}},
		})
		if !errors.Is(err, pm.ErrPlanNodeInFlight) {
			t.Fatalf("supersede running err=%v, want ErrPlanNodeInFlight", err)
		}
		if got := h.planVersion(t, planID); got != base {
			t.Fatalf("version=%d want %d after rejected supersede", got, base)
		}
	})

	t.Run("hold at gate with in flight downstream rejected", func(t *testing.T) {
		h := planAdvanceSetup(t)
		pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "hold", CreatedBy: "user:a"})
		h.drain(t)
		a, b := h.startRunningPlanAB(t, pid, planID)
		h.setTaskStatus(t, a, pm.TaskRunning)
		h.setTaskStatus(t, a, pm.TaskCompleted)
		if d, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil || len(d) != 1 || d[0] != b {
			t.Fatalf("advance B = %v err=%v, want [%s]", d, err, b)
		}
		h.setTaskStatus(t, b, pm.TaskRunning)
		base := h.planVersion(t, planID)
		_, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
			PlanID: planID, ParentGenerationID: activeGenerationID(t, h, planID), BaseVersion: base, IdempotencyKey: "evo-hold-conflict",
			Reason: "hold upstream gate", Evidence: "downstream already started", Creator: "user:a",
			Diff: pm.PlanGenerationDiff{NodeDecisions: []pm.PlanGenerationNodeDecision{{TaskID: a, Action: pm.EvolutionHoldAtGate}}},
		})
		if !errors.Is(err, pm.ErrPlanGenerationConflict) {
			t.Fatalf("hold-at-gate err=%v, want ErrPlanGenerationConflict", err)
		}
		if got := h.planVersion(t, planID); got != base {
			t.Fatalf("version=%d want %d after rejected hold-at-gate", got, base)
		}
	})
}

func TestEvolvePlanGeneration_PausedSwitchesGenerationWithoutDispatch(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "paused", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if err := h.svc.PausePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("PausePlan: %v", err)
	}
	base := h.planVersion(t, planID)
	res, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: activeGenerationID(t, h, planID), BaseVersion: base, IdempotencyKey: "evo-paused",
		Reason: "queue work while paused", Evidence: "paused review", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{Ref: "c", Title: "C-paused", AssigneeRef: "user:c1"}}},
	})
	if err != nil {
		t.Fatalf("EvolvePlanGeneration paused: %v", err)
	}
	cSnap := generationTaskByTitle(t, res.Generation, "C-paused")
	if len(res.Dispatched) != 0 {
		t.Fatalf("paused evolution dispatched %v, want none", res.Dispatched)
	}
	p, _ := h.plans.FindByID(h.ctx, planID)
	if p.ActiveGenerationID() != res.Generation.ID || p.Version() != base+1 {
		t.Fatalf("paused plan active/version = %s/%d, want %s/%d", p.ActiveGenerationID(), p.Version(), res.Generation.ID, base+1)
	}
	if dispatchedSet(t, h, planID)[cSnap.TaskID] {
		t.Fatalf("paused evolution task %s got a dispatch record", cSnap.TaskID)
	}
}

func TestEvolvePlanGeneration_LegacyRunningBackfillParent(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "legacy", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	base := clearActiveGenerationForLegacyTest(t, h, planID)
	res, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, BaseVersion: base, IdempotencyKey: "legacy-first-evo",
		Reason: "first evolution after migration", Evidence: "legacy active generation was empty", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{Ref: "b", Title: "B", AssigneeRef: "user:b1"}}},
	})
	if err != nil {
		t.Fatalf("legacy EvolvePlanGeneration: %v", err)
	}
	if res.Generation.ParentGenerationID == "" {
		t.Fatalf("legacy evolution parent is empty: %+v", res.Generation)
	}
	parent, err := h.plans.FindGenerationByID(h.ctx, res.Generation.ParentGenerationID)
	if err != nil {
		t.Fatalf("legacy parent generation missing: %v", err)
	}
	if parent.ParentGenerationID != "" || parent.Reason != "legacy generation backfill" {
		t.Fatalf("legacy parent generation = %+v", parent)
	}
	if len(parent.Snapshot.Tasks) == 0 || parent.Snapshot.PlanVersion != base {
		t.Fatalf("legacy parent snapshot = %+v, want pre-evolution baseline version %d", parent.Snapshot, base)
	}
	p, _ := h.plans.FindByID(h.ctx, planID)
	if p.ActiveGenerationID() != res.Generation.ID || p.Version() != base+1 {
		t.Fatalf("legacy active/version=%s/%d, want %s/%d", p.ActiveGenerationID(), p.Version(), res.Generation.ID, base+1)
	}
}

func TestEvolvePlanGeneration_StageCreateUpdateMembershipAndGateDispatch(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "stage evolution", CreatedBy: "user:a"})
	h.drain(t)
	buildStage, err := h.svc.CreateStage(h.ctx, CreateStageCommand{PlanID: planID, Name: "Build", Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.AssignTaskToStage(h.ctx, planID, a, buildStage, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage: %v", err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	if !dispatchedSet(t, h, planID)[a] {
		dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
		if err != nil {
			t.Fatalf("AdvancePlan initial A: %v", err)
		}
		if len(dispatched) != 1 || dispatched[0] != a {
			t.Fatalf("initial advance dispatched %v, want [%s]", dispatched, a)
		}
	}
	base := h.planVersion(t, planID)
	parent := activeGenerationID(t, h, planID)
	rounds := 2
	res, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: parent, BaseVersion: base, IdempotencyKey: "stage-evo-1",
		Reason: "add remediation stage", Evidence: "stage gate requires follow-up", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{
			Stages: []pm.PlanGenerationStageDraft{{
				Ref: "rem", Name: "Remediation", DependsOnStages: []string{string(buildStage)},
			}},
			StageUpdates: []pm.PlanGenerationStageUpdate{{
				StageID: buildStage, Name: "Build v2", MaxRounds: &rounds,
			}},
			Tasks: []pm.PlanGenerationTaskDraft{{
				Ref: "c", Title: "C remediation", AssigneeRef: "user:c1",
			}},
			StageMemberships: []pm.PlanGenerationStageMembership{{Task: "c", Stage: "rem"}},
		},
	})
	if err != nil {
		t.Fatalf("stage EvolvePlanGeneration: %v", err)
	}
	if len(res.Dispatched) != 0 {
		titles := map[pm.TaskID]string{}
		for _, id := range res.Dispatched {
			task, terr := h.tasks.FindByID(h.ctx, id)
			if terr != nil {
				t.Fatal(terr)
			}
			titles[id] = task.Title()
		}
		t.Fatalf("stage-gated evolution dispatched %v (%v), want none until upstream gate passes", res.Dispatched, titles)
	}
	buildSnap := generationStageByName(t, res.Generation, "Build v2")
	if buildSnap.StageID != buildStage || buildSnap.MaxRounds != rounds {
		t.Fatalf("updated stage snapshot = %+v", buildSnap)
	}
	remSnap := generationStageByName(t, res.Generation, "Remediation")
	if remSnap.StageID == "" || remSnap.GateTaskID == "" || remSnap.GateNodeID == "" || len(remSnap.DependsOnStages) != 1 || remSnap.DependsOnStages[0] != buildStage {
		t.Fatalf("new remediation stage snapshot = %+v", remSnap)
	}
	cSnap := generationTaskByTitle(t, res.Generation, "C remediation")
	if cSnap.StageID != remSnap.StageID || cSnap.NodeID == "" {
		t.Fatalf("new task stage membership snapshot = %+v, want stage %s with graph node", cSnap, remSnap.StageID)
	}
	if dispatchedSet(t, h, planID)[cSnap.TaskID] {
		t.Fatalf("stage-gated task %s got dispatched before upstream stage passed", cSnap.TaskID)
	}
}

func TestEvolvePlanGeneration_RollsBackStageAndTasksOnLateFailure(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "rollback", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	base := h.planVersion(t, planID)
	parent := activeGenerationID(t, h, planID)
	_, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: parent, BaseVersion: base, IdempotencyKey: "stage-rollback",
		Reason: "bad evolution", Evidence: "force late edge failure", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{
			Stages: []pm.PlanGenerationStageDraft{{Ref: "bad", Name: "Bad Stage"}},
			Tasks:  []pm.PlanGenerationTaskDraft{{Ref: "bad-task", Title: "Bad Task", AssigneeRef: "user:b1", StageRef: "bad"}},
			Edges:  []pm.PlanGenerationEdgeDraft{{From: "bad-task", To: "missing-task"}},
		},
	})
	if !errors.Is(err, pm.ErrTaskNotFound) {
		t.Fatalf("bad evolution err=%v, want ErrTaskNotFound", err)
	}
	p, _ := h.plans.FindByID(h.ctx, planID)
	if p.ActiveGenerationID() != parent || p.Version() != base {
		t.Fatalf("rollback active/version=%s/%d, want %s/%d", p.ActiveGenerationID(), p.Version(), parent, base)
	}
	if planHasTaskTitle(t, h, planID, "Bad Task") {
		t.Fatal("bad task survived rolled-back evolution")
	}
	stages, err := h.svc.ListStagesForPlan(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range stages {
		if st.Stage.Name() == "Bad Stage" {
			t.Fatal("bad stage survived rolled-back evolution")
		}
	}
	if _, found, err := h.plans.FindGenerationByIdempotencyKey(h.ctx, planID, "stage-rollback"); err != nil || found {
		t.Fatalf("rolled-back generation lookup found=%v err=%v, want not found", found, err)
	}
}
