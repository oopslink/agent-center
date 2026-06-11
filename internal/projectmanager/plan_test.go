package projectmanager

import (
	"testing"
	"time"
)

func newPlan(t *testing.T) *Plan {
	t.Helper()
	p, err := NewPlan(NewPlanInput{
		ID: "PL-1", ProjectID: "P-1", Name: "v3.0", Description: "the v3.0 plan",
		CreatorRef: "user:alice", CreatedAt: t0,
	})
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	return p
}

func TestNewPlan_Validation(t *testing.T) {
	base := NewPlanInput{ID: "PL-1", ProjectID: "P-1", Name: "v3.0", CreatorRef: "user:a", CreatedAt: t0}
	cases := []struct {
		name string
		mut  func(in *NewPlanInput)
		want error
	}{
		{"ok", func(in *NewPlanInput) {}, nil},
		{"empty id", func(in *NewPlanInput) { in.ID = "" }, nil /* non-nil err, any */},
		{"empty project", func(in *NewPlanInput) { in.ProjectID = "" }, ErrEmptyProjectScope},
		{"empty name", func(in *NewPlanInput) { in.Name = "" }, ErrEmptyPlanName},
		{"bad creator", func(in *NewPlanInput) { in.CreatorRef = "nope" }, nil /* non-nil err */},
		{"zero created", func(in *NewPlanInput) { in.CreatedAt = time.Time{} }, nil /* non-nil err */},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base
			c.mut(&in)
			_, err := NewPlan(in)
			if c.name == "ok" {
				if err != nil {
					t.Fatalf("want nil err, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if c.want != nil && err != c.want {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
}

func TestNewPlan_DefaultsDraft(t *testing.T) {
	p := newPlan(t)
	if p.Status() != PlanDraft {
		t.Fatalf("new plan status = %q, want draft", p.Status())
	}
	if p.Version() != 1 {
		t.Fatalf("new plan version = %d, want 1", p.Version())
	}
	if p.ConversationID() != "" {
		t.Fatalf("new plan conversation = %q, want empty (wired in #284)", p.ConversationID())
	}
}

func TestPlan_Lifecycle(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)

	// draft → running
	if err := p.Start(at); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if p.Status() != PlanRunning {
		t.Fatalf("after Start status = %q, want running", p.Status())
	}
	if p.Version() != 2 {
		t.Fatalf("after Start version = %d, want 2", p.Version())
	}

	// running → running illegal
	if err := p.Start(at); err != ErrIllegalPlanTransition {
		t.Fatalf("Start while running = %v, want ErrIllegalPlanTransition", err)
	}

	// running → draft (stop §9.4)
	if err := p.Stop(at); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if p.Status() != PlanDraft {
		t.Fatalf("after Stop status = %q, want draft", p.Status())
	}

	// draft can't MarkDone
	if err := p.MarkDone(at); err != ErrIllegalPlanTransition {
		t.Fatalf("MarkDone from draft = %v, want ErrIllegalPlanTransition", err)
	}

	// draft → running → done
	if err := p.Start(at); err != nil {
		t.Fatalf("re-Start: %v", err)
	}
	if err := p.MarkDone(at); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	if p.Status() != PlanDone {
		t.Fatalf("after MarkDone status = %q, want done", p.Status())
	}

	// done is terminal: no transition out
	if err := p.Start(at); err != ErrIllegalPlanTransition {
		t.Fatalf("Start from done = %v, want ErrIllegalPlanTransition", err)
	}
	if err := p.Stop(at); err != ErrIllegalPlanTransition {
		t.Fatalf("Stop from done = %v, want ErrIllegalPlanTransition", err)
	}
}

func TestPlanStatus_TransitionMap(t *testing.T) {
	if !PlanDraft.CanTransitionTo(PlanRunning) {
		t.Fatal("draft→running must be allowed")
	}
	if PlanDraft.CanTransitionTo(PlanDone) {
		t.Fatal("draft→done must NOT be allowed")
	}
	if !PlanRunning.CanTransitionTo(PlanDraft) || !PlanRunning.CanTransitionTo(PlanDone) {
		t.Fatal("running→{draft,done} must be allowed")
	}
	// v2.9 P3: draft + done can archive (both non-running); running can NOT (stop
	// first); archived is terminal + irreversible.
	if !PlanDraft.CanTransitionTo(PlanArchived) {
		t.Fatal("draft→archived must be allowed")
	}
	if !PlanDone.CanTransitionTo(PlanArchived) {
		t.Fatal("done→archived must be allowed")
	}
	if PlanRunning.CanTransitionTo(PlanArchived) {
		t.Fatal("running→archived must NOT be allowed (stop/finish first)")
	}
	if len(planTransitions[PlanArchived]) != 0 {
		t.Fatal("archived must be terminal (irreversible)")
	}
	for _, s := range []PlanStatus{PlanDraft, PlanRunning, PlanDone, PlanArchived} {
		if !s.IsValid() {
			t.Fatalf("%q must be IsValid", s)
		}
	}
	if PlanStatus("bogus").IsValid() {
		t.Fatal("bogus status must not be valid")
	}
}

func TestPlan_Mutators(t *testing.T) {
	p := newPlan(t)
	at := t0.Add(time.Hour)
	v0 := p.Version()

	if err := p.Rename("v3.1", at); err != nil {
		t.Fatal(err)
	}
	if p.Name() != "v3.1" {
		t.Fatalf("name = %q", p.Name())
	}
	if err := p.Rename("  ", at); err != ErrEmptyPlanName {
		t.Fatalf("blank rename = %v, want ErrEmptyPlanName", err)
	}

	p.SetDescription("new goal", at)
	if p.Description() != "new goal" {
		t.Fatalf("desc = %q", p.Description())
	}

	td := t0.Add(72 * time.Hour)
	p.SetTargetDate(&td, at)
	if p.TargetDate() == nil || !p.TargetDate().Equal(td.UTC()) {
		t.Fatalf("target date = %v", p.TargetDate())
	}
	p.SetTargetDate(nil, at)
	if p.TargetDate() != nil {
		t.Fatalf("target date should clear to nil, got %v", p.TargetDate())
	}

	p.SetConversationID("C-9", at)
	if p.ConversationID() != "C-9" {
		t.Fatalf("conversation = %q", p.ConversationID())
	}

	if p.Version() <= v0 {
		t.Fatalf("mutators must bump version (was %d, now %d)", v0, p.Version())
	}
}

func TestRehydratePlan(t *testing.T) {
	td := t0.Add(time.Hour)
	p, err := RehydratePlan(RehydratePlanInput{
		ID: "PL-9", ProjectID: "P-9", Name: "v4.0", Status: PlanRunning,
		CreatorRef: "agent:bot", ConversationID: "C-1", TargetDate: &td,
		CreatedAt: t0, UpdatedAt: t0, Version: 7,
	})
	if err != nil {
		t.Fatalf("RehydratePlan: %v", err)
	}
	if p.Status() != PlanRunning || p.Version() != 7 || p.ConversationID() != "C-1" {
		t.Fatalf("rehydrated = %+v", p)
	}
	if _, err := RehydratePlan(RehydratePlanInput{Status: "bogus", Version: 1}); err != ErrInvalidPlanStatus {
		t.Fatalf("bad status = %v, want ErrInvalidPlanStatus", err)
	}
	if _, err := RehydratePlan(RehydratePlanInput{Status: PlanDraft, Version: 0}); err == nil {
		t.Fatal("version 0 must error")
	}
}
