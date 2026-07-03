package service

import (
	"context"
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
//   - loopback edge {From:D, To:Dev, When:reject} → NOT added to the graph. The
//     reject/loopback re-run stays on the proven task-level applyLoopbacks driver
//     (reopen tasks + clear dispatch/outcome, bounded by MaxRounds), which
//     syncGraphToTasks then propagates onto the graph nodes. This keeps the bounded
//     loop + exhaustion/escalation semantics in ONE place.
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
		condID, aerr := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryControl), string(orch.ControlKindCondition),
			"decision:"+nodeTitle(t), map[string]any{
				"evaluator":     string(orch.EvaluatorManual),
				"condition_for": string(t.ID()),
				"pass_whens":    passWhens,
			})
		if aerr != nil {
			return aerr
		}
		condOf[t.ID()] = condID
		// node(D) → C_D (C_D gated behind the decision completing).
		if eerr := s.orch.AddEdge(txCtx, graphID, dNode, condID); eerr != nil {
			return eerr
		}
		// C_D → node(X) for each conditional target X (X gated behind the condition).
		for _, x := range condTargets {
			xNode, ok := nodeOf[x]
			if !ok {
				continue
			}
			if eerr := s.orch.AddEdge(txCtx, graphID, condID, xNode); eerr != nil {
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
		if eerr := s.orch.AddEdge(txCtx, graphID, from, to); eerr != nil {
			return eerr
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

// driveGraphConditions resolves each decision's condition node once the decision
// has recorded a pass-outcome (§2.3): for every condition node whose decision has
// an outcome ∈ its pass_whens and that is not yet resolved, ResolveCondition
// success — which completes the condition and (via the ReadyNodes condition-gate
// fix) releases its downstream conditional targets. A reject/non-pass outcome
// leaves the condition open (its downstream stays gated) — the bounded loopback
// re-run is handled by the task-level applyLoopbacks driver, which reopens the
// loop subgraph (cleared outcome included) so the decision re-runs and can pass a
// later round. Idempotent: an already-completed condition is skipped.
//
// Runs inside the graph dispatch, AFTER syncGraphToTasks (so the decision node is
// completed) and BEFORE graphReadySet (so a just-released target is dispatched in
// the same pass).
func (s *Service) driveGraphConditions(txCtx context.Context, p *pm.Plan, outcomes []pm.DecisionOutcome) error {
	graphID := orch.GraphID(p.GraphID())
	nodes, err := s.orch.ListNodes(txCtx, graphID)
	if err != nil {
		return err
	}
	outcomeOf := make(map[pm.TaskID]string, len(outcomes))
	for _, o := range outcomes {
		outcomeOf[o.TaskID] = o.Outcome
	}
	for _, n := range nodes {
		if n.ControlKind() != orch.ControlKindCondition {
			continue
		}
		if n.Status() == orch.NodeCompleted || n.Status() == orch.NodeDiscarded {
			continue // already resolved.
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
		if !metaHasWhen(meta["pass_whens"], outcome) {
			continue // a non-pass outcome → loopback (task-level driver), not a release.
		}
		if rerr := s.orch.ResolveCondition(txCtx, orch.NodeID(n.ID()), "success"); rerr != nil {
			return rerr
		}
	}
	return nil
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
		// A loopback reopened this task (task-level applyLoopbacks): propagate onto the
		// node so it re-enters the ready-set for another round. Only a completed node
		// can reopen (Completed→Reopen); an already open/reopen node is left as-is.
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
