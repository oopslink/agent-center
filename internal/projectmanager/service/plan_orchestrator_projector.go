package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanOrchestratorProjector is the v2.9 P2-1 AUTO-ADVANCE core: it drains the
// outbox and advances a RUNNING Plan's DAG on events — no manual Advance click.
// It is a SIBLING of TaskStatusSyncProjector / PlanParticipantProjector (same
// projector contract: Name + Project consuming events in ONE tx + MarkApplied),
// registered on the SAME production relay (#266 — a defined-but-unregistered
// projector = events have no consumer in prod = auto-advance silently dead).
//
// It consumes TWO event types, both routed to the SAME idempotent dispatch core
// (Service.dispatchReadyNodes):
//
//   - pm.task.state_changed → load the task; if it belongs to a Plan AND that
//     Plan is running, re-dispatch the Plan's newly-ready nodes (a task reaching
//     a terminal state can unblock downstream nodes; calling dispatchReadyNodes
//     is safe/idempotent regardless of WHICH state it moved to). v2.9 P2-2: if
//     the task is now FAILED (TaskDiscarded), also @mention the Plan CREATOR in
//     the Plan conversation — the failure handler (§9.1/§9.7). The failed node's
//     downstream stays `blocked` (ComputePlanView) and the Plan does NOT
//     auto-terminate (MarkDone fires only when ALL nodes are done); the creator
//     notification is the ONLY new effect.
//   - pm.plan.started → dispatch the Plan's INITIAL ready nodes (those with no
//     upstream dependency).
//
// IDEMPOTENT + REPLAY/CONCURRENCY-safe on two layers: (a) the AppliedStore makes
// each event process-once per projector, and (b) the dispatch core's
// RecordDispatch is INSERT-OR-IGNORE on PK (plan_id, task_id), so even a
// redelivered / concurrent event @mentions each ready node EXACTLY once (§9.3).
//
// NO EVENT LOOP: dispatching posts a message (PostMention) which may emit
// conversation events, but those are NOT pm.task.state_changed and NOT
// pm.plan.started — so they never re-enter this projector. A task's own
// state_changed event is consumed once (AppliedStore), and re-dispatching a
// downstream node does not change THIS task's state, so there is no self-trigger.
type PlanOrchestratorProjector struct {
	db      *sql.DB
	svc     *Service
	applied outbox.AppliedStore
	clock   clock.Clock
}

// NewPlanOrchestratorProjector wires the projector. svc supplies the dispatch
// core (plans/tasks repos + planDispatcher) and the task repo lookup; applied
// dedups redelivery.
func NewPlanOrchestratorProjector(db *sql.DB, svc *Service, applied outbox.AppliedStore, clk clock.Clock) *PlanOrchestratorProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &PlanOrchestratorProjector{db: db, svc: svc, applied: applied, clock: clk}
}

// Name is the AppliedStore key (distinct from the sibling projectors so each
// independently tracks applied events on the shared relay).
func (p *PlanOrchestratorProjector) Name() string { return "pm-plan-orchestrator" }

// Project auto-advances a running Plan on a task-state change or plan-start.
// Irrelevant event types are a no-op. Events for a task not in a plan, or a plan
// that is not running, are a no-op + MarkApplied (don't re-process forever).
func (p *PlanOrchestratorProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case EvtTaskStateChanged, EvtPlanStarted:
		// handled below
	default:
		return nil
	}
	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		// Same-tx idempotency: if already applied, this event is done.
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := p.advance(txCtx, e); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// advance resolves the target Plan from the event, loads it, and (when it is
// running) drives the dispatch core. Resolving to no plan / a non-running plan
// is a no-op (the caller still MarkApplied so the event is not retried forever).
//
// v2.9 P2-2: for a pm.task.state_changed event whose task has transitioned to
// FAILED, it ALSO @mentions the Plan creator in the Plan conversation BEFORE
// dispatching — the failure handler (§9.1/§9.7). The notify + the dispatch share
// THIS event's one tx, so the AppliedStore dedups both: a redelivered failure
// event short-circuits (IsApplied) and never re-notifies. The dispatch core is
// still called so INDEPENDENT branches keep advancing (a failed node merely
// leaves its own downstream `blocked`, §9.7).
func (p *PlanOrchestratorProjector) advance(txCtx context.Context, e outbox.Event) error {
	planID, ok, err := p.targetPlan(txCtx, e)
	if err != nil {
		return err
	}
	if !ok {
		return nil // event not tied to a plan (e.g. a backlog task's state change)
	}
	plan, err := p.svc.plans.FindByID(txCtx, planID)
	if err != nil {
		return err
	}
	if plan.Status() != pm.PlanRunning {
		// Draft/done plan → nothing to auto-advance (no-op, idempotent).
		return nil
	}
	// P2-2 failure handling: a plan-task that just transitioned to FAILED notifies
	// the creator once (per-event, deduped by the AppliedStore around this tx).
	if err := p.notifyCreatorOnFailure(txCtx, e, plan); err != nil {
		return err
	}
	// B1 (control-flow): if THIS event is a decision node completing with an outcome
	// that fires a loopback edge, re-activate the loop subgraph BEFORE dispatch (so the
	// reopened nodes re-dispatch in this same pass). No-op for non-decision/non-loopback
	// completions and for pure DAG plans (back-compat).
	//
	// T805 ③: a GRAPHED plan drives its loopback through the engine in
	// driveGraphDecisions (bounded countReopens + task mirror), so the task-level
	// applyLoopbacks is gated OFF for it — production loopback goes ONLY through the
	// engine. A legacy (non-graphed) plan keeps this proven task-level driver unchanged.
	if e.EventType == EvtTaskStateChanged && !p.svc.graphDispatchEnabled(plan) {
		var pl taskEventPayload
		if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
			return err
		}
		if err := p.svc.applyLoopbacks(txCtx, plan, pm.TaskID(pl.TaskID)); err != nil {
			return err
		}
	}
	_, err = p.svc.dispatchReadyNodes(txCtx, plan)
	return err
}

