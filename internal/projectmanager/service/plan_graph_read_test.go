package service

import (
	"errors"
	"testing"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// TestGetPlanGraph_ReflectsEngineGraph is the T769 read-side proof: for a graphed
// plan (a Dev→Review→Decision cycle with a conditional Integrate branch + a bounded
// loopback), GetPlanGraph returns the REAL orchestration graph — the Start/End/
// Condition control nodes, business nodes bound to their tasks (status + org_ref),
// and edges tagged by kind (seq / conditional / loopback).
func TestGetPlanGraph_ReflectsEngineGraph(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "graphread", CreatedBy: "user:a"})
	h.drain(t)
	dev, _, _, integ := buildGraphCycle(t, h, pid, planID)

	view, err := h.svc.GetPlanGraph(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanGraph: %v", err)
	}
	if view.GraphID == "" {
		t.Fatal("graph view has no graph_id")
	}
	if view.Status != string(orch.GraphRunning) {
		t.Fatalf("graph status = %q, want running", view.Status)
	}

	// --- control nodes: exactly one start, one end, >=1 condition ---
	var start, end, condition int
	business := 0
	boundByTask := map[string]PlanGraphNode{}
	for _, n := range view.Nodes {
		switch {
		case n.Category == string(orch.NodeCategoryControl):
			switch n.ControlKind {
			case string(orch.ControlKindStart):
				start++
			case string(orch.ControlKindEnd):
				end++
			case string(orch.ControlKindCondition):
				condition++
			default:
				t.Fatalf("control node %s has unexpected control_kind %q", n.ID, n.ControlKind)
			}
		case n.Category == string(orch.NodeCategoryBusiness):
			business++
			if n.TaskID == "" {
				t.Fatalf("business node %s has no bound task_id", n.ID)
			}
			if n.ControlKind != "" {
				t.Fatalf("business node %s should have no control_kind, got %q", n.ID, n.ControlKind)
			}
			boundByTask[n.TaskID] = n
		default:
			t.Fatalf("node %s has unexpected category %q", n.ID, n.Category)
		}
	}
	if start != 1 || end != 1 {
		t.Fatalf("control anchors = start:%d end:%d, want 1/1", start, end)
	}
	if condition < 1 {
		t.Fatalf("condition nodes = %d, want >=1 (decision gate)", condition)
	}
	if business != 4 {
		t.Fatalf("business nodes = %d, want 4 (one per task)", business)
	}

	// --- a business node carries its bound task's status + org_ref (§ T769.1) ---
	devNode, ok := boundByTask[string(dev)]
	if !ok {
		t.Fatalf("Dev task %s not present as a business node", dev)
	}
	if devNode.TaskStatus == "" {
		t.Fatal("Dev business node has empty task_status — bound task state not resolved")
	}
	if devNode.TaskOrgRef == "" {
		t.Fatal("Dev business node has empty org_ref — bound task org_ref not resolved")
	}
	_ = integ

	// --- edge kinds: seq + conditional (from the graph) + loopback (overlay) ---
	kinds := map[string]int{}
	for _, e := range view.Edges {
		kinds[e.Kind]++
		if e.From == "" || e.To == "" {
			t.Fatalf("edge has empty endpoint: %+v", e)
		}
	}
	if kinds["seq"] == 0 {
		t.Fatalf("no seq edges; kinds=%v", kinds)
	}
	if kinds["conditional"] == 0 {
		t.Fatalf("no conditional edges (condition gate); kinds=%v", kinds)
	}
	if kinds["loopback"] == 0 {
		t.Fatalf("no loopback edges (reject overlay); kinds=%v", kinds)
	}
}

