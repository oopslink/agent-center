package service

import (
	"context"
	"fmt"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

type ProgressWakeAttempt struct {
	PlanID          pm.PlanID
	OrganizationID  string
	OwnerRef        pm.IdentityRef
	Severity        pm.ProgressWakeSeverity
	Channel         string
	IdempotencyKey  string
	Capacity        int
	ReservedP0      int
	RefillPerMinute int
}

func (s *Service) acquireProgressFence(ctx context.Context, p *pm.Plan, ttl time.Duration) (pm.ProgressFence, bool, error) {
	fresh, err := s.plans.FindByID(ctx, p.ID())
	if err != nil {
		return pm.ProgressFence{}, false, err
	}
	p = fresh
	now := s.clock.Now().UTC()
	lease, ok, err := s.plans.AcquireProgressLease(ctx, p.ID(), progressLeaseScope(p.ID()), s.progressControllerID, now, ttl)
	if err != nil || !ok {
		return pm.ProgressFence{}, ok, err
	}
	if err := s.plans.RecordProgressWatchdogHeartbeat(ctx, p.ID(), "progress_reconciler", now); err != nil {
		return pm.ProgressFence{}, false, err
	}
	return pm.ProgressFence{PlanID: p.ID(), PlanRevision: p.Version(), HolderID: lease.HolderID, FencingToken: lease.FencingToken}, true, nil
}

func (s *Service) ReconcilePlanProgressWithFence(ctx context.Context, planID pm.PlanID, fence pm.ProgressFence) error {
	ok, err := s.plans.ValidateProgressFence(ctx, fence)
	if err != nil {
		return err
	}
	if !ok {
		now := s.clock.Now().UTC()
		v := pm.ObservationVector{PlanID: planID, Decision: pm.CannotDetermine, Quality: pm.ProgressQualitySuspect, AsOf: now, EvaluatedAt: now, SuspectKey: "lease_fence_conflict"}
		inc := progressIncident(v, pm.IncidentLeaseFenceConflict, stableID("pfact", string(planID), fence.HolderID, "lease_fence_conflict"), now)
		return s.plans.UpsertProgressIncident(context.Background(), inc)
	}
	return s.ReconcilePlanProgress(ctx, planID)
}

func (s *Service) ProgressWatchdogTick(ctx context.Context, silence time.Duration) error {
	if silence <= 0 {
		silence = 3 * time.Minute
	}
	now := s.clock.Now().UTC()
	rows, err := s.plans.ListStaleProgressWatchdogs(ctx, now.Add(-silence))
	if err != nil {
		return err
	}
	for _, row := range rows {
		v := pm.ObservationVector{PlanID: row.PlanID, Decision: pm.CannotDetermine, Quality: pm.ProgressQualitySuspect, AsOf: now, EvaluatedAt: now, SuspectKey: "watchdog_silent", Facts: []pm.ProgressFact{{ID: stableID("pfact", string(row.PlanID), row.Component, "watchdog_silent"), SourceKind: "pm_progress_watchdog_heartbeats", SourceID: row.Component, OccurredAt: row.LastSeenAt, ObservedAt: now, Revision: row.LastSeenAt.Format(time.RFC3339Nano), Summary: "watchdog_silent", Quality: pm.ProgressFactQualityUnknown, CannotAbsent: true}}}
		if err := s.plans.UpsertProgressIncident(ctx, progressIncident(v, pm.IncidentWatchdogSilent, v.Facts[0].ID, now)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RecordProgressWakeAttempt(ctx context.Context, a ProgressWakeAttempt) (pm.ProgressWakeBucketDiagnostic, error) {
	return s.recordProgressWakeAttempt(ctx, a, true)
}

func (s *Service) recordProgressWakeAttempt(ctx context.Context, a ProgressWakeAttempt, persist bool) (pm.ProgressWakeBucketDiagnostic, error) {
	now := s.clock.Now().UTC()
	if a.Capacity <= 0 {
		a.Capacity = 10
	}
	if a.RefillPerMinute <= 0 {
		a.RefillPerMinute = a.Capacity
	}
	if a.Severity == "" {
		a.Severity = pm.ProgressWakeSeverityDefault
	}
	if a.Channel == "" {
		a.Channel = "default"
	}
	keys := []string{"global", "org:" + a.OrganizationID, "severity:" + string(a.Severity), "channel:" + a.Channel}
	states := make([]pm.ProgressWakeBucketState, 0, len(keys))
	allowed, before := true, a.Capacity
	err := s.runInTx(ctx, func(tx context.Context) error {
		for _, key := range keys {
			st, found, err := s.plans.GetProgressWakeBucketState(tx, key)
			if err != nil {
				return err
			}
			if !found {
				st = pm.ProgressWakeBucketState{ScopeKey: key, Tokens: a.Capacity, Capacity: a.Capacity, RefillPerMinute: a.RefillPerMinute, LastRefillAt: now}
			}
			mins := int(now.Sub(st.LastRefillAt) / time.Minute)
			if mins > 0 {
				st.Tokens = min(a.Capacity, st.Tokens+mins*a.RefillPerMinute)
				st.LastRefillAt = st.LastRefillAt.Add(time.Duration(mins) * time.Minute)
			}
			floor := 0
			if a.Severity != pm.ProgressWakeSeverityP0 {
				floor = a.ReservedP0
			}
			if st.Tokens <= floor {
				allowed = false
			}
			before = min(before, st.Tokens)
			st.UpdatedAt = now
			states = append(states, st)
		}
		for _, st := range states {
			if allowed {
				st.Tokens--
			}
			if err := s.plans.UpsertProgressWakeBucketState(tx, st); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return pm.ProgressWakeBucketDiagnostic{}, err
	}
	reason := "delivered"
	after := before - 1
	if !allowed {
		reason = "token_bucket_suppressed"
		after = before
	}
	d := pm.ProgressWakeBucketDiagnostic{ID: stableID("pwake", string(a.PlanID), string(a.OwnerRef), a.IdempotencyKey), PlanID: a.PlanID, OrganizationID: a.OrganizationID, OwnerRef: a.OwnerRef, Severity: a.Severity, Allowed: allowed, Reason: reason, TokensBefore: before, TokensAfter: after, Capacity: a.Capacity, ReservedP0: a.ReservedP0, RefillPerMinute: a.RefillPerMinute, AttemptedAt: now, NextRefillAt: now.Add(time.Minute), EvidenceJSON: fmt.Sprintf(`{"idempotency_key":%q}`, a.IdempotencyKey)}
	if err := s.plans.UpsertProgressWakeBucketDiagnostic(ctx, d); err != nil {
		return pm.ProgressWakeBucketDiagnostic{}, err
	}
	if !allowed && persist {
		w := pm.ProgressSuppressedWake{ID: stableID("pswake", a.OrganizationID, string(a.OwnerRef), string(a.Severity), a.Channel), OrganizationID: a.OrganizationID, OwnerRef: a.OwnerRef, Severity: a.Severity, Channel: a.Channel, PlanIDs: []pm.PlanID{a.PlanID}, AttemptCount: 1, NextAttemptAt: d.NextRefillAt, CreatedAt: now, UpdatedAt: now}
		if err := s.plans.UpsertProgressSuppressedWake(ctx, w); err != nil {
			return pm.ProgressWakeBucketDiagnostic{}, err
		}
		v := pm.ObservationVector{PlanID: a.PlanID, Decision: pm.ResponsibilityBound, Quality: pm.ProgressQualitySuspect, AsOf: now, EvaluatedAt: now, SuspectKey: "wake_suppressed", Facts: []pm.ProgressFact{{ID: d.ID, SourceKind: "pm_progress_wake_bucket_diagnostics", SourceID: d.ID, OccurredAt: now, ObservedAt: now, Revision: d.ID, Summary: "wake_suppressed", Quality: pm.ProgressFactQualityUnknown, CannotAbsent: true}}}
		o := progressObligation(v, pm.ObligationAckWake, now)
		o.OwnerRef = a.OwnerRef
		o.OwnerDisplay = string(a.OwnerRef)
		if err := s.plans.UpsertProgressObligation(ctx, o); err != nil {
			return pm.ProgressWakeBucketDiagnostic{}, err
		}
	}
	return d, nil
}

func (s *Service) DrainProgressSuppressedWakes(ctx context.Context, limit int, deliver func(context.Context, pm.ProgressSuppressedWake) error) error {
	rows, err := s.plans.ListDueProgressSuppressedWakes(ctx, s.clock.Now().UTC(), limit)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if deliver == nil || deliver(ctx, row) != nil {
			continue
		}
		if err := s.plans.DeleteProgressSuppressedWake(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}

func progressLeaseScope(id pm.PlanID) string { return "progress_control:plan:" + string(id) }
