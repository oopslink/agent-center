package inbound_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

func TestCardCallback_RespondHappy(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action":           "input_request_respond",
			"input_request_id": string(irID),
			"option_text":      "B",
		},
	}
	dec, err := f.card.Handle(context.Background(), ev, user)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionCardCallback {
		t.Errorf("decision: %v", dec)
	}
	// Verify IR is now responded.
	got, err := f.irs.FindByID(context.Background(), irID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Status() != inputrequest.StatusResponded {
		t.Errorf("status: %s", got.Status())
	}
	if !f.hasEvent(t, "bridge.card_action_received") {
		t.Error("card_action_received not emitted")
	}
	if !f.hasEvent(t, "input_request.responded") {
		t.Error("input_request.responded not emitted")
	}
}

func TestCardCallback_RespondMissingIR(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action":           "input_request_respond",
			"input_request_id": "I-missing",
			"option_text":      "B",
		},
	}
	dec, err := f.card.Handle(context.Background(), ev, user)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionDropUnknown {
		t.Errorf("decision: %v", dec)
	}
}

func TestCardCallback_RespondMissingIRID(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action":      "input_request_respond",
			"option_text": "B",
		},
	}
	dec, _ := f.card.Handle(context.Background(), ev, user)
	if dec.Kind != inbound.RouteDecisionDropUnknown || dec.Reason != "malformed_card_action" {
		t.Errorf("decision: %v", dec)
	}
}

func TestCardCallback_RespondAlreadyResolved(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	// First click resolves the IR.
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action": "input_request_respond", "input_request_id": string(irID),
			"option_text": "A",
		},
	}
	if _, err := f.card.Handle(context.Background(), ev, user); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Second click → silent ack.
	dec, err := f.card.Handle(context.Background(), ev, user)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionCardCallback || dec.Reason != "already_responded" {
		t.Errorf("decision: %v", dec)
	}
}

func TestCardCallback_UnknownAction(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action": "some_unknown_action_v2",
		},
	}
	dec, _ := f.card.Handle(context.Background(), ev, user)
	if dec.Kind != inbound.RouteDecisionDropUnknown {
		t.Errorf("decision: %v", dec)
	}
}

func TestCardCallback_NilActionValue(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	dec, err := f.card.Handle(context.Background(), inbound.CardActionEvent{
		CardMessageID: "om-1",
	}, user)
	if err == nil {
		t.Fatalf("want error, got %v", dec)
	}
	if !errors.Is(err, inbound.ErrCardActionMalformed) {
		t.Errorf("want ErrCardActionMalformed, got %v", err)
	}
}

func TestCardCallback_BadIdentity(t *testing.T) {
	f := newFixture(t)
	_, err := f.card.Handle(context.Background(), inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue:   map[string]any{"action": "input_request_respond", "input_request_id": "x", "option_text": "y"},
	}, identity.IdentityID("malformed:"))
	if err == nil {
		t.Fatal("want bad identity error")
	}
}

func TestCardCallback_CancelTreatedAsRespondCancel(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action": "input_request_cancel", "input_request_id": string(irID),
		},
	}
	dec, err := f.card.Handle(context.Background(), ev, user)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionCardCallback {
		t.Errorf("decision: %v", dec)
	}
}

func TestCardCallback_CancelMissingIRID(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	dec, _ := f.card.Handle(context.Background(), inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue:   map[string]any{"action": "input_request_cancel"},
	}, user)
	if dec.Kind != inbound.RouteDecisionDropUnknown {
		t.Errorf("decision: %v", dec)
	}
}

func TestCardCallback_NewCardCallback_MissingDeps(t *testing.T) {
	_, err := inbound.NewCardCallback(inbound.CardCallbackDeps{})
	if err == nil {
		t.Fatal("want missing deps error")
	}
}

func TestCardCallback_CustomRespondNoOptionText(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action":           "input_request_respond_custom",
			"input_request_id": string(irID),
		},
	}
	dec, err := f.card.Handle(context.Background(), ev, user)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionCardCallback {
		t.Errorf("decision: %v", dec)
	}
}
