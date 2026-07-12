package service

import (
	"context"
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// seedTwoMultiNodeStagePlan builds: Stage A = {a1 → a2}, Stage B = {b1 → b2},
// B depends_on A. BOTH stages are multi-node (unlike seedTwoStagePlan whose
// downstream B is single-node). b1 is stage B's entry; the barrier must hold it.
func seedTwoMultiNodeStagePlan(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, planID pm.PlanID, maxRounds int) (a1, a2, b1, b2 pm.TaskID, stageA, stageB pm.StageID) {
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
	b2 = h.seedAssignedTask(t, pid, planID, "b2", "user:b2")
	for _, tk := range []pm.TaskID{a1, a2} {
		if err := h.svc.AssignTaskToStage(h.ctx, planID, tk, stageA, "user:a"); err != nil {
			t.Fatalf("AssignTaskToStage A: %v", err)
		}
	}
	for _, tk := range []pm.TaskID{b1, b2} {
		if err := h.svc.AssignTaskToStage(h.ctx, planID, tk, stageB, "user:a"); err != nil {
			t.Fatalf("AssignTaskToStage B: %v", err)
		}
	}
	// Intra-stage edges: a2 depends_on a1, b2 depends_on b1 (a1/b1 are the entries).
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: a2, ToTaskID: a1, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: b2, ToTaskID: b1, Kind: pm.EdgeSeq})
	return a1, a2, b1, b2, stageA, stageB
}

// mustRunnable / mustNotRunnable assert the §13.A run-gate (start_work / list_my_tasks)
// verdict for a task — the surface issue-b867bbe6 was bypassing.
func mustNotRunnable(t *testing.T, h *planAdvanceHarness, tid pm.TaskID, what string) {
	t.Helper()
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("%s: EnsureTaskRunnable = %v, want ErrTaskNotRunnable (barrier must hold)", what, err)
	}
}

func mustRunnable(t *testing.T, h *planAdvanceHarness, tid pm.TaskID, what string) {
	t.Helper()
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("%s: EnsureTaskRunnable = %v, want nil (should be released)", what, err)
	}
}

// TestStage_Barrier_MultiNodeDownstream_RunGate is the issue-b867bbe6 regression lock:
// a MULTI-node downstream stage's ENTRY must be held behind the upstream gate barrier
// at BOTH surfaces — the dispatch path (graphReadySet, already correct) AND the run-gate
// (EnsureTaskRunnable → list_my_tasks / start_work, which was STAGE-UNAWARE and let an
// assigned agent start the entry, bypassing the closed barrier). It differs from
// TestStage_Barrier_GatePass (single-node downstream) by giving stage B two nodes, and
// it drives the barrier through hold → release at the run-gate, not only the dispatcher.
func TestStage_Barrier_MultiNodeDownstream_RunGate(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, b2, stageA, _ := seedTwoMultiNodeStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)

	// --- structural: the downstream-multi-node barrier edge (gateA -> b1) is laid ---
	detA, _ := h.svc.GetStage(ctx, stageA)
	gateA := orch.NodeID(detA.Stage.GateNodeID())
	if gateA == "" {
		t.Fatal("stage A gate_node_id not stamped")
	}
	b1Node := taskNodeID(t, h, orchSvc, p.GraphID(), "b1")
	if !hasEdge(t, ctx, orchSvc, p.GraphID(), gateA, b1Node) {
		t.Fatalf("barrier edge gateA(%s) -> b1(%s) missing — buildStages did not wire the multi-node barrier", gateA, b1Node)
	}

	// --- hold: dispatch releases only a1; b1 is NOT dispatched and NOT runnable ---
	d1, _ := h.svc.AdvancePlan(ctx, planID, "user:a")
	if len(d1) != 1 || d1[0] != a1 {
		t.Fatalf("dispatch #1 = %v, want [a1]", d1)
	}
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched before gate A passed — barrier bypassed (dispatch)")
	}
	mustNotRunnable(t, h, b1, "b1 while stage A gate open")

	// --- drive stage A to all-done; b1 STILL held (gate not yet resolved) ---
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched after stage A members done but gate unresolved — barrier bypassed")
	}
	mustNotRunnable(t, h, b1, "b1 after stage A members done, gate still open")
	// b2 (non-entry) is held by its in-stage predecessor b1 too.
	mustNotRunnable(t, h, b2, "b2 (non-entry) while b1 not done")

	// --- release: gate A pass lifts the barrier at BOTH surfaces ---
	if err := h.svc.ResolveStageGate(ctx, detA.Stage.GateNodeID(), "pass", "user:a"); err != nil {
		t.Fatalf("ResolveStageGate pass: %v", err)
	}
	if !dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 not dispatched after gate A pass — barrier did not lift (dispatch)")
	}
	mustRunnable(t, h, b1, "b1 after gate A pass")
	// b2 still gated by b1 (not yet done) — intra-stage dep, must stay blocked.
	mustNotRunnable(t, h, b2, "b2 after gate A pass but b1 not done")
}

// taskNodeID resolves the orchestration node id for a task by its title.
func taskNodeID(t *testing.T, h *planAdvanceHarness, orchSvc *orch.Service, graphID, title string) orch.NodeID {
	t.Helper()
	nodes, err := orchSvc.ListNodes(h.ctx, orch.GraphID(graphID))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Title() == title {
			return n.ID()
		}
	}
	t.Fatalf("no node titled %q in graph %s", title, graphID)
	return ""
}

// hasEdge reports whether the graph has a from->to edge.
func hasEdge(t *testing.T, ctx context.Context, orchSvc *orch.Service, graphID string, from, to orch.NodeID) bool {
	t.Helper()
	edges, err := orchSvc.ListEdges(ctx, orch.GraphID(graphID))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edges {
		if e.FromNodeID == from && e.ToNodeID == to {
			return true
		}
	}
	return false
}
