package inbound_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
)

// TestRouter_SlashEmpty exercises the ErrSlashEmpty branch in OnVendorEvent.
func TestRouter_SlashEmpty(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	dec, err := f.router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "t", VendorUserID: "ou-1",
		Context: inbound.MessageContextDM,
		Text:    "/ ",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionRejectSlash {
		t.Errorf("decision: %v", dec)
	}
}

// TestRouter_SlashInsufficientArgs exercises the
// ErrSlashInsufficientArgs branch.
func TestRouter_SlashInsufficientArgs(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	dec, _ := f.router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "t", VendorUserID: "ou-1",
		Context: inbound.MessageContextDM,
		Text:    "/track",
		ReceivedAt: time.Now(),
	})
	if dec.Kind != inbound.RouteDecisionRejectSlash {
		t.Errorf("decision: %v", dec)
	}
}

// TestRouter_EmptyTextFallsThroughToFreeText: a message with empty text
// is still a valid free-text message — should fall through to
// directAddMessage.
func TestRouter_EmptyTextFallsThroughToFreeText(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	dec, err := f.router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "t", VendorUserID: "ou-1",
		Context: inbound.MessageContextDM,
		Text:    "",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionDirectAddMessage {
		t.Errorf("decision: %v", dec)
	}
}

// TestRouter_ExistingConversationReuse: a second event with the same
// vendor_thread_key (and different vendor_msg_ref) hits the existing
// conversation branch of findOrCreateConversation.
func TestRouter_ExistingConversationReuse(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	ev1 := inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-A", VendorThreadKey: "thread-reuse",
		VendorUserID: "ou-1", Context: inbound.MessageContextDM, Text: "first",
		ReceivedAt: time.Now(),
	}
	d1, _ := f.router.OnVendorEvent(context.Background(), ev1)
	ev2 := ev1
	ev2.VendorMsgRef = "ref-B"
	ev2.Text = "second"
	d2, err := f.router.OnVendorEvent(context.Background(), ev2)
	if err != nil {
		t.Fatal(err)
	}
	if d1.ConversationID != d2.ConversationID {
		t.Errorf("conversation should be reused: %s vs %s", d1.ConversationID, d2.ConversationID)
	}
}

// TestRouter_LongTextStillFreeText: a long free-text message goes to
// add-message; covers the natural fallthrough path.
func TestRouter_LongTextStillFreeText(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	dec, err := f.router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "t", VendorUserID: "ou-1",
		Context: inbound.MessageContextDM,
		Text:    "this is a long free text response that should be added as a message",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionDirectAddMessage {
		t.Errorf("decision: %v", dec)
	}
}
