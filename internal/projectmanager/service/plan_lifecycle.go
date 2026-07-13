package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// taskRefToken returns a task's human-friendly id ("T<n>") for inclusion in
// plan-conversation system notices, or "" when the org number is unallocated
// (pre-allocator rows). Per @oopslink: plan-conversation system reminders should
// name the task by its id (T123), not only its title.
func taskRefToken(t *pm.Task) string {
	if t == nil || t.OrgNumber() <= 0 {
		return ""
	}
	return "T" + strconv.Itoa(t.OrgNumber())
}

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
		// T768: build the orchestration graph for this plan (business node per task +
		// seq-dependency edges) and stamp plan.graph_id, so dispatch/advance switch to
		// the new engine (graphReadySet). No-op when the engine is unwired or the plan
		// has control-flow edges (those stay on the legacy plan-DAG path). Joins THIS
		// tx (orch.Service shares the reentrant RunInTx) → plan+graph commit atomically.
		if err := s.buildPlanGraph(txCtx, p, tasks, edges, now); err != nil {
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
		if err := s.emit(txCtx, EvtPlanStarted,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
			planEventPayload{
				PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
				OrganizationID: proj.OrganizationID(),
				OwnerRef:       "pm://plans/" + string(p.ID()),
			}); err != nil {
			return err
		}
		// audit §5: record the draft→running start.
		s.auditPlan(txCtx, p, pm.AuditPlanStarted, actor, map[string]any{"status": string(p.Status())})
		return nil
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
// skipped (DerivePlanView derives `dispatched` from the records → a
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
	// Decision outcomes drive the engine's condition resolution / bounded loopback in
	// driveGraphDecisions. Empty for a pure DAG plan (no outcomes recorded). (T810 ⑤:
	// the dispatch-record read moved into the graph branch as freshRecords — it must be
	// read AFTER driveGraphDecisions clears the reopened loop tasks' records.)
	outcomes, err := s.plans.ListDecisionOutcomes(txCtx, planID)
	if err != nil {
		return nil, err
	}
	// T810 ⑤: a structured plan is dispatched off the orchestration engine — the SINGLE
	// dispatch path (the old DerivePlanView fallback + graphDispatchEnabled switch were
	// deleted; every running plan is graphed by StartPlan). graphReadySet syncs the graph
	// to task state then reads GetReadyNodes (dispatch-record-idempotent) + IsAutoDone.
	var readySet []pm.TaskID
	var allDone bool
	if serr := s.syncGraphToTasks(txCtx, p, tasks); serr != nil {
		return nil, serr
	}
	// T805 ③: drive decision adjudication + bounded loopback through the engine. PASS
	// releases the forward branch (ResolveCondition success); REJECT re-runs the loop
	// subgraph via the engine's bounded countReopens (+ reopenLoopSubgraph task mirror);
	// exhaustion records "<outcome>_exhausted" + escalates to the owner.
	if cerr := s.driveGraphDecisions(txCtx, p, edges, outcomes); cerr != nil {
		return nil, cerr
	}
	// T805 ③: a reject/loopback round CLEARS the dispatch records of the reopened loop
	// tasks (reopenLoopSubgraph) INSIDE driveGraphDecisions — so the `records` loaded
	// above are now stale. Re-read them so graphReadySet's dispatch-idempotency sees the
	// cleared set and re-dispatches the reopened tasks THIS pass.
	freshRecords, rerr := s.plans.ListDispatchRecords(txCtx, planID)
	if rerr != nil {
		return nil, rerr
	}
	readySet, allDone, err = s.graphReadySet(txCtx, p, tasks, freshRecords)
	if err != nil {
		return nil, err
	}

	// v2.10 (ADR-0053): load the Plan's shared findings ONCE and format them into a
	// compact block appended to every newly-dispatched node's @mention, so a
	// downstream/sibling agent starts with the plan's accumulated verified progress
	// (DeLM shared context) instead of only its own task title/description. nil-safe:
	// pre-v2.10 constructions (no findings repo) skip injection entirely.
	var findingsBlock string
	if s.findings != nil {
		// review #4: bounded read — count + the latest dispatchFindingsCap rows, not
		// the whole history loaded into the dispatch tx.
		total, cerr := s.findings.CountByPlan(txCtx, planID)
		if cerr != nil {
			return nil, cerr
		}
		if total > 0 {
			latest, ferr := s.findings.ListLatestByPlan(txCtx, planID, dispatchFindingsCap)
			if ferr != nil {
				return nil, ferr
			}
			findingsBlock = formatFindingsForDispatch(latest, total)
		}
	}

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
	// dispatch record — DerivePlanView derives `dispatched` from the records,
	// so a dispatched-but-not-running node is NodeDispatched, never NodeReady).
	var dispatched []pm.TaskID
	for _, taskID := range readySet {
		assignee := assigneeOf[taskID]
		// content is the BODY only — the dispatcher resolves assignee → display_name
		// and prepends "@<display_name> " so the wake+mention path (#220) fires.
		// Per @oopslink: name the task by its id (T<n>) so the plan-conversation
		// reminder is unambiguous; fall back to title-only when unallocated.
		var content string
		if ref := taskRefToken(taskOf[taskID]); ref != "" {
			content = fmt.Sprintf("your task %s %q is ready — all upstream dependencies are done.", ref, titleOf[taskID])
		} else {
			content = fmt.Sprintf("your task %q is ready — all upstream dependencies are done.", titleOf[taskID])
		}
		// v2.10 (ADR-0053): append the plan's shared findings so the agent builds on
		// prior progress. Empty block (no findings) → content unchanged.
		if findingsBlock != "" {
			content += "\n\n" + findingsBlock
		}
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
		// authored @mention never triggers a wake (v2.7 #185 loop-break). So we ALSO
		// drive the REAL work-delivery path: emit pm.task.assigned (the SAME event
		// AssignTask emits, same payload shape via emitTaskAssignEvent) for the
		// dispatched task. T465 (issue I34): the DispatchWakeProjector consumes it →
		// emits a content-free agent.work_available onto the assignee's Worker control
		// stream → the agent is woken immediately and pulls the work via its MCP loop
		// (NOT the human-only conversational @mention-wake). This REPLACED the retired
		// v2.14.0-F7 WorkItemProjector, which used to consume the same event.
		//
		// IDEMPOTENT: this fires only inside the view.ReadySet loop — i.e. ONLY for a
		// node with no dispatch record yet. RecordDispatch (INSERT-OR-IGNORE on the PK)
		// above makes the node NodeDispatched on the next DerivePlanView, so it leaves
		// the ready-set and is never dispatched again → exactly one pm.task.assigned per
		// node-dispatch → no double wake under replay or concurrent done-events.
		//
		// LOOP-SAFE: this wakes the DETERMINED assignee of a NEW ready DAG node (forward,
		// one-way, dispatch-idempotent) — not a conversational agent→agent reply (what
		// #185 guards). A human assignee is not an "agent:" ref, so the DispatchWakeProjector
		// naturally no-ops for them (the @mention is their notification); we still emit so
		// the participant/effective-set stays consistent.
		if t := taskOf[taskID]; t != nil {
			if eerr := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); eerr != nil {
				return nil, eerr
			}
		}
		dispatched = append(dispatched, taskID)
	}

	// §9.1: a Plan is done iff EVERY node is done. Mark it here so a final
	// advance (after the last task completes) transitions running→done.
	if allDone {
		if merr := p.MarkDone(now); merr != nil {
			return nil, merr
		}
		if uerr := s.plans.Update(txCtx, p); uerr != nil {
			return nil, uerr
		}
		// reminder-event: emit pm.plan.completed so on_event reminders watching this
		// plan (event=completed) are armed. Additive marker — no other consumer.
		if eerr := s.emitPlanLifecycle(txCtx, p, EvtPlanCompleted); eerr != nil {
			return nil, eerr
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
// record on the first sweep. IDEMPOTENT + replay-safe: DerivePlanView derives a
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
	// Builtin pool is a FLAT plan — no decisions/conditional edges, so nil outcomes.
	// The flat pool's ready-set is its equivalent graph read — a flat plan has no
	// conditional gating, so DerivePlanView reduces to "in-plan open task with all
	// (here: no) upstream done → ready/dispatched". pause never changes the
	// ready-set/AllDone (T53).
	view := pm.DerivePlanView(tasks, edges, records, nil, nil)
	// Index tasks by id so we can read each newly-dispatched node's assignee for the
	// F1 wake emit below (avoids a per-node FindByID).
	byID := make(map[pm.TaskID]*pm.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID()] = t
	}
	var dispatched []pm.TaskID
	for _, taskID := range view.ReadySet {
		// PULL: record the dispatch (no message, no work-item, no @mention). An empty
		// dispatch_message_id reflects that no @mention was posted for this node.
		if rerr := s.plans.RecordDispatch(txCtx, planID, taskID, now, ""); rerr != nil {
			return nil, rerr
		}
		// F1 (issue-ca51e07c): the built-in pool is PULL/no-@mention, but an ASSIGNED
		// pool member still needs a PUSH wake — it will NOT self-claim (the task is
		// already its own, not ownerless pool work), and this dispatch transition
		// (NodeReady→NodeDispatched) is the exact moment it becomes runnable
		// (EnsureTaskRunnable: a builtin member is runnable ONLY once NodeDispatched).
		// Emit the SAME pm.task.assigned the structured dispatch emits
		// (dispatchReadyNodes above) so the EXISTING DispatchWakeProjector (T465/I34)
		// wakes the assignee exactly once. This is NOT a new emitter and does NOT revive
		// the v2.14.0-retired AgentWorkItem: DispatchWakeProjector is event-id idempotent
		// + runnable-gated, and a node leaves the ready-set the moment it is dispatched
		// (RecordDispatch is INSERT-OR-IGNORE → DerivePlanView derives NodeDispatched),
		// so this fires AT MOST ONCE per pool-dispatch — no double-wake loop. An
		// UNASSIGNED pool member emits nothing here: it stays claimable and the
		// auto-assign path wakes whoever it later assigns (that assign emits
		// pm.task.assigned on an already-dispatched, runnable task).
		if t := byID[taskID]; t != nil && strings.TrimSpace(string(t.Assignee())) != "" {
			if eerr := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); eerr != nil {
				return nil, eerr
			}
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
		if err := s.plans.Update(txCtx, p); err != nil {
			return err
		}
		// audit §5: record the running→draft stop (显式审计写 — no event on this path).
		s.auditPlan(txCtx, p, pm.AuditPlanStopped, actor, map[string]any{"status": string(p.Status())})
		// reminder-event: emit pm.plan.stopped so on_event reminders watching this
		// plan (event=stopped) are armed. Additive marker — no other consumer.
		return s.emitPlanLifecycle(txCtx, p, EvtPlanStopped)
	})
}

