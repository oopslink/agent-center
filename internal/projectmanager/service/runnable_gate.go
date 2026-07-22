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
	// ADR-0054 — the park gate, checked BEFORE everything else. A parked task
	// (blocked) is non-terminal and often still node_status ready/dispatched,
	// so without this arm it walks straight through the plan/dep gates below and answers
	// "runnable" — which is precisely how a park failed to stop dispatch: every re-drive
	// (ListRunnableAgentTasks, the wake re-push, the 60s sweep) funnels through here, and
	// a "yes" forks a fresh empty-context executor onto work deliberately paused.
	// Un-park it first (unblock); until then it is not runnable, and this is the one
	// chokepoint that says so.
	if t.Status().IsParked() {
		return pm.ErrTaskNotRunnable
	}
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
	// issue-d2f14e0e (P0): a merge-to-main task is HARD-GATED on a passed acceptance
	// verdict, checked here — the single server-side chokepoint every executor passes
	// through at start_work — so the merge cannot run regardless of executor behavior.
	// This is additive to (and independent of) the dep / stage-barrier gates below;
	// it fail-closes an ungated merge node (the P67 hole) even when its plain deps are
	// satisfied. A non-merge task returns immediately (no graph reads).
	if blocked, aerr := s.acceptanceVerdictBlocks(ctx, p, t); aerr != nil {
		return aerr
	} else if blocked {
		return pm.ErrTaskNotRunnable
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

// acceptanceVerdictBlocks reports whether taskID is a merge-to-main task
// (pm.TagMergeToMain) that has NOT cleared its acceptance verdict — the P0 hard
// gate (issue-d2f14e0e). A merge-to-main task is runnable ONLY when its graph node
// sits directly downstream of one or more acceptance/decision CONDITION gates and
// EVERY such gate has resolved to a PASS (Completed + outcome "success", the string
// ResolveCondition("success") / RecordDecisionOutcome("pass") stamp). Anything else
// FAIL-CLOSES to blocked:
//   - the task carries no merge-to-main tag → not gated (returns false immediately);
//   - the task is not in a structured plan with a graph (no verdict to verify);
//   - the node has NO upstream condition gate at all — author forgot to gate the
//     merge behind acceptance (the exact P67 hole: a Ship node whose only dep was
//     Dev, run in parallel with the verdict);
//   - an upstream gate is unresolved (reject loopback) or resolved via a non-pass
//     outcome (force_success / discard bounded-retry escape) — NOT an acceptance pass.
//
// It is a pure READ (no mutation, no event) so it composes inside any caller's tx.
// The check is independent of the dep / stage-barrier gates: it holds even when the
// node's plain blockedBy deps are satisfied, which is precisely what closes the hole.
func (s *Service) acceptanceVerdictBlocks(ctx context.Context, p *pm.Plan, t *pm.Task) (bool, error) {
	if !pm.HasTag(t.Tags(), pm.TagMergeToMain) {
		return false, nil // not a merge-to-main task — nothing to gate
	}
	// A merge-to-main task must run inside a structured plan whose graph can PROVE a
	// passed verdict. No engine / no graph / an unbound node ⇒ fail closed (a builtin
	// pool has no graph, so a merge-to-main pool task is never runnable — it must be
	// authored as a gated node in a structured plan).
	if s.orch == nil || p.GraphID() == "" || t.NodeID() == "" {
		return true, nil
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
	nodes, err := s.orch.ListNodes(ctx, graphID)
	if err != nil {
		return false, err
	}
	byID := make(map[orch.NodeID]*orch.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID()] = n
	}
	passedGates := 0
	for _, up := range upstream {
		n := byID[up]
		if n == nil {
			continue
		}
		if n.Category() != orch.NodeCategoryControl || n.ControlKind() != orch.ControlKindCondition {
			continue // a plain business dep is gated by DerivePlanView, not an acceptance gate
		}
		// Every direct-upstream acceptance/decision gate MUST have passed. A gate still
		// unresolved (reject → loopback keeps it open/reopen) or completed with a non-
		// "success" outcome (force_success on max-rounds exhaustion, discard escape) is
		// NOT a genuine acceptance pass — any such gate blocks the merge.
		if n.Status() != orch.NodeCompleted || n.Outcome() != "success" {
			return true, nil
		}
		passedGates++
	}
	// No acceptance/decision gate upstream at all ⇒ the merge node is ungated (P67):
	// fail closed. This is the invariant an executor cannot bypass — a merge-to-main
	// task with no passed verdict directly upstream never becomes runnable.
	if passedGates == 0 {
		return true, nil
	}
	return false, nil
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
