package service

import (
	"context"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// =============================================================================
// T768 — graph-backed plan dispatch (new orchestration engine wired into prod).
//
// When the pm Service has an orchestration engine (s.orch != nil), StartPlan
// builds a graph mirroring the plan DAG (buildPlanGraph) and stamps the plan
// with its graph_id. From then on the plan is dispatched off the GRAPH: the
// ready-source switches from ComputePlanView to orchestration GetReadyNodes
// (graphReadySet), and plan-done is graph.IsAutoDone.
//
// The switch is plan.GraphID(): a plan WITH a graph_id uses the new engine; a
// plan WITHOUT one (every pre-T768 / in-flight plan, or a plan the engine chose
// not to graph) stays on the legacy plan-DAG path unchanged — the zero-regression
// fallback. Nothing here deletes or alters the legacy path.
//
// Graph node status is kept in lock-step with task status by syncGraphToTasks,
// run at the top of every graph dispatch. It is SELF-HEALING (idempotent, derives
// the node's target state purely from the task's current status), so a graph
// dispatch is correct no matter which caller triggered it (the orchestrator
// projector on a task event, the reconcile sweep, or a manual advance) and a
// missed event can never leave the graph permanently out of sync.
// =============================================================================

// graphDispatchEnabled reports whether this plan should be dispatched off the
// orchestration graph: the engine is wired AND the plan carries a graph_id.
func (s *Service) graphDispatchEnabled(p *pm.Plan) bool {
	return s.orch != nil && p.GraphID() != ""
}

// buildPlanGraph creates the orchestration graph for a freshly-started plan and
// wires it back onto the aggregates: one business node per task (task_id in the
// node metadata + Task.SetNodeID); one CONDITION control node per decision node
// gating that decision's conditional (pass) targets; a graph edge per seq
// dependency; then Plan.SetGraphID + StartGraph. It runs INSIDE StartPlan's tx
// (orch.Service uses the same reentrant RunInTx, so every graph write joins that
// tx → the plan and its graph commit atomically). Returns nil (no-op) when the
// engine is unwired.
//
// Control-flow mapping (aligned with the plan-DAG §2.2 edge kinds):
//   - seq edge {From depends_on To}         → orch edge node(To)→node(From).
//   - conditional edge {From:X, To:D, When:w} (X taken when decision D's outcome==w)
//     → route X through D's condition node: node(D)→C_D and C_D→node(X). C_D gates
//     X (the T768 ReadyNodes fix makes a condition dep block downstream until it is
//     resolved). driveGraphConditions resolves C_D to success once D records a
//     pass-outcome, releasing X.
//   - loopback edge {From:D, To:Dev, When:reject} → NOT added as a graph EDGE (it is a
//     back-edge, excluded from the acyclic graph), but ENCODED into D's condition node
//     metadata (on_failure = the loop-target node, max_rounds). T805 ③: the engine then
//     drives the bounded reject re-run (driveGraphDecisions → ResolveCondition("reject")
//     → ApplyConditionResult countReopens), and reopenLoopSubgraph mirrors the reopen
//     onto the tasks. Exhaustion escalation stays a plan-side shim (parity; ⑤ ports it).
//
// A task that is ALREADY terminal at plan-start (pre-done, §9.6) has its node
// advanced to match immediately, so graphReadySet/IsAutoDone see the true state.
func (s *Service) buildPlanGraph(txCtx context.Context, p *pm.Plan, tasks []*pm.Task, edges []pm.Dependency, now time.Time) error {
	if s.orch == nil {
		return nil
	}
	planID := string(p.ID())
	graphID, err := s.orch.CreateGraph(txCtx, planID)
	if err != nil {
		return err
	}
	// One business node per task; remember task→node so edges + the pre-done sync
	// can resolve node ids.
	nodeOf := make(map[pm.TaskID]orch.NodeID, len(tasks))
	for _, t := range tasks {
		nodeID, aerr := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryBusiness), "", nodeTitle(t), map[string]any{
			"task_id": string(t.ID()),
		})
		if aerr != nil {
			return aerr
		}
		nodeOf[t.ID()] = nodeID
		t.SetNodeID(string(nodeID), now)
		if uerr := s.tasks.Update(txCtx, t); uerr != nil {
			return uerr
		}
	}
	// Condition node per decision, gating that decision's conditional (pass) targets.
	// condOf[D] = the condition node interposed after decision D.
	condOf := make(map[pm.TaskID]orch.NodeID)
	// T800: track business-node in/out degree across every edge added below, so
	// after wiring we can connect the seeded Start/End control anchors — Start→each
	// ROOT business node (no incoming), each SINK business node (no outgoing)→End.
	// buildPlanGraph previously left Start/End as orphans (End had no incoming edge),
	// which the DAG renderer then mis-laid-out. Anchor control nodes are treated as
	// satisfied by ReadyNodes, so these anchor edges are non-breaking for dispatch.
	hasIncoming := make(map[orch.NodeID]bool)
	hasOutgoing := make(map[orch.NodeID]bool)
	addEdge := func(from, to orch.NodeID) error {
		if eerr := s.orch.AddEdge(txCtx, graphID, from, to); eerr != nil {
			return eerr
		}
		hasOutgoing[from] = true
		hasIncoming[to] = true
		return nil
	}
	for _, t := range tasks {
		if !pm.IsDecisionNode(edges, t.ID()) {
			continue
		}
		dNode := nodeOf[t.ID()]
		// pass_whens = the set of When labels on conditional edges pointing at D (the
		// outcomes that release a forward branch). condTargets = the gated targets.
		var passWhens []any
		seen := map[string]bool{}
		var condTargets []pm.TaskID
		for _, e := range edges {
			if pm.NormalizeEdgeKind(e.Kind) == pm.EdgeConditional && e.ToTaskID == t.ID() {
				condTargets = append(condTargets, e.FromTaskID)
				if !seen[e.When] {
					seen[e.When] = true
					passWhens = append(passWhens, e.When)
				}
			}
		}
		// T805 ③: encode the decision's loopback into the condition node so the engine's
		// ApplyConditionResult drives the bounded reject re-run (countReopens). on_failure
		// = the loop-target's node — ReopenChain(condition → loop-target) reopens exactly
		// the LoopbackResetSet business nodes; max_rounds = the loopback edge's MaxRounds.
		// The condition's max_rounds is the record; driveGraphDecisions owns the exhaustion
		// BOUNDARY (so the engine never settles the condition on exhaustion — which would
		// wrongly release the gated pass branch) and keeps the escalate/legacy-escape
		// shim for byte-for-byte parity. ⑤ ports that escalation into the engine.
		condMeta := map[string]any{
			"evaluator":     string(orch.EvaluatorManual),
			"condition_for": string(t.ID()),
			"pass_whens":    passWhens,
		}
		var onFailure []any
		maxRounds := 0
		for _, e := range edges {
			if e.IsLoopback() && e.FromTaskID == t.ID() {
				if tn, ok := nodeOf[e.ToTaskID]; ok {
					onFailure = append(onFailure, string(tn))
				}
				if e.MaxRounds > maxRounds {
					maxRounds = e.MaxRounds
				}
			}
		}
		if len(onFailure) > 0 {
			condMeta["on_failure"] = onFailure
			condMeta["max_rounds"] = maxRounds
		}
		condID, aerr := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryControl), string(orch.ControlKindCondition),
			"decision:"+nodeTitle(t), condMeta)
		if aerr != nil {
			return aerr
		}
		condOf[t.ID()] = condID
		// node(D) → C_D (C_D gated behind the decision completing).
		if eerr := addEdge(dNode, condID); eerr != nil {
			return eerr
		}
		// C_D → node(X) for each conditional target X (X gated behind the condition).
		for _, x := range condTargets {
			xNode, ok := nodeOf[x]
			if !ok {
				continue
			}
			if eerr := addEdge(condID, xNode); eerr != nil {
				return eerr
			}
		}
	}
	// Seq dependency edges: plan Dependency{From depends_on To} means From runs after
	// To. Orchestration Edge{From,To} means To depends on From. So the plan edge maps
	// to orch edge node(To)→node(From). Conditional edges are routed through the
	// condition node above; loopback edges stay on the task-level driver — both skipped.
	for _, e := range edges {
		if e.IsLoopback() || pm.NormalizeEdgeKind(e.Kind) == pm.EdgeConditional {
			continue
		}
		from, ok1 := nodeOf[e.ToTaskID]
		to, ok2 := nodeOf[e.FromTaskID]
		if !ok1 || !ok2 {
			continue // edge referencing a task not selected into the plan — skip defensively.
		}
		if eerr := addEdge(from, to); eerr != nil {
			return eerr
		}
	}
	// T800: wire the structural Start/End anchors so the graph has a single inlined
	// source and sink (fixes the DAG renderer laying End out as an orphan). Start →
	// every ROOT business node (no incoming graph edge); every SINK business node
	// (no outgoing) → End. A loopback dep is NOT a graph edge, so a loop target still
	// counts as a root and correctly gets a Start edge. Anchor edges are seq-kinded
	// (Start/End aren't condition nodes) and, per ReadyNodes, never gate dispatch.
	g, gerr := s.orch.GetGraph(txCtx, graphID)
	if gerr != nil {
		return gerr
	}
	startID, endID := g.StartNodeID(), g.EndNodeID()
	for _, t := range tasks {
		nid, ok := nodeOf[t.ID()]
		if !ok {
			continue
		}
		if !hasIncoming[nid] {
			if eerr := s.orch.AddEdge(txCtx, graphID, startID, nid); eerr != nil {
				return eerr
			}
		}
		if !hasOutgoing[nid] {
			if eerr := s.orch.AddEdge(txCtx, graphID, nid, endID); eerr != nil {
				return eerr
			}
		}
	}
	if serr := s.orch.StartGraph(txCtx, graphID); serr != nil {
		return serr
	}
	p.SetGraphID(string(graphID), now)
	if uerr := s.plans.Update(txCtx, p); uerr != nil {
		return uerr
	}
	// Pre-done tasks (§9.6): reflect their terminal state onto the freshly-built
	// nodes so the first dispatch's ready-set / IsAutoDone are accurate.
	return s.syncGraphToTasks(txCtx, p, tasks)
}

