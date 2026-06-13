package service

import (
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

// Tester lane — v2.9.1 Thread P2 @agent-in-thread wake class-guard (§5.1: confirm
// at the EVENT-EMIT layer, not just the projector — the plan-conv wake gap was an
// emit-gate miss one layer above the projector).
//
// The wake mechanism is parent-agnostic: a thread reply is an ordinary
// same-conversation message and must emit conversation.message_added exactly like a
// top-level message, in EVERY wake-eligible owner-ref kind. Dev's
// TestAddMessage_ThreadReply_EmitsWakeOutbox covers task conversations; this guard
// generalizes it to the kinds the §5.1 lesson is actually about — PLAN and ISSUE —
// so a regression that made replies skip the wake in those kinds is caught as a class.
func TestClassGuard_ThreadReplyWake_AcrossOwnerRefKinds(t *testing.T) {
	cases := []struct {
		name  string
		kind  conversation.ConversationKind
		owner conversation.OwnerRef
	}{
		{"plan", conversation.ConversationKindPlan, conversation.OwnerRef("pm://plans/PCG1")},
		{"issue", conversation.ConversationKindIssue, conversation.OwnerRef("pm://issues/ICG1")},
		{"task", conversation.ConversationKindTask, conversation.NewTaskOwnerRef("TCG1")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWakeFixture(t)
			convID := conversation.ConversationID("conv-" + tc.name + "-thread")
			f.saveConv(t, convID, tc.kind, tc.owner, "")

			root, err := f.w.AddMessage(f.ctx, AddMessageCommand{
				ConversationID: convID, SenderIdentityID: "user:bob",
				ContentKind: conversation.MessageContentText, Content: "root",
				Direction: conversation.DirectionInbound, Actor: observability.Actor("user:bob"),
			})
			if err != nil {
				t.Fatalf("AddMessage root: %v", err)
			}
			reply, err := f.w.AddMessage(f.ctx, AddMessageCommand{
				ConversationID: convID, SenderIdentityID: "user:carol",
				ContentKind: conversation.MessageContentText, Content: "@agent ping in " + tc.name + " thread",
				Direction: conversation.DirectionInbound, ParentMessageID: root.MessageID,
				Actor: observability.Actor("user:carol"),
			})
			if err != nil {
				t.Fatalf("AddMessage reply: %v", err)
			}

			evs := f.messageAddedEvents(t)
			if len(evs) != 2 {
				t.Fatalf("%s: want 2 wake events (root + reply), got %d", tc.name, len(evs))
			}
			// The reply's OWN wake event must be present, carrying the reply id + text
			// (so the projector wakes on the reply, not just the root).
			found := false
			for _, e := range evs {
				if strings.Contains(e.Payload, `"message_id":"`+string(reply.MessageID)+`"`) {
					found = true
					if !strings.Contains(e.Payload, `"text":"@agent ping in `+tc.name+` thread"`) {
						t.Fatalf("%s: reply wake payload wrong: %s", tc.name, e.Payload)
					}
				}
			}
			if !found {
				t.Fatalf("%s: thread reply emitted NO wake event → @agent-in-thread would not fire", tc.name)
			}
		})
	}
}
