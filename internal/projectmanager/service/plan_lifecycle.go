package service

import (
	"context"
	"fmt"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanDispatcher posts the node-ready @mention into the Plan's conversation
// (v2.9 #285, design §4/§9.3). Dispatch = posting `@assignee …ready` into the
// 1:1 Plan conversation; the existing wake+mention path (#220) wakes an agent
// assignee, and a human is notified. It is the SOLE cross-BC effect the advance
// orchestrator triggers, kept behind a narrow interface so the pm Service stays
// decoupled from the Conversation BC (the production wiring adapts MessageWriter).
//
// PostMention returns the new message id, which AdvancePlan persists as the
// dispatch record's dispatch_message_id (§9.3). It is OPTIONAL on the Service
// (nil ⇒ AdvancePlan returns ErrDispatcherUnavailable, fail-loud).
type PlanDispatcher interface {
	PostMention(ctx context.Context, conversationID, assigneeRef, content string) (messageID string, err error)
}

// StartPlan validates and moves a Plan draft→running (§9.6). Rejects unless:
//
//	(a) the DAG is acyclic (ValidateNoCycle over the plan's edges);
//	(b) the Plan has ≥1 selected task;
//	(c) EVERY task has a resolvable assignee — identity present, and if an agent,
//	    the agent exists and is not archived/deleted (verified via AgentDirectory,
//	    same OrgOfAgent probe AssignTask uses: an unresolvable agent errors);
//	(d) every task belongs to the Plan's project.
//
// Pre-done tasks are allowed (counted satisfied immediately, §9.6). The actor
// must be a project member. After validation it calls plan.Start + plans.Update.
func (s *Service) StartPlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		// v2.9 #297: can't START a plan on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		tasks, err := s.tasks.ListByPlan(txCtx, planID)
		if err != nil {
			return err
		}
		// (b) ≥1 task.
		if len(tasks) == 0 {
			return pm.ErrPlanNoTasks
		}
		// (a) acyclic DAG.
		edges, err := s.plans.ListDependencies(txCtx, planID)
		if err != nil {
			return err
		}
		if err := pm.ValidateNoCycle(edges); err != nil {
			return err
		}
		// (c)+(d): every task resolvable-assignee + same project.
		for _, t := range tasks {
			if t.ProjectID() != p.ProjectID() {
				return pm.ErrPlanProjectMismatch
			}
			if err := s.validateResolvableAssignee(txCtx, p, t); err != nil {
				return err
			}
		}
		if err := p.Start(now); err != nil {
			return err
		}
		if err := s.plans.Update(txCtx, p); err != nil {
			return err
		}
		// v2.9 P2-1 auto-advance: emit pm.plan.started so the orchestrator projector
		// dispatches the Plan's INITIAL ready nodes (no manual Advance). The project's
		// org is carried so the payload mirrors planEventPayload (the orchestrator
		// only needs PlanID, but org keeps the payload shape consistent).
		proj, perr := s.projects.FindByID(txCtx, p.ProjectID())
		if perr != nil {
			return perr
		}
		return s.emit(txCtx, EvtPlanStarted,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
			planEventPayload{
				PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
				OrganizationID: proj.OrganizationID(),
				OwnerRef:       "pm://plans/" + string(p.ID()),
			})
	})
}

// validateResolvableAssignee enforces §9.6(c): the task must have an assignee,
// and an agent assignee must resolve to the Plan's project org (exists + not
// archived/deleted — an archived/deleted/cross-org agent is unresolvable via the
// directory, mirroring AssignTask's cross-org guard). A human (`user:`) assignee
// only needs to be present. A nil AgentDirectory with an agent assignee is a hard
// error (fail-closed, same as grantAgentProjectMembership).
func (s *Service) validateResolvableAssignee(ctx context.Context, p *pm.Plan, t *pm.Task) error {
	assignee := t.Assignee()
	if strings.TrimSpace(string(assignee)) == "" {
		return fmt.Errorf("%w: task %s has no assignee", pm.ErrPlanUnassignedTask, t.ID())
	}
	if !strings.HasPrefix(string(assignee), "agent:") {
		return nil // human assignee: presence is enough.
	}
	if s.agentDir == nil {
		return pm.ErrAgentDirectoryUnavailable
	}
	agentID := strings.TrimPrefix(string(assignee), "agent:")
	agentOrg, err := s.agentDir.OrgOfAgent(ctx, agentID)
	if err != nil {
		// Unresolvable agent (not found / archived / deleted) → reject (§9.6c).
		return fmt.Errorf("%w: agent assignee %s of task %s is unresolvable", pm.ErrPlanUnresolvableAssignee, assignee, t.ID())
	}
	proj, perr := s.projects.FindByID(ctx, p.ProjectID())
	if perr != nil {
		return perr
	}
	if agentOrg != proj.OrganizationID() {
		// Cross-org agent can never be dispatched into this plan's conversation.
		return pm.ErrCrossOrgAssignee
	}
	return nil
}

