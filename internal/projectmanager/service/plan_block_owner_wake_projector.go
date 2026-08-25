package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanBlockOwnerWakeProjector is the production consumer for pm.plan.block_owner_wake.
// It sends the initial owner @mention and performs durable reminder/escalation scans
// from pm_plan_block_events, so replay/restart cannot duplicate notifications.
type PlanBlockOwnerWakeProjector struct {
	db         *sql.DB
	plans      pm.PlanRepository
	members    pm.ProjectMemberRepository
	dispatcher PlanDispatcher
	applied    outbox.AppliedStore
	clock      clock.Clock
}

func NewPlanBlockOwnerWakeProjector(db *sql.DB, plans pm.PlanRepository, members pm.ProjectMemberRepository, dispatcher PlanDispatcher, applied outbox.AppliedStore, clk clock.Clock) *PlanBlockOwnerWakeProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &PlanBlockOwnerWakeProjector{db: db, plans: plans, members: members, dispatcher: dispatcher, applied: applied, clock: clk}
}

func (p *PlanBlockOwnerWakeProjector) Name() string { return "pm-plan-block-owner-wake" }

func (p *PlanBlockOwnerWakeProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != EvtPlanBlockOwnerWake {
		return nil
	}
	if p.db == nil || p.plans == nil || p.members == nil || p.dispatcher == nil || p.applied == nil {
		return nil
	}
	now := p.clock.Now()
	var ev *pm.PlanBlockEvent
	var phase planBlockNotifyPhase
	if err := persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		eventID, ok := eventIDFromRefs(e.Refs)
		if !ok {
			return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
		}
		fresh, found, err := p.plans.FindPlanBlockEventByID(txCtx, eventID)
		if err != nil {
			return err
		}
		if !found || !fresh.Active || !fresh.Effective || !fresh.ResolvedAt.IsZero() {
			return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
		}
		if fresh.NotificationState == pm.PlanBlockNotifyPending || fresh.NotificationState == pm.PlanBlockNotifyFailed {
			phase = planBlockNotifyInitial
			if err := p.claimNotify(txCtx, fresh.EventID, phase, now); err != nil {
				return err
			}
			ev = fresh
			return nil
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	}); err != nil {
		return err
	}
	if ev == nil || phase == "" {
		return nil
	}
	if err := p.sendClaimed(ctx, ev, phase, now); err != nil {
		return err
	}
	return p.applied.MarkApplied(ctx, p.Name(), e.ID, now)
}

