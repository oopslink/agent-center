package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupMsgDB(t *testing.T) (*ConversationRepo, *MessageRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewConversationRepo(db), NewMessageRepo(db)
}

func mkMsg(t *testing.T, id conversation.MessageID, convID conversation.ConversationID) *conversation.Message {
	t.Helper()
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:               id,
		ConversationID:   convID,
		SenderIdentityID: "user:hayang",
		ContentKind:      conversation.MessageContentText,
		Content:          "hello",
		Direction:        conversation.DirectionInbound,
		PostedAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMessageRepo_AppendAndFind(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	if err := msgR.Append(context.Background(), mkMsg(t, "m-1", "c-1")); err != nil {
		t.Fatal(err)
	}
	got, err := msgR.FindByID(context.Background(), "m-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content() != "hello" || got.ConversationID() != "c-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestMessageRepo_FindByIDs_Batch(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	for _, id := range []conversation.MessageID{"m-1", "m-2", "m-3"} {
		if err := msgR.Append(context.Background(), mkMsg(t, id, "c-1")); err != nil {
			t.Fatal(err)
		}
	}
	// Lookup with one missing id — missing one is silently skipped.
	got, err := msgR.FindByIDs(context.Background(),
		[]conversation.MessageID{"m-1", "missing", "m-3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows want 2 (m-1 + m-3)", len(got))
	}
	ids := make(map[conversation.MessageID]bool, len(got))
	for _, m := range got {
		ids[m.ID()] = true
	}
	if !ids["m-1"] || !ids["m-3"] || ids["missing"] {
		t.Fatalf("unexpected id set: %v", ids)
	}
}

func TestMessageRepo_FindByIDs_Empty(t *testing.T) {
	_, msgR := setupMsgDB(t)
	got, err := msgR.FindByIDs(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %d", len(got))
	}
}

func TestMessageRepo_FindByID_NotFound(t *testing.T) {
	_, msgR := setupMsgDB(t)
	_, err := msgR.FindByID(context.Background(), "nope")
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMessageRepo_FindByConversationID_Order(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	now := time.Now().UTC()
	for i, ms := range []time.Duration{0, time.Second, 2 * time.Second} {
		m, _ := conversation.NewMessage(conversation.NewMessageInput{
			ID: conversation.MessageID(string(rune('a' + i))), ConversationID: "c-1",
			SenderIdentityID: "user:h", ContentKind: conversation.MessageContentText,
			Direction: conversation.DirectionInbound, PostedAt: now.Add(ms),
		})
		_ = msgR.Append(context.Background(), m)
	}
	got, err := msgR.FindByConversationID(context.Background(), "c-1", conversation.MessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	// ASC order
	if !got[0].PostedAt().Before(got[1].PostedAt()) {
		t.Fatal("not ASC")
	}
}

func TestMessageRepo_FindByConversationID_Tail(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		m, _ := conversation.NewMessage(conversation.NewMessageInput{
			ID: conversation.MessageID(string(rune('a' + i))), ConversationID: "c-1",
			SenderIdentityID: "user:h", ContentKind: conversation.MessageContentText,
			Direction: conversation.DirectionInbound, PostedAt: now.Add(time.Duration(i) * time.Second),
		})
		_ = msgR.Append(context.Background(), m)
	}
	got, _ := msgR.FindByConversationID(context.Background(), "c-1", conversation.MessageFilter{Tail: 2})
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestMessageRepo_FindRecent(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		m, _ := conversation.NewMessage(conversation.NewMessageInput{
			ID: conversation.MessageID(string(rune('a' + i))), ConversationID: "c-1",
			SenderIdentityID: "user:h", ContentKind: conversation.MessageContentText,
			Direction: conversation.DirectionInbound, PostedAt: now.Add(time.Duration(i) * time.Second),
		})
		_ = msgR.Append(context.Background(), m)
	}
	got, err := msgR.FindRecent(context.Background(), "c-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	// ASC (oldest→newest) after flip
	if !got[0].PostedAt().Before(got[1].PostedAt()) {
		t.Fatal("not ASC")
	}
}

func TestMessageRepo_AppendNil(t *testing.T) {
	_, msgR := setupMsgDB(t)
	if err := msgR.Append(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

// v2.9.1 Thread (P1): parent_message_id / root_message_id round-trip through the
// INSERT + scan. A root message stores NULL for both; a reply stores its root.
func TestMessageRepo_ThreadRefs_RoundTrip(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))

	root := mkMsg(t, "m-root", "c-1")
	if err := msgR.Append(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	reply, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: "m-reply", ConversationID: "c-1", SenderIdentityID: "user:hayang",
		ContentKind: conversation.MessageContentText, Content: "re", Direction: conversation.DirectionInbound,
		PostedAt: time.Now().UTC(), ParentMessageID: "m-root", RootMessageID: "m-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := msgR.Append(context.Background(), reply); err != nil {
		t.Fatal(err)
	}

	gotRoot, err := msgR.FindByID(context.Background(), "m-root")
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot.ParentMessageID() != "" || gotRoot.RootMessageID() != "" {
		t.Fatalf("root row should keep NULL parent/root, got parent=%q root=%q", gotRoot.ParentMessageID(), gotRoot.RootMessageID())
	}
	gotReply, err := msgR.FindByID(context.Background(), "m-reply")
	if err != nil {
		t.Fatal(err)
	}
	if gotReply.ParentMessageID() != "m-root" || gotReply.RootMessageID() != "m-root" {
		t.Fatalf("reply row parent/root not round-tripped, got parent=%q root=%q", gotReply.ParentMessageID(), gotReply.RootMessageID())
	}
}
