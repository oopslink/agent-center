package projectmanager

import (
	"errors"
	"testing"
	"time"
)

func TestPlan_Archive_RequiresTerminalLifecycle(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	if err := p.Archive(at); !errors.Is(err, ErrPlanNotTerminal) {
		t.Fatalf("Archive from pending = %v, want ErrPlanNotTerminal", err)
	}
	if err := p.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := p.Archive(at); !errors.Is(err, ErrPlanNotTerminal) {
		t.Fatalf("Archive from running = %v, want ErrPlanNotTerminal", err)
	}
}

func TestPlan_Archive_IsOrthogonalMarker(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	if err := p.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := p.MarkDone(at); err != nil {
		t.Fatal(err)
	}
	v0 := p.Version()
	if err := p.Archive(at, "user:a"); err != nil {
		t.Fatalf("Archive from done: %v", err)
	}
	if p.Status() != PlanDone || !p.IsArchived() || p.ArchivedBy() != "user:a" {
		t.Fatalf("status=%q archived=%v by=%q", p.Status(), p.IsArchived(), p.ArchivedBy())
	}
	if p.Version() != v0+1 {
		t.Fatalf("version = %d want %d", p.Version(), v0+1)
	}
	if err := p.Archive(at, "user:a"); !errors.Is(err, ErrPlanArchived) {
		t.Fatalf("double archive = %v want ErrPlanArchived", err)
	}
}
