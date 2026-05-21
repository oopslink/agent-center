package inbound

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVendorEventKind_IsValid(t *testing.T) {
	cases := []struct {
		k    VendorEventKind
		want bool
	}{
		{VendorEventMessageReceive, true},
		{VendorEventCardActionTrigger, true},
		{"", false},
		{"im.unknown", false},
	}
	for _, c := range cases {
		if got := c.k.IsValid(); got != c.want {
			t.Errorf("IsValid(%q) = %v, want %v", c.k, got, c.want)
		}
		if c.k.String() != string(c.k) {
			t.Errorf("String(%q) mismatch", c.k)
		}
	}
}

func TestMessageContext_IsValid(t *testing.T) {
	cases := []struct {
		c    MessageContext
		want bool
	}{
		{MessageContextDM, true},
		{MessageContextGroupAdhoc, true},
		{MessageContextGroupThread, true},
		{"", false},
		{"unknown", false},
	}
	for _, c := range cases {
		if got := c.c.IsValid(); got != c.want {
			t.Errorf("IsValid(%q) = %v want %v", c.c, got, c.want)
		}
		if c.c.String() != string(c.c) {
			t.Errorf("String(%q) mismatch", c.c)
		}
	}
}

func TestVendorEvent_Validate(t *testing.T) {
	good := func() VendorEvent {
		return VendorEvent{
			Kind:            VendorEventMessageReceive,
			VendorMsgRef:    "msg-1",
			VendorThreadKey: "ou-x",
			VendorUserID:    "ou-user",
			Context:         MessageContextDM,
			Text:            "hi",
			ReceivedAt:      time.Now(),
		}
	}
	if err := good().Validate(); err != nil {
		t.Fatalf("happy: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*VendorEvent)
		want string
	}{
		{"bad kind", func(e *VendorEvent) { e.Kind = "unknown" }, "unknown kind"},
		{"missing msg ref", func(e *VendorEvent) { e.VendorMsgRef = "" }, "vendor_msg_ref"},
		{"missing user", func(e *VendorEvent) { e.VendorUserID = "" }, "vendor_user_id"},
		{"missing context", func(e *VendorEvent) { e.Context = "" }, "context"},
		{"missing thread key", func(e *VendorEvent) { e.VendorThreadKey = "" }, "vendor_thread_key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := good()
			c.mut(&e)
			err := e.Validate()
			if err == nil {
				t.Fatalf("want error containing %q, got nil", c.want)
			}
			if !errors.Is(err, ErrVendorEventMalformed) {
				t.Fatalf("want ErrVendorEventMalformed, got %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q lacks %q", err, c.want)
			}
		})
	}
}

func TestVendorEvent_Validate_Card(t *testing.T) {
	good := VendorEvent{
		Kind:         VendorEventCardActionTrigger,
		VendorMsgRef: "ref",
		VendorUserID: "u",
		CardAction: CardActionEvent{
			CardMessageID: "om_1",
			ActionValue: map[string]any{
				"action":           "input_request_respond",
				"input_request_id": "ir-1",
				"option_text":      "B",
			},
		},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("happy: %v", err)
	}
	bad := good
	bad.CardAction.CardMessageID = ""
	if err := bad.Validate(); err == nil {
		t.Fatal("missing card_message_id should fail")
	}
	bad = good
	bad.CardAction.ActionValue = nil
	if err := bad.Validate(); err == nil {
		t.Fatal("missing action_value should fail")
	}
	bad = good
	bad.CardAction.ActionValue = map[string]any{"input_request_id": "x"}
	if err := bad.Validate(); err == nil {
		t.Fatal("missing action field should fail")
	}
}

func TestCardActionEvent_Getters(t *testing.T) {
	empty := CardActionEvent{}
	if empty.Action() != "" || empty.InputRequestID() != "" || empty.OptionText() != "" {
		t.Fatal("empty getters should return empty strings")
	}
	ce := CardActionEvent{ActionValue: map[string]any{
		"action":           "input_request_respond",
		"input_request_id": "ir-7",
		"option_text":      "A",
	}}
	if ce.Action() != "input_request_respond" {
		t.Errorf("Action: %q", ce.Action())
	}
	if ce.InputRequestID() != "ir-7" {
		t.Errorf("ir: %q", ce.InputRequestID())
	}
	if ce.OptionText() != "A" {
		t.Errorf("ot: %q", ce.OptionText())
	}
	// Wrong types — should not panic.
	ce2 := CardActionEvent{ActionValue: map[string]any{
		"action": 42, "input_request_id": []byte("bad"), "option_text": nil,
	}}
	if ce2.Action() != "" || ce2.InputRequestID() != "" || ce2.OptionText() != "" {
		t.Error("wrong-type getters should yield empty strings")
	}
}

func TestSlashVerb_IsValid(t *testing.T) {
	for _, v := range []SlashVerb{SlashVerbTrack, SlashVerbAnswer, SlashVerbDispatch} {
		if !v.IsValid() {
			t.Errorf("verb %q should be valid", v)
		}
	}
	if SlashVerb("nope").IsValid() {
		t.Error("nope should not be valid")
	}
	if SlashVerbTrack.String() != "track" {
		t.Error("String() should match")
	}
}

func TestRouteDecisionKind_String(t *testing.T) {
	cases := map[RouteDecisionKind]string{
		RouteDecisionUnspecified:       "unspecified",
		RouteDecisionDirectAddMessage:  "direct_add_message",
		RouteDecisionSlashRoute:        "slash_route",
		RouteDecisionCardCallback:      "card_callback",
		RouteDecisionDropDedupe:        "drop_dedupe",
		RouteDecisionDropUnknown:       "drop_unknown",
		RouteDecisionDropPanic:         "drop_panic",
		RouteDecisionRejectSlash:       "reject_slash",
		RouteDecisionKind(42):          "unspecified", // unknown falls to default
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("String(%d) = %q want %q", k, got, want)
		}
	}
}

func TestRouteDecision_IsSuccessful(t *testing.T) {
	for _, k := range []RouteDecisionKind{
		RouteDecisionDirectAddMessage, RouteDecisionSlashRoute, RouteDecisionCardCallback,
	} {
		if !(RouteDecision{Kind: k}).IsSuccessful() {
			t.Errorf("kind %s should be successful", k)
		}
	}
	for _, k := range []RouteDecisionKind{
		RouteDecisionDropDedupe, RouteDecisionDropUnknown, RouteDecisionDropPanic,
		RouteDecisionRejectSlash, RouteDecisionUnspecified,
	} {
		if (RouteDecision{Kind: k}).IsSuccessful() {
			t.Errorf("kind %s should NOT be successful", k)
		}
	}
}
