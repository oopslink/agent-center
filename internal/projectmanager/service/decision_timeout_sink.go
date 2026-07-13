package service

import (
	"context"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// decision_timeout_sink.go (I103 §2) — the PRODUCTION TimeoutSink. The deadline engine
// records every elapsed-deadline probe on the BlockedOn row itself (probe_count /
// last_probe_at, routeTimeouts); this sink is the OPTIONAL side-effect layer wired in
// production. It acts on exactly ONE wait_type — human_decision — turning a stuck
// ruling into an owner-visible, durable "please rule" reminder (and, after repeated
// timeouts, an escalation). EVERY OTHER wait_type is a deliberate NO-OP here:
//
//   - upstream_completion / stage_barrier / acceptance_verdict — released only by their
//     own resolver (the upstream completing, the gate passing). The clock never releases
//     them; re-probing them would be noise.
//   - executor_liveness — the running-node lease marker. The center does NOT re-take an
//     executor here: ready-node re-dispatch is already covered by ReconcileRunningPlans'
//     per-sweep dispatchReadyNodes, and a dead executor is recovered by the stuck-Running
//     self-heal (issue-6ff12523) + the runtime's own self-recovery (v2.34.0). Re-building
//     takeover in the timeout sink would duplicate — and race — those owners.
//   - external_event — no producer today (deferred I103 concern; see WaitExternalEvent).
//   - timeout_only — the catch-all fallback; the recorded probe IS its whole handling.
//
// RED LINE (I103 §5 P2): this sink holds NO dispatch / start / resolve capability — only
// a DecisionReminderPort. It can arm a reminder; it structurally CANNOT release a node a
// gate holds. Liveness notifies; it never bypasses a gate.

// DecisionReminderPort arms (or, on escalation, strengthens) a durable, owner-visible
// reminder for a human_decision node whose deadline has elapsed. It is the cross-BC
// seam the HumanDecisionTimeoutSink calls: the production implementation (cli wiring)
// resolves the decision owner and (re)creates a one-shot reminder over the cognition
// reminder AppService, so the pm BC never imports the reminder BC directly (mirrors the
// PlanDispatcher / NodeResumer port shape).
//
// PROPOSE-ONLY: arming a reminder is a NOTIFY. An implementation MUST NOT dispatch,
// start, or resolve the timed-out node (I103 §5 P2).
type DecisionReminderPort interface {
	ArmDecisionReminder(ctx context.Context, req DecisionReminderRequest) error
}

// DecisionReminderRequest is one human_decision timeout to surface to its owner.
type DecisionReminderRequest struct {
	PlanID pm.PlanID
	// TaskID is the blocked/decision node observed overdue (the snapshot's own node).
	TaskID pm.TaskID
	NodeID string
	// DecisionKeys are the pending DECISION task id(s) whose owner must rule — the
	// node's own id when it IS the decision, or the upstream decision id(s) when it is
	// blocked behind one (BlockedOn.WaitKeys). The port resolves the remindee from them.
	DecisionKeys []string
	ProbeCount   int
	Overdue      time.Duration
	// Escalate is true once the wait has timed out EscalateAfter+ times — the owner has
	// not ruled across several deadlines and the reminder is strengthened.
	Escalate bool
}

// DefaultDecisionEscalateAfter is how many consecutive deadline timeouts a human_decision
// may accrue before the sink escalates (a conservative default; overridable at wiring).
const DefaultDecisionEscalateAfter = 3

// HumanDecisionTimeoutSink is the production TimeoutSink (I103 §2). See the file header
// for the full wait_type policy and the red line. It is nil-safe over its port.
type HumanDecisionTimeoutSink struct {
	port          DecisionReminderPort
	escalateAfter int
}

// NewHumanDecisionTimeoutSink wires the sink. A nil port makes OnTimeout a no-op (the
// engine still records probes — this only drops the reminder side-effect). escalateAfter
// <= 1 falls back to DefaultDecisionEscalateAfter.
func NewHumanDecisionTimeoutSink(port DecisionReminderPort, escalateAfter int) *HumanDecisionTimeoutSink {
	if escalateAfter <= 1 {
		escalateAfter = DefaultDecisionEscalateAfter
	}
	return &HumanDecisionTimeoutSink{port: port, escalateAfter: escalateAfter}
}

// OnTimeout implements TimeoutSink. For a human_decision timeout it ARMS a reminder at
// the FIRST timeout (probe_count == 1) and ESCALATES exactly once when the count reaches
// escalateAfter; any other probe_count is a repeat sweep of an already-armed, still-live
// reminder and is a NO-OP. Idempotency is thus keyed on the DURABLE probe_count the
// engine persists before calling the sink — the same decision is never re-armed every
// sweep, and a process restart cannot double-arm (the count only ever increments). Every
// non-human_decision wait_type is a no-op (see the file header).
func (s *HumanDecisionTimeoutSink) OnTimeout(ctx context.Context, ev pm.TimeoutEvent) error {
	if ev.WaitType != pm.WaitHumanDecision || s.port == nil {
		return nil
	}
	escalate := ev.ProbeCount >= s.escalateAfter
	switch {
	case ev.ProbeCount == 1:
		// initial arm — the first missed deadline.
	case escalate && ev.ProbeCount == s.escalateAfter:
		// escalate — exactly at the threshold, once.
	default:
		// A repeat overdue sweep: the reminder is already armed (or already escalated).
		// Do NOT re-arm — the durable reminder stands until the owner rules.
		return nil
	}
	return s.port.ArmDecisionReminder(ctx, DecisionReminderRequest{
		PlanID:       ev.PlanID,
		TaskID:       ev.TaskID,
		NodeID:       ev.NodeID,
		DecisionKeys: ev.WaitKeys,
		ProbeCount:   ev.ProbeCount,
		Overdue:      ev.Overdue,
		Escalate:     escalate,
	})
}
