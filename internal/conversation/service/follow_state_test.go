package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type followFixture struct {
	svc      *FollowStateService
	fsRepo   *convsqlite.FollowStateRepo
	convRepo *convsqlite.ConversationRepo
	clock    *clock.FakeClock
}

func setupFollowStateService(t *testing.T) *followFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	convRepo := convsqlite.NewConversationRepo(db)
	fsRepo := convsqlite.NewFollowStateRepo(db)
	return &followFixture{
		svc:      NewFollowStateService(fsRepo, convRepo, fc),
		fsRepo:   fsRepo,
		convRepo: convRepo,
		clock:    fc,
	}
}

func (f *followFixture) seedConv(t *testing.T, id conversation.ConversationID, parent conversation.ConversationID) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:                   id,
		Kind:                 conversation.ConversationKindChannel,
		Name:                 "fs-" + string(id),
		CreatedBy:            conversation.IdentityRef("user:hayang"),
		OpenedAt:             f.clock.Now(),
		ParentConversationID: parent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.convRepo.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

func TestFollow_DefaultTopLevelFollowed(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")
	got, err := f.svc.IsFollowed(context.Background(), "user:hayang", "chan-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("top-level conversation should default to followed")
	}
}

func TestFollow_DefaultThreadNotFollowed(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")
	f.seedConv(t, "thread-1", "chan-1")
	got, err := f.svc.IsFollowed(context.Background(), "user:hayang", "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("thread should default to NOT followed")
	}
}

func TestFollow_ExplicitUnfollowThenRefollow(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")
	ctx := context.Background()

	if err := f.svc.Unfollow(ctx, "user:hayang", "chan-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.IsFollowed(ctx, "user:hayang", "chan-1"); got {
		t.Fatal("after explicit unfollow, should be not followed")
	}
	// Idempotent unfollow.
	if err := f.svc.Unfollow(ctx, "user:hayang", "chan-1"); err != nil {
		t.Fatalf("idempotent unfollow: %v", err)
	}
	if err := f.svc.Follow(ctx, "user:hayang", "chan-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.IsFollowed(ctx, "user:hayang", "chan-1"); !got {
		t.Fatal("after re-follow, should be followed")
	}
}

func TestFollow_AutoFollowThread(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")
	f.seedConv(t, "thread-1", "chan-1")
	ctx := context.Background()

	if err := f.svc.AutoFollow(ctx, "user:hayang", "thread-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.IsFollowed(ctx, "user:hayang", "thread-1"); !got {
		t.Fatal("auto-follow should make the thread followed")
	}
}

func TestFollow_AutoFollowNeverResurrectsUnfollow(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")
	f.seedConv(t, "thread-1", "chan-1")
	ctx := context.Background()

	// User auto-followed (participate), then explicitly unfollowed.
	if err := f.svc.AutoFollow(ctx, "user:hayang", "thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Unfollow(ctx, "user:hayang", "thread-1"); err != nil {
		t.Fatal(err)
	}
	// A later participate must NOT re-follow.
	if err := f.svc.AutoFollow(ctx, "user:hayang", "thread-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.IsFollowed(ctx, "user:hayang", "thread-1"); got {
		t.Fatal("auto-follow resurrected an explicit unfollow")
	}
}

func TestFollow_AgentAlwaysFalse_SkipWrite(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")
	ctx := context.Background()

	// Follow/Unfollow are no-ops for agents and write no row.
	if err := f.svc.Follow(ctx, "agent:bot1", "chan-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.IsFollowed(ctx, "agent:bot1", "chan-1"); got {
		t.Fatal("agent must always resolve to not-followed (Q-T1)")
	}
	if err := f.svc.AutoFollow(ctx, "agent:bot1", "chan-1"); err != nil {
		t.Fatal(err)
	}
	// No row written for the agent (skip-write, not just zero-in-DTO).
	if _, err := f.fsRepo.FindByUserAndConv(ctx, "agent:bot1", "chan-1"); err == nil {
		t.Fatal("expected no follow row for agent (skip-write)")
	}
}

func TestFollow_ResolveFollowedBatch(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")         // default followed
	f.seedConv(t, "chan-2", "")         // will explicit-unfollow
	f.seedConv(t, "thread-1", "chan-1") // thread default not-followed
	ctx := context.Background()

	if err := f.svc.Unfollow(ctx, "user:hayang", "chan-2"); err != nil {
		t.Fatal(err)
	}

	c1, _ := f.convRepo.FindByID(ctx, "chan-1")
	c2, _ := f.convRepo.FindByID(ctx, "chan-2")
	th, _ := f.convRepo.FindByID(ctx, "thread-1")

	got, err := f.svc.ResolveFollowed(ctx, "user:hayang",
		[]*conversation.Conversation{c1, c2, th})
	if err != nil {
		t.Fatal(err)
	}
	if !got["chan-1"] {
		t.Fatal("chan-1 should be followed (default)")
	}
	if got["chan-2"] {
		t.Fatal("chan-2 should be not-followed (explicit unfollow)")
	}
	if got["thread-1"] {
		t.Fatal("thread-1 should be not-followed (thread default)")
	}
}

func TestFollow_ResolveFollowedBatch_AgentAllFalse(t *testing.T) {
	f := setupFollowStateService(t)
	f.seedConv(t, "chan-1", "")
	ctx := context.Background()
	c1, _ := f.convRepo.FindByID(ctx, "chan-1")
	got, err := f.svc.ResolveFollowed(ctx, "agent:bot1",
		[]*conversation.Conversation{c1})
	if err != nil {
		t.Fatal(err)
	}
	if got["chan-1"] {
		t.Fatal("agent should resolve all conversations to not-followed")
	}
}
