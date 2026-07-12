package service

import (
	"errors"
	"strings"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// dispatchedSet returns the set of task ids with a dispatch record for the plan.
func dispatchedSet(t *testing.T, h *planAdvanceHarness, planID pm.PlanID) map[pm.TaskID]bool {
	t.Helper()
	recs, err := h.plans.ListDispatchRecords(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[pm.TaskID]bool{}
	for _, r := range recs {
		out[r.TaskID] = true
	}
	return out
}

// seedTwoStagePlan builds: Stage A = {a1 → a2}, Stage B = {b1}, B depends_on A.
// Returns the task ids + stage ids. StartPlan is left to the caller.
func seedTwoStagePlan(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, planID pm.PlanID, maxRounds int) (a1, a2, b1 pm.TaskID, stageA, stageB pm.StageID) {
	t.Helper()
	stageA, err := h.svc.CreateStage(h.ctx, CreateStageCommand{PlanID: planID, Name: "A", MaxRounds: maxRounds, Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage A: %v", err)
	}
	stageB, err = h.svc.CreateStage(h.ctx, CreateStageCommand{PlanID: planID, Name: "B", DependsOnStages: []pm.StageID{stageA}, MaxRounds: maxRounds, Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage B: %v", err)
	}
	a1 = h.seedAssignedTask(t, pid, planID, "a1", "user:a1")
	a2 = h.seedAssignedTask(t, pid, planID, "a2", "user:a2")
	b1 = h.seedAssignedTask(t, pid, planID, "b1", "user:b1")
	for _, tk := range []pm.TaskID{a1, a2} {
		if err := h.svc.AssignTaskToStage(h.ctx, planID, tk, stageA, "user:a"); err != nil {
			t.Fatalf("AssignTaskToStage A: %v", err)
		}
	}
	if err := h.svc.AssignTaskToStage(h.ctx, planID, b1, stageB, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage B: %v", err)
	}
	// Intra-stage edge: a2 depends_on a1 (a1 is the stage entry).
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: a2, ToTaskID: a1, Kind: pm.EdgeSeq})
	return a1, a2, b1, stageA, stageB
}

// TestStage_Barrier_GatePass is the end-to-end for the stage barrier + gate PASS: a
// downstream stage's entry does NOT dispatch until every upstream-stage business node
// is done AND the upstream gate passes. Then a gate pass releases it.
func TestStage_Barrier_GatePass(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, stageA, stageB := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)

	// --- 落图: two gate nodes, and stage A's gate stamped on the aggregate ---
	if got := stageGateNodeCountCtx(t, h, orchSvc, p.GraphID()); got != 2 {
		t.Fatalf("stage gate nodes = %d, want 2", got)
	}
	detA, _ := h.svc.GetStage(ctx, stageA)
	if detA.Stage.GateNodeID() == "" {
		t.Fatal("stage A gate_node_id not stamped by buildStages")
	}
	if detA.Status != pm.StageOpen {
		t.Fatalf("stage A status = %q, want open (nothing started)", detA.Status)
	}

	// --- dispatch #1: only a1 (stage A entry). a2 blocked on a1; b1 blocked on gate A ---
	d1, _ := h.svc.AdvancePlan(ctx, planID, "user:a")
	if len(d1) != 1 || d1[0] != a1 {
		t.Fatalf("dispatch #1 = %v, want [a1]", d1)
	}
	// b1 must NOT be dispatched — the stage barrier holds it behind gate A.
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched before stage A gate passed — barrier bypassed")
	}

	// --- complete a1 → a2 ready; complete a2 → stage A business all done ---
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	d2, _ := h.svc.AdvancePlan(ctx, planID, "user:a")
	if len(d2) != 1 || d2[0] != a2 {
		t.Fatalf("dispatch #2 = %v, want [a2]", d2)
	}
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); len(d) != 0 {
		t.Fatalf("dispatch after a2 done = %v, want [] (b1 still gated by unresolved gate A)", d)
	}
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched before gate resolution — barrier bypassed")
	}
	// stage A: all members done but gate pending → running (§4.1).
	detA, _ = h.svc.GetStage(ctx, stageA)
	if detA.Status != pm.StageRunning {
		t.Fatalf("stage A status = %q, want running (gate pending)", detA.Status)
	}

	// --- gate PASS releases stage B's entry b1 (barrier lifts) ---
	if err := h.svc.ResolveStageGate(ctx, detA.Stage.GateNodeID(), "pass", "user:a"); err != nil {
		t.Fatalf("ResolveStageGate pass: %v", err)
	}
	if !dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 not dispatched after gate A pass — barrier did not lift")
	}
	detA, _ = h.svc.GetStage(ctx, stageA)
	if detA.Status != pm.StageDone {
		t.Fatalf("stage A status = %q, want done (all members done + gate passed)", detA.Status)
	}
	// b1 is dispatched but not yet started → stage B projects open (§4.1 全未起).
	detB, _ := h.svc.GetStage(ctx, stageB)
	if detB.Status != pm.StageOpen {
		t.Fatalf("stage B status = %q, want open (b1 dispatched but not started)", detB.Status)
	}
	// Once b1 starts running, stage B projects running.
	h.setTaskStatus(t, b1, pm.TaskRunning)
	detB, _ = h.svc.GetStage(ctx, stageB)
	if detB.Status != pm.StageRunning {
		t.Fatalf("stage B status = %q, want running (b1 running)", detB.Status)
	}
}