// notifyCreatorOnFailure implements the v2.9 P2-2 failure handler: when THIS
// pm.task.state_changed event reports a →FAILED TRANSITION (the task's prev
// status was NOT failed AND it is now FAILED — TaskDiscarded, the same taskIsFailed
// semantics ComputePlanView uses, §9.2/§9.7), it posts an @mention of the Plan
// CREATOR into the Plan conversation noting the task failed and its downstream is
// blocked pending resolution. The creator is already a plan-conversation
// participant (#284 additive sync), so the mention reaches them (an agent creator
// self-handles; a human handles — design decision 1).
//
// TRANSITION-GUARDED (v2.9 fast-follow): the guard is the →failed TRANSITION, not
// the CURRENT-failed status. Gating on current-failed alone re-notified whenever a
// task that was ALREADY failed emitted another state_changed (re-discarding an
// already-discarded task) — the re-discard edge this closes. PrevStatus rides the
// event; we notify ONLY if prev-not-failed AND now-failed. An empty PrevStatus
// (old events / non-transition emits) is the zero TaskStatus → not-failed, so a
// genuine first failure still notifies and only a real already-failed prev is
// skipped.
//
// IDEMPOTENCY: notify ONCE per failure event. The AppliedStore (in Project's
// enclosing tx) makes each pm.task.state_changed processed exactly once per
// projector, so notifying on THIS event's →failed transition = once per the failed
// transition. A replay of the same event short-circuits at IsApplied before
// advance runs → no re-notify. Unrelated events (a sibling task completing) carry
// a different, non-failed task → no scan-and-renotify here.
//
// It is a no-op for non-task events (pm.plan.started) and for tasks that did NOT
// transition to failed (the common advance path, incl. a re-discard whose prev is
// already failed).
//
// v2.9 P3 (failure→agent-creator-wake): after the @mention, when the creator is an
// AGENT it ALSO emits EvtPlanCreatorFailureWake (same tx) so the WakeProjector can
// DIRECTLY wake the agent-creator (a system @mention can never wake an agent — #220
// / #185). For a HUMAN creator nothing extra is emitted. See the emit block below.
func (p *PlanOrchestratorProjector) notifyCreatorOnFailure(txCtx context.Context, e outbox.Event, plan *pm.Plan) error {
	if e.EventType != EvtTaskStateChanged {
		return nil
	}
	var pl taskEventPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if strings.TrimSpace(pl.TaskID) == "" {
		return nil
	}
	t, err := p.svc.tasks.FindByID(txCtx, pm.TaskID(pl.TaskID))
	if err != nil {
		return err
	}
	// Notify ONLY on the →failed TRANSITION: prev was NOT failed AND now IS failed.
	// Gating on the CURRENT status alone re-notified whenever a task was re-discarded
	// while already failed (an already-failed task emitting another state_changed) —
	// the re-discard edge this closes. PrevStatus rides the event (captured by the
	// producer before the AR transition). When PrevStatus is empty (old events /
	// non-transition emits) it is the zero TaskStatus, which TaskIsFailed reports as
	// not-failed — so a genuine first failure still notifies, while a re-discard
	// (prev_status=discarded, already failed) is correctly skipped.
	prevFailed := pm.TaskIsFailed(pm.TaskStatus(pl.PrevStatus))
	if prevFailed || !pm.TaskIsFailed(t.Status()) {
		return nil // not a →failed transition (already failed, or not failed) → nothing to notify.
	}
	if p.svc.planDispatcher == nil {
		return ErrDispatcherUnavailable
	}
	creator := string(plan.CreatorRef())
	// content is the BODY only — the dispatcher resolves creator → display_name and
	// prepends "@<display_name> " so the failed-task notice actually @mentions (and
	// can wake) the creator. Per @oopslink: name the task by its id (T<n>) so the
	// plan-conversation reminder is unambiguous; fall back to title-only when unallocated.
	var content string
	if ref := taskRefToken(t); ref != "" {
		content = fmt.Sprintf("task %s %q failed — its downstream is blocked pending resolution.", ref, t.Title())
	} else {
		content = fmt.Sprintf("task %q failed — its downstream is blocked pending resolution.", t.Title())
	}
	msgID, perr := p.svc.planDispatcher.PostMention(txCtx, plan.ConversationID(), creator, content)
	if perr != nil {
		return perr
	}
	// v2.9 P3 (failure→agent-creator-wake, §9.1 / decision-1): when the creator is an
	// AGENT, the @mention alone can NEVER wake it — the WakeProjector (#220) wakes
	// agents ONLY on human (`user:`) senders (v2.7 #185 loop-break: a system/agent
	// message never wakes an agent), and this @mention is SYSTEM-authored. So emit a
	// sanctioned DIRECT wake-trigger event IN THIS SAME TX (deduped by the enclosing
	// Project tx's AppliedStore around the →failed transition event): the
	// (production-registered) WakeProjector consumes it → enqueues an agent.converse
	// for the agent-creator pointing at the plan conversation → the agent wakes, reads
	// THIS failure @mention, and self-handles via the Stage C MCP plan tools.
	//
	// AGENT-ONLY: for a HUMAN creator we do NOT emit — the @mention in the conversation
	// IS their notification (a human reading the plan conversation needs no system
	// wake). The agent scheme is the "agent:" prefix (ADR-0033 identity vocabulary).
	//
	// IDEMPOTENT: this fires once per failure-transition event. The enclosing Project
	// tx's AppliedStore makes the triggering EvtTaskStateChanged process-once per
	// projector, and the →failed transition guard above means we reach here once per
	// →failed transition. The MessageID (the failure @mention's id) rides the payload
	// as the WakeProjector's converse idempotency anchor, so even a redelivered wake
	// event never double-wakes the creator.
	//
	// LOOP-SAFE (does NOT widen #185): this is a one-shot system→agent wake on a
	// DETERMINED creator for a DETERMINED failure transition — NOT a chat agent→agent
	// reply. The woken creator READS the plan conversation and acts via MCP tools;
	// that reading/acting does NOT re-emit EvtPlanCreatorFailureWake (only a NEW
	// task→failed transition does) → no wake loop.
	if !strings.HasPrefix(creator, "agent:") {
		return nil
	}
	orgID := ""
	if proj, perr := p.svc.projects.FindByID(txCtx, plan.ProjectID()); perr == nil && proj != nil {
		orgID = proj.OrganizationID() // diagnostic context only; absence is non-fatal.
	}
	return p.svc.emit(txCtx, EvtPlanCreatorFailureWake,
		refsJSON(map[string]string{
			"plan_id":         string(plan.ID()),
			"project_id":      string(plan.ProjectID()),
			"conversation_id": plan.ConversationID(),
		}),
		planCreatorFailureWakePayload{
			CreatorRef:     creator,
			ConversationID: plan.ConversationID(),
			MessageID:      msgID,
			PlanID:         string(plan.ID()),
			TaskID:         string(t.ID()),
			OrganizationID: orgID,
		})
}

// targetPlan extracts the Plan id this event should advance. For
// pm.plan.started it is the payload's plan_id. For pm.task.state_changed it is
// the changed task's plan_id (loaded via the task repo) — empty when the task is
// not in a plan (→ ok=false, no-op).
func (p *PlanOrchestratorProjector) targetPlan(txCtx context.Context, e outbox.Event) (pm.PlanID, bool, error) {
	switch e.EventType {
	case EvtPlanStarted:
		var pl planEventPayload
		if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(pl.PlanID) == "" {
			return "", false, nil
		}
		return pm.PlanID(pl.PlanID), true, nil
	case EvtTaskStateChanged:
		var pl taskEventPayload
		if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(pl.TaskID) == "" {
			return "", false, nil
		}
		t, err := p.svc.tasks.FindByID(txCtx, pm.TaskID(pl.TaskID))
		if err != nil {
			return "", false, err
		}
		if t.PlanID() == "" {
			return "", false, nil // backlog task — not part of any plan
		}
		return t.PlanID(), true, nil
	default:
		return "", false, nil
	}
}

var _ outbox.Projector = (*PlanOrchestratorProjector)(nil)
