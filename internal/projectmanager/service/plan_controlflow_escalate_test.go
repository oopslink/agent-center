package service

import (
	"strings"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestApplyLoopbacks_ExhaustionEscalates is the v2.23.0 escape-redesign (A,
// issue-624bfb53) counterpart to the T272 legacy regression: a NEW-scaffold cycle
// has NO Escape vertex / no reject_exhausted conditional edge. When its Decision
// loopback exhausts, the engine must (a) record the terminal `reject_exhausted`
// outcome and (b) escalate to the PD/creator via a single @mention — NOT silently
// drop it, and NOT require an escape vertex. has_failed stays unset (an exhausted
// loop awaiting a human ruling is not a failure, Q1).
func TestApplyLoopbacks_ExhaustionEscalates(t *testing.T) {
	f := newAutoFixture(t, &fakeDecisionGate{verdict: pm.GateRed})

	pid, err := f.svc.CreateProject(f.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := f.svc.CreatePlan(f.ctx, CreatePlanCommand{ProjectID: pid, Name: "cycle", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	// Decision is owned by the PD — escalation should @mention the assignee.
	dec, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "F1 · Decision", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "F1 · Dev", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "F1 · Review", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tid := range []pm.TaskID{dec, dev, rev} {
		if err := f.svc.SelectTaskIntoPlan(f.ctx, planID, tid, "user:pd"); err != nil {
			t.Fatal(err)
		}
	}
	// Forward seq chain Dev → Review → Decision (loopback validity).
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{PlanID: planID, FromTaskID: rev, ToTaskID: dev, Kind: pm.EdgeSeq}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: rev, Kind: pm.EdgeSeq}); err != nil {
		t.Fatal(err)
	}
	// Bounded loopback Decision --loopback(reject, max=1)--> Dev. NO escape edge
	// (new scaffold shape).
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 1}); err != nil {
		t.Fatal(err)
	}

	// Drive the Decision to Completed with outcome=reject, then exhaust the loop.
	dt, err := f.tasks.FindByID(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	now := f.clk.Now()
	if err := dt.Assign("user:pd", now); err != nil {
		t.Fatal(err)
	}
	if err := dt.Start(now); err != nil {
		t.Fatal(err)
	}
	if err := dt.Complete("user:pd", now); err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Update(f.ctx, dt); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.plans.RecordDecisionOutcome(f.ctx, planID, dec, "reject", now); err != nil {
		t.Fatal(err)
	}
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

	// (a) terminal reject_exhausted outcome recorded.
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
		t.Fatalf("decision outcome after exhaustion = %q, want reject_exhausted", got)
	}

	// (b) exactly one escalation @mention fired, targeting the Decision owner.
	if f.disp.posts != 1 {
		t.Fatalf("escalation @mention fired %d times; want exactly 1", f.disp.posts)
	}
	if f.disp.lastTarget != "user:pd" {
		t.Errorf("escalation target = %q, want the Decision owner user:pd", f.disp.lastTarget)
	}
	if !strings.Contains(f.disp.lastContent, "escalated") {
		t.Errorf("escalation content should mention escalation, got %q", f.disp.lastContent)
	}

	// Decision stays Completed (terminal) — NOT blocked/failed (Q1/Q6).
	dt2, err := f.tasks.FindByID(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	if !pm.TaskIsDone(dt2.Status()) {
		t.Errorf("decision status = %q, want a done/completed terminal (escalation must not block/fail it)", dt2.Status())
	}
}