// driveGraphDecisions is the T805 ③ engine-driven decision/loopback driver for a
// graphed plan — it REPLACES the driveGraphConditions bridge (pass-only) AND the
// task-level applyLoopbacks (reject/loopback) so production drives BOTH through the
// orchestration engine, byte-for-byte with the legacy behaviour. For every condition
// node whose decision has recorded an outcome and that is not yet resolved:
//
//   - PASS (outcome ∈ pass_whens): ResolveCondition("success") — completes the
//     condition and (via the ReadyNodes condition-gate) releases its downstream
//     conditional targets.
//   - REJECT within bounds (outcome matches a loopback edge's When AND the
//     engine round countReopens+1 ≤ max_rounds): ResolveCondition("reject") drives
//     the engine's bounded reopen (ApplyConditionResult reopens the on_failure node
//     chain + the condition, bumping countReopens); reopenLoopSubgraph then mirrors
//     that onto the TASKS (Reopen + ClearDispatch + ClearDecisionOutcome over the
//     LoopbackResetSet) — the node↔task mapping the graph dispatch (task-keyed) needs
//     so the reopened decision re-runs and re-decides.
//   - EXHAUSTION (round > max_rounds): a plan-side shim reproduces the legacy
//     applyLoopbacks terminal exactly — record the "<outcome>_exhausted" decision
//     outcome, then either leave a HISTORICAL Escape vertex to pick it up
//     (hasLegacyEscapeEdge) or escalateExhaustion (@mention the owner). The condition
//     is left UNRESOLVED so the gated pass branch never releases (Integrate
//     auto-skips), matching the legacy exhaustion behaviour. ⑤ ports this escalation
//     into the engine's OnMaxExceeded and deletes the shim.
//
// The exhaustion boundary here mirrors the engine's own (countReopens+1 > max_rounds)
// so the driver never lets the engine reach OnMaxExceeded (which would discard/
// force-success the condition and wrongly release downstream). Idempotent: a resolved
// condition is skipped, and a decision already tagged "_exhausted" is skipped so the
// escalation fires exactly once (the legacy path was event-driven / once-per-event).
//
// Runs inside the graph dispatch, AFTER syncGraphToTasks and BEFORE graphReadySet, so
// a just-released target OR a just-reopened loop task is dispatched in the same pass.
func (s *Service) driveGraphDecisions(txCtx context.Context, p *pm.Plan, edges []pm.Dependency, outcomes []pm.DecisionOutcome) error {
	graphID := orch.GraphID(p.GraphID())
	nodes, err := s.orch.ListNodes(txCtx, graphID)
	if err != nil {
		return err
	}
	outcomeOf := make(map[pm.TaskID]string, len(outcomes))
	for _, o := range outcomes {
		outcomeOf[o.TaskID] = o.Outcome
	}
	now := s.clock.Now()
	for _, n := range nodes {
		if n.ControlKind() != orch.ControlKindCondition {
			continue
		}
		if n.Status() == orch.NodeCompleted || n.Status() == orch.NodeDiscarded {
			continue // already resolved (pass released).
		}
		meta := n.Metadata()
		decisionID, _ := meta["condition_for"].(string)
		if decisionID == "" {
			continue
		}
		outcome := outcomeOf[pm.TaskID(decisionID)]
		if outcome == "" {
			continue // decision not decided yet → keep the branch gated.
		}
		if strings.HasSuffix(outcome, exhaustedOutcomeSuffix) {
			continue // exhaustion already surfaced — do not re-escalate (idempotent).
		}
		// PASS: release the forward branch through the engine.
		if metaHasWhen(meta["pass_whens"], outcome) {
			if rerr := s.orch.ResolveCondition(txCtx, orch.NodeID(n.ID()), "success"); rerr != nil {
				return rerr
			}
			continue
		}
		// REJECT: only a loopback edge whose When matches this outcome fires a re-run
		// (parity with legacy applyLoopbacks, which keyed on the matching loopback edge).
		var lb *pm.Dependency
		for i := range edges {
			if edges[i].IsLoopback() && edges[i].FromTaskID == pm.TaskID(decisionID) && edges[i].When == outcome {
				lb = &edges[i]
				break
			}
		}
		if lb == nil {
			continue // non-pass outcome with no matching loopback → leave the branch gated.
		}
		// Bounded round via the engine's countReopens on the condition node.
		round := conditionReopenCount(n) + 1
		if lb.MaxRounds > 0 && round > lb.MaxRounds {
			// EXHAUSTION shim — byte-for-byte with legacy applyLoopbacks (§4 terminal).
			exhausted := outcome + exhaustedOutcomeSuffix
			if rerr := s.plans.RecordDecisionOutcome(txCtx, p.ID(), pm.TaskID(decisionID), exhausted, now); rerr != nil {
				return rerr
			}
			if hasLegacyEscapeEdge(edges, pm.TaskID(decisionID), exhausted) {
				continue // a historical Escape vertex surfaces it — do NOT also escalate.
			}
			dt, ferr := s.tasks.FindByID(txCtx, pm.TaskID(decisionID))
			if ferr != nil {
				return ferr
			}
			if eerr := s.escalateExhaustion(txCtx, p, dt); eerr != nil {
				return eerr
			}
			continue
		}
		// Within bounds: drive the engine reopen (bumps countReopens on the condition),
		// then mirror onto the tasks so the reopened loop subgraph re-dispatches.
		if rerr := s.orch.ResolveCondition(txCtx, orch.NodeID(n.ID()), "reject"); rerr != nil {
			return rerr
		}
		if rerr := s.reopenLoopSubgraph(txCtx, p.ID(), edges, lb.ToTaskID, lb.FromTaskID, now); rerr != nil {
			return rerr
		}
	}
	return nil
}