// AdvancePlan computes the Plan's DERIVED view and dispatches EVERY ready node
// that has no dispatch record yet (§9.3 + the all-ready lock): for each such
// node it posts an `@assignee …ready` message into the Plan conversation
// (PlanDispatcher.PostMention → the wake+mention path #220 wakes an agent) and
// writes the once-only dispatch record. It is IDEMPOTENT: a node already
// dispatched is skipped (no second @mention), so re-running advance / event
// replay / a second upstream completing never double-dispatches. After
// dispatching, if every node is `done` the Plan is marked done (§9.1).
//
// Returns the list of NEWLY-dispatched task ids (empty when nothing was ready or
// everything ready was already dispatched). The Plan MUST be running
// (ErrPlanNotRunning otherwise); the actor must be a project member. The message
// post + dispatch record + (optional) MarkDone all commit in ONE tx — RunInTx is
// reentrant, so the dispatcher's AddMessage joins this tx (atomic dispatch).
func (s *Service) AdvancePlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) ([]pm.TaskID, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	if s.planDispatcher == nil {
		return nil, ErrDispatcherUnavailable
	}
	var dispatched []pm.TaskID
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		dispatched, err = s.dispatchReadyNodes(txCtx, p)
		return err
	})
	if err != nil {
		return nil, err
	}
	return dispatched, nil
}

