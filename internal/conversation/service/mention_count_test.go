package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// seedConvWithContents creates a channel conv and appends messages with the
// given contents (lexically-ordered ids so id > last_seen behaves like ULID
// ordering). Returns the message ids in order.
func seedConvWithContents(t *testing.T, f *readStateFixture, convID conversation.ConversationID, contents []string) []conversation.MessageID {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:        convID,
		Kind:      conversation.ConversationKindChannel,
		Name:      "mc-test-" + string(convID),
		CreatedBy: conversation.IdentityRef("user:hayang"),
		OpenedAt:  f.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.convRepo.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	ids := make([]conversation.MessageID, len(contents))
	for i, content := range contents {
		f.clock.Advance(time.Millisecond)
		m, err := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(string(convID) + "-msg-" + ulidPad(i)),
			ConversationID:   convID,
			SenderIdentityID: conversation.IdentityRef("user:other"),
			ContentKind:      conversation.MessageContentText,
			Content:          content,
			Direction:        conversation.DirectionInbound,
			PostedAt:         f.clock.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.msgRepo.Append(context.Background(), m); err != nil {
			t.Fatal(err)
		}
		ids[i] = m.ID()
	}
	return ids
}

func TestUnreadWithMentions_CountsOnlyMentionsInUnreadTail(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithContents(t, f, "conv-1", []string{
		"plain message",       // 0
		"hey @hayang look",    // 1 mention
		"no mention here",     // 2
		"cc @hayang and @bob", // 3 mention
	})

	// No read row → everything unread.
	sum, err := f.svc.UnreadWithMentions(context.Background(), "user:hayang", "conv-1", "hayang")
	if err != nil {
		t.Fatal(err)
	}
	if sum.UnreadCount != 4 {
		t.Fatalf("unread=%d want 4", sum.UnreadCount)
	}
	if sum.MentionCount != 2 {
		t.Fatalf("mention=%d want 2", sum.MentionCount)
	}

	// Mark first two seen → unread tail = msgs 2,3; only msg 3 mentions.
	if _, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[1], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	sum, err = f.svc.UnreadWithMentions(context.Background(), "user:hayang", "conv-1", "hayang")
	if err != nil {
		t.Fatal(err)
	}
	if sum.UnreadCount != 2 {
		t.Fatalf("unread=%d want 2", sum.UnreadCount)
	}
	if sum.MentionCount != 1 {
		t.Fatalf("mention=%d want 1 (only the unread tail's mention counts)", sum.MentionCount)
	}
}

func TestUnreadWithMentions_LeqUnreadInvariant(t *testing.T) {
	f := setupReadStateService(t)
	// Every message mentions the user → mention == unread (the ≤ bound is tight).
	seedConvWithContents(t, f, "conv-1", []string{
		"@hayang one", "@hayang two", "@hayang three",
	})
	sum, err := f.svc.UnreadWithMentions(context.Background(), "user:hayang", "conv-1", "hayang")
	if err != nil {
		t.Fatal(err)
	}
	if sum.MentionCount > sum.UnreadCount {
		t.Fatalf("mention %d > unread %d violates invariant", sum.MentionCount, sum.UnreadCount)
	}
	if sum.MentionCount != 3 || sum.UnreadCount != 3 {
		t.Fatalf("got unread=%d mention=%d want 3/3", sum.UnreadCount, sum.MentionCount)
	}
}

func TestUnreadWithMentions_TokenBounded(t *testing.T) {
	f := setupReadStateService(t)
	// "@hayanglee" must NOT match "@hayang" (token boundary), but "@hayang!" does.
	seedConvWithContents(t, f, "conv-1", []string{
		"ping @hayanglee not me",
		"ping @hayang!",
	})
	sum, err := f.svc.UnreadWithMentions(context.Background(), "user:hayang", "conv-1", "hayang")
	if err != nil {
		t.Fatal(err)
	}
	if sum.MentionCount != 1 {
		t.Fatalf("mention=%d want 1 (@hayanglee is not @hayang)", sum.MentionCount)
	}
}

func TestUnreadWithMentions_EmptyDisplayName_ZeroMentions(t *testing.T) {
	f := setupReadStateService(t)
	seedConvWithContents(t, f, "conv-1", []string{"@hayang hi"})
	sum, err := f.svc.UnreadWithMentions(context.Background(), "user:hayang", "conv-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.MentionCount != 0 {
		t.Fatalf("mention=%d want 0 with empty display name", sum.MentionCount)
	}
	if sum.UnreadCount != 1 {
		t.Fatalf("unread=%d want 1", sum.UnreadCount)
	}
}

func TestUnreadWithMentions_CaseInsensitive(t *testing.T) {
	f := setupReadStateService(t)
	seedConvWithContents(t, f, "conv-1", []string{"yo @HaYang"})
	sum, err := f.svc.UnreadWithMentions(context.Background(), "user:hayang", "conv-1", "hayang")
	if err != nil {
		t.Fatal(err)
	}
	if sum.MentionCount != 1 {
		t.Fatalf("mention=%d want 1 (case-insensitive)", sum.MentionCount)
	}
}

func TestUnreadWithMentions_InvalidUserID(t *testing.T) {
	f := setupReadStateService(t)
	if _, err := f.svc.UnreadWithMentions(context.Background(), "no-prefix", "conv-1", "x"); err == nil {
		t.Fatal("expected user_id validation error")
	}
}
