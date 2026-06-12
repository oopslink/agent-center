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

// mkReply builds a depth-1 reply message hanging off rootID.
func mkReply(t *testing.T, id, rootID conversation.MessageID, convID conversation.ConversationID, postedAt time.Time) *conversation.Message {
	t.Helper()
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: id, ConversationID: convID, SenderIdentityID: "user:hayang",
		ContentKind: conversation.MessageContentText, Content: "re", Direction: conversation.DirectionInbound,
		PostedAt: postedAt, ParentMessageID: rootID, RootMessageID: rootID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// v2.9.1 Thread (P1) read side: FindThread returns the root + ALL its replies in
// posted_at order, scoped to the conversation; replies from OTHER roots / other
// conversations are excluded.
func TestMessageRepo_FindThread_RootPlusOrderedReplies(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	_ = convR.Save(ctx, mkConv(t, "c-2", conversation.ConversationKindDM, ""))
	base := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	// Thread A in c-1: root + 2 replies (appended out of order to prove sorting).
	_ = msgR.Append(ctx, mkMsg(t, "A", "c-1"))
	_ = msgR.Append(ctx, mkReply(t, "A-r2", "A", "c-1", base.Add(2*time.Minute)))
	_ = msgR.Append(ctx, mkReply(t, "A-r1", "A", "c-1", base.Add(1*time.Minute)))
	// A different root B in c-1, and a same-id thread in c-2 — must NOT leak in.
	_ = msgR.Append(ctx, mkMsg(t, "B", "c-1"))
	_ = msgR.Append(ctx, mkReply(t, "B-r1", "B", "c-1", base.Add(3*time.Minute)))
	_ = msgR.Append(ctx, mkMsg(t, "A", "c-2")) // same id "A" in another conv — isolation

	got, err := msgR.FindThread(ctx, "c-1", "A")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, string(m.ID()))
	}
	want := []string{"A", "A-r1", "A-r2"}
	if len(ids) != len(want) {
		t.Fatalf("thread ids = %v want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("thread ids = %v want %v (root-first then posted_at ASC)", ids, want)
		}
	}
}

// A root with no replies returns just the root.
func TestMessageRepo_FindThread_RootOnly(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	_ = msgR.Append(ctx, mkMsg(t, "solo", "c-1"))
	got, err := msgR.FindThread(ctx, "c-1", "solo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "solo" {
		t.Fatalf("got %d msgs, want just the root", len(got))
	}
}

// An unknown root id (or one in another conversation) yields an empty thread —
// the handler turns this into a 404 (existence-non-disclosure).
func TestMessageRepo_FindThread_UnknownRoot_Empty(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	got, err := msgR.FindThread(ctx, "c-1", "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unknown root should yield empty, got %d", len(got))
	}
}

// ThreadReplyCounts groups reply counts by root for the whole conversation in one
// query — the foundation for the message-list thread badge (no N+1).
func TestMessageRepo_ThreadReplyCounts(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	base := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	_ = msgR.Append(ctx, mkMsg(t, "A", "c-1"))
	_ = msgR.Append(ctx, mkReply(t, "A-r1", "A", "c-1", base.Add(time.Minute)))
	_ = msgR.Append(ctx, mkReply(t, "A-r2", "A", "c-1", base.Add(2*time.Minute)))
	_ = msgR.Append(ctx, mkMsg(t, "B", "c-1")) // no replies → absent from the map

	counts, err := msgR.ThreadReplyCounts(ctx, "c-1")
	if err != nil {
		t.Fatal(err)
	}
	if counts["A"] != 2 {
		t.Fatalf("A reply count = %d want 2", counts["A"])
	}
	if _, ok := counts["B"]; ok {
		t.Fatalf("B has no replies; should be absent from counts, got %v", counts)
	}
}
