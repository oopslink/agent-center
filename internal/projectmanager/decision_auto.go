package projectmanager

// =============================================================================
// B3 — decision automation (v2.13.0 I18/B3 — docs/design/v2.13.0/
// control-flow-engine-spec.md §2.3 / §10).
//
// B1 made a control-flow `decision` node route its conditional/loopback out-edges
// by an OUTCOME label (`pass`/`reject`), recorded MANUALLY at complete (the
// complete_task `outcome` arg). B3 is the AUTOMATIC producer of that outcome: from
// the §-1 gate verdict + open review comments it derives `pass` / `reject`, or
// abstains (→ a human ruling). B3 ONLY produces the outcome — it does not change the
// B1 engine contract (ComputePlanView / applyLoopbacks consume the recorded outcome
// exactly as before, whether a human or B3 wrote it).
//
// The decision RULE lives here as a pure function (AutoDecideOutcome) so it is
// trivially exhaustively testable and carries the safety invariant — NEVER
// false-pass — independent of any I/O. The service layer (service/decision_auto.go)
// gathers the inputs (gate verdict via the DecisionGate port, open comments) and
// records the outcome.
// =============================================================================

// GateVerdict is the tri-state result of a feature branch's §-1 gate
// (build / lint / tsc / test — cycle-node-graph-spec.md §2.3). It is tri-state on
// purpose: "couldn't determine" (GateUnknown) is NOT the same as "failed"
// (GateRed) — an indeterminate gate defers to a human rather than auto-rejecting.
type GateVerdict int

const (
	// GateUnknown — the gate did not run, or its result could not be determined
	// (no gate wired, no branch/base/repo, or an infra error). Routes to a human
	// ruling (never an auto outcome) so a flaky/missing gate cannot drive routing.
	GateUnknown GateVerdict = iota
	// GateGreen — the gate ran and PASSED (every check green).
	GateGreen
	// GateRed — the gate ran and FAILED (at least one check red).
	GateRed
)

// String renders the verdict for logs / @mentions.
func (v GateVerdict) String() string {
	switch v {
	case GateGreen:
		return "green"
	case GateRed:
		return "red"
	default:
		return "unknown"
	}
}

// Decision outcome labels (the cycle convention from control-flow-engine-spec.md
// §2.3 — a decision's `conditional`/`loopback` out-edges carry When=="pass"/"reject").
// B3 records exactly these; B2's scaffold wires the matching edges.
const (
	// OutcomePass routes the decision's conditional(when=pass) edge → Integrate.
	OutcomePass = "pass"
	// OutcomeReject fires the decision's loopback(when=reject) edge → back to Dev
	// (the B1 bounded loop); once rounds are exhausted the engine records the terminal
	// reject_exhausted outcome + escalates (v2.23.0 — no longer an escape-vertex edge).
	OutcomeReject = "reject"
)

// AutoDecideOutcome is the pure B3 decision rule (control-flow-engine-spec.md §2.3 /
// §10). Given the gate verdict and the count of OPEN (unresolved) review comments on
// the decision node, it returns the outcome label to record and whether a decision
// was reached at all:
//
//	gate GREEN  &&  openComments == 0   → ("pass",   true)   auto-pass
//	gate RED                            → ("reject", true)   auto-reject
//	gate GREEN  &&  openComments > 0    → ("",       false)  boundary → human
//	gate UNKNOWN                        → ("",       false)  boundary → human
//
// The single hard invariant (B3-Review's "不误放"/no-false-pass): a `pass` is
// returned ONLY on positive evidence — the gate is GREEN *and* there is not a single
// open comment. Every ambiguous case (gate couldn't be determined, or the gate is
// green but a reviewer left an unresolved objection) abstains to a human rather than
// risk waving a bad change through. `decided==false` means "B3 records nothing; a
// human must rule" — it is NOT an error.
func AutoDecideOutcome(gate GateVerdict, openComments int) (outcome string, decided bool) {
	switch gate {
	case GateRed:
		// A red gate is unambiguous: reject and let B1's bounded loopback re-run Dev.
		return OutcomeReject, true
	case GateGreen:
		if openComments <= 0 {
			return OutcomePass, true
		}
		// Green but a reviewer objection is unresolved — a human must weigh it.
		return "", false
	default: // GateUnknown
		return "", false
	}
}

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

// AutoDecideFromVerdict is the T468 verdict-driven B3 rule, applied ONLY on a GREEN
// gate (red/unknown are handled by the gate floor in the service). Given the
// CURRENT-round review verdict it returns the outcome to record:
//
//	verdict=pass  && !blocking → ("pass",   true)   auto-pass (a non-blocking nit no longer wedges it)
//	verdict=reject | blocking  → ("reject", true)   auto-reject (B1 bounded loopback re-runs Dev)
//	anything else              → ("",       false)  no verdict-based decision (caller falls back / defers)
//
// The no-false-pass invariant still holds: a `pass` is returned only on a positive,
// non-blocking reviewer verdict; the service NEVER calls this on a non-green gate.
func AutoDecideFromVerdict(verdict string, blocking bool) (outcome string, decided bool) {
	switch verdict {
	case ReviewPass:
		if blocking {
			return OutcomeReject, true
		}
		return OutcomePass, true
	case ReviewReject:
		return OutcomeReject, true
	default:
		return "", false
	}
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
//   - a CONDITIONAL edge is From=downstream depends_on To=decision (ComputePlanView's
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
