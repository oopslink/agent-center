package service

import (
	"context"
	"fmt"
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
// ready-source switches from DerivePlanView to orchestration GetReadyNodes
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
//     resolved). driveGraphDecisions resolves C_D to success once D records a
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
		nodeMeta := map[string]any{"task_id": string(t.ID())}
		// Stage落图 (2026-07-03 design §4.1/§4.2): a task's stage membership rides onto
		// its graph node as metadata.stage_id, so the node is groupable by stage for the
		// gate/barrier wiring (buildStages) and the status projection (get_stage) without
		// the generic engine needing a first-class stage concept. "" (stageless) omits it.
		if sid := t.StageID(); sid != "" {
			nodeMeta["stage_id"] = string(sid)
		}
		nodeID, aerr := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryBusiness), "", nodeTitle(t), nodeMeta)
		if aerr != nil {
			return aerr
		}
		nodeOf[t.ID()] = nodeID
		t.SetNodeID(string(nodeID), now)
		// issue-d2f14e0e (P0) PRODUCER: auto-stamp TagMergeToMain onto a ship /
		// merge-to-main node so the run-gate consumer (acceptanceVerdictBlocks) hard-gates
		// it on a passed acceptance verdict — WITHOUT relying on the author to tag it. This
		// closes the "consumer wired, source not provisioned" gap: b5ddb42e added the
		// consumer but nothing produced the tag, leaving the P0 gate opt-in / inert in
		// production. Detection (pm.RequiresAcceptance) is fail-closed (explicit marker tag
		// OR a tight "merge → main" title phrase); the stamp is idempotent (HasTag guard)
		// and persisted in the same task Update that records the node id below.
		if pm.RequiresAcceptance(t) && !pm.HasTag(t.Tags(), pm.TagMergeToMain) {
			if terr := t.SetTags(append(t.Tags(), pm.TagMergeToMain), now); terr != nil {
				return fmt.Errorf("stamp %s gate tag on ship node %s: %w", pm.TagMergeToMain, t.ID(), terr)
			}
		}
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
	// Stage落图 (2026-07-03 design §4.2): group the graph nodes by stage_id, create a
	// gate CONDITION node per stage (upstream = the stage's business nodes), and wire the
	// outer stage DAG as barrier edges (downstream stage entry depends_on the upstream
	// stage's gate). No-op when the plan has no stages (§8 zero-regression). It reuses the
	// same addEdge helper so the Start/End anchoring below sees the stage edges' in/out
	// degree. gateIDs are the created gate nodes, anchored to End below.
	gateIDs, serr := s.buildStages(txCtx, graphID, p, tasks, edges, nodeOf, addEdge, now)
	if serr != nil {
		return serr
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
	// Anchor each terminal-stage gate (no outgoing barrier edge) to End so the renderer
	// has a single sink even when a stage gate is the graph's true tail (parity with the
	// business-sink anchoring above; gates are control nodes, excluded from IsAutoDone).
	for _, gate := range gateIDs {
		if !hasOutgoing[gate] {
			if eerr := s.orch.AddEdge(txCtx, graphID, gate, endID); eerr != nil {
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
//   - EXHAUSTION (round > max_rounds): record the "<outcome>_exhausted" decision
//     outcome and escalateExhaustion (@mention the owner). The condition is left
//     UNRESOLVED so the gated pass branch never releases (Integrate auto-skips).
//     escalate lives in the SERVICE layer — the pure orchestration engine has no
//     plan/conversation knowledge, so the driver (not the engine's OnMaxExceeded) owns
//     the boundary + escalation (T810 ⑤ "纯引擎 + 服务层驱动" layering).
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
		round := conditionLoopRound(n) + 1
		if lb.MaxRounds > 0 && round > lb.MaxRounds {
			// EXHAUSTION terminal (§4): record the "<outcome>_exhausted" outcome, leave the
			// condition UNRESOLVED so the gated pass branch never releases (Integrate
			// auto-skips), and escalate to the owner. T810 ⑤: the pre-v2.23.0 legacy-escape
			// double-track (hasLegacyEscapeEdge) was removed — escape is a Decision terminal
			// since v2.23.0, the current authoring (add_plan_dependency) never emits an
			// `_exhausted` conditional in-edge, and in-flight legacy plans are out of scope
			// ("全删不留尾巴") — so that branch was dead and always escalated.
			exhausted := outcome + exhaustedOutcomeSuffix
			if rerr := s.plans.RecordDecisionOutcome(txCtx, p.ID(), pm.TaskID(decisionID), exhausted, now); rerr != nil {
				return rerr
			}
			dt, ferr := s.tasks.FindByID(txCtx, pm.TaskID(decisionID))
			if ferr != nil {
				return ferr
			}
			// Ledger: the engine-driven terminal gate result (bounded loopback exhausted).
			s.auditPlanByID(txCtx, p.ProjectID(), p.ID(), pm.AuditPlanDecisionOutcome, pm.SystemActor("plan-engine"), map[string]any{
				"decision_id": decisionID,
				"decision":    dt.Title(),
				"outcome":     exhausted,
				"exhausted":   true,
			})
			if eerr := s.escalateExhaustion(txCtx, p, dt); eerr != nil {
				return eerr
			}
			continue
		}
		// Within bounds: preserve the completed round and materialize a fresh task/node
		// chain for the next round. Historical tasks remain terminal and auditable.
		if rerr := s.forkLoopSubgraph(txCtx, p, n, edges, lb.ToTaskID, lb.FromTaskID, round, now); rerr != nil {
			return rerr
		}
		// Ledger: the engine-driven bounded loopback re-run (design §4.2/§5 — the reject
		// that reopened the subgraph). System actor; best-effort via recordChange.
		s.auditPlanByID(txCtx, p.ProjectID(), p.ID(), pm.AuditPlanLoopback, pm.SystemActor("plan-engine"), map[string]any{
			"decision_id": decisionID,
			"outcome":     outcome,
			"round":       round,
			"from":        string(lb.FromTaskID),
			"to":          string(lb.ToTaskID),
		})
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

func conditionLoopRound(n *orch.Node) int {
	var base int
	switch v := n.Metadata()["loop_round_base"].(type) {
	case int:
		base = v
	case float64:
		base = int(v)
	}
	return base + conditionReopenCount(n)
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
	// Load the plan's dispatch records once so advanceNodeTo can tell a genuinely-running
	// node (dispatched → started) from a RECONCILED node awaiting re-dispatch (issue-77d9beff
	// ①: reopenStuckPlanNode reopened it + cleared its record while its task stays running —
	// see the TaskRunning branch of advanceNodeTo).
	records, rerr := s.plans.ListDispatchRecords(txCtx, p.ID())
	if rerr != nil {
		return rerr
	}
	dispatched := make(map[pm.TaskID]struct{}, len(records))
	for _, r := range records {
		dispatched[r.TaskID] = struct{}{}
	}
	for _, t := range tasks {
		if t.NodeID() == "" {
			continue // task not mapped to a node (shouldn't happen for a graphed plan).
		}
		n, err := s.orch.GetNode(txCtx, orch.NodeID(t.NodeID()))
		if err != nil {
			return err
		}
		_, isDispatched := dispatched[t.ID()]
		if err := s.advanceNodeTo(txCtx, n, t.Status(), isDispatched); err != nil {
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
func (s *Service) advanceNodeTo(txCtx context.Context, n *orch.Node, taskStatus pm.TaskStatus, dispatched bool) error {
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
		if n.Status() == orch.NodeOpen {
			return s.orch.StartNode(txCtx, orch.NodeID(n.ID()))
		}
		// A REOPEN node is started only once its task has actually been (re-)dispatched
		// (issue-77d9beff ①). A reopened loop task re-dispatches (RecordDispatch) BEFORE its
		// agent re-claims + StartTask, so by the time it is Running it carries a record →
		// start it. But reopenStuckPlanNode reopens a node whose task NEVER left running
		// (unblock recovery) and CLEARS its record so graphReadySet re-dispatches it — that
		// node must stay Reopen through this sync (dispatched=false), else this StartNode
		// would drive it straight back to Running and re-wedge it out of the ready-set.
		if n.Status() == orch.NodeReopen && dispatched {
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
		// open → node stays open.
		//
		// ADR-0054: PARKED statuses land here, and doing NOTHING is the correct sync.
		// The node keeps whatever state it already had (Running, since the task was
		// running before it parked), which keeps downstream blocked and IsAutoDone false.
		// The node is not driven backwards to open because it is not startable. Recovery
		// flows in through the task: unblock returns it to running, and
		// reopenStuckPlanNode re-arms the node for re-dispatch.
		return nil
	}
}

// reload re-fetches a node so a follow-up transition sees the status the previous
// transition just wrote (StartNode/CompleteNode persist, they don't mutate n).
func (s *Service) reload(txCtx context.Context, n *orch.Node) (*orch.Node, error) {
	return s.orch.GetNode(txCtx, orch.NodeID(n.ID()))
}

// reopenStuckPlanNode is the SHARED self-heal for issue-77d9beff ① — the unified root-cause
// fix for a STRUCTURED (graphed, non-builtin) plan node wedged Running while its executor is
// gone. A wedged node never re-enters graphReadySet (GetReadyNodes surfaces only open/reopen
// nodes) and its dispatch record keeps it out of the ready-set even after reopen, so the
// stage/plan can never re-dispatch or converge. This reopens the node (Running→NodeReopen)
// and clears its dispatch record so the NEXT dispatch (dispatchReadyNodes) re-surfaces and
// re-dispatches it.
//
// Call it from every recovery entrypoint that leaves the node's executor dead but the node
// stuck Running:
//   - ResetTask (tier-3 recovery: task running→open, assignee cleared), and
//   - UnblockTask (blocked→resume: the block terminated the prior WorkItem; task stays
//     running with no live lease).
//
// It is a no-op (idempotent, safe under replay / double-trigger) for: a task with no plan,
// a built-in pool plan (pool re-dispatch IS the auto-assign path — unchanged), an ungraphed
// plan, an unmapped task, or a node that is not currently Running.
func (s *Service) reopenStuckPlanNode(txCtx context.Context, t *pm.Task, reason string) error {
	pid := t.PlanID()
	if pid == "" {
		return nil // pure backlog task — no graph node to reconcile.
	}
	p, err := s.plans.FindByID(txCtx, pid)
	if err != nil {
		return err
	}
	// Built-in pool: dead-executor recovery is the auto-assign re-dispatch (the record and
	// pool membership are kept ON PURPOSE) — leave it untouched. Ungraphed plans have no node.
	if p.IsBuiltin() || p.GraphID() == "" {
		return nil
	}
	if t.NodeID() == "" {
		return nil // task not mapped to a node (shouldn't happen for a graphed plan).
	}
	n, err := s.orch.GetNode(txCtx, orch.NodeID(t.NodeID()))
	if err != nil {
		return err
	}
	if n.Status() == orch.NodeRunning {
		if rerr := s.orch.ReopenNode(txCtx, orch.NodeID(n.ID()), reason); rerr != nil {
			return rerr
		}
	}
	// Clear the dispatch record even if the node was not Running (e.g. already reopened by a
	// prior trigger): graphReadySet skips a node that still has a record, so the record MUST
	// be gone for the reopened node to re-enter the ready-set. ClearDispatch is a delete —
	// idempotent when the record is already absent.
	return s.plans.ClearDispatch(txCtx, pid, t.ID())
}

// graphReadySet returns the plan's ready task-ids off the orchestration graph: the
// GetReadyNodes business nodes (upstream all completed/discarded) whose task has
// no dispatch record yet (dispatch idempotency — a dispatched node is skipped just
// as DerivePlanView derives NodeDispatched). It also reports graph.IsAutoDone
// (every business node completed/discarded) as the plan-done signal.
//
// The caller MUST have synced the graph first (syncGraphToTasks).
func (s *Service) graphReadySet(txCtx context.Context, p *pm.Plan, tasks []*pm.Task, records []pm.DispatchRecord) ([]pm.TaskID, bool, error) {
	graphID := orch.GraphID(p.GraphID())
	readyNodes, err := s.orch.GetReadyNodes(txCtx, graphID)
	if err != nil {
		return nil, false, err
	}
	dispatched := make(map[pm.TaskID]struct{}, len(records))
	for _, r := range records {
		dispatched[r.TaskID] = struct{}{}
	}
	// A FAILED (discarded) task is never ready — advanceNodeTo deliberately leaves its
	// node OPEN (§9.7: NodeDiscarded is satisfied-terminal, which would wrongly release
	// downstream), so GetReadyNodes can surface a failed ROOT (no unsatisfied upstream).
	// Exclude it here so a failed task is not (re)dispatched — matching DerivePlanView,
	// which derives NodeFailed from task status directly (never NodeReady). Production
	// relies on the dispatch-record skip for this, but a task discarded BEFORE dispatch
	// (no record) would otherwise re-enter the ready-set.
	failed := make(map[pm.TaskID]bool, len(tasks))
	for _, t := range tasks {
		if pm.TaskIsFailed(t.Status()) {
			failed[t.ID()] = true
		}
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
		if failed[taskID] {
			continue // failed task is terminal-failed, never ready (§9.7 parity).
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

// =============================================================================
// T810 ⑤ — new-engine service-layer driver support (relocated from the deleted
// plan_controlflow.go). These are NOT the old control-flow engine (applyLoopbacks/
// SetDecisionOutcome-as-driver were deleted): they are the graph-era driver's own
// plan-layer helpers — the decision-outcome INPUT the engine consumes, the node↔task
// reopen MIRROR the graph dispatch needs, and the exhaustion escalation the engine
// (a pure graph domain) cannot own. driveGraphDecisions above calls them.
// =============================================================================

// RecordDecisionOutcome records (latest-wins) a decision node's outcome — the INPUT
// the new engine consumes: driveGraphDecisions reads it at dispatch (pass releases the
// forward branch via ResolveCondition; reject drives the bounded loopback), and the
// reader DerivePlanView reads it for skipped/blocked routing. The complete_task handler
// records it in the SAME tx as the completion so the subsequent auto-advance sees it.
// (T810 ⑤: renamed from the old SetDecisionOutcome — it is the engine's decision input,
// not the deleted old-engine driver.) The actor must be a project member; the task must
// belong to a plan.
func (s *Service) RecordDecisionOutcome(ctx context.Context, taskID pm.TaskID, outcome string, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if t.PlanID() == "" {
			return fmt.Errorf("projectmanager: task %s is not in a plan — no decision outcome to record", taskID)
		}
		if rerr := s.plans.RecordDecisionOutcome(txCtx, t.PlanID(), taskID, outcome, now); rerr != nil {
			return rerr
		}
		// Ledger: the human's gate ruling (design §4.2/§5 — the '事后查不到 gate 判了什么'
		// case §1 motivates). actor is the real member; best-effort via recordChange.
		s.auditPlanByID(txCtx, t.ProjectID(), t.PlanID(), pm.AuditPlanDecisionOutcome, actor, map[string]any{
			"decision_id": string(taskID),
			"decision":    t.Title(),
			"outcome":     outcome,
		})
		return nil
	})
}

// forkLoopSubgraph materializes the next bounded-loopback round as fresh tasks and
// graph nodes. It deliberately leaves every prior-round task completed, preserving
// its immutable lifecycle and conversation audit trail.
func (s *Service) forkLoopSubgraph(txCtx context.Context, p *pm.Plan, condition *orch.Node, edges []pm.Dependency, to, from pm.TaskID, round int, now time.Time) error {
	reset := pm.LoopbackResetSet(edges, to, from)
	cloned := make(map[pm.TaskID]pm.TaskID, len(reset))
	nodeOf := make(map[pm.TaskID]orch.NodeID, len(reset))
	graphID := orch.GraphID(p.GraphID())
	for _, id := range reset {
		original, err := s.tasks.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		cloneID, err := s.CreateTask(txCtx, CreateTaskCommand{
			ProjectID: original.ProjectID(), Title: loopRoundTitle(original.Title(), round+1),
			Description: original.Description(), DerivedFromIssue: original.DerivedFromIssue(), CreatedBy: original.CreatedBy(),
			Assignee: original.Assignee(), Model: original.Model(), DispatchMode: original.DispatchMode(), RequiredCapabilities: original.RequiredCapabilities(),
		})
		if err != nil {
			return err
		}
		clone, err := s.tasks.FindByID(txCtx, cloneID)
		if err != nil {
			return err
		}
		if err := clone.SetPlan(p.ID(), now); err != nil {
			return err
		}
		if original.StageID() != "" {
			if err := clone.SetStage(original.StageID(), now); err != nil {
				return err
			}
		}
		nodeID, err := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryBusiness), "", nodeTitle(clone), map[string]any{"task_id": string(cloneID), "loop_round": round + 1})
		if err != nil {
			return err
		}
		clone.SetNodeID(string(nodeID), now)
		if err := s.tasks.Update(txCtx, clone); err != nil {
			return err
		}
		cloned[id], nodeOf[id] = cloneID, nodeID
	}

	graphEdges, err := s.orch.ListEdges(txCtx, graphID)
	if err != nil {
		return err
	}
	oldTargets := make([]orch.NodeID, 0)
	for _, edge := range graphEdges {
		if edge.FromNodeID == orch.NodeID(condition.ID()) {
			oldTargets = append(oldTargets, edge.ToNodeID)
			if err := s.orch.RemoveEdge(txCtx, graphID, edge.FromNodeID, edge.ToNodeID); err != nil {
				return err
			}
		}
	}
	// With its forward gates detached, success safely settles the historical
	// condition without releasing the pass branch or reopening old business nodes.
	if err := s.orch.ResolveCondition(txCtx, orch.NodeID(condition.ID()), "success"); err != nil {
		return err
	}

	// Insert forward ancestry before control edges: AddDependency validates a
	// loopback against the already-persisted forward path.
	for pass := 0; pass < 2; pass++ {
		for _, edge := range edges {
			originalFrom, originalTo := edge.FromTaskID, edge.ToTaskID
			newFrom, fromOK := cloned[originalFrom]
			newTo, toOK := cloned[originalTo]
			if !fromOK && !toOK {
				continue
			}
			control := edge.IsLoopback() || pm.NormalizeEdgeKind(edge.Kind) == pm.EdgeConditional
			if (pass == 0) == control {
				continue
			}
			if fromOK {
				edge.FromTaskID = newFrom
			}
			if toOK {
				edge.ToTaskID = newTo
			}
			if err := s.plans.AddDependency(txCtx, edge); err != nil {
				return err
			}
			if fromOK && toOK && !control {
				if err := s.orch.AddEdge(txCtx, graphID, nodeOf[originalTo], nodeOf[originalFrom]); err != nil {
					return err
				}
			}
		}
	}

	newDecision := cloned[from]
	decisionNode := nodeOf[from]
	newTarget := nodeOf[to]
	meta := condition.Metadata()
	meta["condition_for"] = string(newDecision)
	meta["on_failure"] = []any{string(newTarget)}
	meta["loop_round_base"] = round
	condID, err := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryControl), string(orch.ControlKindCondition), fmt.Sprintf("decision:round-%d", round+1), meta)
	if err != nil {
		return err
	}
	if err := s.orch.AddEdge(txCtx, graphID, decisionNode, condID); err != nil {
		return err
	}
	for _, target := range oldTargets {
		if err := s.orch.AddEdge(txCtx, graphID, condID, target); err != nil {
			return err
		}
	}
	g, err := s.orch.GetGraph(txCtx, graphID)
	if err != nil {
		return err
	}
	if err := s.orch.AddEdge(txCtx, graphID, g.StartNodeID(), newTarget); err != nil {
		return err
	}
	return nil
}

func loopRoundTitle(title string, round int) string {
	if i := strings.LastIndex(title, " (round "); i >= 0 && strings.HasSuffix(title, ")") {
		var prior int
		if _, err := fmt.Sscanf(title[i:], " (round %d)", &prior); err == nil {
			title = title[:i]
		}
	}
	return fmt.Sprintf("%s (round %d)", title, round)
}

// escalateExhaustion is the bounded-loopback terminal handler: when a cycle's Decision
// loopback exhausts, escape is not a graph vertex (v2.23.0), so the driver surfaces the
// exhaustion by @mentioning the Decision's owner (or, absent one, the plan creator) in
// the plan conversation — the human then rules. It deliberately does NOT BlockTask
// anything and leaves has_failed UNSET (an exhausted loop awaiting a human ruling is not
// a failure). It lives in the SERVICE layer (not the pure orchestration engine, which
// has no plan/conversation knowledge): driveGraphDecisions calls it once on exhaustion.
// Best-effort: a nil dispatcher is a no-op.
func (s *Service) escalateExhaustion(txCtx context.Context, plan *pm.Plan, decision *pm.Task) error {
	if s.planDispatcher == nil {
		return nil
	}
	target := string(decision.Assignee())
	if target == "" {
		target = string(plan.CreatorRef())
	}
	content := fmt.Sprintf("decision %q exhausted its review-reject loopback rounds (escalated). The feature will NOT auto-integrate — please rule: reopen for another round, re-decide the outcome, or abandon the feature.", decision.Title())
	_, err := s.planDispatcher.PostMention(txCtx, plan.ConversationID(), target, content)
	return err
}

// =============================================================================
// I103 §1/§3 — BlockedOn materialization (deterministic-resume PRE-BASE task).
//
// materializeBlockedOn is the reconcile-sweep step that keeps a旁路 OBSERVATIONAL
// BlockedOn snapshot in lock-step with each non-terminal plan node: for every node
// it CLASSIFIES why the node is not making terminal progress and either refreshes a
// single-slot BlockedOn (persisted latest-wins) or clears it once the node enters
// ready/running/terminal.
//
// It is PURE OBSERVATION — it reads the SAME derived view / gate predicates the
// dispatcher and run-gate use and writes ONLY to the separate pm_plan_blocked_on
// store. It changes NO gating/readiness semantics (the T1041 acceptance hard gate,
// the reject gate, the stage barrier stay authoritative). It classifies + persists and
// (I103 §2) ASSIGNS each node's deadline + on_timeout from the deadline policy (data
// only). It does NOT ACT on those deadlines (the router routeTimeouts does, after this),
// run a human-decision queue, subscribe to external events, or detect a dead executor —
// those are downstream I103 concerns that CONSUME this snapshot.
//
// IDEMPOTENT: re-running a sweep over an unchanged plan produces no churn — an
// unchanged classification re-writes the same row (waited_since preserved while the
// wait_type is unchanged, so the assigned deadline never drifts; the router-owned probe
// fields preserved).
//
// Scope: structured, graphed plans only. A builtin pool is FLAT (no upstream, no
// gates) and an ungraphed plan predates the engine — both skip (nothing to classify);
// a skip is safe because the snapshot is observational.
func (s *Service) materializeBlockedOn(txCtx context.Context, p *pm.Plan) error {
	if s.orch == nil || p.IsBuiltin() || p.GraphID() == "" {
		return nil
	}
	planID := p.ID()
	tasks, err := s.tasks.ListByPlan(txCtx, planID)
	if err != nil {
		return err
	}
	edges, err := s.plans.ListDependencies(txCtx, planID)
	if err != nil {
		return err
	}
	records, err := s.plans.ListDispatchRecords(txCtx, planID)
	if err != nil {
		return err
	}
	outcomes, err := s.plans.ListDecisionOutcomes(txCtx, planID)
	if err != nil {
		return err
	}
	paused, err := s.pausedSet(txCtx, tasks)
	if err != nil {
		return err
	}
	// The derived view is the SAME read model the dispatcher/run-gate consult, so the
	// classification agrees with the engine's own notion of blocked/ready/running.
	view := pm.DerivePlanView(tasks, edges, records, outcomes, paused)
	taskByID := make(map[pm.TaskID]*pm.Task, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID()] = t
	}
	hasOutcome := make(map[pm.TaskID]bool, len(outcomes))
	for _, o := range outcomes {
		hasOutcome[o.TaskID] = true
	}
	// nodeStatusByID lets the upstream-completion classifier tell a satisfied upstream
	// (done/skipped) from one still being waited on.
	nodeStatusByID := make(map[pm.TaskID]pm.NodeStatus, len(view.Nodes))
	for _, n := range view.Nodes {
		nodeStatusByID[n.TaskID] = n.NodeStatus
	}
	now := s.clock.Now()
	for _, n := range view.Nodes {
		t := taskByID[n.TaskID]
		if t == nil {
			continue // node without a loaded task (defensive) — nothing to classify.
		}
		cls, clear, cerr := s.classifyBlockedOn(txCtx, p, t, n, edges, hasOutcome, nodeStatusByID)
		if cerr != nil {
			return cerr
		}
		if clear {
			if err := s.plans.ClearBlockedOn(txCtx, planID, n.TaskID); err != nil {
				return err
			}
			continue
		}
		b := pm.BlockedOn{
			NodeID:           t.NodeID(),
			TaskID:           n.TaskID,
			PlanID:           planID,
			WaitType:         cls.waitType,
			WaitKeys:         cls.waitKeys,
			TriggerCondition: cls.trigger,
			WaitedSince:      now,
		}
		// Refresh-preserve: for an ONGOING wait of the SAME kind, keep waited_since (the
		// 基准 the deadline is measured from — so the deadline never drifts across sweeps)
		// and carry the router-owned probe history (last_probe_at / probe_count) forward so
		// the sweep never clobbers it. A wait_type CHANGE is a genuinely new wait —
		// waited_since resets to now (above) and the stale probe history is dropped.
		if prev, ok, gerr := s.plans.GetBlockedOn(txCtx, planID, n.TaskID); gerr != nil {
			return gerr
		} else if ok && prev.WaitType == cls.waitType {
			b.WaitedSince = prev.WaitedSince
			b.LastProbeAt = prev.LastProbeAt
			b.ProbeCount = prev.ProbeCount
		}
		// I103 §2: (re)ASSIGN the deadline + on_timeout action from the policy, measured
		// from waited_since. Because waited_since is preserved for an unchanged wait, the
		// deadline recomputes to the SAME instant every sweep (idempotent, no drift). A zero
		// (inert) policy assigns none — Deadline stays zero, the router is a no-op. Assigning
		// is data-only; the router (routeTimeouts) is what ACTS when the deadline elapses.
		if dl, action, ok := s.deadlinePolicy.DeadlineFor(cls.waitType, b.WaitedSince); ok {
			b.Deadline = dl
			b.OnTimeout = string(action)
		}
		if err := s.plans.UpsertBlockedOn(txCtx, b); err != nil {
			return err
		}
	}
	return nil
}

// blockedOnClass is the classifier result for one node: the wait_type plus its
// wait_keys and a human-readable release condition.
type blockedOnClass struct {
	waitType pm.WaitType
	waitKeys []string
	trigger  string
}

// classifyBlockedOn derives WHY one plan node is not making terminal progress (I103
// §3). It returns clear=true when the node is settled or genuinely runnable (nothing
// to record). It reuses the engine's OWN gate predicates (acceptanceVerdictBlocks /
// stageGateBlocks) so the recorded reason matches what EnsureTaskRunnable would
// actually reject on. Priority order (most-specific first):
//
//  1. terminal (done/failed/skipped)        → clear
//  2. running/paused (holds an exec lease)   → executor_liveness (marker; detection downstream)
//  3. acceptance hard gate holds it          → acceptance_verdict
//  4. stage barrier holds it                 → stage_barrier
//  5. it IS a pending decision/review node   → human_decision
//  6. blocked behind an unresolved decision  → human_decision
//  7. blocked on unfinished upstream deps    → upstream_completion
//  8. ready/dispatched, no gate (runnable)   → clear (awaiting agent pickup, not blocked)
//  9. fallback (unknown non-terminal state)  → timeout_only
//
// external_event is intentionally NOT derived here — no current graph state maps to
// it (external_event subscription is a downstream I103 task); the enum reserves it.
func (s *Service) classifyBlockedOn(ctx context.Context, p *pm.Plan, t *pm.Task, n pm.PlanNodeView, edges []pm.Dependency, hasOutcome map[pm.TaskID]bool, nodeStatusByID map[pm.TaskID]pm.NodeStatus) (blockedOnClass, bool, error) {
	switch n.NodeStatus {
	case pm.NodeDone, pm.NodeFailed, pm.NodeSkipped:
		return blockedOnClass{}, true, nil // settled — clear any snapshot.
	case pm.NodeRunning, pm.NodePaused:
		// In motion holding an execution lease → a marker so the downstream executor-
		// liveness detector/takeover has a record to probe. Detection is NOT this task.
		var keys []string
		if a := strings.TrimSpace(string(t.Assignee())); a != "" {
			keys = []string{a}
		}
		return blockedOnClass{pm.WaitExecutorLiveness, keys, "the executor holding the lease stays alive"}, false, nil
	}
	// Non-terminal, non-running (ready / dispatched / blocked). Gate-aware checks first:
	// a node deriving `ready` off the stage-UNAWARE view may still be gate-held.
	if blocked, err := s.acceptanceVerdictBlocks(ctx, p, t); err != nil {
		return blockedOnClass{}, false, err
	} else if blocked {
		keys, kerr := s.upstreamConditionNodeIDs(ctx, p, t, false)
		if kerr != nil {
			return blockedOnClass{}, false, kerr
		}
		return blockedOnClass{pm.WaitAcceptanceVerdict, keys, "an upstream acceptance/decision gate passes"}, false, nil
	}
	if blocked, err := s.stageGateBlocks(ctx, p, t); err != nil {
		return blockedOnClass{}, false, err
	} else if blocked {
		keys, kerr := s.upstreamConditionNodeIDs(ctx, p, t, true)
		if kerr != nil {
			return blockedOnClass{}, false, kerr
		}
		return blockedOnClass{pm.WaitStageBarrier, keys, "the upstream stage gate resolves"}, false, nil
	}
	// human_decision: this node IS a pending decision/review (no outcome recorded yet).
	if pm.IsDecisionNode(edges, t.ID()) && !hasOutcome[t.ID()] {
		return blockedOnClass{pm.WaitHumanDecision, []string{string(t.ID())}, "a human records the decision outcome"}, false, nil
	}
	if n.NodeStatus == pm.NodeBlocked {
		var pendingDecisions, unmet []string
		for _, e := range edges {
			if e.FromTaskID != t.ID() || e.IsLoopback() {
				continue // only forward upstream deps of t (t depends_on e.ToTaskID).
			}
			up := e.ToTaskID
			switch nodeStatusByID[up] {
			case pm.NodeDone, pm.NodeSkipped:
				continue // satisfied — not waited on.
			}
			unmet = append(unmet, string(up))
			if pm.IsDecisionNode(edges, up) && !hasOutcome[up] {
				pendingDecisions = append(pendingDecisions, string(up))
			}
		}
		// Blocked behind an unresolved upstream decision → the ruling is the real wait.
		if len(pendingDecisions) > 0 {
			return blockedOnClass{pm.WaitHumanDecision, pendingDecisions, "a human records the upstream decision outcome"}, false, nil
		}
		return blockedOnClass{pm.WaitUpstreamCompletion, unmet, "all upstream dependencies complete"}, false, nil
	}
	if n.NodeStatus == pm.NodeReady || n.NodeStatus == pm.NodeDispatched {
		// Deps satisfied, no gate held — the node is runnable, just awaiting the agent
		// to pull it. NOT blocked → clear.
		return blockedOnClass{}, true, nil
	}
	// Defensive fallback: a non-terminal state we did not specifically classify. Only a
	// deadline can release it.
	return blockedOnClass{pm.WaitTimeoutOnly, nil, "a deadline elapses"}, false, nil
}

// upstreamConditionNodeIDs returns the ids of the CONDITION control nodes directly
// upstream of t's graph node (optionally only STAGE-GATE conditions) — the wait_keys
// for an acceptance_verdict / stage_barrier BlockedOn (WHAT the node waits on). It is
// a pure READ; returns nil for a plan with no engine / no graph / an unbound node.
func (s *Service) upstreamConditionNodeIDs(ctx context.Context, p *pm.Plan, t *pm.Task, stageGateOnly bool) ([]string, error) {
	if s.orch == nil || p.GraphID() == "" || t.NodeID() == "" {
		return nil, nil
	}
	graphID := orch.GraphID(p.GraphID())
	nodeID := orch.NodeID(t.NodeID())
	edges, err := s.orch.ListEdges(ctx, graphID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.orch.ListNodes(ctx, graphID)
	if err != nil {
		return nil, err
	}
	byID := make(map[orch.NodeID]*orch.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID()] = n
	}
	var out []string
	for _, e := range edges {
		if e.ToNodeID != nodeID {
			continue
		}
		n := byID[e.FromNodeID]
		if n == nil {
			continue
		}
		if n.Category() != orch.NodeCategoryControl || n.ControlKind() != orch.ControlKindCondition {
			continue
		}
		if stageGateOnly {
			if _, isStageGate := n.Metadata()["stage_gate"]; !isStageGate {
				continue
			}
		}
		out = append(out, string(n.ID()))
	}
	return out, nil
}
