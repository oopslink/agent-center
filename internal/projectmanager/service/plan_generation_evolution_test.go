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

	cmd := EvolvePlanGenerationCommand{
		PlanID:             planID,
		ParentGenerationID: "",
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
			PlanID: planID, BaseVersion: base, IdempotencyKey: "evo-supersede-running",
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
			PlanID: planID, BaseVersion: base, IdempotencyKey: "evo-hold-conflict",
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
		PlanID: planID, BaseVersion: base, IdempotencyKey: "evo-paused",
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
