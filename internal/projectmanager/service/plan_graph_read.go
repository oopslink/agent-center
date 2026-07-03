package service

import (
	"context"
	"errors"
	"strconv"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// =============================================================================
// T769 — plan-detail DAG read off the NEW orchestration engine graph.
//
// The webconsole plan-detail DAG historically rendered the DERIVED plan-layer
// read model (ComputePlanView, GetPlanDetail.View.Nodes) — task nodes + depends_on
// edges, with Start/End SYNTHESIZED client-side. T768 wired a real orchestration
// GRAPH behind every started plan (plan.GraphID), whose nodes carry the true
// control nodes (Start/End/Condition) and whose edges encode the real dependency
// structure. GetPlanGraph exposes that graph as a read model so the detail page
// can reflect the ACTUAL engine graph instead of a client-side reconstruction.
//
// NON-BREAKING: a plan WITHOUT a graph_id (every pre-T768 / draft / never-started
// plan, or a deployment with the engine unwired) returns ErrPlanHasNoGraph, and
// the caller falls back to the legacy ComputePlanView rendering — zero regression.
// =============================================================================

// ErrPlanHasNoGraph is returned by GetPlanGraph when the plan carries no
// orchestration graph (no graph_id, or the engine is unwired). It is NOT an error
// condition for the caller — it is the signal to fall back to the legacy
// ComputePlanView plan-DAG rendering (the non-breaking path).
var ErrPlanHasNoGraph = errors.New("projectmanager: plan has no orchestration graph")

// PlanGraphNode is one node of the orchestration-graph read model. category is
// business|control; for control nodes controlKind is start|end|condition (empty
// for business). A business node binds a task — TaskID + the bound task's
// TaskStatus / TaskOrgRef / Assignee are resolved here so the DAG can label the
// node without a second lookup (§ T769.1 "绑定 task 的状态/org_ref").
type PlanGraphNode struct {
	ID          string
	Category    string
	ControlKind string
	Title       string
	Status      string // raw engine node status: open|running|completed|reopen|discarded
	TaskID      string // bound task id ("" for control nodes)
	TaskStatus  string // bound task status ("" when unbound)
	TaskOrgRef  string // bound task org_ref "T123" ("" when unallocated / unbound)
	Assignee    string // bound task assignee ref ("" when unbound / unassigned)
}

// PlanGraphEdge is one directed edge from→to with its derived kind:
//   - seq         — a plain forward dependency (the default).
//   - conditional — an edge OUT of a condition control node (its downstream is
//     gated behind the condition resolving).
//   - loopback    — a decision→upstream back-edge (reject re-run). Loopbacks are
//     NOT stored in the graph (T768 keeps the bounded loop on the task-level
//     driver); they are overlaid here from the plan's loopback dependencies so the
//     DAG can still draw them.
type PlanGraphEdge struct {
	From string
	To   string
	Kind string
}

// PlanGraphView is the whole plan-detail graph read model: the graph header +
// its nodes + edges.
type PlanGraphView struct {
	GraphID string
	Status  string
	Nodes   []PlanGraphNode
	Edges   []PlanGraphEdge
}

// GetPlanGraph returns the orchestration-graph read model for a plan (T769). It
// returns ErrPlanHasNoGraph when the plan carries no graph (the non-breaking
// fallback signal — the caller renders the legacy ComputePlanView DAG instead).
func (s *Service) GetPlanGraph(ctx context.Context, id pm.PlanID) (*PlanGraphView, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	p, err := s.plans.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.planGraph(ctx, p)
}

// planGraph builds the graph read model for an already-loaded plan. It resolves
// each business node's bound task (status/org_ref/assignee) and derives edge
// kinds from the node categories + the plan's loopback dependencies.
func (s *Service) planGraph(ctx context.Context, p *pm.Plan) (*PlanGraphView, error) {
	if s.orch == nil || p.GraphID() == "" {
		return nil, ErrPlanHasNoGraph
	}
	graphID := orch.GraphID(p.GraphID())
	g, err := s.orch.GetGraph(ctx, graphID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.orch.ListNodes(ctx, graphID)
	if err != nil {
		return nil, err
	}
	edges, err := s.orch.ListEdges(ctx, graphID)
	if err != nil {
		return nil, err
	}

	// Bound-task lookup: task id → task, and node id → task (via Task.NodeID), so
	// business nodes can carry the live task status/org_ref and the loopback overlay
	// can map task ids back onto node ids.
	tasks, err := s.tasks.ListByPlan(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	taskByID := make(map[pm.TaskID]*pm.Task, len(tasks))
	nodeIDByTask := make(map[pm.TaskID]string, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID()] = t
		if t.NodeID() != "" {
			nodeIDByTask[t.ID()] = t.NodeID()
		}
	}

	// Category/controlKind per node id — needed for edge-kind derivation below.
	kindOf := make(map[string]orch.ControlKind, len(nodes))
	outNodes := make([]PlanGraphNode, 0, len(nodes))
	for _, n := range nodes {
		kindOf[string(n.ID())] = n.ControlKind()
		gn := PlanGraphNode{
			ID:          string(n.ID()),
			Category:    string(n.Category()),
			ControlKind: string(n.ControlKind()),
			Title:       n.Title(),
			Status:      string(n.Status()),
		}
		if n.Category() == orch.NodeCategoryBusiness {
			if tid, ok := n.Metadata()["task_id"].(string); ok && tid != "" {
				gn.TaskID = tid
				if t := taskByID[pm.TaskID(tid)]; t != nil {
					gn.TaskStatus = string(t.Status())
					gn.TaskOrgRef = orgRef("T", t.OrgNumber())
					gn.Assignee = string(t.Assignee())
				}
			}
		}
		outNodes = append(outNodes, gn)
	}

	// Edge kinds. A stored edge OUT of a condition control node is `conditional`
	// (the condition gates its downstream); everything else is `seq`.
	outEdges := make([]PlanGraphEdge, 0, len(edges))
	for _, e := range edges {
		kind := "seq"
		if kindOf[string(e.FromNodeID)] == orch.ControlKindCondition {
			kind = "conditional"
		}
		outEdges = append(outEdges, PlanGraphEdge{
			From: string(e.FromNodeID),
			To:   string(e.ToNodeID),
			Kind: kind,
		})
	}

	// Loopback overlay: the reject/loopback back-edges are NOT stored in the graph
	// (T768 keeps the bounded loop on the task-level driver). Overlay them from the
	// plan's loopback dependencies, mapped task→node, so the DAG can still draw the
	// loop {decision → upstream}.
	deps, err := s.plans.ListDependencies(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	for _, d := range deps {
		if !d.IsLoopback() {
			continue
		}
		from, ok1 := nodeIDByTask[d.FromTaskID]
		to, ok2 := nodeIDByTask[d.ToTaskID]
		if !ok1 || !ok2 {
			continue // a task not mapped to a node — skip defensively.
		}
		outEdges = append(outEdges, PlanGraphEdge{From: from, To: to, Kind: "loopback"})
	}

	return &PlanGraphView{
		GraphID: string(g.ID()),
		Status:  string(g.Status()),
		Nodes:   outNodes,
		Edges:   outEdges,
	}, nil
}

// orgRef renders the "T<n>"/"P<n>" display token, or "" when unallocated. Mirrors
// the webconsole orgRefToken so the graph read is self-contained.
func orgRef(prefix string, n int) string {
	if n <= 0 {
		return ""
	}
	return prefix + strconv.Itoa(n)
}
