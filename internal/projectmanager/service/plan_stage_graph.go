package service

import (
	"context"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// =============================================================================
// Plan Stage落图 + driver (2026-07-03 plan-stage-model design §4.2/§5). Stage is laid
// onto the SAME orchestration graph the plan already builds (§4.2 "不另起引擎") — no
// second engine, no rewritten execution. This file adds:
//
//   - buildStages: the落图. Per stage it creates ONE gate CONDITION node (upstream =
//     the stage's business nodes) and wires the outer stage DAG as barrier edges
//     (downstream stage entry depends_on the upstream stage's gate). It also enforces
//     the build-time structural invariants (§5): no cross-stage business edge, acyclic
//     outer stage DAG.
//   - ResolveStageGate: the driver. It REUSES the engine's condition machinery
//     (ResolveCondition / ApplyConditionResult / countReopens) — pass releases the
//     downstream stages; a bounded reject reopens THIS stage's sub-DAG (mirrored onto
//     the member tasks); an exhausted reject escalates to a human (§5 卡死升级). Stage
//     does NOT re-implement dispatch — the barrier/gate/retry are all engine primitives.
//
// The barrier itself needs NO driver code: a downstream stage entry depends_on the
// upstream gate CONDITION node, and the engine's ReadyNodes already refuses to release
// a node whose upstream condition is unresolved (T768). So "上游 stage 全完成 + gate
// pass 才放行" falls out of the existing ready-set computation for free.
// =============================================================================

// stageGateReopenLimitDefault mirrors pm.DefaultStageMaxRounds — the fallback bound
// when a gate node carries no max_rounds (defensive; NewStage already defaults it).
const stageGateReopenLimitDefault = pm.DefaultStageMaxRounds

// buildStages lays a plan's stages onto the orchestration graph (§4.2). It is called
// from buildPlanGraph AFTER the business nodes + intra-plan edges are created and
// BEFORE the Start/End anchoring, sharing buildPlanGraph's nodeOf map + addEdge helper
// (so the anchor step sees the stage edges' in/out degree). It returns the created gate
// node ids (anchored to End by the caller). No-op — returns (nil, nil) — when the stage
// repository is unwired or the plan has no stages (§8 zero-regression: a pure-node DAG
// is byte-identical to today).
func (s *Service) buildStages(
	txCtx context.Context, graphID orch.GraphID, p *pm.Plan, tasks []*pm.Task, edges []pm.Dependency,
	nodeOf map[pm.TaskID]orch.NodeID, addEdge func(from, to orch.NodeID) error, now time.Time,
) ([]orch.NodeID, error) {
	if s.stages == nil {
		return nil, nil
	}
	stages, err := s.stages.ListByPlan(txCtx, p.ID())
	if err != nil {
		return nil, err
	}
	if len(stages) == 0 {
		return nil, nil
	}
	stageOf := pm.StageOf(tasks)
	// Build-time invariants (§5): a manual cross-stage business edge bypasses the gate
	// barrier (报错), and the outer stage DAG must be acyclic + reference existing
	// sibling stages. Fail the plan start loudly — a mis-authored stage graph must not
	// silently lay down a barrier-bypassing edge.
	if verr := pm.ValidateStageEdges(stageOf, edges); verr != nil {
		return nil, verr
	}
	if verr := pm.ValidateStageDAG(stages); verr != nil {
		return nil, verr
	}
	// Author-time stage-coverage guard (§5, quick-fix 1a): this plan HAS stages (len>0
	// above), so every business node must belong to one. A stageless node would run ahead
	// of the staged flow (bypassing the gate/barrier). Reject the plan start loudly, naming
	// the orphan node(s). Only reached when the plan has ≥1 stage — a stage-less plan
	// returned above, so this never touches the zero-regression pure-node path.
	if verr := pm.ValidateStageMembership(tasks); verr != nil {
		return nil, verr
	}

	gateOf := make(map[pm.StageID]orch.NodeID, len(stages))
	entriesOf := make(map[pm.StageID][]pm.TaskID, len(stages))
	var gateIDs []orch.NodeID

	// Pass 1: one gate CONDITION node per stage; upstream = all its business nodes.
	for _, st := range stages {
		allMembers := pm.StageMembers(tasks, st.ID())
		members := make([]pm.TaskID, 0, len(allMembers))
		for _, member := range allMembers {
			if member != st.GateTaskID() {
				members = append(members, member)
			}
		}
		entries := pm.StageEntries(members, stageOf, st.ID(), edges)
		entriesOf[st.ID()] = entries

		// on_failure = the stage's ENTRY nodes: a gate reject reopens ReopenChain(gate →
		// entry) = every member from the entry down to the gate, i.e. this stage's whole
		// sub-DAG (§5 "reopen 本 stage 子 DAG"), and NOT any upstream stage (§2 决策5 — no
		// cross-stage rollback). max_rounds is the stage-local bounded-retry cap.
		onFailure := make([]any, 0, len(entries))
		for _, e := range entries {
			if nid, ok := nodeOf[e]; ok {
				onFailure = append(onFailure, string(nid))
			}
		}
		maxRounds := st.MaxRounds()
		if maxRounds <= 0 {
			maxRounds = stageGateReopenLimitDefault
		}
		gateMeta := map[string]any{
			"evaluator":     string(orch.EvaluatorManual),
			"stage_gate":    string(st.ID()),
			"max_rounds":    maxRounds,
			"condition_for": string(st.GateTaskID()),
			"pass_whens":    []any{"pass"},
		}
		if len(onFailure) > 0 {
			gateMeta["on_failure"] = onFailure
		}
		gateID, aerr := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryControl), string(orch.ControlKindCondition),
			"gate:"+st.Name(), gateMeta)
		if aerr != nil {
			return nil, aerr
		}
		gateOf[st.ID()] = gateID
		gateIDs = append(gateIDs, gateID)
		// Stamp the gate node id back onto the Stage aggregate (§4.1 gate_node_id).
		st.SetGateNodeID(string(gateID), now)
		if uerr := s.stages.Update(txCtx, st); uerr != nil {
			return nil, uerr
		}
		gateTaskNode, ok := nodeOf[st.GateTaskID()]
		if !ok {
			return nil, pm.ErrMissingGateEvaluator
		}
		// Ordinary members release the executable evaluator task. Its business
		// node then feeds the condition projection that owns pass/reject routing.
		for _, m := range members {
			mn, ok := nodeOf[m]
			if !ok {
				continue
			}
			if eerr := addEdge(mn, gateTaskNode); eerr != nil {
				return nil, eerr
			}
		}
		if eerr := addEdge(gateTaskNode, gateID); eerr != nil {
			return nil, eerr
		}
	}

	// Pass 2: the outer stage DAG barrier (§4.2). A downstream stage's ENTRY nodes
	// depend_on its upstream stages' GATE nodes — so a downstream stage's sub-DAG only
	// dispatches once every upstream stage is 全完成+gate pass (ReadyNodes gates the
	// entries behind the unresolved upstream gate condition).
	for _, st := range stages {
		for _, up := range st.DependsOnStages() {
			upGate, ok := gateOf[up]
			if !ok {
				continue // validated to exist by ValidateStageDAG; defensive.
			}
			for _, entry := range entriesOf[st.ID()] {
				en, ok := nodeOf[entry]
				if !ok {
					continue
				}
				if eerr := addEdge(upGate, en); eerr != nil {
					return nil, eerr
				}
			}
		}
	}
	return gateIDs, nil
}
