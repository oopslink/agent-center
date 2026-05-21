package cognition_test

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestSupervisorInvocation_GettersFullCoverage(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1", "E2"})
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID: "INV1", Scope: scope, TriggerEvents: tes, PromptBlobRef: "blob:x",
		StartedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ID() != "INV1" {
		t.Error("ID")
	}
	if inv.Scope() != scope {
		t.Error("Scope")
	}
	if inv.TriggerEvents().Len() != 2 {
		t.Error("TriggerEvents")
	}
	if inv.Status() != cognition.StatusRunning {
		t.Error("Status")
	}
	if inv.HardTimeoutSeconds() != 180 {
		t.Error("HardTimeoutSeconds")
	}
	if !inv.StartedAt().Equal(now) {
		t.Error("StartedAt")
	}
	if inv.EndedAt() != nil {
		t.Error("EndedAt")
	}
	if inv.FailedReason() != "" {
		t.Error("FailedReason")
	}
	if inv.FailedMessage() != "" {
		t.Error("FailedMessage")
	}
	if inv.TimedOutAt() != nil {
		t.Error("TimedOutAt")
	}
	if !inv.TokenUsage().IsZero() {
		t.Error("TokenUsage")
	}
	if inv.DecisionsMade() != 0 {
		t.Error("DecisionsMade")
	}
	if inv.PromptBlobRef() != "blob:x" {
		t.Error("PromptBlobRef")
	}
	if !inv.CreatedAt().Equal(now) {
		t.Error("CreatedAt")
	}
	if !inv.UpdatedAt().Equal(now) {
		t.Error("UpdatedAt")
	}
	if inv.Version() != 1 {
		t.Error("Version")
	}
	if inv.IsTerminal() {
		t.Error("IsTerminal")
	}

	// transition + verify pointer-returning getters are copies
	if err := inv.MarkSucceeded(now.Add(time.Second), cognition.TokenUsage{Input: 1}, 1); err != nil {
		t.Fatal(err)
	}
	e1 := inv.EndedAt()
	if e1 == nil {
		t.Fatal("EndedAt nil after Mark")
	}
	*e1 = time.Time{}
	if inv.EndedAt() == nil || inv.EndedAt().IsZero() {
		t.Error("EndedAt should return a copy that callers cannot mutate")
	}
}

func TestDecisionRecord_GettersFullCoverage(t *testing.T) {
	now := time.Now().UTC()
	d, err := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID: "D1", InvocationID: "I1", Kind: cognition.DecisionDispatch,
		TargetRefsJSON: `{"task_id":"T-1"}`, Rationale: "r",
		Outcome: cognition.OutcomeSucceeded, OutcomeMessage: "",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != "D1" {
		t.Error("ID")
	}
	if d.InvocationID() != "I1" {
		t.Error("InvocationID")
	}
	if d.Kind() != cognition.DecisionDispatch {
		t.Error("Kind")
	}
	if d.TargetRefsJSON() != `{"task_id":"T-1"}` {
		t.Error("TargetRefsJSON")
	}
	if d.Rationale() != "r" {
		t.Error("Rationale")
	}
	if d.Outcome() != cognition.OutcomeSucceeded {
		t.Error("Outcome")
	}
	if d.OutcomeMessage() != "" {
		t.Error("OutcomeMessage")
	}
	if !d.CreatedAt().Equal(now) {
		t.Error("CreatedAt")
	}
}

func TestTimedOutAt_ReturnsCopy(t *testing.T) {
	now := time.Now().UTC()
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INV", Scope: scope, TriggerEvents: tes, StartedAt: now})
	_ = inv.MarkTimedOut(now.Add(time.Minute))
	t1 := inv.TimedOutAt()
	if t1 == nil {
		t.Fatal("nil")
	}
	*t1 = time.Time{}
	if inv.TimedOutAt() == nil || inv.TimedOutAt().IsZero() {
		t.Error("TimedOutAt should be a copy")
	}
}
