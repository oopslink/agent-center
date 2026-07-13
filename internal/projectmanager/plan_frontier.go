package projectmanager

// =============================================================================
// I103 §2/§6 — the un-advanced FRONTIER read model + pending-decision queue.
//
// These are PURE derivations over a plan's materialized BlockedOn snapshots (the
// 旁路 OBSERVATIONAL descriptors the reconcile sweep keeps per non-terminal node).
// They compute NOTHING new about gating/readiness — they only re-shape the already-
// materialized snapshots into the two read views I103 asks for:
//
//   - DeriveFrontier groups the snapshots by wait_type → "who is un-advanced, and
//     why", the frontier board.
//   - DerivePendingDecisions filters the human_decision waits → the read-only
//     "待裁决队列" (the queue's re-reminder/escalate is a downstream I103 task; this
//     is READ only).
//
// Both are pure functions over the caller-supplied snapshot list (the readers load
// it via ListBlockedOn and pass it in), so they carry no I/O and are trivially
// unit-testable. They preserve the caller's input order WITHIN a group (ListBlockedOn
// is task_id-stable), and order the GROUPS by the canonical WaitType enum order so
// the board is deterministic sweep-to-sweep.
// =============================================================================

// IsTerminal reports whether a DERIVED node status is TERMINAL (settled — the node
// will make no further progress): done, failed, or skipped (a pruned conditional
// branch, §3.1). A terminal node is never part of the frontier and carries no
// BlockedOn snapshot (the reconcile sweep clears it on entry to a terminal state).
// The non-terminal statuses (blocked/ready/dispatched/running/paused) are the ones a
// blocked_on snapshot can describe.
func (s NodeStatus) IsTerminal() bool {
	switch s {
	case NodeDone, NodeFailed, NodeSkipped:
		return true
	}
	return false
}

// frontierWaitOrder is the canonical, most-specific-first display order for the
// frontier groups (mirrors the classifier's own priority order in classifyBlockedOn).
// A wait_type absent from a plan's snapshots simply produces no group.
var frontierWaitOrder = []WaitType{
	WaitUpstreamCompletion,
	WaitAcceptanceVerdict,
	WaitStageBarrier,
	WaitHumanDecision,
	WaitExternalEvent,
	WaitExecutorLiveness,
	WaitTimeoutOnly,
}

// FrontierGroup is one wait_type bucket of the un-advanced frontier: every blocked_on
// node currently waiting for that KIND of release. Nodes keep the input order (task_id-
// stable from ListBlockedOn).
type FrontierGroup struct {
	WaitType WaitType
	Nodes    []BlockedOn
}

// PlanFrontier is the aggregated "un-advanced frontier" read model (I103 §2): the
// plan's blocked_on snapshots grouped by wait_type (canonical order), plus the total
// blocked node count. An empty frontier (no snapshots) yields no groups and Total 0 —
// the plan is fully advancing (or terminal).
type PlanFrontier struct {
	Groups []FrontierGroup
	Total  int
}

// DeriveFrontier groups a plan's BlockedOn snapshots by wait_type into the frontier
// board (I103 §2). Groups are ordered by the canonical WaitType priority; a wait_type
// with no snapshots is omitted. An UNKNOWN wait_type (not in the enum order — defensive,
// should not occur since the classifier only emits enum values) is appended after the
// canonical groups in first-seen order so nothing is silently dropped. Nodes within a
// group preserve the caller's input order.
func DeriveFrontier(blocked []BlockedOn) PlanFrontier {
	byType := make(map[WaitType][]BlockedOn, len(frontierWaitOrder))
	var extraOrder []WaitType // unknown wait_types, first-seen order (defensive)
	seenExtra := make(map[WaitType]bool)
	for _, b := range blocked {
		byType[b.WaitType] = append(byType[b.WaitType], b)
		if !b.WaitType.IsValid() && !seenExtra[b.WaitType] {
			seenExtra[b.WaitType] = true
			extraOrder = append(extraOrder, b.WaitType)
		}
	}
	f := PlanFrontier{Total: len(blocked)}
	appendGroup := func(wt WaitType) {
		if nodes := byType[wt]; len(nodes) > 0 {
			f.Groups = append(f.Groups, FrontierGroup{WaitType: wt, Nodes: nodes})
		}
	}
	for _, wt := range frontierWaitOrder {
		appendGroup(wt)
	}
	for _, wt := range extraOrder {
		appendGroup(wt)
	}
	return f
}

// DerivePendingDecisions filters a plan's BlockedOn snapshots down to the human_decision
// waits — the read-only "待裁决队列" (I103 §2 pending-decision read面): every node held
// awaiting a human ruling (the decision node itself with no recorded outcome, or a node
// blocked behind such an unresolved decision). Input order is preserved (task_id-stable).
// Returns nil when there are none (a plan with nothing to rule on). This is READ ONLY —
// the queue's re-reminder / escalate is a downstream I103 task.
func DerivePendingDecisions(blocked []BlockedOn) []BlockedOn {
	var out []BlockedOn
	for _, b := range blocked {
		if b.WaitType == WaitHumanDecision {
			out = append(out, b)
		}
	}
	return out
}
