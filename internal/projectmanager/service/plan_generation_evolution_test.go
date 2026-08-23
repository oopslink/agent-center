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

func activePlanGeneration(t *testing.T, h *planAdvanceHarness, planID pm.PlanID) (*pm.Plan, *pm.PlanGeneration) {
	t.Helper()
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActiveGenerationID() == "" {
		t.Fatalf("plan %s has no active generation", planID)
	}
	g, err := h.plans.FindGenerationByID(h.ctx, p.ActiveGenerationID())
	if err != nil {
		t.Fatal(err)
	}
	return p, g
}

func TestStartPlan_FreezesImmutableG0AndRequiresItAsFirstParent(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "g0", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, g0 := activePlanGeneration(t, h, planID)
	if g0.ParentGenerationID != "" {
		t.Fatalf("G0 parent=%s, want empty", g0.ParentGenerationID)
	}
	if g0.Snapshot.ActiveGenerationID != g0.ID || g0.Snapshot.PlanVersion != p.Version() {
		t.Fatalf("G0 snapshot active/version=%s/%d, want %s/%d", g0.Snapshot.ActiveGenerationID, g0.Snapshot.PlanVersion, g0.ID, p.Version())
	}
	if len(g0.Snapshot.Tasks) != 1 || generationTaskByTitle(t, g0, "A").TaskID != a {
		t.Fatalf("G0 tasks=%+v, want the activation topology", g0.Snapshot.Tasks)
	}
	if g0.Snapshot.Tasks[0].NodeID == "" {
		t.Fatalf("G0 was frozen before graph node assignment: %+v", g0.Snapshot.Tasks[0])
	}
	if len(g0.Diff.Tasks) != 0 || len(g0.Diff.Edges) != 0 || len(g0.Diff.NodeDecisions) != 0 {
		t.Fatalf("G0 diff=%+v, want empty activation baseline", g0.Diff)
	}

	base := p.Version()
	withoutG0 := EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: "", BaseVersion: base,
		IdempotencyKey: "first-with-empty-parent", Reason: "extend plan", Evidence: "review",
		Creator: "user:a", Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{Ref: "c", Title: "C", AssigneeRef: "user:c1", Detached: true}}},
	}
	if _, err := h.svc.EvolvePlanGeneration(h.ctx, withoutG0); !errors.Is(err, pm.ErrPlanGenerationConflict) {
		t.Fatalf("first evolution with empty parent err=%v, want ErrPlanGenerationConflict", err)
	}
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("empty-parent evolution changed version to %d, want %d", got, base)
	}
	if _, found, err := h.plans.FindGenerationByIdempotencyKey(h.ctx, planID, withoutG0.IdempotencyKey); err != nil || found {
		t.Fatalf("empty-parent generation persisted found=%v err=%v", found, err)
	}

	// Mutating live state after activation cannot rewrite the stored G0 JSON copy.
	liveA, err := h.tasks.FindByID(h.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := liveA.Rename("A changed live", h.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.tasks.Update(h.ctx, liveA); err != nil {
		t.Fatal(err)
	}
	if err := h.plans.RecordDispatch(h.ctx, planID, a, h.clk.Now(), "later-dispatch"); err != nil {
		t.Fatal(err)
	}
	reloadedG0, err := h.plans.FindGenerationByID(h.ctx, g0.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := generationTaskByTitle(t, reloadedG0, "A").Title; got != "A" {
		t.Fatalf("G0 task title drifted to %q", got)
	}
	if len(reloadedG0.Snapshot.DispatchRecords) != 0 {
		t.Fatalf("G0 dispatch snapshot drifted to %+v", reloadedG0.Snapshot.DispatchRecords)
	}

	withoutG0.ParentGenerationID = g0.ID
	withoutG0.IdempotencyKey = "first-with-g0-parent"
	res, err := h.svc.EvolvePlanGeneration(h.ctx, withoutG0)
	if err != nil {
		t.Fatalf("first evolution from G0: %v", err)
	}
	if res.Generation.ParentGenerationID != g0.ID {
		t.Fatalf("G1 parent=%s, want G0 %s", res.Generation.ParentGenerationID, g0.ID)
	}
	reloadedG0, err = h.plans.FindGenerationByID(h.ctx, g0.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedG0.Snapshot.Tasks) != 1 || reloadedG0.Snapshot.ActiveGenerationID != g0.ID {
		t.Fatalf("G0 changed after G1 commit: %+v", reloadedG0.Snapshot)
	}
}

func TestStartPlan_G0PersistenceFailureRollsBackActivation(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "g0-atomic", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	before := h.planVersion(t, planID)
	if _, err := h.svc.db.ExecContext(h.ctx, `CREATE TRIGGER reject_g0_insert
		BEFORE INSERT ON pm_plan_generations BEGIN SELECT RAISE(ABORT, 'reject G0'); END`); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err == nil {
		t.Fatal("StartPlan succeeded while G0 persistence was forced to fail")
	}
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != pm.PlanPending || p.ActiveGenerationID() != "" || p.GraphID() != "" || p.Version() != before {
		t.Fatalf("failed G0 left partial activation: status=%s active=%s graph=%s version=%d want pending/empty/empty/%d", p.Status(), p.ActiveGenerationID(), p.GraphID(), p.Version(), before)
	}
	liveA, err := h.tasks.FindByID(h.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if liveA.NodeID() != "" {
		t.Fatalf("failed G0 left graph node %s on task %s", liveA.NodeID(), a)
	}
	if _, found, err := h.plans.FindGenerationByIdempotencyKey(h.ctx, planID, initialPlanGenerationIdempotencyKey); err != nil || found {
		t.Fatalf("failed activation left G0 row found=%v err=%v", found, err)
	}
	if _, err := h.svc.db.ExecContext(h.ctx, `DROP TRIGGER reject_g0_insert`); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan retry after removing failure: %v", err)
	}
	activePlanGeneration(t, h, planID)
}

func TestStartPlan_ConcurrentUnbasedEvolutionCannotBootstrapGeneration(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "start-race", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	base := h.planVersion(t, planID)

	startGate := make(chan struct{})
	startErr := make(chan error, 1)
	evolveErr := make(chan error, 1)
	go func() {
		<-startGate
		startErr <- h.svc.StartPlan(h.ctx, planID, "user:a")
	}()
	go func() {
		<-startGate
		_, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
			PlanID: planID, ParentGenerationID: "", BaseVersion: base,
			IdempotencyKey: "start-race-empty-parent", Reason: "race start", Evidence: "race",
			Creator: "user:a", Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{Ref: "c", Title: "C", AssigneeRef: "user:c1", Detached: true}}},
		})
		evolveErr <- err
	}()
	close(startGate)
	if err := <-startErr; err != nil {
		t.Fatalf("concurrent StartPlan: %v", err)
	}
	err := <-evolveErr
	if !errors.Is(err, pm.ErrPlanNotRunning) && !errors.Is(err, pm.ErrPlanVersionConflict) && !errors.Is(err, pm.ErrPlanGenerationConflict) {
		t.Fatalf("concurrent empty-parent evolution err=%v, want fail-closed plan conflict", err)
	}
	_, g0 := activePlanGeneration(t, h, planID)
	if g0.ParentGenerationID != "" || len(g0.Snapshot.Tasks) != 1 {
		t.Fatalf("race G0=%+v, want one-task parentless baseline", g0)
	}
	if _, found, ferr := h.plans.FindGenerationByIdempotencyKey(h.ctx, planID, "start-race-empty-parent"); ferr != nil || found {
		t.Fatalf("concurrent unbased evolution persisted found=%v err=%v", found, ferr)
	}
}

