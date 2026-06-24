package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// reviewCycle builds a Dev→Review→Decision cycle (Decision routes pass→Integrate,
// reject_exhausted→Escape, loopback(reject)→Dev). The Decision carries branch/base so
// B3's gate runs on it. Returns plan + review + decision + dev ids.
func (f *autoFixture) reviewCycle(t *testing.T) (pm.PlanID, pm.TaskID, pm.TaskID, pm.TaskID) {
	t.Helper()
	pid, err := f.svc.CreateProject(f.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pm.NewCodeRepoRef(pm.NewCodeRepoRefInput{
		ID: "repo-1", ProjectID: pid, URL: "https://example.com/repo.git", AddedBy: "user:pd", CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.codeRepoRefs.Save(f.ctx, ref); err != nil {
		t.Fatal(err)
	}
	planID, err := f.svc.CreatePlan(f.ctx, CreatePlanCommand{ProjectID: pid, Name: "cycle", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(title string, role pm.CycleNodeRole, branch string) pm.TaskID {
		cmd := CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:pd", Role: role}
		if branch != "" {
			cmd.Branch, cmd.Base = branch, "dev/v2.13.0"
		}
		id, err := f.svc.CreateTask(f.ctx, cmd)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.svc.SelectTaskIntoPlan(f.ctx, planID, id, "user:pd"); err != nil {
			t.Fatal(err)
		}
		return id
	}
	dev := mk("Dev", pm.CycleRoleDev, "T9")
	review := mk("Review", pm.CycleRoleReview, "")
	dec := mk("Decision", pm.CycleRoleReview, "T9")
	integ := mk("Integrate", pm.CycleRoleIntegrate, "")
	esc := mk("Escape", pm.CycleRoleReview, "")
	edges := []pm.Dependency{
		{PlanID: planID, FromTaskID: review, ToTaskID: dev, Kind: pm.EdgeSeq},
		{PlanID: planID, FromTaskID: dec, ToTaskID: review, Kind: pm.EdgeSeq},
		{PlanID: planID, FromTaskID: integ, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"},
		{PlanID: planID, FromTaskID: esc, ToTaskID: dec, Kind: pm.EdgeConditional, When: "reject_exhausted"},
		{PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 3},
	}
	for _, e := range edges {
		if err := f.svc.plans.AddDependency(f.ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	return planID, review, dec, dev
}

// A non-blocking pass verdict auto-passes the Decision — EVEN with an open failure
// finding (a nit) that would have wedged the legacy comment-count rule into a defer.
func TestComputeAutoDecision_VerdictPassNonBlocking_AutoPass_T468(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateGreen})
	planID, review, dec, _ := f.reviewCycle(t)
	// A lingering nit on the decision node — legacy rule would DEFER on this.
	f.addFailureFinding(t, planID, dec, f.projectOf(t, dec))

	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewPass, false, "minor nit, non-blocking", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	ad, err := f.svc.ComputeAutoDecision(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	if !ad.Decided || ad.Outcome != pm.OutcomePass {
		t.Fatalf("got %+v; want auto-pass (verdict overrides the nit)", ad)
	}
	if ad.Verdict == nil || ad.Verdict.Verdict != pm.ReviewPass {
		t.Fatalf("expected the consulted verdict surfaced, got %+v", ad.Verdict)
	}
}

func TestComputeAutoDecision_VerdictReject_AutoReject_T468(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateGreen})
	_, review, dec, _ := f.reviewCycle(t)
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewReject, false, "missing X", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	ad, _ := f.svc.ComputeAutoDecision(f.ctx, dec)
	if !ad.Decided || ad.Outcome != pm.OutcomeReject {
		t.Fatalf("got %+v; want auto-reject", ad)
	}
}

func TestComputeAutoDecision_VerdictPassBlocking_AutoReject_T468(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateGreen})
	_, review, dec, _ := f.reviewCycle(t)
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewPass, true, "blocker found", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	ad, _ := f.svc.ComputeAutoDecision(f.ctx, dec)
	if !ad.Decided || ad.Outcome != pm.OutcomeReject {
		t.Fatalf("got %+v; want auto-reject (blocking)", ad)
	}
}

// A verdict from an EARLIER round (a loopback bumped the round, reviewer hasn't
// re-recorded) must NOT auto-route — B3 defers, surfacing the stale verdict.
func TestComputeAutoDecision_StaleRoundVerdict_Defers_T468(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateGreen})
	planID, review, dec, dev := f.reviewCycle(t)
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewPass, false, "", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	// A reject loopback bumps the round to 1; the verdict stays round 0 (stale).
	if _, err := f.svc.plans.IncrementLoopRound(f.ctx, planID, dec, dev); err != nil {
		t.Fatal(err)
	}
	ad, _ := f.svc.ComputeAutoDecision(f.ctx, dec)
	if ad.Decided {
		t.Fatalf("got %+v; want DEFER on a stale-round verdict (never auto-route)", ad)
	}
	if ad.Verdict == nil || ad.Verdict.Round != 0 {
		t.Fatalf("expected the stale verdict (round 0) surfaced, got %+v", ad.Verdict)
	}
}

// After a loopback bumps the round, a freshly-recorded verdict (current round) auto-decides.
func TestComputeAutoDecision_NewRoundVerdict_AutoDecides_T468(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateGreen})
	planID, review, dec, dev := f.reviewCycle(t)
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewReject, false, "r0", "sha0", "user:pd"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.plans.IncrementLoopRound(f.ctx, planID, dec, dev); err != nil { // round → 1
		t.Fatal(err)
	}
	// Reviewer re-records for round 1 (overwrites the round-0 slot, stamped round 1).
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewPass, false, "fixed", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := f.svc.plans.GetReviewVerdict(f.ctx, planID, review)
	if err != nil || !ok || got.Round != 1 || got.Verdict != pm.ReviewPass {
		t.Fatalf("verdict slot after re-record = (%+v,%t,%v), want round1/pass", got, ok, err)
	}
	ad, _ := f.svc.ComputeAutoDecision(f.ctx, dec)
	if !ad.Decided || ad.Outcome != pm.OutcomePass {
		t.Fatalf("got %+v; want auto-pass on the fresh round-1 verdict", ad)
	}
}

