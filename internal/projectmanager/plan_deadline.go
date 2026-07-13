package projectmanager

import "time"

// =============================================================================
// I103 §2 — deadline engine policy (the deterministic-resume LIVENESS base).
//
// A BlockedOn snapshot (plan_blocked_on.go) records WHY a plan node is not making
// terminal progress. This file is the POLICY half of the deadline engine that consumes
// it: it maps a wait_type + when-the-wait-began (waited_since) to an absolute deadline
// and the on_timeout action that fires when the clock passes it.
//
// Liveness is a PULL-AUTHORITATIVE back-stop, not a gate: the on_timeout action only
// PROPOSES resume / RECORDS the timeout — it NEVER releases a node the authoritative
// gates hold (the T1041 acceptance hard gate, the reject gate, the stage barrier stay
// in sole control of readiness, I103 §5 P2). A deadline elapsing re-probes or escalates;
// it never auto-decides a gate and never bypasses one.
// =============================================================================

// TimeoutAction is what the on_timeout router does when a BlockedOn node's deadline
// elapses (I103 §2). Every action is PROPOSE-ONLY / RECORD-ONLY — none releases a
// gated node or auto-resolves a gate.
type TimeoutAction string

const (
	// TimeoutReprobe re-proposes resume for the node (a gate-respecting re-dispatch /
	// executor re-probe) and bumps the probe counter — the push path may have dropped an
	// event, so the pull back-stop re-offers the node. A gated node stays gated: the
	// re-dispatch runs the SAME idempotent, gate-checked dispatch core, so a node still
	// held by a gate is simply not dispatched (no bypass).
	TimeoutReprobe TimeoutAction = "reprobe"
	// TimeoutEscalate records a human-visible timeout notice (the wait has run past its
	// budget) WITHOUT acting on the gate — the appropriate action for a wait only a human
	// / an upstream authority can release (acceptance_verdict, stage_barrier,
	// human_decision).
	TimeoutEscalate TimeoutAction = "escalate"
	// TimeoutRouteToHandler routes the timed-out node to a dedicated timeout handler
	// (a compensation/route-to-error path) — still a PROPOSAL: it records the routing and
	// hands off; it does not itself force the node past any gate.
	TimeoutRouteToHandler TimeoutAction = "route_to_timeout_handler"
)

// IsValid reports enum membership (the 3 I103 §2 on_timeout actions).
func (a TimeoutAction) IsValid() bool {
	switch a {
	case TimeoutReprobe, TimeoutEscalate, TimeoutRouteToHandler:
		return true
	}
	return false
}

// WaitDeadline is a per-wait-type deadline rule: how long a wait of this type may run
// before its on_timeout action fires, and which action fires. A non-positive Timeout
// DISABLES the deadline for the type — no deadline is ever assigned, so the wait is
// released only by its own resolver, never by the clock.
type WaitDeadline struct {
	Timeout   time.Duration
	OnTimeout TimeoutAction
}

// DeadlinePolicy configures the deadline engine (I103 §2): a Default rule applied to
// every wait_type plus per wait_type overrides, and a probe back-off cadence. The zero
// value is INERT — no default, no overrides ⇒ no deadline is ever assigned and the
// engine is a no-op (pre-I103 behaviour, zero-regression). Wire DefaultDeadlinePolicy
// (or a custom policy) to turn the engine on.
type DeadlinePolicy struct {
	// Default applies to a wait_type with no ByWaitType override. A non-positive
	// Default.Timeout ⇒ un-overridden types get NO deadline.
	Default WaitDeadline
	// ByWaitType overrides Default per wait_type. A present entry with a non-positive
	// Timeout explicitly DISABLES the deadline for that type (distinct from "fall through
	// to Default").
	ByWaitType map[WaitType]WaitDeadline
	// ProbeBackoff throttles repeat on_timeout routing for a node that stays overdue
	// across sweeps: after a probe, the next probe waits until last_probe_at+ProbeBackoff.
	// <=0 ⇒ the node is re-probed every overdue sweep.
	ProbeBackoff time.Duration
}

