package service

import (
	"context"
	"log/slog"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TimeoutSink receives a TimeoutEvent when a BlockedOn node's deadline elapses and the
// on_timeout router fires (I103 §2). It is an OPTIONAL, nil-safe port: when nil the
// engine still RECORDS the timeout on the BlockedOn row (last_probe_at / probe_count)
// but emits no external side-effect; when wired, the sink enacts the routed action.
//
// PROPOSE-ONLY contract: a sink proposes resume (a gate-respecting re-dispatch /
// executor re-probe), escalates a human-visible timeout notice, or routes to a timeout
// handler. It MUST NOT release a node the authoritative gates hold (the T1041 acceptance
// hard gate, the reject gate, the stage barrier) — liveness never bypasses a gate (I103
// §5 P2). Implemented at composition over the dispatcher / notification / handler paths.
type TimeoutSink interface {
	OnTimeout(ctx context.Context, ev pm.TimeoutEvent) error
}

// routeTimeouts is the I103 §2 deadline-check + on_timeout router — the ACTING half of
// the deadline engine (materializeBlockedOn is the ASSIGNING half). For a plan it scans
// the materialized BlockedOn snapshots and, for each whose deadline has elapsed (and
// whose probe back-off has passed), RECORDS the timeout (bumps probe_count, stamps
// last_probe_at) and ROUTES its on_timeout action to the sink.
//
// AUTHORITATIVE BACK-STOP, PROPOSE-ONLY: it writes ONLY the pm_plan_blocked_on store and
// calls the sink — it has NO code path that mutates task readiness, so it CANNOT release
// a node held by the acceptance / reject / stage gates (I103 §5 P2). The gates stay in
// sole control of readiness; a deadline elapsing only re-probes / escalates / routes.
//
// BEST-EFFORT: the sink side-effect is treated exactly like an audit write — a sink
// error is logged (never returned), so the recorded probe (the primary write) still
// commits and one node's failed action never stops the scan. Only a BlockedOn
// persistence error is returned, for the caller to log; the caller runs routeTimeouts in
// its OWN tx so it can never roll back a dispatch or the materialize (I103 §3).
func (s *Service) routeTimeouts(txCtx context.Context, p *pm.Plan) error {
	if s.plans == nil {
		return nil
	}
	planID := p.ID()
	list, err := s.plans.ListBlockedOn(txCtx, planID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	backoff := s.deadlinePolicy.ProbeBackoff
	var firstErr error
	for _, b := range list {
		if b.Deadline.IsZero() || !now.After(b.Deadline) {
			continue // no deadline assigned, or not yet elapsed — nothing to route.
		}
		// Probe back-off: once probed, wait ProbeBackoff before re-routing a node that
		// stays overdue across sweeps, so a stuck wait is not re-escalated every tick.
		if backoff > 0 && !b.LastProbeAt.IsZero() && now.Before(b.LastProbeAt.Add(backoff)) {
			continue
		}
		action := pm.TimeoutAction(b.OnTimeout)
		// Record the timeout on the row FIRST — the always-on "record timeout", committed
		// independent of any sink so a stuck wait's probe history is durable.
		b.ProbeCount++
		b.LastProbeAt = now
		if uerr := s.plans.UpsertBlockedOn(txCtx, b); uerr != nil {
			if firstErr == nil {
				firstErr = uerr
			}
			continue // this node's routing failed to persist; the scan goes on.
		}
		if s.timeoutSink == nil {
			continue // recorded; no external action wired.
		}
		ev := pm.TimeoutEvent{
			PlanID:     planID,
			TaskID:     b.TaskID,
			NodeID:     b.NodeID,
			WaitType:   b.WaitType,
			Action:     action,
			Deadline:   b.Deadline,
			Overdue:    now.Sub(b.Deadline),
			ProbeCount: b.ProbeCount,
			At:         now,
		}
		if serr := s.timeoutSink.OnTimeout(txCtx, ev); serr != nil {
			// BEST-EFFORT: a sink failure must not roll back the recorded probe or abort
			// the scan — log and move on (mirrors recordChange's audit philosophy). The
			// gates are untouched regardless, so a failed action never leaks readiness.
			slog.WarnContext(txCtx, "pm: on_timeout sink failed (best-effort, gates unaffected)",
				"err", serr, "plan_id", planID, "task_id", b.TaskID, "wait_type", b.WaitType, "action", action)
		}
	}
	return firstErr
}