// ReconcileRunningPlans is the v2.9 P2-3 reconciliation sweep: the background
// safety net for missed events / crash recovery. It lists EVERY running Plan
// (global) and re-runs the idempotent dispatch core (dispatchReadyNodes) for
// each, so a ready-but-undispatched node (an event the orchestrator projector
// never saw) still gets dispatched. It is IDEMPOTENT: an already-dispatched node
// is skipped (DerivePlanView derives it as NodeDispatched, never NodeReady) and
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
		// I103 §3: materialize/refresh the plan's旁路 BlockedOn snapshots. It runs in
		// its OWN tx AFTER dispatch (so it observes the post-dispatch committed state)
		// and is BEST-EFFORT — a materialize failure NEVER rolls back a dispatch, never
		// sets firstErr, and never aborts the sweep. BlockedOn is pure observation, so it
		// must not be able to break the reconciliation safety net.
		merr := s.runInTx(ctx, func(txCtx context.Context) error {
			fresh, ferr := s.plans.FindByID(txCtx, p.ID())
			if ferr != nil {
				return ferr
			}
			if fresh.Status() != pm.PlanRunning {
				return nil
			}
			return s.materializeBlockedOn(txCtx, fresh)
		})
		if merr != nil && errFn != nil {
			errFn(p.ID(), fmt.Errorf("materialize blocked_on: %w", merr))
		}
		// I103 §2: run the deadline engine's on_timeout router over the just-materialized
		// snapshots (deadline-check → route reprobe/escalate/route-to-handler + record
		// probe). Like the materialize it runs in its OWN tx and is BEST-EFFORT: a router
		// failure NEVER rolls back the dispatch or the materialize, never sets firstErr,
		// and never aborts the sweep. The router is PROPOSE-ONLY — it touches only the
		// pm_plan_blocked_on store + the sink, so it can never release a gated node (I103
		// §5 P2). It is the authoritative pull back-stop for the push dispatch path.
		rerr := s.runInTx(ctx, func(txCtx context.Context) error {
			fresh, ferr := s.plans.FindByID(txCtx, p.ID())
			if ferr != nil {
				return ferr
			}
			if fresh.Status() != pm.PlanRunning {
				return nil
			}
			return s.routeTimeouts(txCtx, fresh)
		})
		if rerr != nil && errFn != nil {
			errFn(p.ID(), fmt.Errorf("route blocked_on timeouts: %w", rerr))
		}
	}
	return firstErr
}

