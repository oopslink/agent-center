package service

import (
	"context"
	"errors"
	"strings"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// taskIDFromRef extracts the Task id from a "pm://tasks/{id}" ref. v2.14.0 F7
// (issue I14): previously lived in the deleted work_item_projector.go; the run
// gate is the sole remaining caller, so it is re-homed here.
func taskIDFromRef(ref string) (string, bool) {
	const prefix = "pm://tasks/"
	if strings.HasPrefix(ref, prefix) && len(ref) > len(prefix) {
		return strings.TrimPrefix(ref, prefix), true
	}
	return "", false
}

// runnable_gate.go (T130, rewritten v2.14.0 I14/F3 §13.A) — the open→running
// invariant, the sibling of the T83 claimability guard (claim_flow.go). It is the
// 抢跑 (run-ahead) gate: an agent may start a task ONLY once that task's blockedBy
// dependencies are resolved. §八 of the I14 plan listed "delete TaskRunGate" — §13.A
// corrects that as a CRITICAL error: deleting this gate revives the run-ahead bug
// (an agent starting a DAG node before its upstream finished). The gate is kept and
// its HOST moves from the (deleted) AgentWorkItem to the Task dependency model.
//
// §13.A rule (host = Task.blockedBy, the plan's forward depends_on edges):
//   - assign ≠ runnable: AssignTask only records ownership (assignee + open); it does
//     NOT make the task immediately startable.
//   - start_task must pass the gate: open→running is allowed ONLY when every blockedBy
//     dependency is completed/discarded — i.e. the plan engine has derived the node to
//     `ready` or `dispatched`. A node still `blocked` on an unfinished upstream (or
//     `skipped` on a dead conditional branch) is rejected with pm.ErrTaskNotRunnable.
//   - list_my_tasks returns only runnable tasks (ListRunnableAgentTasks), so an agent
//     never treats a dependency-blocked task as work it can pull.
//
// The dependency check reuses the engine's own readiness derivation (DerivePlanView
// via planNodeStatus) so the gate and the dispatcher agree on "deps satisfied" for
// BOTH hard-AND (seq) and conditional edges — one source of truth, no drift.
//
// T83 closed the POOL self-claim path (ClaimPoolTask). This closes the OTHER path
// into running: direct assign → assignee start_work, which activates a work item and
// (via TaskStatusSyncProjector.startTaskIfOpen) flips the task open→running. The gate
// is enforced synchronously at start_work through the agentsvc.TaskRunGate port (the
// Agent BC owns no plan knowledge), and again at AssignTask (assign_flow.go).

// EnsureTaskRunnable returns nil when the task may enter running, or
// pm.ErrTaskNotRunnable when its blockedBy dependencies are not yet satisfied
// (§13.A). It is a READ — no mutation, no event — so it composes inside any
// caller's tx.
//
// Predicate:
//   - status == running       → already IN MOTION (a resume, not a fresh start) → ok.
//   - planID == ""            → pure backlog (no plan / no deps placed) → not runnable.
//   - node status ready/dispatched → all blockedBy deps resolved → runnable.
//   - node status blocked/skipped/other → deps unsatisfied / dead branch → NOT runnable.
//
// Built-in pool and structured (DAG) plans share the ready/dispatched notion but
// take separate branches: a DAG node reaches `ready`/`dispatched` only once its
// upstream is done (deps gated); a built-in pool member is FLAT (no upstream), so
// `ready` already means deps-satisfied and an ASSIGNED ready member is runnable
// even before the async dispatch record lands — only an UNASSIGNED member must
// wait for `dispatched` (its claimable-into-pool signal).
func (s *Service) EnsureTaskRunnable(ctx context.Context, taskID pm.TaskID) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	// The gate authorizes the open→running TRANSITION (a dependency-blocked task must
	// not be STARTED). A task already `running` is past that transition and IN MOTION:
	// re-activating a work item for it — e.g. after the prior work item failed (or the
	// agent's session died mid-flight) and a fresh queued item was minted — is a
	// resume, NOT a fresh start, so it is runnable. This mirrors the agent-tools
	// rejectIfBacklog gate, which likewise flags only the PRE-START states
	// (open/reopened) as inert and spares in-motion tasks. Without this a running pool
	// task derives nodeStatus=NodeRunning (never NodeDispatched), is misclassified as
	// not-runnable, and its requeued work item can NEVER start — wedging the task
	// behind a misleading `task_not_runnable`.
	if t.Status() == pm.TaskRunning {
		return nil
	}
	planID := t.PlanID()
	if planID == "" {
		return pm.ErrTaskNotRunnable // pure backlog — no plan, no deps placed
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return err
	}
	ns, err := s.planNodeStatus(ctx, p, taskID)
	if err != nil {
		return err
	}
	if p.IsBuiltin() {
		// Built-in Assignment Pool: a DISPATCHED member is runnable (= it is in the pool).
		// The pool's own "claimable" path (ClaimPoolTask) is the entry into running for an
		// ownerless pool task (§13.F-②). A claimed pool task keeps its dispatch record, so
		// the post-claim start_task (claim→open, then run) still reads NodeDispatched.
		if ns == pm.NodeDispatched {
			return nil
		}
		// An ASSIGNED-but-not-yet-dispatched member (node_status=ready) is ALSO runnable.
		// The built-in pool is FLAT (no upstream edges), so NodeReady already means "all
		// (zero) blockedBy deps satisfied" — there is NO run-ahead risk, the sole concern
		// of this gate. The dispatch RECORD only gates CLAIMABILITY (ownerless pool work)
		// and the dispatch wake; it is written asynchronously by the pool sweep
		// (dispatchBuiltinPool). Requiring it here coupled an assigned member's runnability
		// to that async write, so a directly assign_task'd pool member (assignment is
		// decoupled from dispatch — assign_flow.go) was误拒 task_not_runnable in the window
		// before the sweep ran — AND the assign-time dispatch wake (gated on this same
		// EnsureTaskRunnable) never fired, so the assignee could not even be nudged. An
		// owned, dep-satisfied pool member must be startable now; the assignee gate keeps an
		// UNASSIGNED ready member inert (it must be dispatched to become claimable — nobody
		// owns it to start it).
		if ns == pm.NodeReady && strings.TrimSpace(string(t.Assignee())) != "" {
			return nil
		}
		return pm.ErrTaskNotRunnable
	}
	// T329 (issue-9d4b3895 §13.A): a structured plan node is runnable ONLY when its
	// plan is RUNNING — a draft/stopped plan's nodes must not start (being a plan
	// member is not enough). Without this a node of a draft plan can derive a
	// ready/dispatched status and wrongly start (抢跑 of a not-yet-launched plan).
	if p.Status() != pm.PlanRunning {
		return pm.ErrTaskNotRunnable
	}
	// §13.A dependency gate (structured DAG plan): derive the node's status from the
	// plan view (the engine's own readiness logic — seq AND + conditional routing) and
	// allow the start ONLY when the blockedBy deps are resolved (ready = deps done, not
	// yet dispatched; dispatched = deps done, mention posted). A `blocked` node
	// (upstream unfinished) or a `skipped` node (dead conditional branch) is rejected —
	// that IS the run-ahead guard §八 would have deleted.
	switch ns {
	case pm.NodeReady, pm.NodeDispatched:
		// issue-b867bbe6: DerivePlanView (planNodeStatus) is STAGE-UNAWARE. A stage
		// barrier is an orchestration-graph gate CONDITION node + edge (downstream stage
		// ENTRY depends_on the upstream stage's gate), NOT a task-level depends_on edge.
		// So a downstream stage's entry derives `ready` here even while its upstream
		// stage gate is unresolved — and an assigned agent's start_work / list_my_tasks
		// (ListRunnableAgentTasks) would bypass the closed barrier. The dispatch path
		// (graphReadySet → GetReadyNodes) already gates this correctly off the graph; the
		// run-gate must agree. Reuse the engine's gate: a downstream entry is runnable
		// only once its upstream stage gate has RESOLVED (completed/discarded), mirroring
		// orch ReadyNodes' condition-gate satisfaction test.
		blocked, berr := s.stageGateBlocks(ctx, p, t)
		if berr != nil {
			return berr
		}
		if blocked {
			return pm.ErrTaskNotRunnable // upstream stage gate unresolved — barrier holds
		}
		return nil // all blockedBy deps + upstream stage barrier(s) satisfied
	default:
		return pm.ErrTaskNotRunnable // deps unsatisfied (blocked) / dead branch (skipped)
	}
}

