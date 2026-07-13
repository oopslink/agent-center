package projectmanager

import "time"

// WaitType classifies WHY a plan node is not making terminal progress (I103 §1).
// It is DERIVED from the live graph state by the reconcile materialize (never
// authored) and is the routing key the downstream I103 tasks consume (each wait_type
// has its own resolver — deadline/on_timeout, human_decision queue, external_event
// subscription, executor_liveness detection). It is a pure OBSERVATION label — it
// changes no gating/readiness semantics.
type WaitType string

const (
	// WaitUpstreamCompletion — the node is `blocked` on one or more depends_on upstream
	// nodes that are not yet complete (the plain hard-AND / seq dependency wait).
	WaitUpstreamCompletion WaitType = "upstream_completion"
	// WaitAcceptanceVerdict — a merge-to-main node held by the T1041 acceptance HARD
	// gate: an upstream acceptance/decision condition gate has not resolved to a pass.
	WaitAcceptanceVerdict WaitType = "acceptance_verdict"
	// WaitStageBarrier — the node's stage entry is held behind an unresolved upstream
	// STAGE GATE (the stage barrier, issue-b867bbe6).
	WaitStageBarrier WaitType = "stage_barrier"
	// WaitHumanDecision — a decision/review node awaiting a human ruling (no outcome
	// recorded yet), or a node blocked behind such an unresolved decision.
	WaitHumanDecision WaitType = "human_decision"
	// WaitExternalEvent — the node is waiting on an external signal/subscription.
	// DEFERRED (I103): no current graph state derives it and there is no consumer, so
	// the classifier never emits it and no subscription/resolver is built. The enum
	// value is reserved ONLY so the vocabulary is complete — wire the derivation +
	// consumer when a real external-signal source exists (avoid dead scaffolding until
	// then).
	WaitExternalEvent WaitType = "external_event"
	// WaitExecutorLiveness — a running (or paused) node holding an execution lease; the
	// snapshot is a MARKER so the downstream executor-liveness detector/takeover has a
	// record to probe. This task only stamps the marker (detection is downstream).
	WaitExecutorLiveness WaitType = "executor_liveness"
	// WaitTimeoutOnly — the catch-all: a non-terminal, non-running, non-ready node with
	// no more specific derivable reason. Only a deadline can release it.
	WaitTimeoutOnly WaitType = "timeout_only"
)

// IsValid reports enum membership (the 7 I103 §1 wait types).
func (w WaitType) IsValid() bool {
	switch w {
	case WaitUpstreamCompletion, WaitAcceptanceVerdict, WaitStageBarrier,
		WaitHumanDecision, WaitExternalEvent, WaitExecutorLiveness, WaitTimeoutOnly:
		return true
	}
	return false
}

// BlockedOn is the I103 §1 descriptor: a旁路 OBSERVATIONAL snapshot of why one plan
// node is not making terminal progress, materialized by the reconcile sweep and
// cleared when the node enters ready/running/terminal. SINGLE-SLOT latest-wins per
// (PlanID, TaskID) — one row per node.
//
// This is PURE OBSERVATION: it drives NO gating (the acceptance hard gate, reject
// gate, stage barrier stay authoritative). The materialize populates node_id, wait
// classification, wait_keys, trigger_condition and waited_since; the downstream-owned
// fields (Deadline, OnTimeout, LastProbeAt, ProbeCount) are preserved across
// refreshes so a downstream resolver/prober can own them without the sweep clobbering
// its state.
type BlockedOn struct {
	NodeID           string   // bound orchestration graph node id ("" if unbound)
	TaskID           TaskID   // the bound task (identity, with PlanID)
	PlanID           PlanID   // the owning plan (scoping — persistence + cascade)
	WaitType         WaitType // the classified reason
	WaitKeys         []string // the ids being waited on (upstream task/gate node ids, assignee, …)
	TriggerCondition string   // human-readable release condition
	// WaitedSince is when THIS wait began. The materialize preserves it while the
	// wait_type is unchanged (an ongoing wait) and resets it to now when the wait_type
	// changes (a genuinely new kind of wait started) — the基准 the downstream deadline
	// engine measures against.
	WaitedSince time.Time
	// Deadline (optional; zero = none) and OnTimeout are set/consumed by the downstream
	// deadline engine + on_timeout router. The materialize preserves them.
	Deadline  time.Time
	OnTimeout string
	// LastProbeAt / ProbeCount are owned by the downstream prober (executor_liveness /
	// external_event polling). The materialize preserves them across refreshes.
	LastProbeAt time.Time
	ProbeCount  int
}
