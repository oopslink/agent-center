package projectmanager

import "testing"

// T810 ⑤: TestAutoDecideOutcome + TestGateVerdictString were removed — the B3 auto-
// decision rule (AutoDecideOutcome) and the GateVerdict tri-state were deleted (the
// gate was removed in v2.28.0 so B3 always deferred; the engine now owns routing).

// TestIsDecisionNode confirms structural identification: a node with a conditional
// OR loopback out-edge is a decision node; a plain seq node is not.
func TestIsDecisionNode(t *testing.T) {
	// Canonical cycle shape (plan_controlflow_test.go directions):
	//   conditional: From=downstream depends_on To=decision  (decision = To)
	//   loopback:    From=decision → To=loop-target           (decision = From)
	const dec, plain, dev, integ TaskID = "dec", "plain", "dev", "integ"
	edges := []Dependency{
		// plain → seq dependency only (not a decision)
		{FromTaskID: plain, ToTaskID: dev, Kind: EdgeSeq},
		// dec routes: integ depends_on dec when pass (conditional, dec=To);
		//             dec loops back to dev when reject (loopback, dec=From).
		{FromTaskID: integ, ToTaskID: dec, Kind: EdgeConditional, When: "pass"},
		{FromTaskID: dec, ToTaskID: dev, Kind: EdgeLoopback, When: "reject", MaxRounds: 3},
	}
	if !IsDecisionNode(edges, dec) {
		t.Fatalf("dec should be a decision node (loopback From + conditional To)")
	}
	if IsDecisionNode(edges, plain) {
		t.Fatalf("plain should NOT be a decision node (only a seq edge)")
	}
	if IsDecisionNode(edges, integ) {
		t.Fatalf("integ should NOT be a decision node (it is the From/downstream of the conditional)")
	}
	if IsDecisionNode(nil, dec) {
		t.Fatalf("no edges ⇒ no decision node")
	}

	// A node that is only the To of a conditional edge is a decision node.
	condOnly := []Dependency{{FromTaskID: integ, ToTaskID: dec, Kind: EdgeConditional, When: "pass"}}
	if !IsDecisionNode(condOnly, dec) {
		t.Fatalf("a conditional edge with To=dec alone makes dec a decision node")
	}
	// A node that is only the From of a loopback edge is a decision node.
	loopOnly := []Dependency{{FromTaskID: dec, ToTaskID: dev, Kind: EdgeLoopback, When: "reject", MaxRounds: 2}}
	if !IsDecisionNode(loopOnly, dec) {
		t.Fatalf("a loopback edge with From=dec alone makes dec a decision node")
	}
}
