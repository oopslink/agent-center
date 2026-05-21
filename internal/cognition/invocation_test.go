package cognition_test

import (
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

func newSpawn(t *testing.T, kind cognition.ScopeKind, key string, at time.Time) *cognition.SupervisorInvocation {
	t.Helper()
	scope := cognition.MustNewInvocationScope(kind, key)
	tes, err := cognition.NewTriggerEventSet([]observability.EventID{"01H1"})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID:            cognition.InvocationID("01HINV"),
		Scope:         scope,
		TriggerEvents: tes,
		StartedAt:     at,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return inv
}

func TestSpawn_Happy(t *testing.T) {
	now := time.Date(2026, 5, 22, 1, 0, 0, 0, time.UTC)
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	if inv.Status() != cognition.StatusRunning {
		t.Errorf("status = %s", inv.Status())
	}
	if inv.HardTimeoutSeconds() != 180 {
		t.Errorf("ht = %d", inv.HardTimeoutSeconds())
	}
	if inv.Version() != 1 {
		t.Errorf("version = %d", inv.Version())
	}
	if inv.IsTerminal() {
		t.Error("running terminal?")
	}
	if !inv.StartedAt().Equal(now) {
		t.Errorf("startedAt = %v", inv.StartedAt())
	}
	if inv.EndedAt() != nil {
		t.Errorf("endedAt non-nil while running")
	}
	if inv.TimedOutAt() != nil {
		t.Errorf("timedOutAt non-nil while running")
	}
	if !inv.TokenUsage().IsZero() {
		t.Errorf("tokens non-zero")
	}
	if inv.DecisionsMade() != 0 {
		t.Errorf("decisions = %d", inv.DecisionsMade())
	}
	if inv.PromptBlobRef() != "" {
		t.Errorf("blobref = %q", inv.PromptBlobRef())
	}
}

func TestSpawn_GlobalHasLargerTimeout(t *testing.T) {
	scope, _ := cognition.NewInvocationScope(cognition.ScopeGlobal, "")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"01H1"})
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID:            "X",
		Scope:         scope,
		TriggerEvents: tes,
		StartedAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if inv.HardTimeoutSeconds() != 600 {
		t.Errorf("ht = %d, want 600", inv.HardTimeoutSeconds())
	}
}

