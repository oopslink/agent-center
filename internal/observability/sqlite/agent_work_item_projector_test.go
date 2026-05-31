package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
)

// These tests prove the #111 Phase-1 aggregation logic against synthetic
// fixtures whose payloads MATCH workerdaemon.streamActivityPayload EXACTLY
// (tool_use / result / assistant_text shapes). They run before any real daemon
// data exists.

// appendActivity inserts one agent_activity_event with the given work_item_ref
// + payload via the real ActivityEventRepo (so the row shape matches prod).
func appendActivity(t *testing.T, repo *agentsql.ActivityEventRepo, id, agentID, wiRef, eventType string, payload map[string]any, occurredAt time.Time) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	ev, err := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID:          id,
		AgentID:     agent.AgentID(agentID),
		WorkItemRef: wiRef,
		EventType:   eventType,
		Payload:     string(b),
		OccurredAt:  occurredAt,
	})
	if err != nil {
		t.Fatalf("NewActivityEvent: %v", err)
	}
	if err := repo.Append(context.Background(), ev); err != nil {
		t.Fatalf("append activity: %v", err)
	}
}

func transitionEvent(t *testing.T, eventID string, pl agentsvc.WorkItemTransitionPayload) outbox.Event {
	t.Helper()
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return outbox.Event{
		ID:        eventID,
		EventType: agentsvc.EvtAgentWorkItemTransitioned,
		Payload:   string(b),
		CreatedAt: pl.OccurredAt,
	}
}

func TestAgentWorkItemProjector_Aggregates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	projRepo := NewAgentWorkItemProjectionRepo(db)
	actRepo := agentsql.NewActivityEventRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	proj := projection.NewAgentWorkItemProjector(db, projRepo, actRepo, applied, clock.SystemClock{})

	const wi = "WI-agg"
	const agentID = "A-1"
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)

	// Two tool_use rows (→ total_tool_calls = 2).
	appendActivity(t, actRepo, "e1", agentID, wi, "tool_use",
		map[string]any{"type": "tool_use", "tool_name": "read_file", "tool_use_id": "tu1"}, base.Add(1*time.Second))
	appendActivity(t, actRepo, "e2", agentID, wi, "tool_use",
		map[string]any{"type": "tool_use", "tool_name": "edit_file", "tool_use_id": "tu2"}, base.Add(2*time.Second))
	// Two result rows with tokens (→ in=30, out=12).
	appendActivity(t, actRepo, "e3", agentID, wi, "result",
		map[string]any{"type": "result", "subtype": "ok", "result": "done", "stop_reason": "end_turn",
			"is_error": false, "cost_usd": 0.01, "tokens_in": 10, "tokens_out": 5}, base.Add(3*time.Second))
	appendActivity(t, actRepo, "e4", agentID, wi, "result",
		map[string]any{"type": "result", "subtype": "ok", "result": "done2", "stop_reason": "end_turn",
			"is_error": false, "cost_usd": 0.02, "tokens_in": 20, "tokens_out": 7}, base.Add(4*time.Second))
	// An assistant_text — the NEWEST among {tool_use, assistant_text, thinking}
	// (t+5) → current_activity should be its text.
	appendActivity(t, actRepo, "e5", agentID, wi, "assistant_text",
		map[string]any{"type": "assistant_text", "text": "Refactoring the handler"}, base.Add(5*time.Second))

	// Transition event occurs at t+10 (later than all activity) → last_activity_at = t+10.
	evtAt := base.Add(10 * time.Second)
	e := transitionEvent(t, "evt-1", agentsvc.WorkItemTransitionPayload{
		WorkItemID: wi, AgentID: agentID, TaskRef: "pm://tasks/T-1",
		PrevStatus: "queued", Status: "active", Version: 2, OccurredAt: evtAt,
	})
	if err := proj.Project(ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}

	got, err := projRepo.FindByID(ctx, wi)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != "active" || got.AgentID != agentID {
		t.Fatalf("status/agent: %+v", got)
	}
	if got.TotalToolCalls != 2 {
		t.Fatalf("total_tool_calls = %d, want 2", got.TotalToolCalls)
	}
	if got.TotalTokensInput != 30 || got.TotalTokensOutput != 12 {
		t.Fatalf("tokens in/out = %d/%d, want 30/12", got.TotalTokensInput, got.TotalTokensOutput)
	}
	if got.CurrentActivity != "Refactoring the handler" {
		t.Fatalf("current_activity = %q, want assistant_text", got.CurrentActivity)
	}
	if !got.CurrentActivityAt.Equal(base.Add(5 * time.Second)) {
		t.Fatalf("current_activity_at = %v, want t+5", got.CurrentActivityAt)
	}
	if !got.LastActivityAt.Equal(evtAt) {
		t.Fatalf("last_activity_at = %v, want transition time %v", got.LastActivityAt, evtAt)
	}
}