// TestGetPlanGraph_StartEndAnchorsWired is the T800 proof: buildPlanGraph now wires
// the seeded Start/End control anchors — Start → every ROOT business node (no
// incoming graph edge), every SINK business node (no outgoing) → End — so the graph
// has a single inlined source and sink instead of orphan Start/End (End previously
// had no incoming edge, which the DAG renderer mis-laid-out). For the Dev→Review→
// Decision→(cond)Integrate cycle: Dev is the sole root (its only "incoming" is the
// loopback, which is NOT a graph edge), Integrate is the sole sink.
func TestGetPlanGraph_StartEndAnchorsWired(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "anchors", CreatedBy: "user:a"})
	h.drain(t)
	dev, _, _, integ := buildGraphCycle(t, h, pid, planID)

	view, err := h.svc.GetPlanGraph(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanGraph: %v", err)
	}

	var startID, endID, devNodeID, integNodeID string
	for _, n := range view.Nodes {
		switch {
		case n.ControlKind == string(orch.ControlKindStart):
			startID = n.ID
		case n.ControlKind == string(orch.ControlKindEnd):
			endID = n.ID
		case n.TaskID == string(dev):
			devNodeID = n.ID
		case n.TaskID == string(integ):
			integNodeID = n.ID
		}
	}
	if startID == "" || endID == "" || devNodeID == "" || integNodeID == "" {
		t.Fatalf("missing node ids: start=%q end=%q dev=%q integ=%q", startID, endID, devNodeID, integNodeID)
	}

	edge := func(from, to string) *PlanGraphEdge {
		for i := range view.Edges {
			if view.Edges[i].From == from && view.Edges[i].To == to {
				return &view.Edges[i]
			}
		}
		return nil
	}
	// Start → root (Dev): present and seq-kinded.
	if e := edge(startID, devNodeID); e == nil {
		t.Fatalf("missing Start→root(Dev) anchor edge; edges=%+v", view.Edges)
	} else if e.Kind != "seq" {
		t.Fatalf("Start→Dev edge kind = %q, want seq", e.Kind)
	}
	// Sink (Integrate) → End: present and seq-kinded.
	if e := edge(integNodeID, endID); e == nil {
		t.Fatalf("missing sink(Integrate)→End anchor edge; edges=%+v", view.Edges)
	} else if e.Kind != "seq" {
		t.Fatalf("Integrate→End edge kind = %q, want seq", e.Kind)
	}
	// Start stays a pure source (no incoming); End stays a pure sink (no outgoing).
	for _, e := range view.Edges {
		if e.To == startID {
			t.Fatalf("Start anchor has an incoming edge %+v — should be a pure source", e)
		}
		if e.From == endID {
			t.Fatalf("End anchor has an outgoing edge %+v — should be a pure sink", e)
		}
	}
}

// TestGetPlanGraph_NoGraph_FallbackSignal is the NON-BREAKING guard: a plan with NO
// graph (never started — no graph_id) returns ErrPlanHasNoGraph, which the handler
// maps to has_graph:false so the FE renders the legacy ComputePlanView DAG. Zero
// regression for pre-T768 / draft plans.
func TestGetPlanGraph_NoGraph_FallbackSignal(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "nograph", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "A", "user:a1") // selected but plan not started → no graph

	if _, err := h.svc.GetPlanGraph(ctx, planID); !errors.Is(err, ErrPlanHasNoGraph) {
		t.Fatalf("GetPlanGraph on an ungraphed plan = %v, want ErrPlanHasNoGraph", err)
	}
}

// TestGetPlanGraph_EngineUnwired_FallbackSignal proves the fallback also holds when
// the orchestration engine is UNWIRED (Deps.Orch nil) — even a STARTED plan has no
// graph_id, so GetPlanGraph returns ErrPlanHasNoGraph and the legacy DAG is used.
func TestGetPlanGraph_EngineUnwired_FallbackSignal(t *testing.T) {
	h := planAdvanceSetup(t) // no Orch wired
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "unwired", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if _, err := h.svc.GetPlanGraph(ctx, planID); !errors.Is(err, ErrPlanHasNoGraph) {
		t.Fatalf("GetPlanGraph with engine unwired = %v, want ErrPlanHasNoGraph", err)
	}
}
