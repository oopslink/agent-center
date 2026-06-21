package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestApplyLoopbacks_ExhaustionRoutesToEscape is the T272 regression guard for the
// B-track loopback escape-edge bug (acceptance finding F-T272-1): when a Decision's
// bounded loopback EXHAUSTS its rounds, the engine must auto-rewrite the outcome to
// <outcome>_exhausted and route to the Escape node — NOT silently fall through to
// notifyLoopStuck. The escape edge convention is From=Escape / To=Decision, so the
// driver must match it by ToTaskID==decision (the bug used FromTaskID).
func TestApplyLoopbacks_ExhaustionRoutesToEscape(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateRed})

	pid, err := f.svc.CreateProject(f.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := f.svc.CreatePlan(f.ctx, CreatePlanCommand{ProjectID: pid, Name: "cycle", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "Decision", CreatedBy: "user:pd",
		Role: pm.CycleRoleDecision, Branch: "T9", Base: "dev/v2.13.0"})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "Dev", CreatedBy: "user:pd", Role: pm.CycleRoleDev})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "Review", CreatedBy: "user:pd", Role: pm.CycleRoleReview})
	if err != nil {
		t.Fatal(err)
	}
	esc, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "Escape", CreatedBy: "user:pd", Role: pm.CycleRoleEscape})
	if err != nil {
		t.Fatal(err)
	}
	for _, tid := range []pm.TaskID{dec, dev, rev, esc} {
		if err := f.svc.SelectTaskIntoPlan(f.ctx, planID, tid, "user:pd"); err != nil {
			t.Fatal(err)
		}
	}
	// Forward seq chain so Dev is a forward ancestor of Decision (loopback validity
	// requires it): Dev → Review → Decision.
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{
		PlanID: planID, FromTaskID: rev, ToTaskID: dev, Kind: pm.EdgeSeq,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{
		PlanID: planID, FromTaskID: dec, ToTaskID: rev, Kind: pm.EdgeSeq,
	}); err != nil {
		t.Fatal(err)
	}
	// Bounded loopback: Decision --loopback(reject, max=1)--> Dev (From=decision).
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{
		PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 1,
	}); err != nil {
		t.Fatal(err)
	}
	// Escape edge: Escape --conditional(reject_exhausted)--> Decision (To=decision).
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{
		PlanID: planID, FromTaskID: esc, ToTaskID: dec, Kind: pm.EdgeConditional, When: "reject_exhausted",
	}); err != nil {
		t.Fatal(err)
	}

	// Drive the decision to Completed with outcome=reject.
	dt, err := f.tasks.FindByID(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	now := f.clk.Now()
	if err := dt.Assign("user:dev", now); err != nil {
		t.Fatal(err)
	}
	if err := dt.Start(now); err != nil {
		t.Fatal(err)
	}
	if err := dt.Complete("user:dev", now); err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Update(f.ctx, dt); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.plans.RecordDecisionOutcome(f.ctx, planID, dec, "reject", now); err != nil {
		t.Fatal(err)
	}
	// Exhaust the loop: round reaches MaxRounds(1).
	if _, err := f.svc.plans.IncrementLoopRound(f.ctx, planID, dec, dev); err != nil {
		t.Fatal(err)
	}

	plan, err := f.svc.plans.FindByID(f.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.applyLoopbacks(f.ctx, plan, dec); err != nil {
		t.Fatalf("applyLoopbacks: %v", err)
	}

	// Post-fix expectation: the decision's outcome was auto-rewritten to
	// reject_exhausted (→ Escape becomes ready), and NO loop-stuck mention fired.
	outs, err := f.svc.plans.ListDecisionOutcomes(f.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, o := range outs {
		if o.TaskID == dec {
			got = o.Outcome
		}
	}
	if got != "reject_exhausted" {
		t.Fatalf("decision outcome after exhaustion = %q, want %q (escape edge must route on exhaustion)", got, "reject_exhausted")
	}
	if f.disp.posts != 0 {
		t.Fatalf("loop-stuck @mention fired %d times; want 0 (escape branch exists, so route to it instead of notifying stuck)", f.disp.posts)
	}
}
