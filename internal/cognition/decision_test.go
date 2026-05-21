package cognition_test

import (
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
)

func TestNewDecisionRecord_Happy(t *testing.T) {
	now := time.Now().UTC()
	d, err := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID:             "D1",
		InvocationID:   "I1",
		Kind:           cognition.DecisionDispatch,
		TargetRefsJSON: `{"task_id":"T-1"}`,
		Rationale:      "W-1 idle",
		Outcome:        cognition.OutcomeSucceeded,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("happy: %v", err)
	}
	if d.ID() != "D1" || d.InvocationID() != "I1" {
		t.Errorf("ids: %s %s", d.ID(), d.InvocationID())
	}
	if d.Kind() != cognition.DecisionDispatch {
		t.Errorf("kind = %s", d.Kind())
	}
	if d.Outcome() != cognition.OutcomeSucceeded {
		t.Errorf("outcome = %s", d.Outcome())
	}
	if d.TargetRefsJSON() != `{"task_id":"T-1"}` {
		t.Errorf("refs = %q", d.TargetRefsJSON())
	}
	if d.OutcomeMessage() != "" {
		t.Errorf("msg %q", d.OutcomeMessage())
	}
}

func TestNewDecisionRecord_DefaultEmptyRefs(t *testing.T) {
	d, err := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID: "D1", InvocationID: "I1", Kind: cognition.DecisionNoOp,
		TargetRefsJSON: "", Rationale: "thinking", Outcome: cognition.OutcomeSucceeded,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("empty refs: %v", err)
	}
	if d.TargetRefsJSON() != "{}" {
		t.Errorf("default refs = %q", d.TargetRefsJSON())
	}
}

func TestNewDecisionRecord_Validation(t *testing.T) {
	now := time.Now()
	base := cognition.NewDecisionInput{
		ID: "D1", InvocationID: "I1", Kind: cognition.DecisionNoOp,
		Rationale: "x", Outcome: cognition.OutcomeSucceeded, CreatedAt: now,
	}
	cases := []struct {
		name string
		mut  func(in *cognition.NewDecisionInput)
		want error
	}{
		{"empty id", func(in *cognition.NewDecisionInput) { in.ID = "" }, nil}, // generic error string
		{"empty invocation", func(in *cognition.NewDecisionInput) { in.InvocationID = "" }, cognition.ErrInvocationIDRequired},
		{"bad kind", func(in *cognition.NewDecisionInput) { in.Kind = cognition.DecisionKind("bogus") }, cognition.ErrUnknownDecisionKind},
		{"empty rationale", func(in *cognition.NewDecisionInput) { in.Rationale = " " }, cognition.ErrRationaleRequired},
		{"bad outcome", func(in *cognition.NewDecisionInput) { in.Outcome = cognition.DecisionOutcome("xx") }, nil},
		{"failed without msg", func(in *cognition.NewDecisionInput) {
			in.Outcome = cognition.OutcomeFailed
		}, nil},
		{"zero created_at", func(in *cognition.NewDecisionInput) { in.CreatedAt = time.Time{} }, nil},
	}
	for _, tc := range cases {
		in := base
		tc.mut(&in)
		_, err := cognition.NewDecisionRecord(in)
		if err == nil {
			t.Errorf("%s: expected error", tc.name)
			continue
		}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestNewDecisionRecord_FailedNeedsMsg(t *testing.T) {
	now := time.Now()
	d, err := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID: "D1", InvocationID: "I1", Kind: cognition.DecisionDispatch,
		Rationale: "r", Outcome: cognition.OutcomeFailed, OutcomeMessage: "exit 1",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("failed with msg: %v", err)
	}
	if d.OutcomeMessage() != "exit 1" {
		t.Errorf("msg = %q", d.OutcomeMessage())
	}
}

func TestRehydrateDecision(t *testing.T) {
	now := time.Now()
	d, err := cognition.RehydrateDecision(cognition.RehydrateDecisionInput{
		ID: "D", InvocationID: "I", Kind: cognition.DecisionConcludeIssue,
		TargetRefsJSON: `{}`, Rationale: "r", Outcome: cognition.OutcomeSucceeded,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if d.Kind() != cognition.DecisionConcludeIssue {
		t.Errorf("kind = %s", d.Kind())
	}
	if _, err := cognition.RehydrateDecision(cognition.RehydrateDecisionInput{
		ID: "D", InvocationID: "I", Kind: cognition.DecisionKind("bogus"),
		Rationale: "r", Outcome: cognition.OutcomeSucceeded, CreatedAt: now,
	}); !errors.Is(err, cognition.ErrUnknownDecisionKind) {
		t.Fatal("expected ErrUnknownDecisionKind")
	}
	if _, err := cognition.RehydrateDecision(cognition.RehydrateDecisionInput{
		ID: "D", InvocationID: "I", Kind: cognition.DecisionNoOp,
		Rationale: "r", Outcome: cognition.DecisionOutcome("xx"), CreatedAt: now,
	}); err == nil {
		t.Fatal("expected outcome err")
	}
}
