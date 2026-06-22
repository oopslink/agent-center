package service

import (
	"context"
	"errors"
	"strings"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// runnable_gate.go (T130) — the open→running invariant, the sibling of the T83
// claimability guard (claim_flow.go). A task may enter `running` ONLY when it is
// a real (non-builtin) Plan node OR a DISPATCHED member of the built-in
// Assignment Pool. A backlog task — no plan, or a not-yet-dispatched built-in
// task — must NOT run.
//
// T83 closed the POOL self-claim path (ClaimPoolTask). This closes the OTHER
// path into running: direct assign → assignee start_work, which activates a work
// item and (via TaskStatusSyncProjector.startTaskIfOpen) flips the task
// open→running with no plan/pool check. The gate is enforced synchronously at
// start_work through the agentsvc.TaskRunGate port (the Agent BC owns no plan
// knowledge), and again at AssignTask (assign_flow.go) so a backlog assignment to
// an agent never mints a work item that can never legally start.

// EnsureTaskRunnable returns nil when the task may enter running, or
// pm.ErrTaskNotRunnable when it is backlog (T130). It is a READ — no mutation,
// no event — so it composes inside any caller's tx.
//
// Predicate (requirement 2 — the built-in plan is backlog, NOT a "real plan"):
//   - planID == ""            → backlog (captured, no plan)        → not runnable
//   - plan is NOT built-in     → a real structured-plan node        → runnable
//   - plan IS built-in         → runnable IFF the node is DISPATCHED (= it is in
//     the Assignment Pool); a non-dispatched built-in task is backlog → not runnable
func (s *Service) EnsureTaskRunnable(ctx context.Context, taskID pm.TaskID) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	// The gate authorizes the open→running TRANSITION (a backlog task must not be
	// STARTED). A task already `running` is past that transition and IN MOTION:
	// re-activating a work item for it — e.g. after the prior work item failed (or
	// the agent's session died mid-flight) and a fresh queued item was minted — is
	// a resume, NOT a backlog start, so it is runnable. This mirrors the agent-tools
	// rejectIfBacklog gate, which likewise flags only the PRE-START states
	// (open/reopened) as inert backlog and spares in-motion tasks. Without this a
	// running pool task derives nodeStatus=NodeRunning (never NodeDispatched), is
	// misclassified as backlog, and its requeued work item can NEVER start —
	// wedging the task behind a misleading `task_backlog_not_actionable`.
	if t.Status() == pm.TaskRunning {
		return nil
	}
	planID := t.PlanID()
	if planID == "" {
		return pm.ErrTaskNotRunnable // pure backlog — no plan at all
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return err
	}
	if !p.IsBuiltin() {
		// T329 (issue-9d4b3895 §13.A): a structured plan node is runnable ONLY when
		// its plan is RUNNING and its DAG dependencies are satisfied — being a plan
		// member is NOT sufficient. The pre-fix unconditional `return nil` let a node
		// start before its upstream Integrate/S0 finished (抢跑, ignoring the
		// depends_on edges add_plan_dependency creates), and let nodes of a stopped
		// (draft) plan start. NodeReady/NodeDispatched ⇒ all forward deps done (seq +
		// matched conditional); NodeBlocked/NodeSkipped/terminal ⇒ not runnable. This
		// mirrors EnsureTaskDispatchable so start and dispatch agree.
		if p.Status() != pm.PlanRunning {
			return pm.ErrTaskNotRunnable
		}
		ns, err := s.planNodeStatus(ctx, p, taskID)
		if err != nil {
			return err
		}
		if ns == pm.NodeReady || ns == pm.NodeDispatched {
			return nil
		}
		return pm.ErrTaskNotRunnable
	}
	// Built-in plan: runnable ONLY as a DISPATCHED pool member. A built-in task
	// that has not been dispatched into the pool is backlog (requirement 2).
	ns, err := s.planNodeStatus(ctx, p, taskID)
	if err != nil {
		return err
	}
	if ns == pm.NodeDispatched {
		return nil // in the Assignment Pool
	}
	return pm.ErrTaskNotRunnable
}