// exhaustedOutcomeSuffix tags a decision outcome whose bounded reject-loopback has
// exhausted (kept as the legacy value name for backward compat — see applyLoopbacks).
const exhaustedOutcomeSuffix = "_exhausted"

// conditionReopenCount counts how many times a condition node has been reopened —
// the engine's bounded-loopback counter (mirrors orchestration.countReopens, which
// is unexported). Each engine reject reopen appends a "reopened" action log.
func conditionReopenCount(n *orch.Node) int {
	count := 0
	for _, log := range n.ActionLogs() {
		if log.Action == "reopened" {
			count++
		}
	}
	return count
}

// metaHasWhen reports whether the JSON-round-tripped pass_whens metadata ([]any of
// string) contains the given outcome label.
func metaHasWhen(passWhens any, outcome string) bool {
	list, ok := passWhens.([]any)
	if !ok {
		return false
	}
	for _, w := range list {
		if s, ok := w.(string); ok && s == outcome {
			return true
		}
	}
	return false
}

// nodeTitle returns a non-empty node title (NewNode rejects a blank title). Falls
// back to the task id when the task has no title.
func nodeTitle(t *pm.Task) string {
	if title := t.Title(); title != "" {
		return title
	}
	return string(t.ID())
}

// syncGraphToTasks brings every bound graph node into lock-step with its task's
// current status (§9.2 derive-not-store, applied to the engine's own node store).
// Self-healing + idempotent: advanceNodeTo only applies the transitions still
// needed to reach the task's terminal/running state, so re-running is a no-op.
// Run at the top of graph dispatch so GetReadyNodes/IsAutoDone always reflect the
// live task states regardless of which caller triggered dispatch.
func (s *Service) syncGraphToTasks(txCtx context.Context, p *pm.Plan, tasks []*pm.Task) error {
	for _, t := range tasks {
		if t.NodeID() == "" {
			continue // task not mapped to a node (shouldn't happen for a graphed plan).
		}
		n, err := s.orch.GetNode(txCtx, orch.NodeID(t.NodeID()))
		if err != nil {
			return err
		}
		if err := s.advanceNodeTo(txCtx, n, t.Status()); err != nil {
			return err
		}
	}
	return nil
}

