package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

const (
	defaultHoldAckDeadline = 15 * time.Minute
	defaultMaxHoldDuration = 4 * time.Hour
)

func (s *Service) ensureProgressHoldForBlockedOn(ctx context.Context, p *pm.Plan, b pm.BlockedOn) error {
	if s.progress == nil || p == nil {
		return nil
	}
	now := s.clock.Now()
	owner := string(p.CreatorRef())
	if strings.TrimSpace(owner) == "" {
		owner = "role:operational-owner"
	}
	hold := pm.ProgressHold{
		ID:               s.id("hold"),
		PlanID:           p.ID(),
		TaskID:           b.TaskID,
		NodeID:           b.NodeID,
		ReasonKind:       "blocked_on",
		ReasonID:         string(b.WaitType) + ":" + strings.Join(b.WaitKeys, ","),
		OwnerRef:         owner,
		OwnerDisplay:     owner,
		EnteredAt:        b.WaitedSince,
		HoldAckDeadline:  now.Add(defaultHoldAckDeadline),
		MaxHoldDuration:  defaultMaxHoldDuration,
		EscalationLevel:  0,
		NextEscalationAt: now.Add(defaultHoldAckDeadline),
		BlocksDispatch:   true,
		BlocksAcceptance: true,
		BlocksCompletion: true,
	}
	if _, err := s.progress.UpsertHold(ctx, hold); err != nil {
		return err
	}
	if b.WaitType != pm.WaitHumanDecision {
		return nil
	}
	deadline := b.Deadline
	if deadline.IsZero() {
		deadline = now.Add(defaultMaxHoldDuration)
	}
	// A notification is not resolution. Materialize the named owner's durable,
	// decision-fact-bound responsibility in the same transaction as the hold.
	_, err := s.progress.UpsertObligation(ctx, pm.ProgressObligation{
		ID: "human:" + string(p.ID()) + ":" + string(b.TaskID), PlanID: p.ID(), TaskID: b.TaskID, NodeID: b.NodeID,
		Kind: pm.ObligationHumanDecision, OwnerRef: pm.IdentityRef(owner), OwnerDisplay: owner,
		DeadlineAt: deadline, AckRequired: true, EscalateToRef: "role:operational-owner",
		EscalationDeadlineAt: deadline, SourceFactRefs: []string{"blocked_on:" + string(b.WaitType) + ":" + strings.Join(b.WaitKeys, ",")},
		Status: pm.ResponsibilityOpen, CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	return err
}

// WaitHumanDecisionForPrerequisite records the only legal form of “continue
// waiting”: an explicit prerequisite fact, automatic re-evaluation subscription
// and next deadline. It deliberately leaves the owner human_decision obligation
// open until the subscribed fact produces a Decision fact.
func (s *Service) WaitHumanDecisionForPrerequisite(ctx context.Context, planID pm.PlanID, decisionTaskID, prerequisiteTaskID pm.TaskID, owner pm.IdentityRef, reasonFactRef string, nextDeadline time.Time) error {
	if s.progress == nil || planID == "" || decisionTaskID == "" || prerequisiteTaskID == "" || owner == "" || strings.TrimSpace(reasonFactRef) == "" || nextDeadline.IsZero() {
		return fmt.Errorf("projectmanager: prerequisite wait requires plan, decision, prerequisite, named owner, reason fact and next deadline")
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		_, err := s.progress.UpsertPrerequisiteSubscription(txCtx, pm.ProgressPrerequisiteSubscription{
			ID:     "prereq:" + string(planID) + ":" + string(decisionTaskID) + ":" + string(prerequisiteTaskID),
			PlanID: planID, DecisionTaskID: decisionTaskID, PrerequisiteTaskID: prerequisiteTaskID,
			OwnerRef: owner, NextDeadlineAt: nextDeadline, Action: "unblock_resume", ReasonFactRef: reasonFactRef,
			Status: pm.ResponsibilityOpen, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		_, err = s.progress.UpsertObligation(txCtx, pm.ProgressObligation{
			ID: "wait:" + string(planID) + ":" + string(decisionTaskID), PlanID: planID, TaskID: decisionTaskID,
			Kind: pm.ObligationSourceRecovery, OwnerRef: owner, OwnerDisplay: string(owner), DeadlineAt: nextDeadline,
			AckRequired: true, SourceFactRefs: []string{reasonFactRef, "prerequisite_task:" + string(prerequisiteTaskID), "subscription:automatic"},
			Status: pm.ResponsibilityOpen, CreatedAt: now, UpdatedAt: now, Version: 1,
		})
		return err
	})
}

func (s *Service) recordProgressWakeRequested(ctx context.Context, p *pm.Plan, b pm.BlockedOn) error {
	if s.progress == nil || p == nil {
		return nil
	}
	now := s.clock.Now()
	key := fmt.Sprintf("blocked_on:%s:%s:%s:%s", p.ID(), b.TaskID, b.WaitType, strings.Join(b.WaitKeys, ","))
	w := pm.ProgressWake{
		ID:                   s.id("wake"),
		PlanID:               p.ID(),
		TaskID:               b.TaskID,
		NodeID:               b.NodeID,
		OwnerRef:             p.CreatorRef(),
		OwnerDisplay:         string(p.CreatorRef()),
		Reason:               b.TriggerCondition,
		Status:               pm.ProgressWakeRequested,
		IdempotencyKey:       key,
		RequestedAt:          now,
		DeliveredAt:          now,
		AckDeadline:          now.Add(defaultHoldAckDeadline),
		MaxHoldDuration:      defaultMaxHoldDuration,
		EscalationLevel:      0,
		NextEscalationAt:     now.Add(defaultHoldAckDeadline),
		OrganizationOwnerRef: "role:operational-owner",
	}
	created, err := s.progress.RecordWake(ctx, w)
	if err != nil {
		return err
	}
	if created {
		return s.progress.MarkWakeDelivered(ctx, w.ID, now)
	}
	return nil
}

func (s *Service) AcknowledgeProgressWake(ctx context.Context, wakeID string, actor pm.IdentityRef) error {
	if s.progress == nil {
		return nil
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		ok, err := s.progress.AcknowledgeWake(txCtx, wakeID, actor, now, pm.ProgressWakeAcknowledged+":"+wakeID)
		if err != nil || !ok {
			return err
		}
		return nil
	})
}

func (s *Service) RecordProgressDecision(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef, decisionID string) error {
	if s.progress == nil {
		return nil
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		factRef := pm.ProgressDecisionRecorded + ":" + decisionID
		if _, err := s.progress.ReleaseHoldsByFact(txCtx, planID, taskID, actor, factRef, now); err != nil {
			return err
		}
		_, err := s.progress.ResolveOpenObligationsByFact(txCtx, planID, taskID, actor, factRef, now)
		return err
	})
}

func (s *Service) ReconcileProgressControl(ctx context.Context, limit int) error {
	if s.progress == nil {
		return nil
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		// Second clock: a satisfied prerequisite is an authoritative fact. Consume
		// it without relying on the owner to remember to return, and atomically move
		// the blocked projection away from human_decision.
		subs, err := s.progress.ListOpenPrerequisiteSubscriptions(txCtx, now, limit)
		if err != nil {
			return err
		}
		for _, sub := range subs {
			prerequisite, err := s.tasks.FindByID(txCtx, sub.PrerequisiteTaskID)
			if err != nil {
				return err
			}
			if !prerequisite.Status().IsTerminal() {
				continue
			}
			factRef := "prerequisite_satisfied:" + string(sub.PrerequisiteTaskID)
			if _, err := s.progress.ReleaseHoldsByFact(txCtx, sub.PlanID, sub.DecisionTaskID, sub.OwnerRef, factRef, now); err != nil {
				return err
			}
			if _, err := s.progress.ResolveOpenObligationsByFact(txCtx, sub.PlanID, sub.DecisionTaskID, sub.OwnerRef, factRef, now); err != nil {
				return err
			}
			if err := s.plans.RecordDecisionOutcome(txCtx, sub.PlanID, sub.DecisionTaskID, "pass", now); err != nil {
				return err
			}
			if _, err := s.progress.ResolvePrerequisiteSubscription(txCtx, sub.ID, factRef, now); err != nil {
				return err
			}
			p, err := s.plans.FindByID(txCtx, sub.PlanID)
			if err != nil {
				return err
			}
			if err := s.materializeBlockedOn(txCtx, p); err != nil {
				return err
			}
		}
		wakes, err := s.progress.ListExpiredUnackedWakes(txCtx, now, limit)
		if err != nil {
			return err
		}
		for _, w := range wakes {
			obligationID := "obl:" + w.ID
			_, err = s.progress.UpsertObligation(txCtx, pm.ProgressObligation{
				ID: obligationID, PlanID: w.PlanID, TaskID: w.TaskID, NodeID: w.NodeID,
				Kind: pm.ProgressObligationAckWake, OwnerRef: w.OwnerRef, OwnerDisplay: w.OwnerDisplay,
				DeadlineAt: w.AckDeadline, AckRequired: true, EscalateToRef: pm.IdentityRef(w.OrganizationOwnerRef),
				EscalationDeadlineAt: w.NextEscalationAt, SourceFactRefs: []string{w.ID}, Status: "open",
				CreatedAt: now, UpdatedAt: now, Version: 1,
			})
			if err != nil {
				return err
			}
			_, err = s.progress.UpsertIncident(txCtx, pm.ProgressIncident{
				ID: s.id("inc"), PlanID: w.PlanID, TaskID: w.TaskID, NodeID: w.NodeID,
				Kind: pm.ProgressIncidentOperational, Severity: "operational", OwnerRef: pm.IdentityRef(w.OrganizationOwnerRef),
				OwnerDisplay: w.OrganizationOwnerRef, Summary: "wake ack deadline missed; notification is not resolution",
				SourceRef: w.ID, Status: "open", CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				return err
			}
			_, err = s.progress.UpsertHold(txCtx, pm.ProgressHold{
				ID: s.id("hold"), PlanID: w.PlanID, TaskID: w.TaskID, NodeID: w.NodeID,
				ReasonKind: string(pm.ProgressObligationAckWake), ReasonID: obligationID,
				OwnerRef: string(w.OwnerRef), OwnerDisplay: w.OwnerDisplay, EnteredAt: w.RequestedAt,
				HoldAckDeadline: w.AckDeadline, MaxHoldDuration: w.MaxHoldDuration,
				EscalationLevel: w.EscalationLevel + 1, NextEscalationAt: w.NextEscalationAt.Add(defaultHoldAckDeadline),
				BlocksDispatch: true, BlocksAcceptance: true, BlocksCompletion: true,
			})
			if err != nil {
				return err
			}
		}
		due, err := s.progress.ListDueHolds(txCtx, now, limit)
		if err != nil {
			return err
		}
		for _, h := range due {
			_, err = s.progress.RecordEscalation(txCtx, pm.ProgressEscalation{
				ID: s.id("esc"), PlanID: h.PlanID, TaskID: h.TaskID, NodeID: h.NodeID,
				HoldID: h.ID, Kind: pm.ProgressEscalationRaised, Severity: "operational",
				EscalateToRef: h.OwnerRef, DeadlineAt: h.NextEscalationAt, CreatedAt: now,
			})
			if err != nil {
				return err
			}
		}
		breached, err := s.progress.ListBreachedHolds(txCtx, now, limit)
		if err != nil {
			return err
		}
		for _, h := range breached {
			_, err = s.progress.UpsertIncident(txCtx, pm.ProgressIncident{
				ID: s.id("inc"), PlanID: h.PlanID, TaskID: h.TaskID, NodeID: h.NodeID,
				Kind: pm.ProgressIncidentHoldSLOBreach, Severity: "P0", OwnerRef: pm.IdentityRef(h.OwnerRef),
				OwnerDisplay: h.OwnerDisplay, Summary: "progress_hold max duration breached",
				SourceRef: h.ID, Status: "open", CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) guardPlanProgressHolds(ctx context.Context, planID pm.PlanID, dispatch, acceptance, completion bool) error {
	if s.progress == nil {
		return nil
	}
	holds, err := s.progress.ListOpenHoldsByPlan(ctx, planID)
	if err != nil {
		return err
	}
	return progressHoldBlocks(holds, dispatch, acceptance, completion)
}

func (s *Service) guardTaskProgressHolds(ctx context.Context, taskID pm.TaskID, dispatch, acceptance, completion bool) error {
	if s.progress == nil {
		return nil
	}
	holds, err := s.progress.ListOpenHoldsByTask(ctx, taskID)
	if err != nil {
		return err
	}
	return progressHoldBlocks(holds, dispatch, acceptance, completion)
}

func progressHoldBlocks(holds []pm.ProgressHold, dispatch, acceptance, completion bool) error {
	for _, h := range holds {
		if (dispatch && h.BlocksDispatch) || (acceptance && h.BlocksAcceptance) || (completion && h.BlocksCompletion) {
			return fmt.Errorf("%w: %s", pm.ErrProgressHoldOpen, h.ID)
		}
	}
	return nil
}

func (s *Service) id(prefix string) string {
	if s.idgen != nil {
		return s.idgen.NewEntityID(prefix)
	}
	return fmt.Sprintf("%s-%d", prefix, s.clock.Now().UnixNano())
}
