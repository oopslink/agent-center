package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type readStateFixture struct {
	svc       *ReadStateService
	rsRepo    *convsqlite.ReadStateRepo
	msgRepo   *convsqlite.MessageRepo
	convRepo  *convsqlite.ConversationRepo
	eventRepo observability.EventRepository
	clock     *clock.FakeClock
}

func setupReadStateService(t *testing.T) *readStateFixture {
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
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	conv := convsqlite.NewConversationRepo(db)
	msg := convsqlite.NewMessageRepo(db)
	rs := convsqlite.NewReadStateRepo(db)
	svc := NewReadStateService(db, rs, msg, sink, fc)
	return &readStateFixture{
		svc:       svc,
		rsRepo:    rs,
		msgRepo:   msg,
		convRepo:  conv,
		eventRepo: er,
		clock:     fc,
	}
}

// helper: create a channel conv + N messages, returning the ids.
func seedConvWithMessages(t *testing.T, f *readStateFixture, convID conversation.ConversationID, n int) []conversation.MessageID {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:        convID,
		Kind:      conversation.ConversationKindProjectChannel,
		Name:      "rs-test-" + string(convID),
		CreatedBy: conversation.IdentityRef("user:hayang"),
		OpenedAt:  f.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.convRepo.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	ids := make([]conversation.MessageID, n)
	for i := 0; i < n; i++ {
		f.clock.Advance(time.Millisecond)
		m, err := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(string(convID) + "-msg-" + ulidPad(i)),
			ConversationID:   convID,
			SenderIdentityID: conversation.IdentityRef("user:hayang"),
			ContentKind:      conversation.MessageContentText,
			Content:          "hi",
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

// ulidPad gives lexically-sortable suffixes so our test message ids
// behave like ULIDs in the only-forward comparison. Pads to 6 digits
// (supports tests up to one million messages).
func ulidPad(n int) string {
	return fmt.Sprintf("%06d", n)
}

func TestMarkSeen_FirstTime_Inserts(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 3)

	res, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID:            "user:hayang",
		ConversationID:    "conv-1",
		LastSeenMessageID: ids[0],
		Actor:             "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Bumped {
		t.Fatal("expected Bumped=true on first insert")
	}
	if res.Version != 1 || res.EventID == "" {
		t.Fatalf("got %+v", res)
	}
	// Row exists.
	got, _ := f.rsRepo.FindByUserAndConv(context.Background(), "user:hayang", "conv-1")
	if got.LastSeenMessageID != ids[0] {
		t.Fatalf("row last_seen=%s want %s", got.LastSeenMessageID, ids[0])
	}
}

func TestMarkSeen_Forward_Bumps(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 3)

	if _, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[0], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[2], Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Bumped || res.Version != 2 {
		t.Fatalf("got %+v", res)
	}
}

func TestMarkSeen_Backward_NoOp(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 3)

	// Move forward to ids[2].
	if _, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[2], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	// Try to move backward to ids[0] → no-op.
	res, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[0], Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Bumped {
		t.Fatal("expected Bumped=false on backward")
	}
	if res.LastSeenMessageID != ids[2] {
		t.Fatalf("cursor moved: %s", res.LastSeenMessageID)
	}
	if res.EventID != "" {
		t.Fatal("no-op should not emit an event")
	}
}

func TestMarkSeen_SameMessage_NoOp(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 2)

	if _, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[1], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[1], Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Bumped {
		t.Fatal("expected Bumped=false on identical")
	}
}

func TestMarkSeen_MessageInWrongConv(t *testing.T) {
	f := setupReadStateService(t)
	a := seedConvWithMessages(t, f, "conv-a", 1)
	_ = seedConvWithMessages(t, f, "conv-b", 1)

	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID:            "user:hayang",
		ConversationID:    "conv-b",
		LastSeenMessageID: a[0],
		Actor:             "user:hayang",
	})
	if !errors.Is(err, conversation.ErrReadStateMessageNotInConversation) {
		t.Fatalf("got %v", err)
	}
}

func TestMarkSeen_MessageNotFound(t *testing.T) {
	f := setupReadStateService(t)
	seedConvWithMessages(t, f, "conv-1", 1)

	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: "missing-msg", Actor: "user:hayang",
	})
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMarkSeen_InvalidActor(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 1)
	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[0], Actor: "",
	})
	if err == nil {
		t.Fatal("expected actor validation error")
	}
}

func TestMarkSeen_InvalidUserID(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 1)
	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "no-prefix", ConversationID: "conv-1",
		LastSeenMessageID: ids[0], Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected user_id validation error")
	}
}

func TestMarkSeen_MissingMessageID(t *testing.T) {
	f := setupReadStateService(t)
	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected missing message id error")
	}
}

func TestMarkSeen_MissingConvID(t *testing.T) {
	f := setupReadStateService(t)
	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID:            "user:hayang",
		LastSeenMessageID: "msg-1",
		Actor:             "user:hayang",
	})
	if err == nil {
		t.Fatal("expected missing conv id error")
	}
}

