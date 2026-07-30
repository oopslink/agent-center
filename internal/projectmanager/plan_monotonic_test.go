package projectmanager

import (
	"errors"
	"testing"
	"time"
)

func TestPlanMonotonicLifecycle(t *testing.T) {
	t0 := time.Unix(1000, 0).UTC()
	p, err := NewPlan(NewPlanInput{ID: "plan-1", ProjectID: "project-1", Name: "ship", CreatorRef: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != PlanPending {
		t.Fatalf("new status = %q, want pending", p.Status())
	}
	if err := p.Start(t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := p.Pause(t0.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if p.Status() != PlanPaused {
		t.Fatalf("paused status = %q", p.Status())
	}
	if err := p.Resume(t0.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := p.MarkDone(t0.Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func() error{
		func() error { return p.Start(t0) },
		func() error { return p.Pause(t0) },
		func() error { return p.Resume(t0) },
		func() error { return p.Discard(t0) },
	} {
		if err := mutate(); !errors.Is(err, ErrIllegalPlanTransition) {
			t.Fatalf("terminal mutation = %v, want illegal transition", err)
		}
	}
}

func TestPlanArchiveIsOrthogonalAndTerminalOnly(t *testing.T) {
	t0 := time.Unix(1000, 0).UTC()
	p, _ := NewPlan(NewPlanInput{ID: "plan-1", ProjectID: "project-1", Name: "ship", CreatorRef: "user:a", CreatedAt: t0})
	if err := p.Archive(t0, "user:a"); !errors.Is(err, ErrPlanNotTerminal) {
		t.Fatalf("archive pending = %v, want not terminal", err)
	}
	if err := p.Discard(t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := p.Archive(t0.Add(2*time.Second), "user:a"); err != nil {
		t.Fatal(err)
	}
	if p.Status() != PlanDiscarded || !p.IsArchived() || p.ArchivedBy() != "user:a" {
		t.Fatalf("archive changed lifecycle: status=%q archived=%v by=%q", p.Status(), p.IsArchived(), p.ArchivedBy())
	}
	if err := p.Archive(t0.Add(3*time.Second), "user:a"); !errors.Is(err, ErrPlanArchived) {
		t.Fatalf("double archive = %v", err)
	}
}
