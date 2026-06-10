package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TestFollowState_CrossVersionUpgrade_NonEmptyDB is the §9.4 real upgrade
// smoke (per the #245/#251 lesson): migration 0050 must run on a database
// that already holds real conversations + read-state from the prior version,
// and the no-backfill / absence=kind-default model must resolve old rows to
// the correct default. This is NOT an empty-DB round-trip.
func TestFollowState_CrossVersionUpgrade_NonEmptyDB(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mig := persistence.NewMigrator(db)
	// Bring the DB to the PRIOR schema version (49) — before 0050 exists.
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mig.Down(ctx, 49); err != nil {
		t.Fatalf("down to 49: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 49 {
		t.Fatalf("pre-upgrade version=%d want 49", v)
	}
	if tableExists(t, db, "user_conversation_follow_state") {
		t.Fatal("follow-state table should NOT exist at v49")
	}

	// Seed REAL prior-version data: a top-level channel + a thread + a
	// read-state row (all valid at v49).
	fc := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	convRepo := convsqlite.NewConversationRepo(db)
	seedConvAt := func(id, parent conversation.ConversationID) {
		c, err := conversation.NewConversation(conversation.NewConversationInput{
			ID:                   id,
			Kind:                 conversation.ConversationKindChannel,
			Name:                 "up-" + string(id),
			CreatedBy:            conversation.IdentityRef("user:hayang"),
			OpenedAt:             fc.Now(),
			ParentConversationID: parent,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := convRepo.Save(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	seedConvAt("old-chan", "")
	seedConvAt("old-thread", "old-chan")
	rsRepo := convsqlite.NewReadStateRepo(db)
	if err := rsRepo.Upsert(ctx, &conversation.UserConversationReadState{
		UserID: "user:hayang", ConversationID: "old-chan",
		LastSeenMessageID: "old-chan-msg-1", UpdatedAt: fc.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// UPGRADE across the 0050 boundary on the NON-EMPTY DB (full Up → latest 54:
	// v2.9 #283 added 0054_v29_plan_orchestration, so this drift-guard now expects 54).
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("upgrade to latest: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 54 {
		t.Fatalf("post-upgrade version=%d want 54", v)
	}
	if !tableExists(t, db, "user_conversation_follow_state") {
		t.Fatal("follow-state table must exist after upgrade")
	}

	// Prior data intact (read-state row survived the migration).
	if _, err := rsRepo.FindByUserAndConv(ctx, "user:hayang", "old-chan"); err != nil {
		t.Fatalf("prior read-state lost across upgrade: %v", err)
	}

	// The watermark: old rows (no follow row) resolve to the correct
	// kind-default — top-level followed, thread not — without any backfill.
	svc := NewFollowStateService(convsqlite.NewFollowStateRepo(db), convRepo, fc)
	if got, _ := svc.IsFollowed(ctx, "user:hayang", "old-chan"); !got {
		t.Fatal("old top-level channel should resolve followed=true (default)")
	}
	if got, _ := svc.IsFollowed(ctx, "user:hayang", "old-thread"); got {
		t.Fatal("old thread should resolve followed=false (default)")
	}

	// Post-upgrade participate/@mention auto-follows the thread, and a later
	// explicit unfollow is never resurrected — verified on the upgraded DB.
	if err := svc.AutoFollow(ctx, "user:hayang", "old-thread"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.IsFollowed(ctx, "user:hayang", "old-thread"); !got {
		t.Fatal("auto-follow should follow the old thread post-upgrade")
	}
	if err := svc.Unfollow(ctx, "user:hayang", "old-thread"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AutoFollow(ctx, "user:hayang", "old-thread"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.IsFollowed(ctx, "user:hayang", "old-thread"); got {
		t.Fatal("auto-follow resurrected an explicit unfollow after upgrade")
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	if err != nil {
		t.Fatalf("table-exists query: %v", err)
	}
	return n > 0
}
