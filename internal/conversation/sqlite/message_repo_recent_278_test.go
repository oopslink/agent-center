package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// v2.8.1 #278: RecentByConversations batch-fetches the last-n messages per
// conversation across multiple conversations in ONE window-function query —
// newest-first per conversation, capped at n.
func TestMessageRepo_RecentByConversations_Batch(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindChannel, "chan-1"))
	_ = convR.Save(ctx, mkConv(t, "c-2", conversation.ConversationKindChannel, "chan-2"))
	now := time.Now().UTC()

	// c-1: 4 messages; c-2: 2 messages. Ascending posted_at by index.
	seed := func(conv conversation.ConversationID, prefix string, count int) {
		for i := 0; i < count; i++ {
			m, err := conversation.NewMessage(conversation.NewMessageInput{
				ID:               conversation.MessageID(prefix + string(rune('a'+i))),
				ConversationID:   conv,
				SenderIdentityID: "user:h",
				ContentKind:      conversation.MessageContentText,
				Content:          "msg",
				Direction:        conversation.DirectionInbound,
				PostedAt:         now.Add(time.Duration(i) * time.Second),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := msgR.Append(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("c-1", "1", 4)
	seed("c-2", "2", 2)

	got, err := msgR.RecentByConversations(ctx,
		[]conversation.ConversationID{"c-1", "c-2"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	// c-1: last 3 of 4 → newest-first (1d, 1c, 1b).
	c1 := got["c-1"]
	if len(c1) != 3 {
		t.Fatalf("c-1 got %d msgs, want 3", len(c1))
	}
	if c1[0].ID() != "1d" || c1[1].ID() != "1c" || c1[2].ID() != "1b" {
		t.Fatalf("c-1 not newest-first: %s %s %s", c1[0].ID(), c1[1].ID(), c1[2].ID())
	}
	if !c1[0].PostedAt().After(c1[1].PostedAt()) {
		t.Fatal("c-1 not DESC by posted_at")
	}
	// c-2: only 2 messages → both, newest-first (2b, 2a).
	c2 := got["c-2"]
	if len(c2) != 2 {
		t.Fatalf("c-2 got %d msgs, want 2", len(c2))
	}
	if c2[0].ID() != "2b" || c2[1].ID() != "2a" {
		t.Fatalf("c-2 not newest-first: %s %s", c2[0].ID(), c2[1].ID())
	}
}

// An empty/no-message conversation simply has no map entry; empty input + n<=0 are
// no-ops.
func TestMessageRepo_RecentByConversations_EdgeCases(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "empty", conversation.ConversationKindChannel, "empty-chan"))

	got, err := msgR.RecentByConversations(ctx, []conversation.ConversationID{"empty"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["empty"]) != 0 {
		t.Fatalf("empty channel should have no entry, got %d", len(got["empty"]))
	}

	// n <= 0 → empty map.
	if m, err := msgR.RecentByConversations(ctx, []conversation.ConversationID{"empty"}, 0); err != nil || len(m) != 0 {
		t.Fatalf("n<=0 want empty map, got %v err %v", m, err)
	}
	// empty input → empty map.
	if m, err := msgR.RecentByConversations(ctx, nil, 3); err != nil || len(m) != 0 {
		t.Fatalf("nil input want empty map, got %v err %v", m, err)
	}
}
