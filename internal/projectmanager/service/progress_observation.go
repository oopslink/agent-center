package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

type progressReconcileOptions struct {
	WatermarkLagSLA  time.Duration
	SourceGraceCycle int
	SuspectMaxCycles int
	SourceWatermark  time.Time
}

func defaultProgressReconcileOptions() progressReconcileOptions {
	return progressReconcileOptions{
		WatermarkLagSLA:  2 * time.Minute,
		SourceGraceCycle: 1,
		SuspectMaxCycles: 2,
	}
}

func (s *Service) ReconcilePlanProgress(ctx context.Context, planID pm.PlanID) error {
	return s.reconcilePlanProgress(ctx, planID, defaultProgressReconcileOptions())
}

func (s *Service) reconcilePlanProgress(ctx context.Context, planID pm.PlanID, opt progressReconcileOptions) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now().UTC()
	if opt.WatermarkLagSLA <= 0 {
		opt.WatermarkLagSLA = defaultProgressReconcileOptions().WatermarkLagSLA
	}
	if opt.SourceGraceCycle <= 0 {
		opt.SourceGraceCycle = defaultProgressReconcileOptions().SourceGraceCycle
	}
	if opt.SuspectMaxCycles <= 0 {
		opt.SuspectMaxCycles = defaultProgressReconcileOptions().SuspectMaxCycles
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		if werr := s.persistSourceReadIncident(ctx, planID, "", now, "plan:"+err.Error()); werr != nil {
			return werr
		}
		return nil
	}
	tasks, err := s.tasks.ListByPlan(ctx, planID)
	if err != nil {
		return s.persistSourceReadIncident(ctx, planID, "", now, "tasks:"+err.Error())
	}
	records, err := s.plans.ListDispatchRecords(ctx, planID)
	if err != nil {
		return s.persistSourceReadIncident(ctx, planID, "", now, "dispatch:"+err.Error())
	}
	blocked, err := s.plans.ListBlockedOn(ctx, planID)
	if err != nil {
		return s.persistSourceReadIncident(ctx, planID, "", now, "blocked_on:"+err.Error())
	}

	dispatched := map[pm.TaskID]bool{}
	for _, r := range records {
		dispatched[r.TaskID] = true
	}
	blockedByTask := map[pm.TaskID]pm.BlockedOn{}
	for _, b := range blocked {
		blockedByTask[b.TaskID] = b
	}
	coverage := progressCoverage(tasks, now)
	for _, t := range tasks {
		v := s.evaluateTaskProgress(ctx, p, t, dispatched[t.ID()], blockedByTask[t.ID()], coverage, now, opt)
		if err := s.plans.SaveProgressObservation(ctx, v); err != nil {
			return err
		}
		for _, fact := range v.Facts {
			switch fact.Summary {
			case "watermark_lag":
				if err := s.plans.UpsertProgressIncident(ctx, progressIncident(v, pm.IncidentWatermarkLag, fact.ID, now)); err != nil {
					return err
				}
			case "persistent_suspect":
				if err := s.plans.UpsertProgressIncident(ctx, progressIncident(v, pm.IncidentProgressClassificationUnknown, fact.ID, now)); err != nil {
					return err
				}
			case "missing_progress_contract":
				if err := s.plans.UpsertProgressIncident(ctx, progressIncident(v, pm.IncidentMigrationGap, fact.ID, now)); err != nil {
					return err
				}
			}
		}
		if v.Quality == pm.ProgressQualitySuspect && v.SuspectKey != "" {
			if err := s.plans.UpsertProgressObligation(ctx, progressObligation(v, pm.ObligationSourceRecovery, now)); err != nil {
				return err
			}
		} else {
			// A fresh authoritative observation closes the recovery episode while
			// retaining the durable obligation row as resolved audit evidence.
			factRef := ""
			if len(v.Facts) > 0 {
				factRef = v.Facts[0].ID
			}
			if _, err := s.plans.ResolveOpenProgressObligations(ctx, planID, t.ID(), pm.ObligationSourceRecovery, factRef, now); err != nil {
				return err
			}
		}
		if t.Status() == pm.TaskRunning && t.Delivery() == nil {
			if err := s.plans.UpsertProgressObligation(ctx, progressObligation(v, pm.ObligationProduceDelivery, now)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) evaluateTaskProgress(ctx context.Context, p *pm.Plan, t *pm.Task, dispatched bool, blocked pm.BlockedOn, cov pm.ProgressCoverage, now time.Time, opt progressReconcileOptions) pm.ObservationVector {
	contract := t.DeliveryContract().Effective()
	defaulted := t.DeliveryContract() == ""
	watermark := opt.SourceWatermark
	if watermark.IsZero() {
		watermark = now
	}
	facts := []pm.ProgressFact{{
		ID:         progressFactID(p.ID(), t.ID(), "task_lifecycle", strconv.Itoa(t.Version())),
		SourceKind: "pm_tasks", SourceID: string(t.ID()), OccurredAt: t.StatusChangedAt(), ObservedAt: now,
		Revision: strconv.Itoa(t.Version()), Summary: "task_status:" + string(t.Status()), Quality: pm.ProgressFactQualityValid,
	}}
	quality := pm.ProgressQualityValid
	decision := pm.ProgressFactVerified
	suspectKey := ""
	suspectCycles := 0
	if now.Sub(watermark) > opt.WatermarkLagSLA {
		quality = pm.ProgressQualitySuspect
		decision = pm.CannotDetermine
		suspectKey = "watermark_lag"
		facts = append(facts, pm.ProgressFact{
			ID:         progressFactID(p.ID(), t.ID(), "watermark_lag", watermark.Format(time.RFC3339Nano)),
			SourceKind: "pm_progress_checkpoints", SourceID: string(p.ID()), OccurredAt: watermark, ObservedAt: now,
			Revision: watermark.Format(time.RFC3339Nano), Summary: "watermark_lag", Quality: pm.ProgressFactQualityUnknown, CannotAbsent: true,
		})
	} else if !t.Status().IsTerminal() {
		if t.Status() == pm.TaskRunning && t.Delivery() == nil {
			decision = pm.ResponsibilityBound
			quality = pm.ProgressQualitySuspect
			suspectKey = "running_without_delivery"
		} else if !dispatched && blocked.TaskID == "" {
			decision = pm.ResponsibilityBound
			quality = pm.ProgressQualitySuspect
			suspectKey = "negative_fact_unconfirmed"
		} else {
			decision = pm.ResponsibilityBound
		}
	}
	if defaulted {
		facts = append(facts, pm.ProgressFact{
			ID:         progressFactID(p.ID(), t.ID(), "missing_progress_contract", strconv.Itoa(t.Version())),
			SourceKind: "pm_tasks", SourceID: string(t.ID()), OccurredAt: t.CreatedAt(), ObservedAt: now,
			Revision: strconv.Itoa(t.Version()), Summary: "missing_progress_contract", Quality: pm.ProgressFactQualitySuspect, CannotAbsent: true,
		})
	}
	if suspectKey != "" {
		if prev, ok, _ := s.plans.LatestProgressObservation(ctx, p.ID(), t.ID()); ok && prev.SuspectKey == suspectKey {
			suspectCycles = prev.SuspectCycles + 1
		} else {
			suspectCycles = 1
		}
		if suspectCycles >= opt.SuspectMaxCycles {
			decision = pm.CannotDetermine
			facts = append(facts, pm.ProgressFact{
				ID:         progressFactID(p.ID(), t.ID(), "persistent_suspect", suspectKey),
				SourceKind: "pm_progress_observations", SourceID: string(t.ID()), OccurredAt: now, ObservedAt: now,
				Revision: strconv.Itoa(suspectCycles), Summary: "persistent_suspect", Quality: pm.ProgressFactQualityUnknown, CannotAbsent: true,
			})
		}
	}
	return pm.ObservationVector{
		ID:     progressObservationID(p.ID(), t.ID(), now, suspectKey, suspectCycles),
		PlanID: p.ID(), TaskID: t.ID(), NodeID: t.NodeID(), Decision: decision, Quality: quality,
		AsOf: now, EvaluatedAt: now,
		SourceRevisions: []pm.ObservationSource{
			{Kind: "pm_plans", SourceID: string(p.ID()), Revision: strconv.Itoa(p.Version()), WatermarkAt: watermark, ObservedAt: now},
			{Kind: "pm_tasks", SourceID: string(t.ID()), Revision: strconv.Itoa(t.Version()), WatermarkAt: watermark, ObservedAt: now},
		},
		Facts: facts, SuspectKey: suspectKey, SuspectCycles: suspectCycles,
		ProgressContract: contract, ProgressContractDefaulted: defaulted,
		UncoveredProgressWindowSeconds: cov.UncoveredProgressWindowSeconds, Coverage: cov,
	}
}

func progressCoverage(tasks []*pm.Task, now time.Time) pm.ProgressCoverage {
	c := pm.ProgressCoverage{TotalNodes: len(tasks)}
	for _, t := range tasks {
		if t.DeliveryContract() != "" {
			c.CoveredNodes++
			continue
		}
		if !t.Status().IsTerminal() {
			since := t.StatusChangedAt()
			if since.IsZero() {
				since = t.CreatedAt()
			}
			if d := now.Sub(since); d > 0 {
				c.UncoveredProgressWindowSeconds += int64(d / time.Second)
			}
		}
	}
	if c.TotalNodes > 0 {
		c.CoverageRatio = float64(c.CoveredNodes) / float64(c.TotalNodes)
	}
	return c
}

func (s *Service) persistSourceReadIncident(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, now time.Time, episode string) error {
	v := pm.ObservationVector{
		ID:     progressObservationID(planID, taskID, now, "source_read_failure", 1),
		PlanID: planID, TaskID: taskID, Decision: pm.CannotDetermine, Quality: pm.ProgressQualitySuspect,
		AsOf: now, EvaluatedAt: now, SuspectKey: "source_read_failure", SuspectCycles: 1,
		Facts: []pm.ProgressFact{{
			ID: progressFactID(planID, taskID, "source_read_failure", episode), SourceKind: "projectmanager",
			SourceID: string(planID), OccurredAt: now, ObservedAt: now, Revision: "unknown",
			Summary: "source_read_failure", Quality: pm.ProgressFactQualityUnknown, CannotAbsent: true,
		}},
	}
	if err := s.plans.SaveProgressObservation(ctx, v); err != nil {
		return err
	}
	return s.plans.UpsertProgressIncident(ctx, progressIncident(v, pm.IncidentProjectorUnavailable, v.Facts[0].ID, now))
}

func progressObligation(v pm.ObservationVector, kind pm.ProgressObligationKind, now time.Time) pm.ProgressObligation {
	owner := pm.IdentityRef("system")
	display := "ProjectManager progress reconciler"
	if kind == pm.ObligationProduceDelivery {
		owner = "system"
		display = "Delivery owner"
	}
	return pm.ProgressObligation{
		ID:     stableID("pobl", string(v.PlanID), string(v.TaskID), string(kind), v.SuspectKey),
		PlanID: v.PlanID, TaskID: v.TaskID, NodeID: v.NodeID, Kind: kind, OwnerRef: owner, OwnerDisplay: display,
		DeadlineAt: now.Add(15 * time.Minute), AckRequired: true, EscalateToRef: "system",
		EscalationDeadlineAt: now.Add(30 * time.Minute), SourceFactRefs: factIDs(v.Facts),
		EpisodeKey: nonEmpty(v.SuspectKey, string(kind)), Status: pm.ResponsibilityOpen,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
}

func progressIncident(v pm.ObservationVector, kind pm.ProgressIncidentKind, factID string, now time.Time) pm.ProgressIncident {
	key := nonEmpty(v.SuspectKey, string(kind))
	if factID != "" {
		key += ":" + factID
	}
	return pm.ProgressIncident{
		ID:     stableID("pinc", string(v.PlanID), string(v.TaskID), string(kind), key),
		PlanID: v.PlanID, TaskID: v.TaskID, NodeID: v.NodeID, Kind: kind, OwnerRef: "system",
		OwnerDisplay: "ProjectManager progress reconciler", DeadlineAt: now.Add(10 * time.Minute),
		AckRequired: true, EscalateToRef: "system", EscalationDeadlineAt: now.Add(20 * time.Minute),
		SourceFactRefs: []string{factID}, EpisodeKey: key, Status: pm.ResponsibilityOpen,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
}

func factIDs(facts []pm.ProgressFact) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		if f.ID != "" {
			out = append(out, f.ID)
		}
	}
	return out
}

func progressObservationID(planID pm.PlanID, taskID pm.TaskID, at time.Time, key string, cycle int) string {
	return stableID("pobs", string(planID), string(taskID), at.Format(time.RFC3339Nano), key, strconv.Itoa(cycle))
}

func progressFactID(planID pm.PlanID, taskID pm.TaskID, parts ...string) string {
	all := append([]string{string(planID), string(taskID)}, parts...)
	return stableID("pfact", all...)
}

func stableID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(h[:])[:24])
}

func nonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
