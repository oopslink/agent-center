package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

func TestNewMessageWriter_NilClock(t *testing.T) {
	s := setupSuite(t)
	w := NewMessageWriter(s.db, s.conv, s.msg, s.sink, nil, nil)
	if w == nil {
		t.Fatal()
	}
}

func TestOpenConversation_BadKindAtNew(t *testing.T) {
	s := setupSuite(t)
	// Inject an invalid kind that passes IsValid but fails OpenConversation's
	// own invariant — actually IsValid will reject early. Use empty kind.
	_, err := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: "", Actor: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestClose_BadActor_Constructors(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.Close(context.Background(), CloseCommand{
		ConversationID: "C", Version: 1,
		Reason: "x", Message: "y", Actor: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestClose_EmptyReasonMessageEdge(t *testing.T) {
	s := setupSuite(t)
	cases := []CloseCommand{
		{ConversationID: "C", Version: 1, Reason: "", Message: "y", Actor: "user:x"},
		{ConversationID: "C", Version: 1, Reason: "x", Message: "", Actor: "user:x"},
	}
	for _, cmd := range cases {
		if _, err := s.writer.Close(context.Background(), cmd); err == nil {
			t.Fatalf("expected error: %+v", cmd)
		}
	}
}

func TestOpenConversation_AllAllowedKinds(t *testing.T) {
	s := setupSuite(t)
	for _, k := range []conversation.ConversationKind{
		conversation.ConversationKindDM,
		conversation.ConversationKindGroupThread,
		conversation.ConversationKindAdhoc,
		conversation.ConversationKindNotification,
	} {
		_, err := s.writer.OpenConversation(context.Background(), OpenCommand{
			Kind: k, Actor: "user:x",
		})
		if err != nil {
			t.Fatalf("kind %s: %v", k, err)
		}
	}
}
