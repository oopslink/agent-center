package projectmanager

// =============================================================================
// Control-flow decision helpers (v2.13.0 I18 — docs/design/v2.13.0/
// control-flow-engine-spec.md §2.3 / §10).
//
// A control-flow `decision` node routes its conditional/loopback out-edges by an
// OUTCOME label (`pass`/`reject`), recorded at complete (the complete_task `outcome`
// arg → RecordDecisionOutcome). The new orchestration engine (driveGraphDecisions)
// consumes that recorded outcome to release the pass branch / drive the bounded
// loopback. This file holds the PURE, structural helpers over the edge set the engine
// + authoring + review-verdict flow share (IsDecisionNode / DecisionForReview /
// ForwardUpstreams / LoopbackTargetOf) and the review-verdict vocabulary.
//
// (T810 ⑤: the old B3 auto-decision RULE (AutoDecideOutcome / AutoDecideFromVerdict)
// and the GateVerdict tri-state were deleted — the gate was removed in v2.28.0 so B3
// always deferred to a human, and the orchestration engine now owns routing.)
// =============================================================================

// Decision outcome labels (the cycle convention from control-flow-engine-spec.md
// §2.3 — a decision's `conditional`/`loopback` out-edges carry When=="pass"/"reject").
const (
	// OutcomePass routes the decision's conditional(when=pass) edge → Integrate.
	OutcomePass = "pass"
	// OutcomeReject fires the decision's loopback(when=reject) edge → back to Dev (the
	// bounded loop); once rounds are exhausted the engine records the terminal
	// reject_exhausted outcome + escalates.
	OutcomeReject = "reject"
)

// Review verdict labels (T468 / issue-f7ad5a54). A reviewer records exactly one of
// these as the structured verdict; B3 routes pass→Integrate / reject→loopback.
const (
	ReviewPass   = "pass"
	ReviewReject = "reject"
)

// ValidReviewVerdict reports whether v is a recognised review verdict label.
func ValidReviewVerdict(v string) bool { return v == ReviewPass || v == ReviewReject }

// ReviewVerdict is a Review node's structured, SINGLE-SLOT, ROUND-TAGGED verdict
// (T468 / issue-f7ad5a54). It replaces B3's "open review comment count" proxy: a
// reviewer records ONE verdict per review round (latest-wins overwrite, keyed by
// plan+task), and B3 reads the CURRENT-round verdict to auto-decide — so a reviewer's
// non-blocking nit (`pass` + `blocking=false`) no longer wedges the decision into a
// human ruling the way a non-zero open-comment count did. `Round` defends against
// stale: a verdict from a PRIOR loop round is ignored by B3 (→ defer) so it never
// auto-routes on an out-of-date review.
type ReviewVerdict struct {
	PlanID   PlanID
	TaskID   TaskID // the Review node the verdict is recorded on
	Verdict  string // ReviewPass | ReviewReject
	Blocking bool   // a blocking objection — forces reject even if Verdict==pass
	Reason   string
	SHA      string // the reviewed commit (audit / staleness context)
	Round    int    // the decision's loop round this verdict was recorded for
}

// DecisionForReview returns the control-flow DECISION node that is directly
// downstream of `reviewID` — i.e. the decision node that depends_on the review via a
// forward edge ({From: decision, To: review}). Used at verdict-write time to stamp the
// verdict with the decision's current loop round. ("", false) when the review is not
// directly upstream of a decision (e.g. a non-cycle node).
func DecisionForReview(edges []Dependency, reviewID TaskID) (TaskID, bool) {
	for _, e := range edges {
		if e.IsLoopback() {
			continue
		}
		if e.ToTaskID == reviewID && IsDecisionNode(edges, e.FromTaskID) {
			return e.FromTaskID, true
		}
	}
	return "", false
}

// ForwardUpstreams returns the forward (non-loopback) upstream task ids of `id` — the
// nodes `id` depends_on (the To of id's forward in-edges). B3 reads review verdicts on
// a decision's forward upstreams.
func ForwardUpstreams(edges []Dependency, id TaskID) []TaskID {
	var out []TaskID
	for _, e := range edges {
		if e.IsLoopback() {
			continue
		}
		if e.FromTaskID == id {
			out = append(out, e.ToTaskID)
		}
	}
	return out
}

// LoopbackTargetOf returns the loop-target (To) of the loopback edge whose From is
// `decisionID`, or ("", false) when the decision has no loopback edge (a single-round
// decision). The (decisionID, target) pair keys the LoopRound counter.
func LoopbackTargetOf(edges []Dependency, decisionID TaskID) (TaskID, bool) {
	for _, e := range edges {
		if e.IsLoopback() && e.FromTaskID == decisionID {
			return e.ToTaskID, true
		}
	}
	return "", false
}

// IsDecisionNode reports whether the task `id` is a control-flow DECISION node: a
// node that ROUTES downstream by its outcome. A decision is recognised STRUCTURALLY
// (not by a role — cycle-node-graph-spec.md has no `decision` role; Review fills the
// decision slot in fused form), matching B1's own conventions for edge direction:
//   - a LOOPBACK edge is From=decision → To=loop-target (applyLoopbacks keys on
//     From==decision), so id is a decision if it is the From of a loopback edge; and
//   - a CONDITIONAL edge is From=downstream depends_on To=decision (DerivePlanView's
//     condUp keys the decision as To), so id is a decision if it is the To of a
//     conditional edge.
//
// Pure over the edge set; false for an ordinary task (so B3's auto-decide is a no-op
// for it).
func IsDecisionNode(edges []Dependency, id TaskID) bool {
	for _, e := range edges {
		if e.IsLoopback() && e.FromTaskID == id {
			return true
		}
		if NormalizeEdgeKind(e.Kind) == EdgeConditional && e.ToTaskID == id {
			return true
		}
	}
	return false
}
