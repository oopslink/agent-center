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
	// can wake) the creator.
	content := fmt.Sprintf("task %q failed — its downstream is blocked pending resolution.", t.Title())
	_, perr := p.svc.planDispatcher.PostMention(txCtx, plan.ConversationID(), creator, content)
	return perr
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
