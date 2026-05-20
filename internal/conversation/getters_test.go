package conversation

import (
	"testing"
	"time"
)

func TestConversationGetters_All(t *testing.T) {
	c, _ := NewConversation(NewConversationInput{
		ID: "C-1", Kind: ConversationKindDM, Title: "title",
		PrimaryChannelHint: "feishu", PrimaryChannelThreadKey: "t",
		OpenedAt: time.Now(),
	})
	if c.ID() != "C-1" {
		t.Fatal()
	}
	if c.Title() != "title" {
		t.Fatal()
	}
	if c.OpenedAt().IsZero() {
		t.Fatal()
	}
	if c.CreatedAt().IsZero() {
		t.Fatal()
	}
	if c.UpdatedAt().IsZero() {
		t.Fatal()
	}
}

func TestMessageGetters_All(t *testing.T) {
	m, _ := NewMessage(NewMessageInput{
		ID: "M-1", ConversationID: "C-1", SenderIdentityID: "user:x",
		ContentKind: MessageContentText, Direction: DirectionInbound,
		Content: "hi", VendorMsgRef: "v-1", InputRequestRef: "IR-1",
		PostedAt: time.Now(),
	})
	if m.ConversationID() != "C-1" {
		t.Fatal()
	}
	if m.SenderIdentityID() != "user:x" {
		t.Fatal()
	}
	if m.ContentKind() != MessageContentText {
		t.Fatal()
	}
	if m.Content() != "hi" {
		t.Fatal()
	}
	if m.VendorMsgRef() != "v-1" {
		t.Fatal()
	}
	if m.InputRequestRef() != "IR-1" {
		t.Fatal()
	}
	if m.PostedAt().IsZero() {
		t.Fatal()
	}
	if m.CreatedAt().IsZero() {
		t.Fatal()
	}
}

func TestConversation_NewClosedThenSet(t *testing.T) {
	c, _ := NewConversation(NewConversationInput{
		ID: "C-1", Kind: ConversationKindDM, OpenedAt: time.Now(),
	})
	_ = c.Close(time.Now(), "x", "y")
	// After close, SetPrimaryChannel is still allowed.
	if err := c.SetPrimaryChannel("h", "t", time.Now()); err != nil {
		t.Fatalf("set primary on closed: %v", err)
	}
}