// stageGateBlocks reports whether taskID's graph node is held behind an unresolved
// upstream STAGE GATE — the stage barrier (issue-b867bbe6). It is the run-gate's
// stage-aware complement to the stage-UNAWARE DerivePlanView: buildStages wires a
// downstream stage's ENTRY node to depend_on the upstream stage's gate CONDITION
// node, and that barrier edge lives ONLY in the orchestration graph. This mirrors the
// engine's own ReadyNodes rule (an unresolved condition gates its downstream), so the
// run-gate and the dispatcher agree on "barrier lifted".
//
// It is a pure READ (no mutation, no event) so it composes inside any caller's tx.
// Returns false (no barrier) for a plan with no engine / no graph / a task not bound
// to a node — the §8 zero-regression path (a pure-node DAG has no stage gates). A
// non-entry stage member is gated by its in-stage predecessor at the task level and is
// already held by DerivePlanView, so only the entry carries a direct gate edge here.
func (s *Service) stageGateBlocks(ctx context.Context, p *pm.Plan, t *pm.Task) (bool, error) {
	if s.orch == nil || p.GraphID() == "" || t.NodeID() == "" {
		return false, nil
	}
	graphID := orch.GraphID(p.GraphID())
	nodeID := orch.NodeID(t.NodeID())
	edges, err := s.orch.ListEdges(ctx, graphID)
	if err != nil {
		return false, err
	}
	var upstream []orch.NodeID
	for _, e := range edges {
		if e.ToNodeID == nodeID {
			upstream = append(upstream, e.FromNodeID)
		}
	}
	if len(upstream) == 0 {
		return false, nil
	}
	nodes, err := s.orch.ListNodes(ctx, graphID)
	if err != nil {
		return false, err
	}
	byID := make(map[orch.NodeID]*orch.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID()] = n
	}
	for _, up := range upstream {
		n := byID[up]
		if n == nil {
			continue
		}
		// A stage gate = a CONDITION control node carrying "stage_gate" metadata
		// (stamped by buildStages). It is "lifted" only once RESOLVED — mirroring
		// orch.ReadyNodes, which treats a condition dep as satisfied only when
		// completed/discarded. An unresolved (open/reopen) stage gate HOLDS the entry.
		if n.Category() != orch.NodeCategoryControl || n.ControlKind() != orch.ControlKindCondition {
			continue
		}
		if _, isStageGate := n.Metadata()["stage_gate"]; !isStageGate {
			continue
		}
		if n.Status() != orch.NodeCompleted && n.Status() != orch.NodeDiscarded {
			return true, nil
		}
	}
	return false, nil
}

