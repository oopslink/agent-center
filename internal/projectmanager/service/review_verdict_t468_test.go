package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// reviewCycle builds a Dev→Review→Decision cycle (Decision routes pass→Integrate,
// reject_exhausted→Escape, loopback(reject)→Dev). Returns plan + review + decision + dev ids.
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
	mk := func(title string) pm.TaskID {
		cmd := CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:pd"}
		id, err := f.svc.CreateTask(f.ctx, cmd)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.svc.SelectTaskIntoPlan(f.ctx, planID, id, "user:pd"); err != nil {
			t.Fatal(err)
		}
		return id
	}
	dev := mk("Dev")
	review := mk("Review")
	dec := mk("Decision")
	integ := mk("Integrate")
	esc := mk("Escape")
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

// With branch/base removed from Task, the gate always returns GateUnknown, so the
// gate-dependent tests now expect DEFER (IsDecision=true, Decided=false) instead of
// auto-pass/reject. The review verdict recording and round stamping are still exercised.

func TestComputeAutoDecision_VerdictPassNonBlocking_DefersWithoutGate_T468(t *testing.T) {
	f := newAutoFixture(t)
	_, review, dec, _ := f.reviewCycle(t)

	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewPass, false, "minor nit, non-blocking", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	ad, err := f.svc.ComputeAutoDecision(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	// Gate always returns GateUnknown now (branch/base removed from Task), so B3 defers.
	if ad.IsDecision && ad.Decided {
		t.Fatalf("expected DEFER (gate always unknown without branch/base); got %+v", ad)
	}
}

func TestComputeAutoDecision_StaleRoundVerdict_Defers_T468(t *testing.T) {
	f := newAutoFixture(t)
	planID, review, dec, dev := f.reviewCycle(t)
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewPass, false, "", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.plans.IncrementLoopRound(f.ctx, planID, dec, dev); err != nil {
		t.Fatal(err)
	}
	ad, _ := f.svc.ComputeAutoDecision(f.ctx, dec)
	if ad.Decided {
		t.Fatalf("got %+v; want DEFER on a stale-round verdict (never auto-route)", ad)
	}
}

func TestComputeAutoDecision_NewRoundVerdict_RecordsRound_T468(t *testing.T) {
	f := newAutoFixture(t)
	planID, review, dec, dev := f.reviewCycle(t)
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewReject, false, "r0", "sha0", "user:pd"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.plans.IncrementLoopRound(f.ctx, planID, dec, dev); err != nil {
		t.Fatal(err)
	}
	_ = dec // used only by IncrementLoopRound
	// Reviewer re-records for round 1.
	if err := f.svc.RecordReviewVerdict(f.ctx, review, pm.ReviewPass, false, "fixed", "sha1", "user:pd"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := f.svc.plans.GetReviewVerdict(f.ctx, planID, review)
	if err != nil || !ok || got.Round != 1 || got.Verdict != pm.ReviewPass {
		t.Fatalf("verdict slot after re-record = (%+v,%t,%v), want round1/pass", got, ok, err)
	}
}
