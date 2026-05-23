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
