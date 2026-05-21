package inbound_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// TestCardCallback_RespondTaskUnboundFallback exercises the
// `convID == ""` branch in card_callback.handleInputRequestRespond —
// the IR's task has no conversation_id but Respond still succeeds
// (the IR service Create fallback already auto-binds, so the cleanest
// way to hit this is to bypass IR.Create and seed the IR + execution
// in the input_required state with a fresh, unbound task).
func TestCardCallback_RespondTaskUnboundFallback(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	ctx := context.Background()
	// Create task WITHOUT conversation (the IRSvc.Create fallback would
	// add one if default_channel is set; we override by writing the IR
	// row directly).
	tres, err := f.taskSvc.Create(ctx, trservice.TaskCreateInput{
		ProjectID: "demo", Title: "test", Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := f.clock.Now()
	e, _ := execution.New(execution.NewInput{
		ID:            taskruntime.TaskExecutionID(f.idgen.NewULID()),
		TaskID:        tres.TaskID,
		WorkerID:      "w-1",
		AgentCLI:      "fake",
		WorkspaceMode: execution.WorkspaceWorktree,
		Now:           now,
	})
	_ = e.StartWorking("/tmp", now)
	if err := f.execs.Save(ctx, e); err != nil {
		t.Fatal(err)
	}
	// Manually create a pending IR via the domain factory (skip the
	// service so no conversation is added by fallback).
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID:              taskruntime.InputRequestID(f.idgen.NewULID()),
		TaskExecutionID: e.ID(),
		Question:        "pick",
		Options:         []string{"A", "B"},
		Urgency:         inputrequest.UrgencyNormal,
		Now:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.irs.Save(ctx, ir); err != nil {
		t.Fatal(err)
	}
	// Transition execution to input_required.
	if err := e.EnterInputRequired(ir.ID(), now); err != nil {
		t.Fatal(err)
	}
	if err := f.execs.Update(ctx, e); err != nil {
		t.Fatal(err)
	}
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action":           "input_request_respond",
			"input_request_id": string(ir.ID()),
			"option_text":      "A",
		},
	}
	dec, err := f.card.Handle(ctx, ev, user)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionCardCallback {
		t.Errorf("decision: %v", dec)
	}
	// No conversation should be populated.
	if dec.ConversationID != "" {
		t.Errorf("conversation should be empty, got %s", dec.ConversationID)
	}
}