// EnsureTaskDispatchable is the T329 unified DISPATCH hard-gate (issue-9d4b3895):
// it reports whether a fresh agent work item may be dispatched for taskID RIGHT
// NOW. It is the single predicate the WorkItemProjector consults before minting a
// queued work item, so EVERY dispatch path (assign, plan advance, unblock,
// auto-redispatch, reassign) is gated identically — closing the "initial dispatch
// 抢跑 + re-dispatch churn" the v2.14.0 cycle dogfood exposed. Like EnsureTaskRunnable
// it is a pure READ (composes inside the projector's tx).
//
// Rejects with pm.ErrTaskNotRunnable when:
//   - archived / terminal (completed|discarded) — a settled node is NEVER re-dispatched;
//   - the task carries a blocked_reason — a blocked node is a deliberate pause and
//     must not be blindly re-dispatched (it is cleared by an explicit unblock/resume);
//   - backlog (no plan) or a non-dispatched built-in pool member;
//   - a structured node whose plan is NOT running (draft/stopped) — respect plan state;
//   - a structured node whose DAG dependencies are unsatisfied / dead-branch skipped
//     (NodeBlocked / NodeSkipped / NodePaused).
//
// Allows (returns nil) a dispatched pool member, or a structured node whose deps are
// satisfied and live (NodeReady / NodeDispatched / NodeRunning).
func (s *Service) EnsureTaskDispatchable(ctx context.Context, taskID pm.TaskID) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	if t.IsArchived() {
		return pm.ErrTaskNotRunnable
	}
	if t.Status().IsTerminal() {
		return pm.ErrTaskNotRunnable // done/discarded → never re-dispatch (req: 终态不重投)
	}
	if strings.TrimSpace(t.BlockedReason()) != "" {
		return pm.ErrTaskNotRunnable // blocked → don't blindly re-dispatch (req)
	}
	planID := t.PlanID()
	if planID == "" {
		return pm.ErrTaskNotRunnable // backlog
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
		if ns == pm.NodeDispatched {
			return nil // dispatched Assignment-Pool member
		}
		return pm.ErrTaskNotRunnable
	}
	if p.Status() != pm.PlanRunning {
		return pm.ErrTaskNotRunnable // req: draft/stopped plan dispatches nothing
	}
	switch ns {
	case pm.NodeReady, pm.NodeDispatched, pm.NodeRunning:
		return nil // deps satisfied + live
	default: // NodeBlocked / NodeSkipped / NodePaused / NodeDone / NodeFailed
		return pm.ErrTaskNotRunnable
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

// EnsureWorkItemRunnable resolves the work item's task ref ("pm://tasks/{id}") and
// applies EnsureTaskRunnable, translating the pm sentinel to the agent-BC sentinel
// the start_work HTTP layer maps. A ref that is not task-backed is left to the
// caller (no task to gate) — work items are always task-backed, so this is purely
// defensive.
func (g *AgentTaskRunGate) EnsureWorkItemRunnable(ctx context.Context, taskRef string) error {
	id, ok := taskIDFromRef(taskRef)
	if !ok {
		return nil
	}
	if err := g.svc.EnsureTaskRunnable(ctx, pm.TaskID(id)); err != nil {
		if errors.Is(err, pm.ErrTaskNotRunnable) {
			return agentpkg.ErrWorkItemTaskNotRunnable
		}
		return err
	}
	return nil
}

// EnsureWorkItemDispatchable resolves a work item's task ref and applies the T329
// dispatch gate (EnsureTaskDispatchable). The WorkItemProjector calls this before
// minting a fresh queued work item; a non-task ref or a not-dispatchable task is
// reported via pm.ErrTaskNotRunnable (the projector treats it as "skip the mint",
// not an error). It is the dispatch-side sibling of EnsureWorkItemRunnable.
func (g *AgentTaskRunGate) EnsureWorkItemDispatchable(ctx context.Context, taskRef string) error {
	id, ok := taskIDFromRef(taskRef)
	if !ok {
		return nil
	}
	return g.svc.EnsureTaskDispatchable(ctx, pm.TaskID(id))
}

var _ agentsvc.TaskRunGate = (*AgentTaskRunGate)(nil)