// stageGateNodeCountCtx is the ctx-correct gate counter (the nil-ctx helper above is
// unused; kept minimal here).
func stageGateNodeCountCtx(t *testing.T, h *planAdvanceHarness, orchSvc *orch.Service, graphID string) int {
	t.Helper()
	nodes, err := orchSvc.ListNodes(h.ctx, orch.GraphID(graphID))
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	c := 0
	for _, n := range nodes {
		if _, ok := n.Metadata()["stage_gate"]; ok {
			c++
		}
	}
	return c
}

// TestStage_GateReject_ReopensStageSubgraph asserts a bounded gate reject reopens the
// stage's member tasks (its sub-DAG) and does NOT release the downstream stage.
func TestStage_GateReject_ReopensStageSubgraph(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, stageA, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	// Drive stage A to all-done.
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")

	detA, _ := h.svc.GetStage(ctx, stageA)
	gate := detA.Stage.GateNodeID()

	// Gate REJECT (bounded) → stage A's members reopen; b1 stays gated.
	if err := h.svc.ResolveStageGate(ctx, gate, "reject", "user:a"); err != nil {
		t.Fatalf("ResolveStageGate reject: %v", err)
	}
	ta1, _ := h.tasks.FindByID(ctx, a1)
	ta2, _ := h.tasks.FindByID(ctx, a2)
	if ta1.Status() != pm.TaskReopened || ta2.Status() != pm.TaskReopened {
		t.Fatalf("after reject a1=%s a2=%s, want both reopened", ta1.Status(), ta2.Status())
	}
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched after gate reject — downstream must stay blocked")
	}
	detA, _ = h.svc.GetStage(ctx, stageA)
	if detA.Rounds != 1 {
		t.Fatalf("stage A rounds = %d, want 1 after one reject", detA.Rounds)
	}
	if detA.Status != pm.StageReopen {
		t.Fatalf("stage A status = %q, want reopen", detA.Status)
	}
}

// TestStage_GateReject_ExhaustEscalates asserts that once max_rounds is exhausted the
// gate is LEFT UNRESOLVED (downstream stays blocked — a closed barrier, §5).
func TestStage_GateReject_ExhaustEscalates(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, stageA, _ := seedTwoStagePlan(t, h, pid, planID, 1) // max_rounds = 1
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	driveStageADone := func() {
		h.svc.AdvancePlan(ctx, planID, "user:a")
		h.setTaskStatus(t, a1, pm.TaskCompleted)
		h.svc.AdvancePlan(ctx, planID, "user:a")
		h.setTaskStatus(t, a2, pm.TaskCompleted)
		h.svc.AdvancePlan(ctx, planID, "user:a")
	}
	driveStageADone()
	detA, _ := h.svc.GetStage(ctx, stageA)
	gate := detA.Stage.GateNodeID()

	// Round 1 reject (within bound) reopens; re-complete the members.
	if err := h.svc.ResolveStageGate(ctx, gate, "reject", "user:a"); err != nil {
		t.Fatalf("reject round 1: %v", err)
	}
	// Re-run the reopened members to done again.
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")

	// Round 2 reject EXHAUSTS (round 2 > max_rounds 1): gate left unresolved, escalate.
	if err := h.svc.ResolveStageGate(ctx, gate, "reject", "user:a"); err != nil {
		t.Fatalf("reject round 2 (exhaust): %v", err)
	}
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched after exhaustion — the closed barrier must NOT release downstream")
	}
}

