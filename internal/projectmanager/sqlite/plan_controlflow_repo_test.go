package sqlite

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// v2.13.0 I18/B1: edge kind/when/max_rounds + decision outcomes + loop rounds
// round-trip through the PlanRepo.
func TestPlanRepo_ControlFlowRoundTrip(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	const plan = pm.PlanID("PL-cf")

	// Build Dev → Review → Dec (seq) so the loopback's To (Dev) is an execution
	// ancestor of its From (Dec), then a conditional pass-edge and the loopback.
	seqs := []pm.Dependency{
		{PlanID: plan, FromTaskID: "Review", ToTaskID: "Dev", Kind: pm.EdgeSeq},
		{PlanID: plan, FromTaskID: "Dec", ToTaskID: "Review", Kind: pm.EdgeSeq},
	}
	for _, e := range seqs {
		if err := pr.AddDependency(ctx, e); err != nil {
			t.Fatalf("AddDependency seq %+v: %v", e, err)
		}
	}
	cond := pm.Dependency{PlanID: plan, FromTaskID: "Integrate", ToTaskID: "Dec", Kind: pm.EdgeConditional, When: "pass"}
	if err := pr.AddDependency(ctx, cond); err != nil {
		t.Fatalf("AddDependency conditional: %v", err)
	}
	loop := pm.Dependency{PlanID: plan, FromTaskID: "Dec", ToTaskID: "Dev", Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 3}
	if err := pr.AddDependency(ctx, loop); err != nil {
		t.Fatalf("AddDependency loopback: %v", err)
	}

	// A loopback to a NON-ancestor is rejected by the validity guard.
	bad := pm.Dependency{PlanID: plan, FromTaskID: "Dec", ToTaskID: "Nowhere", Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 3}
	if err := pr.AddDependency(ctx, bad); err != pm.ErrInvalidLoopback {
		t.Fatalf("loopback to non-ancestor err = %v, want ErrInvalidLoopback", err)
	}

	// Round-trip the edges + their kind/when/max_rounds.
	edges, err := pr.ListDependencies(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	byFromTo := map[string]pm.Dependency{}
	for _, e := range edges {
		byFromTo[string(e.FromTaskID)+"->"+string(e.ToTaskID)] = e
	}
	if e := byFromTo["Review->Dev"]; e.Kind != pm.EdgeSeq {
		t.Errorf("Review->Dev kind = %q, want seq", e.Kind)
	}
	if e := byFromTo["Integrate->Dec"]; e.Kind != pm.EdgeConditional || e.When != "pass" {
		t.Errorf("Integrate->Dec = kind:%q when:%q, want conditional/pass", e.Kind, e.When)
	}
	if e := byFromTo["Dec->Dev"]; e.Kind != pm.EdgeLoopback || e.When != "reject" || e.MaxRounds != 3 {
		t.Errorf("Dec->Dev = kind:%q when:%q max:%d, want loopback/reject/3", e.Kind, e.When, e.MaxRounds)
	}

	// Decision outcomes: record (latest-wins) → list → clear.
	if err := pr.RecordDecisionOutcome(ctx, plan, "Dec", "reject", t0); err != nil {
		t.Fatal(err)
	}
	if err := pr.RecordDecisionOutcome(ctx, plan, "Dec", "pass", t0); err != nil { // overwrite
		t.Fatal(err)
	}
	ocs, err := pr.ListDecisionOutcomes(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(ocs) != 1 || ocs[0].TaskID != "Dec" || ocs[0].Outcome != "pass" {
		t.Fatalf("outcomes = %+v, want one {Dec pass} (latest-wins)", ocs)
	}
	if err := pr.ClearDecisionOutcome(ctx, plan, "Dec"); err != nil {
		t.Fatal(err)
	}
	if ocs, _ := pr.ListDecisionOutcomes(ctx, plan); len(ocs) != 0 {
		t.Fatalf("after clear, outcomes = %+v, want empty", ocs)
	}

	// Loop rounds: 0 initially → increment to 1, 2.
	if r, _ := pr.GetLoopRound(ctx, plan, "Dec", "Dev"); r != 0 {
		t.Fatalf("initial loop round = %d, want 0", r)
	}
	if r, err := pr.IncrementLoopRound(ctx, plan, "Dec", "Dev"); err != nil || r != 1 {
		t.Fatalf("increment 1 = %d, %v, want 1", r, err)
	}
	if r, err := pr.IncrementLoopRound(ctx, plan, "Dec", "Dev"); err != nil || r != 2 {
		t.Fatalf("increment 2 = %d, %v, want 2", r, err)
	}
	if r, _ := pr.GetLoopRound(ctx, plan, "Dec", "Dev"); r != 2 {
		t.Fatalf("loop round after 2 increments = %d, want 2", r)
	}
}

// A legacy seq edge (no kind set) reads back as EdgeSeq (back-compat).
func TestPlanRepo_LegacyEdgeDefaultsToSeq(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	if err := pr.AddDependency(ctx, pm.Dependency{PlanID: "PL", FromTaskID: "B", ToTaskID: "A"}); err != nil {
		t.Fatal(err)
	}
	edges, _ := pr.ListDependencies(ctx, "PL")
	if len(edges) != 1 || edges[0].Kind != pm.EdgeSeq {
		t.Fatalf("edge = %+v, want a single seq edge", edges)
	}
}