// advanceNodeTo drives one node through valid transitions until it matches the
// task's status (§9.2). A done task → node completed (outcome "success"); a failed
// task → node discarded; a running task → node running. An open/reopened task
// leaves the node as-is (it is not yet terminal). It tolerates a missed
// intermediate event: a node still `open` whose task jumped straight to completed
// is walked open→running→completed. It never moves a node backwards (a completed
// node whose task is somehow running is left alone — no illegal reverse).
func (s *Service) advanceNodeTo(txCtx context.Context, n *orch.Node, taskStatus pm.TaskStatus) error {
	switch {
	case pm.TaskIsDone(taskStatus):
		// Walk to completed: open/reopen → running → completed.
		if n.Status() == orch.NodeOpen || n.Status() == orch.NodeReopen {
			if err := s.orch.StartNode(txCtx, orch.NodeID(n.ID())); err != nil {
				return err
			}
		}
		if cur, err := s.reload(txCtx, n); err != nil {
			return err
		} else if cur.Status() == orch.NodeRunning {
			return s.orch.CompleteNode(txCtx, orch.NodeID(n.ID()), "success")
		}
		return nil
	case pm.TaskIsFailed(taskStatus):
		// §9.7 parity: a FAILED (discarded) task must BLOCK its downstream and keep the
		// plan running — NOT settle it. In the orchestration domain NodeDiscarded is a
		// SATISFIED-terminal state (ReadyNodes/IsAutoDone treat it like completed, for
		// pruned/skipped branches), so discarding the node would wrongly release
		// downstream and let the plan auto-complete. Instead we leave the node in its
		// current non-satisfying state (open/running): downstream stays blocked and
		// IsAutoDone stays false until a human reopens/re-runs the failed node
		// (RerunFailedNode), exactly as the legacy plan-DAG failure path behaves.
		return nil
	case taskStatus == pm.TaskRunning:
		if n.Status() == orch.NodeOpen || n.Status() == orch.NodeReopen {
			return s.orch.StartNode(txCtx, orch.NodeID(n.ID()))
		}
		return nil
	case taskStatus == pm.TaskReopened:
		// A loopback reopened this task (legacy task-level applyLoopbacks, or the T805 ③
		// engine-driven reopenLoopSubgraph): propagate onto the node so it re-enters the
		// ready-set for another round. Only a completed node can reopen (Completed→Reopen);
		// an already open/reopen node is left as-is.
		if n.Status() == orch.NodeCompleted {
			return s.orch.ReopenNode(txCtx, orch.NodeID(n.ID()), "loopback_reopen")
		}
		return nil
	default:
		// open (non-terminal, not running) → node stays open.
		return nil
	}
}