// DeadlineFor computes the absolute deadline + on_timeout action for a wait of the
// given type that began at waitedSince. ok=false ⇒ this wait_type has no deadline
// (disabled or unconfigured, or waitedSince is zero) and the caller assigns none.
//
// IDEMPOTENT / NO-DRIFT: for a fixed (waitType, waitedSince) it always returns the same
// deadline. Because the reconcile materialize PRESERVES waited_since while the wait_type
// is unchanged, an ongoing wait's deadline is recomputed to the SAME instant every
// sweep — it never drifts forward (I103 §2). When the wait_type changes, waited_since
// resets to now and the deadline legitimately recomputes off the new base.
func (p DeadlinePolicy) DeadlineFor(waitType WaitType, waitedSince time.Time) (time.Time, TimeoutAction, bool) {
	rule, overridden := p.ByWaitType[waitType]
	if !overridden {
		rule = p.Default
	}
	if rule.Timeout <= 0 || waitedSince.IsZero() {
		return time.Time{}, "", false
	}
	action := rule.OnTimeout
	if !action.IsValid() {
		// A configured deadline with no (or invalid) action defaults to escalate — the
		// safe verb: record + surface, never auto-decide a gate.
		action = TimeoutEscalate
	}
	return waitedSince.Add(rule.Timeout), action, true
}

// DefaultDeadlinePolicy is the production deadline policy (I103 §2): a 30-minute default
// re-probe, with longer human-authority waits (acceptance/stage/decision) set to
// ESCALATE rather than re-probe (a human/upstream authority — not the clock — releases
// them), and short liveness/external waits set to re-probe. ProbeBackoff caps repeat
// routing at one probe per 5 minutes while a node stays overdue.
//
// These are conservative starting values — every entry is overridable via a custom
// DeadlinePolicy. Nothing here changes gating: escalate/reprobe only propose/record.
//
// This IS the live production policy: the composition root (cli app.go) passes it as
// Deps.DeadlinePolicy, so the reconcile materialize assigns these deadlines and the
// router acts on them (paired with the production HumanDecisionTimeoutSink).
func DefaultDeadlinePolicy() DeadlinePolicy {
	return DeadlinePolicy{
		Default: WaitDeadline{Timeout: 30 * time.Minute, OnTimeout: TimeoutReprobe},
		ByWaitType: map[WaitType]WaitDeadline{
			WaitUpstreamCompletion: {Timeout: 1 * time.Hour, OnTimeout: TimeoutReprobe},
			WaitAcceptanceVerdict:  {Timeout: 2 * time.Hour, OnTimeout: TimeoutEscalate},
			WaitStageBarrier:       {Timeout: 2 * time.Hour, OnTimeout: TimeoutEscalate},
			WaitHumanDecision:      {Timeout: 4 * time.Hour, OnTimeout: TimeoutEscalate},
			WaitExternalEvent:      {Timeout: 15 * time.Minute, OnTimeout: TimeoutReprobe},
			WaitExecutorLiveness:   {Timeout: 10 * time.Minute, OnTimeout: TimeoutReprobe},
			WaitTimeoutOnly:        {Timeout: 30 * time.Minute, OnTimeout: TimeoutRouteToHandler},
		},
		ProbeBackoff: 5 * time.Minute,
	}
}

// TimeoutEvent is the record handed to the TimeoutSink when a node's deadline elapses
// (I103 §2). It is a pure NOTIFICATION of an observed timeout + the routed action; the
// engine has already recorded the probe (last_probe_at / probe_count) on the BlockedOn
// row before emitting it. A sink acts on it PROPOSE-ONLY — it must never release a gated
// node (see TimeoutAction).
type TimeoutEvent struct {
	PlanID   PlanID
	TaskID   TaskID
	NodeID   string
	WaitType WaitType
	// WaitKeys carries the ids being waited on (copied from the BlockedOn row): for a
	// human_decision timeout these are the pending DECISION task id(s) whose owner must
	// rule (the node's own id when it IS the decision; the upstream decision id(s) when
	// it is blocked behind one) — the sink resolves the reminder remindee from them.
	WaitKeys   []string
	Action     TimeoutAction
	Deadline   time.Time
	Overdue    time.Duration // now - deadline at the moment of routing
	ProbeCount int           // the counter AFTER this probe's increment
	At         time.Time     // now (== last_probe_at just written)
}
