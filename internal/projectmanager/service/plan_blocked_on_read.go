package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// I103 §2/§6 — BlockedOn READ enrichment (the pure observability read面).
//
// fillBlockedOn re-reads the plan's materialized BlockedOn snapshots (the 旁路
// OBSERVATIONAL "why is this node not advancing" descriptors the reconcile sweep owns)
// and hangs them on the READ-facing PlanDetail. It is the mirror of fillStageGates: a
// READ-side enrichment that both agent-facing (GetPlanDetailForMember) and FE-facing
// (GetPlanDetail) detail paths share, so both surfaces tell the SAME frontier truth.
//
// PURE READ — it ONLY re-reads pm_plan_blocked_on (via ListBlockedOn) into the detail;
// it materializes NOTHING (that is the reconcile sweep's job), triggers no resume, and
// touches NO gate/readiness state. A repo error PROPAGATES so the read fails loudly
// rather than silently dropping the frontier.
//
// Scope mirrors the materialize (materializeBlockedOn): a builtin pool is FLAT and an
// ungraphed plan predates the engine — neither has snapshots, so both skip (leaving
// detail.BlockedOn nil → the DTO omits the frontier keys, zero-regression). The
// membership / plan-in-project guard is the caller's; this only reads.
func (s *Service) fillBlockedOn(ctx context.Context, detail *PlanDetail) error {
	if detail == nil || detail.Plan == nil || s.plans == nil {
		return nil
	}
	if detail.Plan.IsBuiltin() || detail.Plan.GraphID() == "" {
		return nil // nothing materialized for a flat/ungraphed plan.
	}
	list, err := s.plans.ListBlockedOn(ctx, detail.Plan.ID())
	if err != nil {
		return err
	}
	detail.BlockedOn = list
	return nil
}

// FrontierOf is the exported convenience over a loaded PlanDetail: the un-advanced
// frontier (blocked_on snapshots grouped by wait_type, I103 §2), derived purely from
// the detail's already-loaded BlockedOn snapshots. Returns an empty frontier when the
// detail carries no snapshots (fully-advancing / builtin / ungraphed plan).
func FrontierOf(detail *PlanDetail) pm.PlanFrontier {
	if detail == nil {
		return pm.PlanFrontier{}
	}
	return pm.DeriveFrontier(detail.BlockedOn)
}

// PendingDecisionsOf is the exported convenience over a loaded PlanDetail: the read-only
// "待裁决队列" (the human_decision waits, I103 §2). Returns nil when the detail carries no
// pending decisions. READ ONLY — the queue's re-reminder/escalate is a downstream task.
func PendingDecisionsOf(detail *PlanDetail) []pm.BlockedOn {
	if detail == nil {
		return nil
	}
	return pm.DerivePendingDecisions(detail.BlockedOn)
}