func TestEvolvePlanGeneration_ConcurrentSiblingsOnlyOneActivates(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "evolve-race", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	p, g0 := activePlanGeneration(t, h, planID)
	type outcome struct {
		key string
		res EvolvePlanGenerationResult
		err error
	}
	gate := make(chan struct{})
	outcomes := make(chan outcome, 2)
	for _, key := range []string{"sibling-a", "sibling-b"} {
		key := key
		go func() {
			<-gate
			res, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
				PlanID: planID, ParentGenerationID: g0.ID, BaseVersion: p.Version(),
				IdempotencyKey: key, Reason: "concurrent sibling", Evidence: key, Creator: "user:a",
				Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{Ref: key, Title: key, AssigneeRef: "user:c1", Detached: true}}},
			})
			outcomes <- outcome{key: key, res: res, err: err}
		}()
	}
	close(gate)
	first, second := <-outcomes, <-outcomes
	successes := 0
	var winner outcome
	for _, got := range []outcome{first, second} {
		if got.err == nil {
			successes++
			winner = got
			continue
		}
		if !errors.Is(got.err, pm.ErrPlanVersionConflict) && !errors.Is(got.err, pm.ErrPlanGenerationConflict) {
			t.Fatalf("losing sibling %s err=%v, want generation/version conflict", got.key, got.err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent sibling successes=%d, outcomes=%+v %+v", successes, first, second)
	}
	activePlan, active := activePlanGeneration(t, h, planID)
	if active.ID != winner.res.Generation.ID || active.ParentGenerationID != g0.ID || activePlan.Version() != p.Version()+1 {
		t.Fatalf("active sibling=%s parent=%s version=%d; winner=%s G0=%s base=%d", active.ID, active.ParentGenerationID, activePlan.Version(), winner.res.Generation.ID, g0.ID, p.Version())
	}
	reloadedG0, err := h.plans.FindGenerationByID(h.ctx, g0.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedG0.Snapshot.Tasks) != 1 || reloadedG0.Snapshot.ActiveGenerationID != g0.ID {
		t.Fatalf("concurrent evolution changed G0: %+v", reloadedG0.Snapshot)
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
	_, parent := activePlanGeneration(t, h, planID)

	cmd := EvolvePlanGenerationCommand{
		PlanID:             planID,
		ParentGenerationID: parent.ID,
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
				Detached: true,
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
		_, parent := activePlanGeneration(t, h, planID)
		_, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
			PlanID: planID, ParentGenerationID: parent.ID, BaseVersion: base, IdempotencyKey: "evo-supersede-running",
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

	t.Run("edge rewrite of dispatched dependent rejects whole request", func(t *testing.T) {
		h := planAdvanceSetup(t)
		pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "edge-conflict", CreatedBy: "user:a"})
		h.drain(t)
		a, _ := h.startRunningPlanAB(t, pid, planID)
		base := h.planVersion(t, planID)
		_, parent := activePlanGeneration(t, h, planID)
		beforeTasks, err := h.tasks.ListByProject(h.ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
			PlanID: planID, ParentGenerationID: parent.ID, BaseVersion: base, IdempotencyKey: "evo-edge-running",
			Reason: "rewrite dispatched dependency", Evidence: "A already dispatched", Creator: "user:a",
			Diff: pm.PlanGenerationDiff{
				Tasks: []pm.PlanGenerationTaskDraft{{Ref: "c", Title: "must roll back", AssigneeRef: "user:c1"}},
				Edges: []pm.PlanGenerationEdgeDraft{{From: string(a), To: "c", Kind: pm.EdgeSeq}},
			},
		})
		if !errors.Is(err, pm.ErrPlanNodeInFlight) {
			t.Fatalf("edge rewrite err=%v want ErrPlanNodeInFlight", err)
		}
		if got := h.planVersion(t, planID); got != base {
			t.Fatalf("version=%d want %d after rejected request", got, base)
		}
		afterTasks, err := h.tasks.ListByProject(h.ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		if len(afterTasks) != len(beforeTasks) {
			t.Fatalf("task count=%d want %d; valid prefix was not rolled back", len(afterTasks), len(beforeTasks))
		}
		if _, found, err := h.plans.FindGenerationByIdempotencyKey(h.ctx, planID, "evo-edge-running"); err != nil || found {
			t.Fatalf("rejected generation persisted found=%v err=%v", found, err)
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
		_, parent := activePlanGeneration(t, h, planID)
		_, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
			PlanID: planID, ParentGenerationID: parent.ID, BaseVersion: base, IdempotencyKey: "evo-hold-conflict",
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

func TestEvolvePlanGeneration_RequiresNewRootBridgeOrDetached(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "connected-evolution", CreatedBy: "user:a"})
	h.drain(t)
	a, _ := h.startRunningPlanAB(t, pid, planID)
	base := h.planVersion(t, planID)
	_, parent := activePlanGeneration(t, h, planID)

	beforeTasks, err := h.tasks.ListByProject(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: parent.ID, BaseVersion: base,
		IdempotencyKey: "evo-disconnected-root", Reason: "add follow-up without bridge", Evidence: "review", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{
			Ref: "c", Title: "C disconnected", AssigneeRef: "user:c1", FollowsTaskID: a,
		}}},
	})
	if !errors.Is(err, pm.ErrPlanGenerationDisconnected) {
		t.Fatalf("disconnected root err=%v, want ErrPlanGenerationDisconnected", err)
	}
	if got := h.planVersion(t, planID); got != base {
		t.Fatalf("version=%d want %d after rejected disconnected root", got, base)
	}
	afterTasks, err := h.tasks.ListByProject(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterTasks) != len(beforeTasks) {
		t.Fatalf("task count=%d want %d; rejected disconnected root created tasks", len(afterTasks), len(beforeTasks))
	}

	res, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: parent.ID, BaseVersion: base,
		IdempotencyKey: "evo-bridged-root", Reason: "add bridged follow-up", Evidence: "review", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{
			Tasks: []pm.PlanGenerationTaskDraft{{
				Ref: "c", Title: "C bridged", AssigneeRef: "user:c1", FollowsTaskID: a,
			}},
			Edges: []pm.PlanGenerationEdgeDraft{{From: "c", To: string(a), Kind: pm.EdgeSeq}},
		},
	})
	if err != nil {
		t.Fatalf("bridged evolution: %v", err)
	}
	cSnap := generationTaskByTitle(t, res.Generation, "C bridged")
	var foundBridge bool
	for _, edge := range res.Generation.Snapshot.Edges {
		if edge.FromTaskID == cSnap.TaskID && edge.ToTaskID == a {
			foundBridge = true
		}
	}
	if !foundBridge {
		t.Fatalf("bridged snapshot missing C->A dependency: %+v", res.Generation.Snapshot.Edges)
	}
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
	_, parent := activePlanGeneration(t, h, planID)
	res, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: parent.ID, BaseVersion: base, IdempotencyKey: "evo-paused",
		Reason: "queue work while paused", Evidence: "paused review", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{Ref: "c", Title: "C-paused", AssigneeRef: "user:c1", Detached: true}}},
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

