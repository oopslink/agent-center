package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupFollowState(t *testing.T) *FollowStateRepo {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewFollowStateRepo(db)
}

func mkFollowState(userID conversation.IdentityRef, convID conversation.ConversationID,
	followed bool, version int,
) *conversation.UserConversationFollowState {
	return &conversation.UserConversationFollowState{
		UserID:         userID,
		ConversationID: convID,
		Followed:       followed,
		UpdatedAt:      time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Version:        version,
	}
}

func TestFollowStateRepo_UpsertInsertThenUpdate(t *testing.T) {
	r := setupFollowState(t)
	ctx := context.Background()

	// Explicit unfollow of a default-followed channel.
	s := mkFollowState("user:hayang", "conv-1", false, 0)
	if err := r.Upsert(ctx, s); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if s.Version != 1 {
		t.Fatalf("post-insert version=%d want 1", s.Version)
	}

	got, err := r.FindByUserAndConv(ctx, "user:hayang", "conv-1")
	if err != nil {
		t.Fatalf("find after insert: %v", err)
	}
	if got.Version != 1 || got.Followed != false {
		t.Fatalf("got %+v", got)
	}

	// Re-follow (CAS-update).
	got.Followed = true
	got.UpdatedAt = time.Date(2026, 5, 24, 12, 1, 0, 0, time.UTC)
	if err := r.Upsert(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Version != 2 {
		t.Fatalf("post-update version=%d want 2", got.Version)
	}

	final, _ := r.FindByUserAndConv(ctx, "user:hayang", "conv-1")
	if final.Followed != true || final.Version != 2 {
		t.Fatalf("final read: %+v", final)
	}
}

func TestFollowStateRepo_UpsertVersionConflict(t *testing.T) {
	r := setupFollowState(t)
	ctx := context.Background()

	if err := r.Upsert(ctx, mkFollowState("user:hayang", "conv-1", true, 0)); err != nil {
		t.Fatal(err)
	}
	stale := mkFollowState("user:hayang", "conv-1", false, 99)
	err := r.Upsert(ctx, stale)
	if !errors.Is(err, conversation.ErrFollowStateVersionConflict) {
		t.Fatalf("got %v want ErrFollowStateVersionConflict", err)
	}
}

func TestFollowStateRepo_UpsertConcurrentInsertConflict(t *testing.T) {
	r := setupFollowState(t)
	ctx := context.Background()

	if err := r.Upsert(ctx, mkFollowState("user:hayang", "conv-1", true, 0)); err != nil {
		t.Fatal(err)
	}
	err := r.Upsert(ctx, mkFollowState("user:hayang", "conv-1", false, 0))
	if !errors.Is(err, conversation.ErrFollowStateVersionConflict) {
		t.Fatalf("duplicate insert: got %v want ErrFollowStateVersionConflict", err)
	}
}

func TestFollowStateRepo_FindByUserAndConv_Absent(t *testing.T) {
	r := setupFollowState(t)
	_, err := r.FindByUserAndConv(context.Background(), "user:hayang", "conv-missing")
	if !errors.Is(err, conversation.ErrFollowStateNotFound) {
		t.Fatalf("got %v want ErrFollowStateNotFound", err)
	}
}

// InsertIfAbsent is the auto-follow primitive: it must create a
// followed=true row when absent, and must NEVER override an existing
// explicit unfollow (followed=false).
func TestFollowStateRepo_InsertIfAbsent(t *testing.T) {
	r := setupFollowState(t)
	ctx := context.Background()

	// Absent → inserts followed=true.
	s := mkFollowState("user:hayang", "thread-1", true, 0)
	inserted, err := r.InsertIfAbsent(ctx, s)
	if err != nil {
		t.Fatalf("insert-if-absent: %v", err)
	}
	if !inserted {
		t.Fatal("expected inserted=true on absent row")
	}
	got, _ := r.FindByUserAndConv(ctx, "user:hayang", "thread-1")
	if got == nil || !got.Followed {
		t.Fatalf("got %+v want followed=true", got)
	}

	// Present (followed=true) → no-op.
	again, err := r.InsertIfAbsent(ctx, mkFollowState("user:hayang", "thread-1", true, 0))
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("expected inserted=false when row already present")
	}
}

func TestFollowStateRepo_InsertIfAbsent_NeverOverridesUnfollow(t *testing.T) {
	r := setupFollowState(t)
	ctx := context.Background()

	// User explicitly unfollowed thread-1.
	if err := r.Upsert(ctx, mkFollowState("user:hayang", "thread-1", false, 0)); err != nil {
		t.Fatal(err)
	}
	// Auto-follow (participate / @mention) must NOT resurrect it.
	inserted, err := r.InsertIfAbsent(ctx, mkFollowState("user:hayang", "thread-1", true, 0))
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("auto-follow overrode an explicit unfollow")
	}
	got, _ := r.FindByUserAndConv(ctx, "user:hayang", "thread-1")
	if got.Followed != false {
		t.Fatalf("explicit unfollow was clobbered: %+v", got)
	}
}

func TestFollowStateRepo_FindByUserBatch(t *testing.T) {
	r := setupFollowState(t)
	ctx := context.Background()
	for _, s := range []*conversation.UserConversationFollowState{
		mkFollowState("user:hayang", "conv-a", true, 0),
		mkFollowState("user:hayang", "conv-b", false, 0),
		mkFollowState("user:hayang", "conv-c", true, 0),
		mkFollowState("user:other", "conv-a", true, 0),
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
	wantIDs := []conversation.ConversationID{"conv-a", "conv-b", "conv-c"}
	for i, s := range got {
		if s.ConversationID != wantIDs[i] {
			t.Fatalf("row %d conv_id=%s want %s", i, s.ConversationID, wantIDs[i])
		}
	}
}

func TestFollowStateRepo_DeleteByConversationID(t *testing.T) {
	r := setupFollowState(t)
	ctx := context.Background()
	if err := r.Upsert(ctx, mkFollowState("user:hayang", "conv-x", true, 0)); err != nil {
		t.Fatal(err)
	}
	if err := r.Upsert(ctx, mkFollowState("user:other", "conv-x", false, 0)); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteByConversationID(ctx, "conv-x"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByUserAndConv(ctx, "user:hayang", "conv-x"); !errors.Is(err, conversation.ErrFollowStateNotFound) {
		t.Fatalf("row not deleted: %v", err)
	}
	// Idempotent.
	if err := r.DeleteByConversationID(ctx, "conv-x"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestFollowStateRepo_UpsertNilStateRejected(t *testing.T) {
	r := setupFollowState(t)
	if err := r.Upsert(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil state")
	}
}