func TestUnread_NoRow_AllUnread(t *testing.T) {
	f := setupReadStateService(t)
	seedConvWithMessages(t, f, "conv-1", 3)

	res, err := f.svc.Unread(context.Background(), "user:hayang", "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.LastSeenMessageID != "" {
		t.Fatalf("expected empty last_seen, got %s", res.LastSeenMessageID)
	}
	if res.UnreadCount != 3 {
		t.Fatalf("unread=%d want 3", res.UnreadCount)
	}
}

func TestUnread_WithRow_PartialUnread(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 3)
	if _, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[0], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.Unread(context.Background(), "user:hayang", "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.LastSeenMessageID != ids[0] {
		t.Fatalf("last_seen=%s", res.LastSeenMessageID)
	}
	if res.UnreadCount != 2 {
		t.Fatalf("unread=%d want 2", res.UnreadCount)
	}
}

func TestUnread_AllRead(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 2)
	if _, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[1], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.Unread(context.Background(), "user:hayang", "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.UnreadCount != 0 {
		t.Fatalf("unread=%d want 0", res.UnreadCount)
	}
}

func TestUnread_Cap_999_Plus(t *testing.T) {
	f := setupReadStateService(t)
	seedConvWithMessages(t, f, "conv-1", MaxUnreadCount+5)

	res, err := f.svc.Unread(context.Background(), "user:hayang", "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.UnreadCount != MaxUnreadCount {
		t.Fatalf("unread=%d want cap %d", res.UnreadCount, MaxUnreadCount)
	}
}

func TestUnread_InvalidUserID(t *testing.T) {
	f := setupReadStateService(t)
	if _, err := f.svc.Unread(context.Background(), "no-prefix", "conv-1"); err == nil {
		t.Fatal("expected user_id validation error")
	}
}

// fakeReadStateRepo lets us inject failures on each repo method to
// cover the service's defensive error branches.
type fakeReadStateRepo struct {
	findErr   error
	batchErr  error
	upsertErr error
	calls     int
}

func (f *fakeReadStateRepo) FindByUserAndConv(ctx context.Context,
	u conversation.IdentityRef, c conversation.ConversationID,
) (*conversation.UserConversationReadState, error) {
	return nil, f.findErr
}

func (f *fakeReadStateRepo) FindByUserBatch(ctx context.Context,
	u conversation.IdentityRef,
) ([]*conversation.UserConversationReadState, error) {
	return nil, f.batchErr
}

func (f *fakeReadStateRepo) Upsert(ctx context.Context,
	s *conversation.UserConversationReadState,
) error {
	f.calls++
	return f.upsertErr
}

func TestNewReadStateService_NilClockDefaults(t *testing.T) {
	// Constructor's nil-clock fallback path.
	svc := NewReadStateService(nil, &fakeReadStateRepo{}, nil, nil, nil)
	if svc == nil {
		t.Fatal("expected svc non-nil")
	}
	if svc.clock == nil {
		t.Fatal("expected default clock installed")
	}
}

func TestMarkSeen_RepoFindErrorPropagates(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 1)
	// Swap in a failing repo.
	boom := errors.New("boom")
	f.svc = NewReadStateService(f.svc.db, &fakeReadStateRepo{findErr: boom},
		f.msgRepo, nil, f.clock)
	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[0], Actor: "user:hayang",
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v want boom", err)
	}
}

func TestMarkSeen_RepoUpsertErrorPropagates(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 1)
	boom := errors.New("upsert boom")
	f.svc = NewReadStateService(f.svc.db,
		&fakeReadStateRepo{findErr: conversation.ErrReadStateNotFound, upsertErr: boom},
		f.msgRepo, nil, f.clock)
	_, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[0], Actor: "user:hayang",
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v want boom", err)
	}
}

func TestUnread_RepoFindErrorPropagates(t *testing.T) {
	f := setupReadStateService(t)
	seedConvWithMessages(t, f, "conv-1", 1)
	boom := errors.New("find boom")
	f.svc = NewReadStateService(f.svc.db, &fakeReadStateRepo{findErr: boom},
		f.msgRepo, nil, f.clock)
	_, err := f.svc.Unread(context.Background(), "user:hayang", "conv-1")
	if !errors.Is(err, boom) {
		t.Fatalf("got %v want boom", err)
	}
}

func TestMarkSeen_EmitsConvReadStateChangedEvent(t *testing.T) {
	f := setupReadStateService(t)
	ids := seedConvWithMessages(t, f, "conv-1", 2)
	if _, err := f.svc.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: "user:hayang", ConversationID: "conv-1",
		LastSeenMessageID: ids[1], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	events, err := f.eventRepo.Find(context.Background(), observability.EventQueryFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range events {
		if e.Type() != "conversation.read_state.changed" {
			continue
		}
		found = true
		if e.Refs().ConversationID != "conv-1" {
			t.Fatalf("event refs conv_id=%s", e.Refs().ConversationID)
		}
		if e.Refs().MessageID != string(ids[1]) {
			t.Fatalf("event refs message_id=%s want %s", e.Refs().MessageID, ids[1])
		}
		p := e.Payload()
		if p["user_id"] != "user:hayang" {
			t.Fatalf("payload user_id=%v", p["user_id"])
		}
		if p["last_seen_message_id"] != string(ids[1]) {
			t.Fatalf("payload last_seen=%v", p["last_seen_message_id"])
		}
	}
	if !found {
		t.Fatal("expected conversation.read_state.changed event")
	}
}
