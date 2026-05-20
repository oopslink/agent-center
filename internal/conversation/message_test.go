package conversation

import (
	"errors"
	"testing"
	"time"
)

func newTestMessage(t *testing.T) *Message {
	t.Helper()
	m, err := NewMessage(NewMessageInput{
		ID:               "M-1",
		ConversationID:   "C-1",
		SenderIdentityID: "user:hayang",
		ContentKind:      MessageContentText,
		Content:          "hello",
		Direction:        DirectionInbound,
		PostedAt:         time.Now(),
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	return m
}

func TestMessage_New_Happy(t *testing.T) {
	m := newTestMessage(t)
	if m.ID() != "M-1" {
		t.Fatal()
	}
	if m.Direction() != DirectionInbound {
		t.Fatal()
	}
	if m.HasVendorMsgRef() {
		t.Fatal()
	}
}

func TestMessage_New_RejectsBadInputs(t *testing.T) {
	cases := []NewMessageInput{
		{},                                                                      // all missing
		{ID: "M"},                                                               // missing conv
		{ID: "M", ConversationID: "C"},                                          // missing identity
		{ID: "M", ConversationID: "C", SenderIdentityID: "bogus:x"},             // bad identity
		{ID: "M", ConversationID: "C", SenderIdentityID: "user:x"},              // missing kind
		{ID: "M", ConversationID: "C", SenderIdentityID: "user:x", ContentKind: "bogus"}, // bad kind
		{ID: "M", ConversationID: "C", SenderIdentityID: "user:x", ContentKind: MessageContentText, Direction: "bad"}, // bad dir
		{ID: "M", ConversationID: "C", SenderIdentityID: "user:x", ContentKind: MessageContentText, Direction: DirectionInbound}, // missing time
	}
	for i, in := range cases {
		if _, err := NewMessage(in); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestMessage_SetVendorMsgRef_FirstTime(t *testing.T) {
	m := newTestMessage(t)
	if err := m.SetVendorMsgRef("vendor-1"); err != nil {
		t.Fatal(err)
	}
	if m.VendorMsgRef() != "vendor-1" || !m.HasVendorMsgRef() {
		t.Fatal()
	}
}

func TestMessage_SetVendorMsgRef_AlreadySet(t *testing.T) {
	m := newTestMessage(t)
	_ = m.SetVendorMsgRef("v-1")
	err := m.SetVendorMsgRef("v-2")
	if !errors.Is(err, ErrMessageImmutable) {
		t.Fatalf("got %v", err)
	}
}

func TestMessage_SetVendorMsgRef_RequiresValue(t *testing.T) {
	m := newTestMessage(t)
	if err := m.SetVendorMsgRef(""); err == nil {
		t.Fatal()
	}
}

func TestMessage_RehydrateBadKind(t *testing.T) {
	_, err := RehydrateMessage(RehydrateMessageInput{ContentKind: "bogus", Direction: DirectionInbound})
	if err == nil {
		t.Fatal()
	}
}

func TestMessage_RehydrateBadDirection(t *testing.T) {
	_, err := RehydrateMessage(RehydrateMessageInput{ContentKind: MessageContentText, Direction: "bogus"})
	if err == nil {
		t.Fatal()
	}
}

func TestMessageContentKind_Validation(t *testing.T) {
	for _, k := range []MessageContentKind{
		MessageContentText, MessageContentSystem, MessageContentAgentFinding,
		MessageContentSupervisorSummary, MessageContentConclusionDraft, MessageContentTaskProposal,
	} {
		if !k.IsValid() {
			t.Fatal()
		}
	}
	if MessageContentKind("x").IsValid() {
		t.Fatal()
	}
	if MessageContentText.String() != "text" {
		t.Fatal()
	}
}

func TestMessageDirection_Validation(t *testing.T) {
	for _, d := range []MessageDirection{DirectionInbound, DirectionOutbound, DirectionInternal} {
		if !d.IsValid() {
			t.Fatal()
		}
	}
	if MessageDirection("x").IsValid() {
		t.Fatal()
	}
	if DirectionInbound.String() != "inbound" {
		t.Fatal()
	}
}

func TestMessage_AllowsInputRequestRefField(t *testing.T) {
	m, err := NewMessage(NewMessageInput{
		ID: "M-1", ConversationID: "C-1", SenderIdentityID: "agent:a-1",
		ContentKind: MessageContentAgentFinding, Direction: DirectionOutbound,
		Content: "request input?", InputRequestRef: "IR-1",
		PostedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.InputRequestRef() != "IR-1" {
		t.Fatal()
	}
}
