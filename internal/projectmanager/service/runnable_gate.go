package service

import (
	"context"
	"errors"

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
	planID := t.PlanID()
	if planID == "" {
		return pm.ErrTaskNotRunnable // pure backlog — no plan at all
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return err
	}
	if !p.IsBuiltin() {
		return nil // real (structured) plan node — runnable
	}
	// Built-in plan: runnable ONLY as a DISPATCHED pool member. A built-in task
	// that has not been dispatched into the pool is backlog (requirement 2).
	ns, err := s.poolNodeStatus(ctx, p, taskID)
	if err != nil {
		return err
	}
	if ns == pm.NodeDispatched {
		return nil // in the Assignment Pool
	}
	return pm.ErrTaskNotRunnable
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

var _ agentsvc.TaskRunGate = (*AgentTaskRunGate)(nil)
