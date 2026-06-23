package service

import (
	"context"
	"errors"
	"strings"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
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
// The dependency check reuses the engine's own readiness derivation (ComputePlanView
// via poolNodeStatus) so the gate and the dispatcher agree on "deps satisfied" for
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
// This is uniform for built-in pool plans (dispatched-on-select) and structured
// (DAG) plans: a built-in member reaches `dispatched` on selection, a DAG node
// reaches `ready`/`dispatched` only once its upstream is done — so the same
// ready-or-dispatched test gates both without a separate built-in branch.
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
	ns, err := s.poolNodeStatus(ctx, p, taskID)
	if err != nil {
		return err
	}
	if p.IsBuiltin() {
		// Built-in Assignment Pool: a member is runnable ONLY once DISPATCHED (= it is
		// in the pool). The pool's own "claimable" path (ClaimPoolTask) is the entry
		// into running for an ownerless pool task (§13.F-②); a not-yet-dispatched
		// built-in task is inert here. A claimed pool task keeps its dispatch record, so
		// the post-claim start_task (claim→open, then run) still reads NodeDispatched.
		if ns == pm.NodeDispatched {
			return nil
		}
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
		return nil // all blockedBy dependencies completed/discarded
	default:
		return pm.ErrTaskNotRunnable // deps unsatisfied (blocked) / dead branch (skipped)
	}
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