// dispatchFindingsCap bounds how many findings ride a single node @mention so the
// dispatch message stays bounded as the shared context grows (ADR-0053 — no silent
// truncation: when capped, the block says so).
const dispatchFindingsCap = 20

// formatFindingsForDispatch renders a bounded window of a Plan's findings into the
// compact block that dispatchReadyNodes appends to each newly-dispatched node's
// @mention (DeLM shared context). `shown` is the already-bounded, oldest-first
// window (the repo's ListLatestByPlan); `total` is the full count, so when
// total > len(shown) the header says "latest N of M" (explicit truncation, §17).
// Returns "" for no findings (caller appends nothing).
func formatFindingsForDispatch(shown []*pm.PlanFinding, total int) string {
	if total == 0 || len(shown) == 0 {
		return ""
	}
	var b strings.Builder
	if total > len(shown) {
		fmt.Fprintf(&b, "Shared context — latest %d of %d findings recorded in this plan:\n", len(shown), total)
	} else {
		fmt.Fprintf(&b, "Shared context — %d finding(s) recorded in this plan so far:\n", total)
	}
	for _, f := range shown {
		fmt.Fprintf(&b, "- [%s] (%s) %s\n", f.Kind(), f.TaskID(), findingOneLine(f.Content()))
	}
	return strings.TrimRight(b.String(), "\n")
}