func TestReopenPlan_AllowsFollowUpEvolutionAfterDone(t *testing.T) {
	oh := orchestratorSetup(t)
	h := oh.planAdvanceHarness
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "reopen", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	h.setTaskStatus(t, a, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.CompletePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("CompletePlan: %v", err)
	}
	donePlan, parent := activePlanGeneration(t, h, planID)
	if donePlan.Status() != pm.PlanDone {
		t.Fatalf("status after complete = %s, want done", donePlan.Status())
	}
	doneVersion := donePlan.Version()

	if err := h.svc.ReopenPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("ReopenPlan: %v", err)
	}
	reopened, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status() != pm.PlanPaused {
		t.Fatalf("reopened status = %s, want paused", reopened.Status())
	}
	if reopened.ActiveGenerationID() != parent.ID || reopened.Version() != doneVersion+1 {
		t.Fatalf("reopened active/version = %s/%d, want %s/%d", reopened.ActiveGenerationID(), reopened.Version(), parent.ID, doneVersion+1)
	}
	h.drain(t)
	stillPaused, _ := h.plans.FindByID(h.ctx, planID)
	if stillPaused.Status() != pm.PlanPaused {
		t.Fatalf("reopened plan should not auto-complete before evolution; status=%s", stillPaused.Status())
	}

	res, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: reopened.ActiveGenerationID(), BaseVersion: reopened.Version(), IdempotencyKey: "evo-after-reopen",
		Reason: "follow-up work after completed plan", Evidence: "owner reopened plan", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{Tasks: []pm.PlanGenerationTaskDraft{{Ref: "c", Title: "C-after-reopen", AssigneeRef: "user:c1", Detached: true}}},
	})
	if err != nil {
		t.Fatalf("EvolvePlanGeneration after reopen: %v", err)
	}
	cSnap := generationTaskByTitle(t, res.Generation, "C-after-reopen")
	if len(res.Dispatched) != 0 {
		t.Fatalf("paused reopened evolution dispatched %v, want none before resume", res.Dispatched)
	}
	evolved, _ := h.plans.FindByID(h.ctx, planID)
	if evolved.Status() != pm.PlanPaused || evolved.ActiveGenerationID() != res.Generation.ID {
		t.Fatalf("evolved plan status/active = %s/%s, want paused/%s", evolved.Status(), evolved.ActiveGenerationID(), res.Generation.ID)
	}

	if err := h.svc.ResumePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("ResumePlan after evolution: %v", err)
	}
	h.drain(t)
	if !dispatchedSet(t, h, planID)[cSnap.TaskID] {
		t.Fatalf("resumed reopened plan did not dispatch new task %s", cSnap.TaskID)
	}
	running, _ := h.plans.FindByID(h.ctx, planID)
	if running.Status() != pm.PlanRunning {
		t.Fatalf("status after resume dispatch = %s, want running", running.Status())
	}
}