func TestAgentWorkItemProjector_CurrentActivity_ToolUseNewest(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	projRepo := NewAgentWorkItemProjectionRepo(db)
	actRepo := agentsql.NewActivityEventRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	proj := projection.NewAgentWorkItemProjector(db, projRepo, actRepo, applied, clock.SystemClock{})

	const wi = "WI-tool-newest"
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	// assistant_text first, tool_use is newer → current_activity = tool_name.
	appendActivity(t, actRepo, "t1", "A-1", wi, "assistant_text",
		map[string]any{"type": "assistant_text", "text": "thinking about it"}, base.Add(1*time.Second))
	appendActivity(t, actRepo, "t2", "A-1", wi, "tool_use",
		map[string]any{"type": "tool_use", "tool_name": "run_tests", "tool_use_id": "tu9"}, base.Add(2*time.Second))

	e := transitionEvent(t, "evt-tn", agentsvc.WorkItemTransitionPayload{
		WorkItemID: wi, AgentID: "A-1", Status: "active", OccurredAt: base,
	})
	if err := proj.Project(ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	got, err := projRepo.FindByID(ctx, wi)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.CurrentActivity != "run_tests" {
		t.Fatalf("current_activity = %q, want tool_name run_tests", got.CurrentActivity)
	}
	// Newest activity (t+2) is later than transition (t0) → last_activity_at = t+2.
	if !got.LastActivityAt.Equal(base.Add(2 * time.Second)) {
		t.Fatalf("last_activity_at = %v, want t+2 (newest activity)", got.LastActivityAt)
	}
}

func TestAgentWorkItemProjector_Idempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	projRepo := NewAgentWorkItemProjectionRepo(db)
	actRepo := agentsql.NewActivityEventRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	proj := projection.NewAgentWorkItemProjector(db, projRepo, actRepo, applied, clock.SystemClock{})

	const wi = "WI-idem"
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	appendActivity(t, actRepo, "i1", "A-1", wi, "tool_use",
		map[string]any{"type": "tool_use", "tool_name": "read_file", "tool_use_id": "tu1"}, base.Add(1*time.Second))

	e := transitionEvent(t, "evt-idem", agentsvc.WorkItemTransitionPayload{
		WorkItemID: wi, AgentID: "A-1", Status: "active", OccurredAt: base.Add(2 * time.Second),
	})
	if err := proj.Project(ctx, e); err != nil {
		t.Fatalf("Project #1: %v", err)
	}
	// Second call with the SAME event ID must be a no-op (MarkApplied dedupes).
	// Even though a duplicate run would re-aggregate identically, the IsApplied
	// guard short-circuits before any write. Assert applied-once via the store.
	if ok, err := applied.IsApplied(ctx, proj.Name(), e.ID); err != nil || !ok {
		t.Fatalf("IsApplied after #1: ok=%v err=%v", ok, err)
	}
	if err := proj.Project(ctx, e); err != nil {
		t.Fatalf("Project #2: %v", err)
	}
	got, err := projRepo.FindByID(ctx, wi)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 1 {
		t.Fatalf("total_tool_calls = %d, want 1 (idempotent, no double-count)", got.TotalToolCalls)
	}
}

func TestAgentWorkItemProjector_NoActivityEvents(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	projRepo := NewAgentWorkItemProjectionRepo(db)
	actRepo := agentsql.NewActivityEventRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	proj := projection.NewAgentWorkItemProjector(db, projRepo, actRepo, applied, clock.SystemClock{})

	const wi = "WI-empty"
	evtAt := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	e := transitionEvent(t, "evt-empty", agentsvc.WorkItemTransitionPayload{
		WorkItemID: wi, AgentID: "A-7", Status: "queued", OccurredAt: evtAt,
	})
	if err := proj.Project(ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	got, err := projRepo.FindByID(ctx, wi)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != "queued" || got.AgentID != "A-7" {
		t.Fatalf("status/agent from event: %+v", got)
	}
	if got.TotalToolCalls != 0 || got.TotalTokensInput != 0 || got.TotalTokensOutput != 0 {
		t.Fatalf("metrics should be 0: %+v", got)
	}
	if got.CurrentActivity != "" {
		t.Fatalf("current_activity should be empty, got %q", got.CurrentActivity)
	}
	if !got.LastActivityAt.Equal(evtAt) {
		t.Fatalf("last_activity_at = %v, want transition time %v", got.LastActivityAt, evtAt)
	}
}