// findingOneLine collapses a finding's gist to a single bounded line for the
// dispatch bullet (whitespace runs → single space; long gists truncated with …).
// Truncation is on a RUNE boundary (review #1): a byte slice would split a
// multi-byte character (e.g. Chinese gists) and inject invalid UTF-8.
func findingOneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const lineCap = 240 // runes
	r := []rune(s)
	if len(r) > lineCap {
		return string(r[:lineCap]) + "…"
	}
	return s
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

// ResumePausedNode is the T53 OPERATOR recovery action: a project member (PD/owner)
// resumes a plan node whose agent paused its work item and went idle, leaving the
// node stuck (shown `paused` since T53 part A). Authz mirrors RerunFailedNode — the
// plan must be running and the actor a project member — plus the task must be a node
// of THIS plan (no foreign task id). The actual resume + agent wake is a cross-BC
// effect delegated to the NodeResumer port (agent service resume + env-control
// wake), so the pm BC stays decoupled. Errors: ErrNodeResumerUnavailable (port not
// wired), ErrPlanNotRunning, ErrTaskNotInPlan, ErrNodeNotPaused (nothing paused),
// or the resumer's error (e.g. the agent is busy on another item). Authz runs in a
// read; the port call runs outside the pm tx (it manages the agent BC's own tx).
func (s *Service) ResumePausedNode(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if s.nodeResumer == nil {
		return ErrNodeResumerUnavailable
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return err
	}
	if err := s.requireProjectMember(ctx, p.ProjectID(), actor); err != nil {
		return err
	}
	if p.Status() != pm.PlanRunning {
		return pm.ErrPlanNotRunning
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	if t.PlanID() != planID {
		return ErrTaskNotInPlan
	}
	return s.nodeResumer.ResumePausedNode(ctx, "pm://tasks/"+string(taskID))
}
