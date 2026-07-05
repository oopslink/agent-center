package service

import (
	"context"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ClaimPoolTask is the T83 open-claim operation: an eligible agent claims a
// built-in assignment-pool task by assigning it to the claimer. It is the pool's
// claim entry point (pool tasks have no WorkItem, so there is no start_work path;
// the agent pulls + claims directly).
//
// v2.14.0 I14/F3 (§13.B / finding 01KVNKJG): claim is now claim→OPEN, NOT
// claim→running. The claimed task stays `open` (assigned) and only goes `running`
// when the agent later start_tasks it — at which point the §13.F-① single-active
// UNIQUE index (idx_pm_tasks_one_active_per_agent, migration 0072) enforces "one
// running, non-blocked task per agent". This decouples the HOLDING cap (how many
// pool tasks an agent may have claimed at once = poolLimit) from the RUN cap (how
// many it may run at once = exactly 1, by the index): an agent can hold N=poolLimit
// claimed-open tasks but run them one at a time. Claiming several therefore never
// trips the single-active index (only one is ever running).
//
// Guards (all fail-closed, service layer — not UI):
//   - §3.1/4.1: a backlog task (no plan) is never claimable → ErrTaskNotClaimable.
//   - §3.2: open-claim applies ONLY to the built-in pool; a structured-plan node
//     stays assignee-gated (claim it via its normal dispatch) → ErrTaskNotClaimable.
//   - §3.4/4.3: only a PROJECT-MEMBER agent may claim; a non-member is rejected
//     via requireProjectMember (the edge maps it to an opaque 403/404 so the task's
//     existence is not leaked).
//   - the task must be an open, dispatched, non-archived pool node.
//   - §3.6/4.6: the agent must HOLD < N concurrent claimed pool tasks (N=poolLimit);
//     held = its open-or-running pool tasks (ListHeldPoolTasks).
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
		ns, err := s.planNodeStatus(txCtx, p, taskID)
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
		// atomic claim: assign in-memory (claim→OPEN, NOT running — §13.B/F3), then
		// persist ONLY if the stored row is still open & unassigned (CAS) — the
		// concurrency guard (4.4). The task stays `open`; the agent start_tasks it later
		// (where the single-active index gates the actual run), so holding several
		// claimed-open tasks never trips that index.
		prev := t.Status()
		if err := t.Assign(actor, now); err != nil {
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
		// audit §5: a pool claim is an assignee change with its own semantic
		// (claimed-from-pool), distinct from a directed assign.
		s.auditTaskAssign(txCtx, t, pm.AuditTaskClaimed, "", actor)
		// claimer joins the pool conversation (additive; NO work-item/wake).
		return s.syncPlanParticipantOnAssign(txCtx, t, actor)
	})
}

