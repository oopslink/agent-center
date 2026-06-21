package projectmanager

import "testing"

// TestAutoDecideOutcome exhaustively pins the B3 decision rule
// (control-flow-engine-spec.md §2.3 / §10) — especially the no-false-pass invariant:
// `pass` appears ONLY for a green gate with zero open comments.
func TestAutoDecideOutcome(t *testing.T) {
	cases := []struct {
		name        string
		gate        GateVerdict
		openComment int
		wantOutcome string
		wantDecided bool
	}{
		{"green+no-comments → pass", GateGreen, 0, OutcomePass, true},
		{"green+one-comment → human", GateGreen, 1, "", false},
		{"green+many-comments → human", GateGreen, 5, "", false},
		{"red → reject (comments ignored)", GateRed, 0, OutcomeReject, true},
		{"red+comments → reject", GateRed, 3, OutcomeReject, true},
		{"unknown → human", GateUnknown, 0, "", false},
		{"unknown+comments → human", GateUnknown, 2, "", false},
		// Defensive: a negative count (shouldn't happen) is treated as "no comments".
		{"green+negative-count → pass", GateGreen, -1, OutcomePass, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotOutcome, gotDecided := AutoDecideOutcome(tc.gate, tc.openComment)
			if gotOutcome != tc.wantOutcome || gotDecided != tc.wantDecided {
				t.Fatalf("AutoDecideOutcome(%v, %d) = (%q, %v); want (%q, %v)",
					tc.gate, tc.openComment, gotOutcome, gotDecided, tc.wantOutcome, tc.wantDecided)
			}
			// Invariant: never emit a `pass` unless gate is green AND no open comments.
			if gotOutcome == OutcomePass && !(tc.gate == GateGreen && tc.openComment <= 0) {
				t.Fatalf("no-false-pass invariant violated for %s", tc.name)
			}
		})
	}
}

func TestGateVerdictString(t *testing.T) {
	for v, want := range map[GateVerdict]string{GateGreen: "green", GateRed: "red", GateUnknown: "unknown"} {
		if got := v.String(); got != want {
			t.Errorf("GateVerdict(%d).String() = %q; want %q", v, got, want)
		}
	}
}

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
