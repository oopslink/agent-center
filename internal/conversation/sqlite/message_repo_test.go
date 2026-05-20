package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
)

func setupConv(t *testing.T) (*ConversationRepo, *MessageRepo, conversation.ConversationID) {
	t.Helper()
	db := openTestDB(t)
	cr := NewConversationRepo(db)
	mr := NewMessageRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	if err := cr.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return cr, mr, c.ID()
}

func newMsg(t *testing.T, convID conversation.ConversationID, at time.Time) *conversation.Message {
	t.Helper()
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(idgen.MustNewULID()),
		ConversationID:   convID,
		SenderIdentityID: "user:hayang",
		ContentKind:      conversation.MessageContentText,
		Content:          "hello",
		Direction:        conversation.DirectionInbound,
		PostedAt:         at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMessageRepo_AppendAndFindByID(t *testing.T) {
	_, mr, convID := setupConv(t)
	m := newMsg(t, convID, time.Now())
	if err := mr.Append(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	got, err := mr.FindByID(context.Background(), m.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Content() != "hello" {
		t.Fatal()
	}
}

func TestMessageRepo_FindByID_NotFound(t *testing.T) {
	_, mr, _ := setupConv(t)
	_, err := mr.FindByID(context.Background(), "M-NEVER")
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMessageRepo_Append_NilMessage(t *testing.T) {
	_, mr, _ := setupConv(t)
	if err := mr.Append(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

func TestMessageRepo_Append_VendorMsgRefDup(t *testing.T) {
	_, mr, convID := setupConv(t)
	m1, _ := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(idgen.MustNewULID()),
		ConversationID:   convID, SenderIdentityID: "user:x",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		VendorMsgRef: "vendor-1", PostedAt: time.Now(),
	})
	if err := mr.Append(context.Background(), m1); err != nil {
		t.Fatal(err)
	}
	m2, _ := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(idgen.MustNewULID()),
		ConversationID:   convID, SenderIdentityID: "user:x",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		VendorMsgRef: "vendor-1", PostedAt: time.Now(),
	})
	err := mr.Append(context.Background(), m2)
	if !errors.Is(err, conversation.ErrMessageDuplicate) {
		t.Fatalf("got %v", err)
	}
}

func TestMessageRepo_UpdateVendorMsgRef_Backfill(t *testing.T) {
	_, mr, convID := setupConv(t)
	m := newMsg(t, convID, time.Now())
	_ = mr.Append(context.Background(), m)
	if err := mr.UpdateVendorMsgRef(context.Background(), m.ID(), "vendor-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := mr.FindByID(context.Background(), m.ID())
	if got.VendorMsgRef() != "vendor-1" {
		t.Fatal()
	}
}

func TestMessageRepo_UpdateVendorMsgRef_AlreadySet(t *testing.T) {
	_, mr, convID := setupConv(t)
	m, _ := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(idgen.MustNewULID()),
		ConversationID:   convID, SenderIdentityID: "user:x",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		VendorMsgRef: "vendor-1", PostedAt: time.Now(),
	})
	_ = mr.Append(context.Background(), m)
	err := mr.UpdateVendorMsgRef(context.Background(), m.ID(), "vendor-2")
	if !errors.Is(err, conversation.ErrMessageImmutable) {
		t.Fatalf("got %v", err)
	}
}

func TestMessageRepo_UpdateVendorMsgRef_NotFound(t *testing.T) {
	_, mr, _ := setupConv(t)
	err := mr.UpdateVendorMsgRef(context.Background(), "M-NEVER", "v")
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMessageRepo_UpdateVendorMsgRef_EmptyValue(t *testing.T) {
	_, mr, _ := setupConv(t)
	if err := mr.UpdateVendorMsgRef(context.Background(), "M-1", ""); err == nil {
		t.Fatal()
	}
}

func TestMessageRepo_FindByConversationID_All(t *testing.T) {
	_, mr, convID := setupConv(t)
	base := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_ = mr.Append(context.Background(), newMsg(t, convID, base.Add(time.Duration(i)*time.Minute)))
	}
	got, err := mr.FindByConversationID(context.Background(), convID, conversation.MessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d", len(got))
	}
}

func TestMessageRepo_FindByConversationID_SinceFilter(t *testing.T) {
	_, mr, convID := setupConv(t)
	base := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_ = mr.Append(context.Background(), newMsg(t, convID, base.Add(time.Duration(i)*time.Hour)))
	}
	since := base.Add(90 * time.Minute)
	got, _ := mr.FindByConversationID(context.Background(), convID, conversation.MessageFilter{Since: &since})
	if len(got) != 1 {
		t.Fatalf("since filter: %d", len(got))
	}
}

func TestMessageRepo_FindByConversationID_TailLimit(t *testing.T) {
	_, mr, convID := setupConv(t)
	base := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_ = mr.Append(context.Background(), newMsg(t, convID, base.Add(time.Duration(i)*time.Minute)))
	}
	got, _ := mr.FindByConversationID(context.Background(), convID, conversation.MessageFilter{Tail: 3})
	if len(got) != 3 {
		t.Fatalf("tail: %d", len(got))
	}
}

func TestMessageRepo_FindByConversationID_Limit(t *testing.T) {
	_, mr, convID := setupConv(t)
	base := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_ = mr.Append(context.Background(), newMsg(t, convID, base.Add(time.Duration(i)*time.Minute)))
	}
	got, _ := mr.FindByConversationID(context.Background(), convID, conversation.MessageFilter{Limit: 2})
	if len(got) != 2 {
		t.Fatalf("limit: %d", len(got))
	}
}

func TestMessageRepo_FindRecent_Chronological(t *testing.T) {
	_, mr, convID := setupConv(t)
	base := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_ = mr.Append(context.Background(), newMsg(t, convID, base.Add(time.Duration(i)*time.Minute)))
	}
	got, err := mr.FindRecent(context.Background(), convID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len %d", len(got))
	}
	// Should be oldest first.
	for i := 1; i < len(got); i++ {
		if got[i].PostedAt().Before(got[i-1].PostedAt()) {
			t.Fatal("not chronological")
		}
	}
}

func TestMessageRepo_FindRecent_DefaultN(t *testing.T) {
	_, mr, convID := setupConv(t)
	_ = mr.Append(context.Background(), newMsg(t, convID, time.Now()))
	got, _ := mr.FindRecent(context.Background(), convID, 0)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
}

func TestMessageRepo_FindByVendorMsgRef(t *testing.T) {
	_, mr, convID := setupConv(t)
	m, _ := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(idgen.MustNewULID()),
		ConversationID:   convID, SenderIdentityID: "user:x",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		VendorMsgRef: "v-1", PostedAt: time.Now(),
	})
	_ = mr.Append(context.Background(), m)
	got, err := mr.FindByVendorMsgRef(context.Background(), "v-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != m.ID() {
		t.Fatal()
	}
}

func TestMessageRepo_FindByVendorMsgRef_NotFound(t *testing.T) {
	_, mr, _ := setupConv(t)
	_, err := mr.FindByVendorMsgRef(context.Background(), "absent")
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("got %v", err)
	}
}