// planNodeStatus derives a task's DERIVED node status within its (already-loaded)
// plan — generic over builtin pools and structured plans (used by the claim guard,
// the runnable gate, and the T329 dispatch gate). A task not found in the view
// yields the zero NodeStatus (→ not claimable / not runnable / not dispatchable).
func (s *Service) planNodeStatus(ctx context.Context, p *pm.Plan, taskID pm.TaskID) (pm.NodeStatus, error) {
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
// tasks (T83 §3.6 cap).
func (s *Service) countHeldPoolTasks(ctx context.Context, actor pm.IdentityRef) (int, error) {
	held, err := s.ListHeldPoolTasks(ctx, actor)
	if err != nil {
		return 0, err
	}
	return len(held), nil
}

// ListHeldPoolTasks returns the actor's currently-held built-in-pool tasks — its
// in-flight CLAIMED pool work (T83). It backs both the §3.6 holding-cap count and
// the get_my_work surface.
//
// v2.14.0 I14/F3 (§13.B): "held" is OPEN-or-RUNNING, not running-only. Since claim
// is now claim→open (the task stays open until the agent start_tasks it), counting
// only running tasks would let an agent claim unboundedly many open pool tasks
// (each held one invisible to the cap) — defeating the holding cap. A claimed pool
// task has NO WorkItem (pull/no-wake), so without this it would also be invisible in
// get_my_work after claiming. Terminal (completed/discarded) tasks are not held.
// Bounded by the actor's assigned-task count; built-in lookups are memoized per
// plan. Structured-plan work is NOT included (it has a WorkItem and already shows in
// get_my_work).
func (s *Service) ListHeldPoolTasks(ctx context.Context, actor pm.IdentityRef) ([]ClaimableTask, error) {
	tasks, err := s.tasks.ListByAssignee(ctx, actor)
	if err != nil {
		return nil, err
	}
	builtin := map[pm.PlanID]bool{}
	var held []ClaimableTask
	for _, t := range tasks {
		if t.Status() != pm.TaskOpen && t.Status() != pm.TaskRunning {
			continue // terminal/other — no longer held
		}
		pid := t.PlanID()
		if pid == "" {
			continue
		}
		isPool, ok := builtin[pid]
		if !ok {
			p, ferr := s.plans.FindByID(ctx, pid)
			if ferr != nil {
				return nil, ferr
			}
			isPool = p != nil && p.IsBuiltin()
			builtin[pid] = isPool
		}
		if isPool {
			held = append(held, ClaimableTask{Task: t, NodeStatus: pm.NodeDispatched})
		}
	}
	return held, nil
}

// ListClaimablePool returns the OPEN, unassigned, dispatched built-in-pool tasks
// the agent `actor` may claim — across the projects in its org where it is a
// member (T83 §3.5 discovery surface; WS2: surfaced read-only via get_my_work's
// "claimable" bucket — the former list_assignment_pool tool was removed).
// It is the DISCOVERY counterpart to ClaimPoolTask and deliberately EXCLUDES the
// agent's own assigned work (that is get_my_work) — it lists only the shared,
// not-yet-taken pool. Fail-closed: nil agentDir / non-agent actor / unresolvable
// agent / non-member project → nothing.
func (s *Service) ListClaimablePool(ctx context.Context, actor pm.IdentityRef) ([]ClaimableTask, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	if s.agentDir == nil || !strings.HasPrefix(string(actor), "agent:") {
		return nil, nil // only agents claim from the pool; no directory ⇒ no scope
	}
	org, err := s.agentDir.OrgOfAgent(ctx, strings.TrimPrefix(string(actor), "agent:"))
	if err != nil || org == "" {
		return nil, nil // unresolvable agent ⇒ no claimable scope (fail-closed)
	}
	projects, err := s.projects.ListByOrg(ctx, org)
	if err != nil {
		return nil, err
	}
	var out []ClaimableTask
	for _, p := range projects {
		// membership gate (fail-closed): only a project member sees its pool.
		if _, merr := s.members.FindByProjectAndIdentity(ctx, p.ID(), actor); merr != nil {
			continue
		}
		pool := s.builtinPoolOf(ctx, p.ID())
		if pool == nil {
			continue
		}
		detail, derr := s.planDetail(ctx, pool)
		if derr != nil {
			return nil, derr
		}
		nodeStatus := make(map[pm.TaskID]pm.NodeStatus, len(detail.View.Nodes))
		for _, n := range detail.View.Nodes {
			nodeStatus[n.TaskID] = n.NodeStatus
		}
		tasks, terr := s.tasks.ListByPlan(ctx, pool.ID())
		if terr != nil {
			return nil, terr
		}
		for _, t := range tasks {
			if t.Assignee() != "" {
				continue // already taken — not part of the available pool
			}
			ns := nodeStatus[t.ID()]
			if pm.ClaimableInPool(t.IsArchived(), t.Status(), t.PlanID(), ns) {
				out = append(out, ClaimableTask{Task: t, NodeStatus: ns})
			}
		}
	}
	return out, nil
}

// builtinPoolOf returns the project's built-in assignment-pool plan (ADR-0047),
// or nil if none / on error (the caller skips).
func (s *Service) builtinPoolOf(ctx context.Context, projectID pm.ProjectID) *pm.Plan {
	plans, err := s.plans.ListByProject(ctx, projectID)
	if err != nil {
		return nil
	}
	for _, p := range plans {
		if p.IsBuiltin() {
			return p
		}
	}
	return nil
}
