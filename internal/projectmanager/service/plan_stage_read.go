package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// =============================================================================
// issue-77d9beff ② — STAGE-AWARE read model for get_plan.
//
// DerivePlanView (plan_view.go) is PURE over task-level deps and is deliberately
// STAGE-UNAWARE: a stage barrier is an orchestration-graph gate CONDITION node +
// edge (a downstream stage's ENTRY depends_on the upstream stage's gate), which lives
// ONLY in the graph, not in the task-level depends_on set. So DerivePlanView derives a
// downstream-stage entry as `ready` (and lists it in ready_set) even while its upstream
// stage gate is unresolved — the read model LIES, misleading the orchestrator into
// assigning + starting a node the run gate (EnsureTaskRunnable → stageGateBlocks) then
// deterministically rejects and the dispatcher (graphReadySet) never dispatches.
//
// These helpers correct the READ model to agree with the run gate + dispatcher (ONE
// source of truth), and surface the gate CONDITION nodes so the plan owner / PD can see
// which gate to resolve. They are wired ONLY into the READ-facing GetPlanDetail /
// GetPlanDetailForMember — the internal planDetail path (planNodeStatus, claim/dispatch
// gates) is untouched, so dispatch/claim keep deriving off the graph directly.
// =============================================================================

// stageBarrierHeldSet returns the plan's task ids whose graph node is a downstream-stage
// ENTRY held behind an UNRESOLVED upstream stage gate. It is the BATCHED counterpart of
// stageGateBlocks (runnable_gate.go, one task at a time for the run gate): one graph load
// answers it for a WHOLE plan in O(nodes+edges). The rule is IDENTICAL — an entry is held
// iff any of its upstream graph edges originates at a stage-gate CONDITION node
// ("stage_gate" metadata) not yet resolved (completed/discarded) — so the read model, the
// run gate, and the dispatcher all agree on "barrier lifted". Nil (no barriers) for a plan
// with no engine / no graph (§8 zero-regression: a pure-node DAG has no stage gates).
func (s *Service) stageBarrierHeldSet(ctx context.Context, p *pm.Plan) (map[pm.TaskID]bool, error) {
	if s.orch == nil || p.GraphID() == "" {
		return nil, nil
	}
	graphID := orch.GraphID(p.GraphID())
	nodes, err := s.orch.ListNodes(ctx, graphID)
	if err != nil {
		return nil, err
	}
	taskOfNode := make(map[orch.NodeID]pm.TaskID, len(nodes))
	unresolvedGate := make(map[orch.NodeID]bool)
	for _, n := range nodes {
		if tid := nodeTaskID(n); tid != "" {
			taskOfNode[n.ID()] = tid
		}
		if n.Category() != orch.NodeCategoryControl || n.ControlKind() != orch.ControlKindCondition {
			continue
		}
		if _, isStageGate := n.Metadata()["stage_gate"]; !isStageGate {
			continue
		}
		if n.Status() != orch.NodeCompleted && n.Status() != orch.NodeDiscarded {
			unresolvedGate[n.ID()] = true
		}
	}
	if len(unresolvedGate) == 0 {
		return nil, nil
	}
	edges, err := s.orch.ListEdges(ctx, graphID)
	if err != nil {
		return nil, err
	}
	held := make(map[pm.TaskID]bool)
	for _, e := range edges {
		if !unresolvedGate[e.FromNodeID] {
			continue
		}
		if tid, ok := taskOfNode[e.ToNodeID]; ok {
			held[tid] = true // a downstream entry behind an unresolved upstream stage gate.
		}
	}
	return held, nil
}

// applyStageBarrierView corrects the STAGE-UNAWARE DerivePlanView on a READ-facing
// PlanDetail: it downgrades each barrier-held entry from `ready` to `blocked` and drops
// it from ReadySet, so get_plan agrees with the run gate (EnsureTaskRunnable) and the
// dispatcher (graphReadySet). It touches ONLY the read model; the barrier / run gate /
// dispatch are unchanged. No-op when there are no stage barriers (pure-node DAG / pool).
func (s *Service) applyStageBarrierView(ctx context.Context, detail *PlanDetail) error {
	if detail == nil || detail.Plan == nil {
		return nil
	}
	held, err := s.stageBarrierHeldSet(ctx, detail.Plan)
	if err != nil {
		return err
	}
	if len(held) == 0 {
		return nil
	}
	for i := range detail.View.Nodes {
		n := &detail.View.Nodes[i]
		if held[n.TaskID] && n.NodeStatus == pm.NodeReady {
			n.NodeStatus = pm.NodeBlocked // stage barrier holds it — not actually startable.
		}
	}
	if len(detail.View.ReadySet) > 0 {
		kept := detail.View.ReadySet[:0]
		for _, id := range detail.View.ReadySet {
			if !held[id] {
				kept = append(kept, id)
			}
		}
		detail.View.ReadySet = kept
	}
	return nil
}

// fillStageGates surfaces the plan's STAGE GATE condition nodes onto the read-facing
// PlanDetail (issue-77d9beff ②): get_plan otherwise exposes only business task nodes, so
// the plan owner cannot see the gate node_id to resolve. It resolves each stage's
// gate_node_id to its live engine node status (pending while unresolved). No-op when the
// stage repo / graph is unwired or the plan has no stages (§8 zero-regression).
func (s *Service) fillStageGates(ctx context.Context, detail *PlanDetail) error {
	if detail == nil || detail.Plan == nil || s.stages == nil || s.orch == nil || detail.Plan.GraphID() == "" {
		return nil
	}
	stages, err := s.stages.ListByPlan(ctx, detail.Plan.ID())
	if err != nil {
		return err
	}
	if len(stages) == 0 {
		return nil
	}
	var gates []StageGateView
	for _, st := range stages {
		gnid := st.GateNodeID()
		if gnid == "" {
			continue // gate not yet stamped (plan not started) — nothing to surface.
		}
		n, nerr := s.orch.GetNode(ctx, orch.NodeID(gnid))
		if nerr != nil {
			return nerr
		}
		status := n.Status()
		gates = append(gates, StageGateView{
			NodeID:    gnid,
			StageID:   st.ID(),
			StageName: st.Name(),
			Status:    string(status),
			Pending:   status != orch.NodeCompleted && status != orch.NodeDiscarded,
		})
	}
	detail.Gates = gates
	return nil
}

// enrichStageView applies the stage-aware read corrections (barrier-held ready→blocked +
// gate surfacing) to a READ-facing PlanDetail. Shared by GetPlanDetail and
// GetPlanDetailForMember so both agent-facing get_plan paths tell the same stage-aware
// truth. Errors from the graph reads propagate (the caller returns them).
func (s *Service) enrichStageView(ctx context.Context, detail *PlanDetail) error {
	if err := s.applyStageBarrierView(ctx, detail); err != nil {
		return err
	}
	return s.fillStageGates(ctx, detail)
}