// Tick scans active block events and advances due notification phases. It is safe
// to run on every outbox pump tick and after restart; DB state is the idempotency key.
func (p *PlanBlockOwnerWakeProjector) Tick(ctx context.Context) error {
	if p.db == nil || p.plans == nil || p.members == nil || p.dispatcher == nil {
		return nil
	}
	events, err := p.plans.ListActivePlanBlockEventsForNotification(ctx)
	if err != nil {
		return err
	}
	now := p.clock.Now()
	for _, ev := range events {
		if phase, ok := p.duePhase(ctx, ev, now); ok {
			claimed := false
			if err := persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
				fresh, found, err := p.plans.FindPlanBlockEventByID(txCtx, ev.EventID)
				if err != nil || !found {
					return err
				}
				if !fresh.Active || !fresh.Effective || !fresh.ResolvedAt.IsZero() {
					return nil
				}
				if phase2, ok := p.duePhase(txCtx, *fresh, now); ok && phase2 == phase {
					if err := p.claimNotify(txCtx, fresh.EventID, phase, now); err != nil {
						return err
					}
					claimed = true
				}
				return nil
			}); err != nil {
				return err
			}
			if claimed {
				if err := p.sendClaimed(ctx, &ev, phase, now); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type planBlockNotifyPhase string

const (
	planBlockNotifyInitial    planBlockNotifyPhase = "initial"
	planBlockNotifyReminder   planBlockNotifyPhase = "reminder"
	planBlockNotifyEscalation planBlockNotifyPhase = "escalation"
)

func (p *PlanBlockOwnerWakeProjector) duePhase(ctx context.Context, ev pm.PlanBlockEvent, now time.Time) (planBlockNotifyPhase, bool) {
	plan, err := p.plans.FindByID(ctx, ev.PlanID)
	if err != nil || plan == nil {
		return "", false
	}
	policy := plan.RecoveryPolicy()
	escalateAt := ev.BlockedAt.Add(time.Duration(policy.EscalateAfterSeconds) * time.Second)
	remindAt := ev.BlockedAt.Add(time.Duration(policy.RemindAfterSeconds) * time.Second)
	switch ev.NotificationState {
	case pm.PlanBlockNotifyPending, pm.PlanBlockNotifyFailed:
		return planBlockNotifyInitial, true
	case pm.PlanBlockNotifySent, pm.PlanBlockNotifyReminded, pm.PlanBlockNotifyReminderFailed, pm.PlanBlockNotifyEscalationFailed:
		if policy.EscalateAfterSeconds > 0 && !now.Before(escalateAt) {
			return planBlockNotifyEscalation, true
		}
		if ev.NotificationState != pm.PlanBlockNotifyReminded && policy.RemindAfterSeconds > 0 && !now.Before(remindAt) {
			return planBlockNotifyReminder, true
		}
	}
	return "", false
}

func (p *PlanBlockOwnerWakeProjector) claimNotify(ctx context.Context, eventID pm.PlanBlockEventID, phase planBlockNotifyPhase, now time.Time) error {
	return p.plans.UpdatePlanBlockEventNotification(ctx, eventID, sendingState(phase), now)
}

func (p *PlanBlockOwnerWakeProjector) sendClaimed(ctx context.Context, ev *pm.PlanBlockEvent, phase planBlockNotifyPhase, now time.Time) error {
	plan, err := p.plans.FindByID(ctx, ev.PlanID)
	if err != nil {
		return err
	}
	target, ok, err := p.target(ctx, plan, phase, now)
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(plan.ConversationID()) == "" {
		state := failureState(phase)
		if uerr := p.plans.UpdatePlanBlockEventNotification(ctx, ev.EventID, state, now); uerr != nil {
			return uerr
		}
		slog.Warn("plan block owner notification has no valid target",
			"event_id", ev.EventID, "plan_id", ev.PlanID, "phase", phase)
		return nil
	}
	if _, err := p.dispatcher.PostMention(ctx, plan.ConversationID(), string(target), p.message(ev, phase)); err != nil {
		state := failureState(phase)
		if uerr := p.plans.UpdatePlanBlockEventNotification(ctx, ev.EventID, state, now); uerr != nil {
			return uerr
		}
		slog.Warn("plan block owner notification failed",
			"event_id", ev.EventID, "plan_id", ev.PlanID, "phase", phase, "err", err)
		return nil
	}
	return p.plans.UpdatePlanBlockEventNotification(ctx, ev.EventID, successState(phase), now)
}

func sendingState(phase planBlockNotifyPhase) pm.PlanBlockNotificationState {
	switch phase {
	case planBlockNotifyReminder:
		return pm.PlanBlockNotifyReminderSending
	case planBlockNotifyEscalation:
		return pm.PlanBlockNotifyEscalationSending
	default:
		return pm.PlanBlockNotifySending
	}
}

func (p *PlanBlockOwnerWakeProjector) target(ctx context.Context, plan *pm.Plan, phase planBlockNotifyPhase, now time.Time) (pm.IdentityRef, bool, error) {
	if phase == planBlockNotifyEscalation {
		if ref, ok, err := p.validEscalationTarget(ctx, plan); err != nil || ok {
			return ref, ok, err
		}
	}
	if ok, err := p.isMember(ctx, plan.ProjectID(), plan.OwnerRef()); err != nil || ok {
		return plan.OwnerRef(), ok, err
	}
	if b := plan.BackupOwnerRef(); b != "" {
		if ok, err := p.isMember(ctx, plan.ProjectID(), b); err != nil {
			return "", false, err
		} else if ok {
			plan.SetOwner(b, "", now)
			plan.SetVersion(plan.Version()+1, now)
			return b, true, p.plans.Update(ctx, plan)
		}
	}
	if owner, ok, err := p.projectOwner(ctx, plan.ProjectID()); err != nil || !ok {
		return "", false, err
	} else {
		plan.SetOwner(owner, "", now)
		plan.SetVersion(plan.Version()+1, now)
		return owner, true, p.plans.Update(ctx, plan)
	}
}

func (p *PlanBlockOwnerWakeProjector) validEscalationTarget(ctx context.Context, plan *pm.Plan) (pm.IdentityRef, bool, error) {
	if b := plan.BackupOwnerRef(); b != "" {
		if ok, err := p.isMember(ctx, plan.ProjectID(), b); err != nil || ok {
			return b, ok, err
		}
	}
	return p.projectOwner(ctx, plan.ProjectID())
}

func (p *PlanBlockOwnerWakeProjector) isMember(ctx context.Context, projectID pm.ProjectID, ref pm.IdentityRef) (bool, error) {
	if ref == "" {
		return false, nil
	}
	_, err := p.members.FindByProjectAndIdentity(ctx, projectID, ref)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pm.ErrMemberNotFound) {
		return false, nil
	}
	return false, err
}

func (p *PlanBlockOwnerWakeProjector) projectOwner(ctx context.Context, projectID pm.ProjectID) (pm.IdentityRef, bool, error) {
	members, err := p.members.ListByProject(ctx, projectID)
	if err != nil {
		return "", false, err
	}
	for _, m := range members {
		if m.Role() == pm.RoleOwner {
			return m.IdentityID(), true, nil
		}
	}
	return "", false, nil
}

func (p *PlanBlockOwnerWakeProjector) message(ev *pm.PlanBlockEvent, phase planBlockNotifyPhase) string {
	base := fmt.Sprintf("plan attention required: task pm://tasks/%s is blocked [%s]: %s", ev.TaskID, ev.ReasonType, ev.BlockedReason)
	switch phase {
	case planBlockNotifyReminder:
		return "reminder: " + base
	case planBlockNotifyEscalation:
		return "escalation: " + base
	default:
		return base
	}
}

func successState(phase planBlockNotifyPhase) pm.PlanBlockNotificationState {
	switch phase {
	case planBlockNotifyReminder:
		return pm.PlanBlockNotifyReminded
	case planBlockNotifyEscalation:
		return pm.PlanBlockNotifyEscalated
	default:
		return pm.PlanBlockNotifySent
	}
}

func failureState(phase planBlockNotifyPhase) pm.PlanBlockNotificationState {
	switch phase {
	case planBlockNotifyReminder:
		return pm.PlanBlockNotifyReminderFailed
	case planBlockNotifyEscalation:
		return pm.PlanBlockNotifyEscalationFailed
	default:
		return pm.PlanBlockNotifyFailed
	}
}

func eventIDFromRefs(refs string) (pm.PlanBlockEventID, bool) {
	var m map[string]string
	if err := json.Unmarshal([]byte(refs), &m); err != nil {
		return "", false
	}
	id := strings.TrimSpace(m["event_id"])
	if id == "" {
		return "", false
	}
	return pm.PlanBlockEventID(id), true
}
