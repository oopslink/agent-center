package projectmanager

import "testing"

// T468: the pure verdict-driven B3 rule (applied on a green gate).
func TestAutoDecideFromVerdict(t *testing.T) {
	cases := []struct {
		verdict  string
		blocking bool
		outcome  string
		decided  bool
	}{
		{ReviewPass, false, OutcomePass, true},     // non-blocking pass → pass (the nit-no-longer-wedges fix)
		{ReviewPass, true, OutcomeReject, true},    // blocking pass → reject
		{ReviewReject, false, OutcomeReject, true}, // reject → reject
		{ReviewReject, true, OutcomeReject, true},  // reject (blocking) → reject
		{"", false, "", false},                     // no verdict → no decision
		{"garbage", false, "", false},              // unknown label → no decision
	}
	for _, c := range cases {
		o, d := AutoDecideFromVerdict(c.verdict, c.blocking)
		if o != c.outcome || d != c.decided {
			t.Errorf("AutoDecideFromVerdict(%q,%t) = (%q,%t); want (%q,%t)", c.verdict, c.blocking, o, d, c.outcome, c.decided)
		}
	}
}

func TestValidReviewVerdict(t *testing.T) {
	for v, want := range map[string]bool{"pass": true, "reject": true, "": false, "approve": false} {
		if got := ValidReviewVerdict(v); got != want {
			t.Errorf("ValidReviewVerdict(%q)=%t want %t", v, got, want)
		}
	}
}

// T468 graph helpers over the cycle shape: Dev→Review→Decision, Decision routes
// pass→Integrate / reject_exhausted→Escape / loopback(reject)→Dev.
func t468Edges() []Dependency {
	return []Dependency{
		{PlanID: "pl", FromTaskID: "Review", ToTaskID: "Dev", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Review", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Integ", ToTaskID: "Dec", Kind: EdgeConditional, When: "pass"},
		{PlanID: "pl", FromTaskID: "Esc", ToTaskID: "Dec", Kind: EdgeConditional, When: "reject_exhausted"},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, When: "reject", MaxRounds: 3},
	}
}

func TestDecisionForReview(t *testing.T) {
	edges := t468Edges()
	if got, ok := DecisionForReview(edges, "Review"); !ok || got != "Dec" {
		t.Fatalf("DecisionForReview(Review) = (%q,%t), want (Dec,true)", got, ok)
	}
	// Dev is not directly upstream of a decision (Review is between them).
	if _, ok := DecisionForReview(edges, "Dev"); ok {
		t.Fatalf("DecisionForReview(Dev) should be false")
	}
}

func TestForwardUpstreams_and_LoopbackTarget(t *testing.T) {
	edges := t468Edges()
	ups := ForwardUpstreams(edges, "Dec")
	if len(ups) != 1 || ups[0] != "Review" {
		t.Fatalf("ForwardUpstreams(Dec) = %v, want [Review] (loopback excluded)", ups)
	}
	if tgt, ok := LoopbackTargetOf(edges, "Dec"); !ok || tgt != "Dev" {
		t.Fatalf("LoopbackTargetOf(Dec) = (%q,%t), want (Dev,true)", tgt, ok)
	}
	if _, ok := LoopbackTargetOf(edges, "Review"); ok {
		t.Fatalf("LoopbackTargetOf(Review) should be false (no loopback from it)")
	}
}
