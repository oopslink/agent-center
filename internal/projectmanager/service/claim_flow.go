package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ClaimPoolTask is the T83 open-claim operation: an eligible agent claims a
// built-in assignment-pool task — atomically assigning it to the claimer and
// moving it open→running. It is the pool's claim entry point (pool tasks have no
// WorkItem, so there is no start_work path; the agent pulls + claims directly).
//
// Guards (all fail-closed, service layer — not UI):
//   - §3.1/4.1: a backlog task (no plan) is never claimable → ErrTaskNotClaimable.
//   - §3.2: open-claim applies ONLY to the built-in pool; a structured-plan node
//     stays assignee-gated (claim it via its normal dispatch) → ErrTaskNotClaimable.
//   - §3.4/4.3: only a PROJECT-MEMBER agent may claim; a non-member is rejected
//     via requireProjectMember (the edge maps it to an opaque 403/404 so the task's
//     existence is not leaked).
//   - the task must be an open, dispatched, non-archived pool node.
//   - §3.6/4.6: the agent must hold < N concurrent claimed pool tasks (N=poolLimit).
//   - §3.3/4.4: the claim persists ONLY while the row is still open & unassigned
//     (ClaimIfUnassigned CAS) — a concurrent loser gets ErrTaskAlreadyClaimed.
//
// On success the claimer is added to the pool conversation (additive); NO
// EvtTaskAssigned is emitted, so no WorkItem/wake is minted — the pull-pool stays
// pull (ADR-0047), the agent is already actively claiming.
func (s *Service) ClaimPoolTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		planID := t.PlanID()
		if planID == "" {
			return pm.ErrTaskNotClaimable // backlog — never claimable (4.1)
		}
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if !p.IsBuiltin() {
			// structured-plan node: not open-claimable (assignee-gated dispatch).
			return pm.ErrTaskNotClaimable
		}
		// authz hard-gate (fail-closed): membership is the prerequisite to claim.
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		ns, err := s.poolNodeStatus(txCtx, p, taskID)
		if err != nil {
			return err
		}
		if !pm.ClaimableInPool(t.IsArchived(), t.Status(), planID, ns) {
			// An already-assigned (claimed/running) task reads as already-claimed;
			// otherwise it is simply not in a claimable state (not dispatched / terminal).
			if t.Status() != pm.TaskOpen && t.Assignee() != "" {
				return pm.ErrTaskAlreadyClaimed
			}
			return pm.ErrTaskNotClaimable
		}
		// holding cap (4.6): count the actor's currently-held (running) pool tasks.
		held, err := s.countHeldPoolTasks(txCtx, actor)
		if err != nil {
			return err
		}
		if held >= s.poolLimit() {
			return pm.ErrPoolClaimLimitReached
		}
		// atomic claim: assign + start in-memory, then persist ONLY if the stored
		// row is still open & unassigned (CAS) — the concurrency guard (4.4).
		prev := t.Status()
		if err := t.Assign(actor, now); err != nil {
			return err
		}
		if err := t.Start(now); err != nil {
			return err
		}
		won, err := s.tasks.ClaimIfUnassigned(txCtx, t)
		if err != nil {
			return err
		}
		if !won {
			return pm.ErrTaskAlreadyClaimed // a concurrent claim took it first (4.4)
		}
		if err := s.emitTaskStateChanged(txCtx, t, prev, ""); err != nil {
			return err
		}
		// claimer joins the pool conversation (additive; NO work-item/wake).
		return s.syncPlanParticipantOnAssign(txCtx, t, actor)
	})
}

// poolNodeStatus derives a task's node status within its (already-loaded) plan.
// A task not found in the view yields the zero NodeStatus (→ not claimable).
func (s *Service) poolNodeStatus(ctx context.Context, p *pm.Plan, taskID pm.TaskID) (pm.NodeStatus, error) {
	detail, err := s.planDetail(ctx, p)
	if err != nil {
		return "", err
	}
	for _, n := range detail.View.Nodes {
		if n.TaskID == taskID {
			return n.NodeStatus, nil
		}
	}
	return "", nil
}

// countHeldPoolTasks counts the actor's currently-held (running) built-in-pool
// tasks (T83 §3.6 cap). Bounded by the actor's assigned-task count; built-in
// lookups are memoized per plan. Structured-plan work does NOT count.
func (s *Service) countHeldPoolTasks(ctx context.Context, actor pm.IdentityRef) (int, error) {
	tasks, err := s.tasks.ListByAssignee(ctx, actor)
	if err != nil {
		return 0, err
	}
	builtin := map[pm.PlanID]bool{}
	held := 0
	for _, t := range tasks {
		if t.Status() != pm.TaskRunning {
			continue
		}
		pid := t.PlanID()
		if pid == "" {
			continue
		}
		isPool, ok := builtin[pid]
		if !ok {
			p, ferr := s.plans.FindByID(ctx, pid)
			if ferr != nil {
				return 0, ferr
			}
			isPool = p != nil && p.IsBuiltin()
			builtin[pid] = isPool
		}
		if isPool {
			held++
		}
	}
	return held, nil
}