// reload re-fetches a node so a follow-up transition sees the status the previous
// transition just wrote (StartNode/CompleteNode persist, they don't mutate n).
func (s *Service) reload(txCtx context.Context, n *orch.Node) (*orch.Node, error) {
	return s.orch.GetNode(txCtx, orch.NodeID(n.ID()))
}

// graphReadySet returns the plan's ready task-ids off the orchestration graph: the
// GetReadyNodes business nodes (upstream all completed/discarded) whose task has
// no dispatch record yet (dispatch idempotency — a dispatched node is skipped just
// as ComputePlanView derives NodeDispatched). It also reports graph.IsAutoDone
// (every business node completed/discarded) as the plan-done signal.
//
// The caller MUST have synced the graph first (syncGraphToTasks).
func (s *Service) graphReadySet(txCtx context.Context, p *pm.Plan, records []pm.DispatchRecord) ([]pm.TaskID, bool, error) {
	graphID := orch.GraphID(p.GraphID())
	readyNodes, err := s.orch.GetReadyNodes(txCtx, graphID)
	if err != nil {
		return nil, false, err
	}
	dispatched := make(map[pm.TaskID]struct{}, len(records))
	for _, r := range records {
		dispatched[r.TaskID] = struct{}{}
	}
	var ready []pm.TaskID
	for _, n := range readyNodes {
		taskID := nodeTaskID(n)
		if taskID == "" {
			continue // unbound node (defensive) — nothing to dispatch.
		}
		if _, done := dispatched[taskID]; done {
			continue // already dispatched — idempotent skip.
		}
		ready = append(ready, taskID)
	}
	g, err := s.orch.GetGraph(txCtx, graphID)
	if err != nil {
		return nil, false, err
	}
	return ready, g.IsAutoDone(), nil
}

// nodeTaskID reads the bound task id from a node's metadata ("task_id").
func nodeTaskID(n *orch.Node) pm.TaskID {
	if v, ok := n.Metadata()["task_id"].(string); ok {
		return pm.TaskID(v)
	}
	return ""
}
