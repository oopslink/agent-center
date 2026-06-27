package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

// readStateHarness wraps the full in-memory service stack with a seeded
// conversation + one message ready for MarkSeen.
type readStateHarness struct {
	svc     *ReadStateService
	convID  conversation.ConversationID
	userRef conversation.IdentityRef
	msgID   conversation.MessageID
	f       *readStateFixture
}

// newReadStateHarness builds the harness by reusing setupReadStateService
// (same sqlite+sink wiring) and seeds one message.
func newReadStateHarness(t *testing.T) *readStateHarness {
	t.Helper()
	f := setupReadStateService(t)
	const convID = conversation.ConversationID("conv-trigger-test")
	const userRef = conversation.IdentityRef("user:hayang")
	ids := seedConvWithMessages(t, f, convID, 1)
	return &readStateHarness{
		svc:     f.svc,
		convID:  convID,
		userRef: userRef,
		msgID:   ids[0],
		f:       f,
	}
}

// firstEventTriggerField fetches the first outbox event whose type is
// "conversation.read_state.changed" and returns its "trigger" payload field.
func (h *readStateHarness) firstEventTriggerField(t *testing.T) string {
	t.Helper()
	events, err := h.f.eventRepo.Find(context.Background(), observability.EventQueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("eventRepo.Find: %v", err)
	}
	for _, e := range events {
		if e.Type() != "conversation.read_state.changed" {
			continue
		}
		p := e.Payload()
		v, ok := p["trigger"]
		if !ok {
			t.Fatal("event payload missing 'trigger' key")
		}
		s, ok := v.(string)
		if !ok {
			t.Fatalf("trigger payload value is %T, want string", v)
		}
		return s
	}
	t.Fatal("no conversation.read_state.changed event found")
	return ""
}

func TestMarkSeen_EmitsTrigger(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   MarkSeenTrigger
		want string
	}{
		{"agent_tool", MarkSeenTriggerAgentTool, "agent_tool"},
		{"delivery", MarkSeenTriggerDelivery, "delivery"},
		{"empty_defaults_human", "", "human"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newReadStateHarness(t)
			_, err := h.svc.MarkSeen(context.Background(), MarkSeenCommand{
				UserID: h.userRef, ConversationID: h.convID, LastSeenMessageID: h.msgID,
				Actor: observability.Actor(h.userRef), Trigger: tc.in,
			})
			if err != nil {
				t.Fatalf("MarkSeen: %v", err)
			}
			got := h.firstEventTriggerField(t)
			if got != tc.want {
				t.Fatalf("trigger = %q, want %q", got, tc.want)
			}
		})
	}
}