// dispatchReadyNodes is the reusable AUTO-ADVANCE dispatch core (v2.9 P2-1).
// Given a LOADED Plan (inside an open tx), it computes the Plan's DERIVED view
// and dispatches EVERY ready node that has no dispatch record yet (§9.3 + the
// all-ready lock): for each such node it posts an `@assignee …ready` message
// into the Plan conversation (PlanDispatcher.PostMention → the wake+mention path
// #220 wakes an agent) and writes the once-only dispatch record. After
// dispatching, if every node is `done` the Plan is marked done (§9.1).
//
// It is IDEMPOTENT and REPLAY/CONCURRENCY-safe: a node already dispatched is
// skipped (ComputePlanView derives `dispatched` from the records → a
// dispatched-but-not-running node is NodeDispatched, never NodeReady), and
// RecordDispatch is INSERT-OR-IGNORE on PK (plan_id, task_id) so two concurrent
// task-done events / an event replay can never double-@mention a node (§9.3).
//
// It asserts the Plan is running (ErrPlanNotRunning otherwise) and has its 1:1
// conversation bound (#284 — fail-loud, not silent). It does NOT check
// project membership — both callers gate that themselves (AdvancePlan via the
// actor, the orchestrator projector is a system path). It MUST run inside a tx
// (RunInTx is reentrant, so the dispatcher's AddMessage joins it → atomic
// dispatch). Returns the NEWLY-dispatched task ids.
func (s *Service) dispatchReadyNodes(txCtx context.Context, p *pm.Plan) ([]pm.TaskID, error) {
	if p.Status() != pm.PlanRunning {
		return nil, pm.ErrPlanNotRunning
	}
	// ADR-0047 PULL split: the built-in pool is a "pull, no-wake" dispatch pool. For
	// each ready node it ONLY RecordDispatch (node_status ready→dispatched → the task
	// becomes claimable; get_my_work surfaces it). It does NOT PostMention and does
	// NOT emit EvtTaskAssigned — there is no push/wake (the agent pulls via
	// get_my_work + claim). It therefore needs no PlanDispatcher and no bound
	// conversation (the structured-plan requirements below do not apply).
	if p.IsBuiltin() {
		return s.dispatchBuiltinPool(txCtx, p)
	}
	if s.planDispatcher == nil {
		return nil, ErrDispatcherUnavailable
	}
	if strings.TrimSpace(p.ConversationID()) == "" {
		// A running Plan must have its 1:1 conversation bound (#284) — dispatch
		// has nowhere to post otherwise (fail-loud, not silent).
		return nil, fmt.Errorf("projectmanager: plan %s has no conversation to dispatch into", p.ID())
	}
	now := s.clock.Now()
	planID := p.ID()
	tasks, err := s.tasks.ListByPlan(txCtx, planID)
	if err != nil {
		return nil, err
	}
	edges, err := s.plans.ListDependencies(txCtx, planID)
	if err != nil {
		return nil, err
	}
	records, err := s.plans.ListDispatchRecords(txCtx, planID)
	if err != nil {
		return nil, err
	}
	view := pm.ComputePlanView(tasks, edges, records)

	// Index task → assignee for the @mention, and task → *Task for the
	// work-delivery emit (v2.9 P2 #1 HEADLINE).
	assigneeOf := make(map[pm.TaskID]pm.IdentityRef, len(tasks))
	titleOf := make(map[pm.TaskID]string, len(tasks))
	taskOf := make(map[pm.TaskID]*pm.Task, len(tasks))
	for _, t := range tasks {
		assigneeOf[t.ID()] = t.Assignee()
		titleOf[t.ID()] = t.Title()
		taskOf[t.ID()] = t
	}

	// Dispatch every ready node (the ready-set is exactly the nodes with no
	// dispatch record — ComputePlanView derives `dispatched` from the records,
	// so a dispatched-but-not-running node is NodeDispatched, never NodeReady).
	var dispatched []pm.TaskID
	for _, taskID := range view.ReadySet {
		assignee := assigneeOf[taskID]
		// content is the BODY only — the dispatcher resolves assignee → display_name
		// and prepends "@<display_name> " so the wake+mention path (#220) fires.
		content := fmt.Sprintf("your task %q is ready — all upstream dependencies are done.", titleOf[taskID])
		msgID, perr := s.planDispatcher.PostMention(txCtx, p.ConversationID(), string(assignee), content)
		if perr != nil {
			return nil, perr
		}
		if rerr := s.plans.RecordDispatch(txCtx, planID, taskID, now, msgID); rerr != nil {
			return nil, rerr
		}
		// v2.9 P2 #1 HEADLINE: the @mention above is for human-readable VISIBILITY
		// (and a human can reply → wake). It does NOT wake an agent assignee: the
		// WakeProjector only wakes on HUMAN (`user:`-sender) messages, and a system-
		// authored @mention never triggers a wake (v2.7 #185 loop-break). It also
		// leaves the assignee with no actionable WorkItem. So we ALSO drive the REAL
		// work-delivery path: emit pm.task.assigned (the SAME event AssignTask emits,
		// same payload shape via emitTaskAssignEvent) for the dispatched task. The
		// already-production-registered WorkItemProjector (#266) consumes it →
		// creates the assignee Agent's queued AgentWorkItem + emits agent.work_available
		// onto its Worker control stream → the agent is woken via the established work
		// path (not the human-only conversational @mention-wake).
		//
		// IDEMPOTENT: this fires only inside the view.ReadySet loop — i.e. ONLY for a
		// node with no dispatch record yet. RecordDispatch (INSERT-OR-IGNORE on the PK)
		// above makes the node NodeDispatched on the next ComputePlanView, so it leaves
		// the ready-set and is never dispatched again → exactly one pm.task.assigned per
		// node-dispatch → no double WorkItem / wake under replay or concurrent done-events.
		//
		// LOOP-SAFE: this wakes the DETERMINED assignee of a NEW ready DAG node (forward,
		// one-way, dispatch-idempotent) — not a conversational agent→agent reply (what
		// #185 guards). A human assignee has no AgentWorkItem, so the WorkItemProjector's
		// agent-only dispatch branch naturally no-ops for them (the @mention is their
		// notification); we still emit so the participant/effective-set stays consistent.
		if t := taskOf[taskID]; t != nil {
			if eerr := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); eerr != nil {
				return nil, eerr
			}
		}
		dispatched = append(dispatched, taskID)
	}

	// §9.1: a Plan is done iff EVERY node is done. Mark it here so a final
	// advance (after the last task completes) transitions running→done.
	if view.AllDone {
		if merr := p.MarkDone(now); merr != nil {
			return nil, merr
		}
		if uerr := s.plans.Update(txCtx, p); uerr != nil {
			return nil, uerr
		}
	}
	return dispatched, nil
}