// TestStage_ZeroRegression asserts a plan with NO stages builds a graph with NO stage
// gate nodes (§8: pm_stages empty + stage_id all empty = today's pure-node DAG).
func TestStage_ZeroRegression(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "nostages", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: b, ToTaskID: a, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if got := stageGateNodeCountCtx(t, h, orchSvc, p.GraphID()); got != 0 {
		t.Fatalf("stage gate nodes = %d in a stageless plan, want 0 (§8 zero-regression)", got)
	}
}

// TestListStagesForPlan_SharesProjectionWithGetStage locks the T981 §7 web-read glue:
// ListStagesForPlan returns EVERY stage's projection, and each one is IDENTICAL to
// GetStage(stageID) — proving the single shared projStage path (pd constraint 1: no
// second copy of the derivation that could drift). Also covers the §8 no-stage empty.
func TestListStagesForPlan_SharesProjectionWithGetStage(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// No-stage plan → ListStagesForPlan is empty (§8 backward compat).
	noStage, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "nostages", CreatedBy: "user:a"})
	h.drain(t)
	if dets, err := h.svc.ListStagesForPlan(ctx, noStage); err != nil || len(dets) != 0 {
		t.Fatalf("ListStagesForPlan(no-stage) = (%d stages, %v), want (0, nil)", len(dets), err)
	}

	// Two-stage plan, driven into a mixed state (stage A members done, gate pending →
	// running; stage B not started → open), so the projection has something to compare.
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	_, _, _, stageA, stageB := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	dets, err := h.svc.ListStagesForPlan(ctx, planID)
	if err != nil {
		t.Fatalf("ListStagesForPlan: %v", err)
	}
	if len(dets) != 2 {
		t.Fatalf("ListStagesForPlan returned %d stages, want 2", len(dets))
	}
	byID := map[pm.StageID]*StageDetail{}
	for _, d := range dets {
		byID[d.Stage.ID()] = d
	}
	// Each listed stage must be byte-for-byte the same projection as GetStage — the
	// single-source guarantee.
	for _, sid := range []pm.StageID{stageA, stageB} {
		listed, ok := byID[sid]
		if !ok {
			t.Fatalf("ListStagesForPlan missing stage %s", sid)
		}
		got, err := h.svc.GetStage(ctx, sid)
		if err != nil {
			t.Fatalf("GetStage(%s): %v", sid, err)
		}
		if listed.Status != got.Status || listed.Rounds != got.Rounds || len(listed.Members) != len(got.Members) {
			t.Fatalf("stage %s: list projection {status=%q rounds=%d members=%d} != get {status=%q rounds=%d members=%d} — projection drift",
				sid, listed.Status, listed.Rounds, len(listed.Members), got.Status, got.Rounds, len(got.Members))
		}
		for i := range listed.Members {
			if listed.Members[i] != got.Members[i] {
				t.Fatalf("stage %s member[%d] drift: list=%+v get=%+v", sid, i, listed.Members[i], got.Members[i])
			}
		}
	}
}

// TestStage_CrossEdge_RejectedAtBuild asserts the §5 build-time invariant: a manual
// plan edge between two DIFFERENT stages is rejected at StartPlan (graph build).
func TestStage_CrossEdge_RejectedAtBuild(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, _, b1, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	// A hand-drawn cross-stage business edge b1 depends_on a1 (bypasses the gate barrier).
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: b1, ToTaskID: a1, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrStageCrossEdge) {
		t.Fatalf("StartPlan with cross-stage edge = %v, want ErrStageCrossEdge", err)
	}
}

// TestStage_StagelessNode_RejectedAtBuild asserts the author-time invariant (§5,
// quick-fix 1a): once a plan has ≥1 stage, a business node with no stage_id is rejected
// at StartPlan (it would run ahead of the staged flow, bypassing the gate/barrier). The
// error names the orphan node. A fully-assigned staged plan is unaffected (covered by
// TestStage_Barrier_GatePass); a plan with NO stages is unaffected (TestStage_ZeroRegression).
func TestStage_StagelessNode_RejectedAtBuild(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	seedTwoStagePlan(t, h, pid, planID, 3)
	// A business task selected into the staged plan but NEVER assigned to a stage — the
	// stageless run-ahead 1a closes.
	orphan := h.seedAssignedTask(t, pid, planID, "run-ahead", "user:x")
	err := h.svc.StartPlan(ctx, planID, "user:a")
	if !errors.Is(err, pm.ErrStageStagelessNode) {
		t.Fatalf("StartPlan with stageless business node = %v, want ErrStageStagelessNode", err)
	}
	if !strings.Contains(err.Error(), string(orphan)) {
		t.Fatalf("error %q does not name the orphan node %s", err, orphan)
	}
}