// stageBarrierHeldSet is the BATCHED counterpart of stageGateBlocks: over a single
// graph load it returns the set of the plan's task ids whose graph node is a
// downstream-stage ENTRY held behind an UNRESOLVED upstream stage gate (issue-<start-
// task-ready-node>). stageGateBlocks answers this ONE task at a time for the run gate;
// this answers it for a WHOLE plan in O(edges+nodes) so a read model (GetPlanDetail /
// GetPlanDetailForMember) can correct the STAGE-UNAWARE DerivePlanView without paying a
// per-node graph round-trip.
//
// The rule is IDENTICAL to stageGateBlocks — an entry is held iff any of its upstream
// graph edges originates at a stage-gate CONDITION node ("stage_gate" metadata) that is
// not yet resolved (completed/discarded) — so the read model, the run gate, and the
// dispatcher (graphReadySet) all agree on "barrier lifted" (one source of truth). Nil
// (no barriers) for a plan with no engine / no graph (§8 zero-regression: a pure-node
// DAG has no stage gates).
func (s *Service) stageBarrierHeldSet(ctx context.Context, p *pm.Plan) (map[pm.TaskID]bool, error) {
	if s.orch == nil || p.GraphID() == "" {
		return nil, nil
	}
	graphID := orch.GraphID(p.GraphID())
	nodes, err := s.orch.ListNodes(ctx, graphID)
	if err != nil {
		return nil, err
	}
	// Index nodes; collect the UNRESOLVED stage-gate condition node ids and the
	// node→task binding so held entries map back to task ids.
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

// applyStageBarrierView corrects the STAGE-UNAWARE DerivePlanView read model on a
// read-facing PlanDetail: DerivePlanView derives a downstream-stage ENTRY as `ready`
// (and lists it in ReadySet) even while its upstream stage gate is unresolved, because
// the barrier edge lives ONLY in the orchestration graph (not in task-level depends_on).
// That misleads a caller (get_plan) into assigning + starting a node the run gate then
// deterministically rejects (task_not_runnable) and the dispatcher never dispatches.
//
// This re-derives the barrier-held set (stageBarrierHeldSet) and downgrades each held
// entry from `ready` to `blocked` + drops it from ReadySet, so the get_plan read agrees
// with the run gate (EnsureTaskRunnable) and the dispatcher (graphReadySet) — one source
// of truth. It touches ONLY the read model; the barrier, the run gate, and dispatch are
// unchanged. No-op when there are no stage barriers (pure-node DAG / built-in pool).
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

// AgentTaskRunGate adapts the pm Service to the agentsvc.TaskRunGate port (T130).
// It authorizes a work item's task to enter running at start_work: the Agent BC
// holds the work-item lifecycle but no plan/pool knowledge, so it delegates the
// runnability decision here. Wired at the composition root
// (agentSvc.SetTaskRunGate(pmservice.NewAgentTaskRunGate(pmSvc))); the Agent BC
// depends only on the port → no import cycle.
type AgentTaskRunGate struct{ svc *Service }

// NewAgentTaskRunGate builds the port adapter over the pm Service.
func NewAgentTaskRunGate(svc *Service) *AgentTaskRunGate { return &AgentTaskRunGate{svc: svc} }

// EnsureTaskRunnable resolves the work item's task ref ("pm://tasks/{id}") and
// applies EnsureTaskRunnable, translating the pm sentinel to the agent-BC sentinel
// the start_work HTTP layer maps. A ref that is not task-backed is left to the
// caller (no task to gate) — work items are always task-backed, so this is purely
// defensive.
func (g *AgentTaskRunGate) EnsureTaskRunnable(ctx context.Context, taskRef string) error {
	id, ok := taskIDFromRef(taskRef)
	if !ok {
		return nil
	}
	if err := g.svc.EnsureTaskRunnable(ctx, pm.TaskID(id)); err != nil {
		if errors.Is(err, pm.ErrTaskNotRunnable) {
			return agentpkg.ErrTaskNotRunnable
		}
		return err
	}
	return nil
}

var _ agentsvc.TaskRunGate = (*AgentTaskRunGate)(nil)
