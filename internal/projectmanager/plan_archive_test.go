package projectmanager

import (
	"testing"
	"time"
)

// TestPlan_Archive_FromDraft: a draft plan archives (non-running), version bumps,
// status becomes archived.
func TestPlan_Archive_FromDraft(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	v0 := p.Version()
	if err := p.Archive(at); err != nil {
		t.Fatalf("Archive from draft: %v", err)
	}
	if p.Status() != PlanArchived {
		t.Fatalf("status = %q want archived", p.Status())
	}
	if p.Version() != v0+1 {
		t.Fatalf("version = %d want %d", p.Version(), v0+1)
	}
}

// TestPlan_Archive_FromDone: a done plan archives (non-running).
func TestPlan_Archive_FromDone(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	if err := p.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := p.MarkDone(at); err != nil {
		t.Fatal(err)
	}
	if err := p.Archive(at); err != nil {
		t.Fatalf("Archive from done: %v", err)
	}
	if p.Status() != PlanArchived {
		t.Fatalf("status = %q want archived", p.Status())
	}
}

// TestPlan_Archive_FromRunningRejected: a running plan cannot be archived —
// ErrPlanRunning (stop/finish first).
func TestPlan_Archive_FromRunningRejected(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	if err := p.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := p.Archive(at); err != ErrPlanRunning {
		t.Fatalf("Archive from running = %v want ErrPlanRunning", err)
	}
	if p.Status() != PlanRunning {
		t.Fatalf("status = %q want running (unchanged)", p.Status())
	}
}

// TestPlan_Archive_DoubleRejected: re-archiving an archived plan → ErrPlanArchived.
func TestPlan_Archive_DoubleRejected(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	if err := p.Archive(at); err != nil {
		t.Fatal(err)
	}
	if err := p.Archive(at); err != ErrPlanArchived {
		t.Fatalf("double archive = %v want ErrPlanArchived", err)
	}
}

// TestPlan_Archived_IsTerminal: nothing transitions out of archived (irreversible).
func TestPlan_Archived_IsTerminal(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	if err := p.Archive(at); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(at); err != ErrIllegalPlanTransition {
		t.Fatalf("Start from archived = %v want ErrIllegalPlanTransition", err)
	}
	if err := p.Stop(at); err != ErrIllegalPlanTransition {
		t.Fatalf("Stop from archived = %v want ErrIllegalPlanTransition", err)
	}
	if err := p.MarkDone(at); err != ErrIllegalPlanTransition {
		t.Fatalf("MarkDone from archived = %v want ErrIllegalPlanTransition", err)
	}
}