// No structured verdict → legacy open-comment fallback (back-compat): clean → pass;
// with a finding → defer.
func TestComputeAutoDecision_NoVerdict_LegacyFallback_T468(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateGreen})
	planID, _, dec, _ := f.reviewCycle(t)
	ad, _ := f.svc.ComputeAutoDecision(f.ctx, dec)
	if !ad.Decided || ad.Outcome != pm.OutcomePass {
		t.Fatalf("no verdict + clean → legacy auto-pass; got %+v", ad)
	}
	f.addFailureFinding(t, planID, dec, f.projectOf(t, dec))
	ad2, _ := f.svc.ComputeAutoDecision(f.ctx, dec)
	if ad2.Decided {
		t.Fatalf("no verdict + open finding → legacy DEFER; got %+v", ad2)
	}
}

// The gate floor is absolute: a red/unknown gate never auto-passes even on verdict=pass.
func TestComputeAutoDecision_GateFloorBeatsVerdict_T468(t *testing.T) {
	// red → reject regardless of a pass verdict.
	fr := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateRed})
	_, review, dec, _ := fr.reviewCycle(t)
	if err := fr.svc.RecordReviewVerdict(fr.ctx, review, pm.ReviewPass, false, "", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	if ad, _ := fr.svc.ComputeAutoDecision(fr.ctx, dec); !ad.Decided || ad.Outcome != pm.OutcomeReject {
		t.Fatalf("red gate must auto-reject even on verdict=pass; got %+v", ad)
	}
	// unknown → defer regardless of a pass verdict.
	fu := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateUnknown})
	_, review2, dec2, _ := fu.reviewCycle(t)
	if err := fu.svc.RecordReviewVerdict(fu.ctx, review2, pm.ReviewPass, false, "", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	if ad, _ := fu.svc.ComputeAutoDecision(fu.ctx, dec2); ad.Decided {
		t.Fatalf("unknown gate must DEFER even on verdict=pass; got %+v", ad)
	}
}