func TestSpawn_BadInputs(t *testing.T) {
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E"})
	now := time.Now()
	cases := []struct {
		name string
		in   cognition.SpawnInput
	}{
		{"empty id", cognition.SpawnInput{Scope: scope, TriggerEvents: tes, StartedAt: now}},
		{"zero scope", cognition.SpawnInput{ID: "X", TriggerEvents: tes, StartedAt: now}},
		{"no trigger events", cognition.SpawnInput{ID: "X", Scope: scope, StartedAt: now}},
		{"zero startedAt", cognition.SpawnInput{ID: "X", Scope: scope, TriggerEvents: tes}},
	}
	for _, tc := range cases {
		if _, err := cognition.Spawn(tc.in); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestMarkSucceeded(t *testing.T) {
	now := time.Date(2026, 5, 22, 1, 0, 0, 0, time.UTC)
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	tu := cognition.TokenUsage{Input: 100, Output: 50}
	end := now.Add(30 * time.Second)
	if err := inv.MarkSucceeded(end, tu, 3); err != nil {
		t.Fatalf("succeeded: %v", err)
	}
	if inv.Status() != cognition.StatusSucceeded {
		t.Errorf("status = %s", inv.Status())
	}
	if inv.EndedAt() == nil || !inv.EndedAt().Equal(end) {
		t.Errorf("endedAt = %v", inv.EndedAt())
	}
	if inv.TokenUsage() != tu {
		t.Errorf("tokens = %+v", inv.TokenUsage())
	}
	if inv.DecisionsMade() != 3 {
		t.Errorf("decisions = %d", inv.DecisionsMade())
	}
	if !inv.IsTerminal() {
		t.Error("terminal")
	}
	if inv.Version() != 2 {
		t.Errorf("version = %d", inv.Version())
	}
	// invalid transition
	if err := inv.MarkSucceeded(end, tu, 1); !errors.Is(err, cognition.ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got %v", err)
	}
	// zero time
	inv2 := newSpawn(t, cognition.ScopeTask, "T-2", now)
	if err := inv2.MarkSucceeded(time.Time{}, tu, 1); err == nil {
		t.Error("expected zero time err")
	}
}

func TestMarkFailed(t *testing.T) {
	now := time.Now().UTC()
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	end := now.Add(10 * time.Second)
	if err := inv.MarkFailed(cognition.FailedReasonClaudeNonZero, "exit=1", end); err != nil {
		t.Fatalf("failed: %v", err)
	}
	if inv.Status() != cognition.StatusFailed {
		t.Errorf("status = %s", inv.Status())
	}
	if inv.FailedReason() != cognition.FailedReasonClaudeNonZero {
		t.Errorf("reason = %s", inv.FailedReason())
	}
	if inv.FailedMessage() != "exit=1" {
		t.Errorf("msg = %q", inv.FailedMessage())
	}
	// invalid reason
	inv2 := newSpawn(t, cognition.ScopeTask, "T-2", now)
	if err := inv2.MarkFailed(cognition.InvocationFailedReason("bogus"), "x", end); err == nil {
		t.Error("expected invalid reason err")
	}
	// missing message
	inv3 := newSpawn(t, cognition.ScopeTask, "T-3", now)
	if err := inv3.MarkFailed(cognition.FailedReasonOOM, "", end); err == nil {
		t.Error("expected missing message err")
	}
	// zero time
	inv4 := newSpawn(t, cognition.ScopeTask, "T-4", now)
	if err := inv4.MarkFailed(cognition.FailedReasonOOM, "x", time.Time{}); err == nil {
		t.Error("expected zero time err")
	}
	// transition from terminal
	if err := inv.MarkFailed(cognition.FailedReasonOOM, "x", end); !errors.Is(err, cognition.ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got %v", err)
	}
}

func TestMarkTimedOut(t *testing.T) {
	now := time.Now().UTC()
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	end := now.Add(time.Minute)
	if err := inv.MarkTimedOut(end); err != nil {
		t.Fatalf("timed_out: %v", err)
	}
	if inv.Status() != cognition.StatusTimedOut {
		t.Errorf("status = %s", inv.Status())
	}
	if inv.TimedOutAt() == nil || !inv.TimedOutAt().Equal(end) {
		t.Errorf("timed_out_at = %v", inv.TimedOutAt())
	}
	if inv.EndedAt() == nil || !inv.EndedAt().Equal(end) {
		t.Errorf("ended_at = %v", inv.EndedAt())
	}
	// zero time
	inv2 := newSpawn(t, cognition.ScopeTask, "T-2", now)
	if err := inv2.MarkTimedOut(time.Time{}); err == nil {
		t.Error("expected zero err")
	}
	// transition from terminal
	if err := inv.MarkTimedOut(end); !errors.Is(err, cognition.ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got %v", err)
	}
}

func TestRehydrate_Happy(t *testing.T) {
	now := time.Now().UTC()
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E"})
	end := now.Add(1 * time.Minute)
	inv, err := cognition.Rehydrate(cognition.RehydrateInput{
		ID:                 "X",
		Scope:              scope,
		TriggerEvents:      tes,
		Status:             cognition.StatusSucceeded,
		HardTimeoutSeconds: 180,
		StartedAt:          now,
		EndedAt:            &end,
		TokenUsage:         cognition.TokenUsage{Input: 1},
		DecisionsMade:      2,
		CreatedAt:          now,
		UpdatedAt:          end,
		Version:            5,
	})
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if inv.Status() != cognition.StatusSucceeded {
		t.Errorf("status %s", inv.Status())
	}
	if inv.Version() != 5 {
		t.Errorf("version %d", inv.Version())
	}
	if inv.EndedAt() == nil {
		t.Error("ended_at nil")
	}
}

func TestRehydrate_TimedOut(t *testing.T) {
	now := time.Now().UTC()
	scope := cognition.MustNewInvocationScope(cognition.ScopeGlobal, "")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E"})
	to := now.Add(time.Minute)
	inv, err := cognition.Rehydrate(cognition.RehydrateInput{
		ID:                 "X",
		Scope:              scope,
		TriggerEvents:      tes,
		Status:             cognition.StatusTimedOut,
		HardTimeoutSeconds: 600,
		StartedAt:          now,
		EndedAt:            &to,
		TimedOutAt:         &to,
		CreatedAt:          now,
		UpdatedAt:          to,
		Version:            2,
	})
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if inv.TimedOutAt() == nil {
		t.Error("timed_out_at nil")
	}
}

func TestRehydrate_BogusStatus(t *testing.T) {
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E"})
	_, err := cognition.Rehydrate(cognition.RehydrateInput{
		ID:                 "X",
		Scope:              scope,
		TriggerEvents:      tes,
		Status:             cognition.InvocationStatus("bogus"),
		HardTimeoutSeconds: 180,
		StartedAt:          time.Now(),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		Version:            1,
	})
	if !errors.Is(err, cognition.ErrUnknownStatus) {
		t.Fatalf("expected ErrUnknownStatus, got %v", err)
	}
}
