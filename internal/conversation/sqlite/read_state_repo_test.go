package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupReadState(t *testing.T) *ReadStateRepo {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewReadStateRepo(db)
}

func mkReadState(userID conversation.IdentityRef, convID conversation.ConversationID,
	msgID conversation.MessageID, version int,
) *conversation.UserConversationReadState {
	return &conversation.UserConversationReadState{
		UserID:            userID,
		ConversationID:    convID,
		LastSeenMessageID: msgID,
		UpdatedAt:         time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Version:           version,
	}
}

func TestReadStateRepo_UpsertInsertThenUpdate(t *testing.T) {
	r := setupReadState(t)
	ctx := context.Background()

	s := mkReadState("user:hayang", "conv-1", "msg-1", 0)
	if err := r.Upsert(ctx, s); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if s.Version != 1 {
		t.Fatalf("post-insert version=%d want 1", s.Version)
	}

	// Read back and verify.
	got, err := r.FindByUserAndConv(ctx, "user:hayang", "conv-1")
	if err != nil {
		t.Fatalf("find after insert: %v", err)
	}
	if got.Version != 1 || got.LastSeenMessageID != "msg-1" {
		t.Fatalf("got %+v", got)
	}

	// CAS-update.
	got.LastSeenMessageID = "msg-5"
	got.UpdatedAt = time.Date(2026, 5, 24, 12, 1, 0, 0, time.UTC)
	if err := r.Upsert(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Version != 2 {
		t.Fatalf("post-update version=%d want 2", got.Version)
	}

	final, _ := r.FindByUserAndConv(ctx, "user:hayang", "conv-1")
	if final.LastSeenMessageID != "msg-5" || final.Version != 2 {
		t.Fatalf("final read: %+v", final)
	}
}

func TestReadStateRepo_UpsertVersionConflict(t *testing.T) {
	r := setupReadState(t)
	ctx := context.Background()

	seed := mkReadState("user:hayang", "conv-1", "msg-1", 0)
	if err := r.Upsert(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// Stale version pointer — should conflict.
	stale := mkReadState("user:hayang", "conv-1", "msg-9", 99)
	err := r.Upsert(ctx, stale)
	if !errors.Is(err, conversation.ErrReadStateVersionConflict) {
		t.Fatalf("got %v want ErrReadStateVersionConflict", err)
	}
}

func TestReadStateRepo_UpsertConcurrentInsertConflict(t *testing.T) {
	r := setupReadState(t)
	ctx := context.Background()

	// Two inserts at the same PK — second one must be the conflict.
	if err := r.Upsert(ctx, mkReadState("user:hayang", "conv-1", "msg-1", 0)); err != nil {
		t.Fatal(err)
	}
	err := r.Upsert(ctx, mkReadState("user:hayang", "conv-1", "msg-2", 0))
	if !errors.Is(err, conversation.ErrReadStateVersionConflict) {
		t.Fatalf("duplicate insert: got %v want ErrReadStateVersionConflict", err)
	}
}

func TestReadStateRepo_FindByUserAndConv_Absent(t *testing.T) {
	r := setupReadState(t)
	_, err := r.FindByUserAndConv(context.Background(), "user:hayang", "conv-missing")
	if !errors.Is(err, conversation.ErrReadStateNotFound) {
		t.Fatalf("got %v want ErrReadStateNotFound", err)
	}
}

func TestReadStateRepo_FindByUserBatch(t *testing.T) {
	r := setupReadState(t)
	ctx := context.Background()
	// Seed: user hayang has 3 convs, user other has 1.
	for _, s := range []*conversation.UserConversationReadState{
		mkReadState("user:hayang", "conv-a", "msg-a", 0),
		mkReadState("user:hayang", "conv-b", "msg-b", 0),
		mkReadState("user:hayang", "conv-c", "msg-c", 0),
		mkReadState("user:other", "conv-a", "msg-a", 0),
	} {
		if err := r.Upsert(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.FindByUserBatch(ctx, "user:hayang")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows want 3", len(got))
	}
	// Ordered by conversation_id ASC.
	wantIDs := []conversation.ConversationID{"conv-a", "conv-b", "conv-c"}
	for i, s := range got {
		if s.ConversationID != wantIDs[i] {
			t.Fatalf("row %d conv_id=%s want %s", i, s.ConversationID, wantIDs[i])
		}
		if s.UserID != "user:hayang" {
			t.Fatalf("row %d user_id=%s want user:hayang", i, s.UserID)
		}
	}
}

func TestReadStateRepo_FindByUserBatch_Empty(t *testing.T) {
	r := setupReadState(t)
	got, err := r.FindByUserBatch(context.Background(), "user:nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d rows want 0", len(got))
	}
}

func TestReadStateRepo_UpsertNilStateRejected(t *testing.T) {
	r := setupReadState(t)
	if err := r.Upsert(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil state")
	}
}