// dispatchBuiltinPool is the ADR-0047 PULL dispatch core for the built-in pool: a
// FLAT (no-edge) always-running pool. For each ready node it writes ONLY the
// once-only dispatch record (node_status ready→dispatched → the task becomes
// claimable, surfaced by get_my_work). It posts NO @mention and emits NO
// EvtTaskAssigned — there is no push/wake; the assignee pulls the task via
// get_my_work and claims it (open→running). Because the pool is flat, every
// assigned open task has no upstream → is immediately `ready` → gets a dispatch
// record on the first sweep. IDEMPOTENT + replay-safe: ComputePlanView derives a
// dispatched node as NodeDispatched (never NodeReady), and RecordDispatch is
// INSERT-OR-IGNORE on the PK, so re-running never double-records. The pool never
// "completes" (it is immutable/resident), so it never MarkDone.
func (s *Service) dispatchBuiltinPool(txCtx context.Context, p *pm.Plan) ([]pm.TaskID, error) {
	now := s.clock.Now()
	planID := p.ID()
	tasks, err := s.tasks.ListByPlan(txCtx, planID)
	if err != nil {
		return nil, err
	}
	edges, err := s.plans.ListDependencies(txCtx, planID)
	if err != nil {
		return nil, err
	}
	records, err := s.plans.ListDispatchRecords(txCtx, planID)
	if err != nil {
		return nil, err
	}
	view := pm.ComputePlanView(tasks, edges, records)
	var dispatched []pm.TaskID
	for _, taskID := range view.ReadySet {
		// PULL: record the dispatch (no message, no work-item, no wake). An empty
		// dispatch_message_id reflects that no @mention was posted for this node.
		if rerr := s.plans.RecordDispatch(txCtx, planID, taskID, now, ""); rerr != nil {
			return nil, rerr
		}
		dispatched = append(dispatched, taskID)
	}
	return dispatched, nil
}

// StopPlan moves a running Plan back to draft (§9.4) so its DAG/tasks become
// editable again. The actor must be a project member.
func (s *Service) StopPlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		if err := p.Stop(now); err != nil {
			return err
		}
		return s.plans.Update(txCtx, p)
	})
}

// ReconcileRunningPlans is the v2.9 P2-3 reconciliation sweep: the background
// safety net for missed events / crash recovery. It lists EVERY running Plan
// (global) and re-runs the idempotent dispatch core (dispatchReadyNodes) for
// each, so a ready-but-undispatched node (an event the orchestrator projector
// never saw) still gets dispatched. It is IDEMPOTENT: an already-dispatched node
// is skipped (ComputePlanView derives it as NodeDispatched, never NodeReady) and
// RecordDispatch is INSERT-OR-IGNORE — so a sweep over a fully-dispatched plan
// dispatches nothing (no double @mention, §9.3).
//
// Each plan is dispatched in its OWN tx so one plan's failure does not abort the
// sweep: a per-plan error is logged via errFn (when non-nil) and the loop
// continues to the next plan. Returns the first error encountered (for surfacing
// in tests / callers that want it), but never stops early on it. Draft/done plans
// are excluded by ListRunningPlans, so they are naturally skipped.
func (s *Service) ReconcileRunningPlans(ctx context.Context, errFn func(planID pm.PlanID, err error)) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if s.planDispatcher == nil {
		return ErrDispatcherUnavailable
	}
	plans, err := s.plans.ListRunningPlans(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, p := range plans {
		perr := s.runInTx(ctx, func(txCtx context.Context) error {
			// Re-load inside the tx so the dispatch core sees a consistent snapshot
			// (status could have changed between the list and this tick).
			fresh, ferr := s.plans.FindByID(txCtx, p.ID())
			if ferr != nil {
				return ferr
			}
			if fresh.Status() != pm.PlanRunning {
				return nil // raced out of running → skip (no-op).
			}
			_, derr := s.dispatchReadyNodes(txCtx, fresh)
			return derr
		})
		if perr != nil {
			if errFn != nil {
				errFn(p.ID(), perr)
			}
			if firstErr == nil {
				firstErr = perr
			}
			// continue — a per-plan error must not abort the whole sweep.
		}
	}
	return firstErr
}

// RerunFailedNode clears one node's dispatch record (§9.3 creator re-run) so the
// next advance re-dispatches it. Used to resolve a failed node after the creator
// reopens/restarts the underlying task. The actor must be a project member; the
// Plan must be running. Normal advance never clears — only this explicit path.
func (s *Service) RerunFailedNode(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		return s.plans.ClearDispatch(txCtx, planID, taskID)
	})
}
