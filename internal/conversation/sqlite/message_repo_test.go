package sqlite

import (
	"context"
	"errors"
	"fmt"
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
	// T189: Tail returns the NEWEST N rows, in oldest→newest order. With a..e
	// posted 0..4s, Tail:2 must be [d, e] — NOT the oldest [a, b].
	if string(got[0].ID()) != "d" || string(got[1].ID()) != "e" {
		t.Fatalf("expected newest two [d e] in ASC order, got [%s %s]", got[0].ID(), got[1].ID())
	}
}

// TestMessageRepo_Tail_BeyondLimit_IncludesLatest is the T189 regression: when a
// conversation has MORE top-level messages than the Tail window, the result must
// include the LATEST message (and drop the oldest), not freeze on the oldest
// window. Uses 250 messages vs a Tail of 200 (the real handler window).
func TestMessageRepo_Tail_BeyondLimit_IncludesLatest(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	now := time.Now().UTC()
	const total = 250
	for i := 0; i < total; i++ {
		m, _ := conversation.NewMessage(conversation.NewMessageInput{
			ID: conversation.MessageID(fmt.Sprintf("m-%04d", i)), ConversationID: "c-1",
			SenderIdentityID: "user:h", ContentKind: conversation.MessageContentText,
			Direction: conversation.DirectionInbound, PostedAt: now.Add(time.Duration(i) * time.Millisecond),
		})
		if err := msgR.Append(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := msgR.FindByConversationID(context.Background(), "c-1",
		conversation.MessageFilter{Tail: 200, TopLevelOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 200 {
		t.Fatalf("expected 200, got %d", len(got))
	}
	// Newest message MUST be present (this is the bug: it used to be absent).
	if last := string(got[len(got)-1].ID()); last != "m-0249" {
		t.Fatalf("newest message missing: last id = %s, want m-0249", last)
	}
	// Window is the newest 200 → oldest 50 dropped; first kept is m-0050, ASC.
	if first := string(got[0].ID()); first != "m-0050" {
		t.Fatalf("expected oldest-in-window m-0050, got %s", first)
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

// v2.9.1 Thread (P1) read side: FindThreadReplies returns ONLY the replies (NOT the
// root) in posted_at order, scoped to the conversation; replies from OTHER roots /
// other conversations are excluded.
func TestMessageRepo_FindThreadReplies_OrderedChildrenOnly(t *testing.T) {
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
	_ = msgR.Append(ctx, mkReply(t, "A-r1", "A", "c-2", base.Add(time.Minute))) // same ids, other conv

	got, err := msgR.FindThreadReplies(ctx, "c-1", "A")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, string(m.ID()))
	}
	want := []string{"A-r1", "A-r2"} // children only, posted_at ASC; root A excluded
	if len(ids) != len(want) {
		t.Fatalf("reply ids = %v want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("reply ids = %v want %v (posted_at ASC, children only)", ids, want)
		}
	}
}

// A root with no replies yields an empty slice.
func TestMessageRepo_FindThreadReplies_None(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	_ = msgR.Append(ctx, mkMsg(t, "solo", "c-1"))
	got, err := msgR.FindThreadReplies(ctx, "c-1", "solo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("root with no replies should yield empty, got %d", len(got))
	}
}

// ThreadReplyDigests groups reply count + last-activity by root for the whole
// conversation in one query — the message-list thread-badge foundation (no N+1).
func TestMessageRepo_ThreadReplyDigests(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	base := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	last := base.Add(2 * time.Minute)
	_ = msgR.Append(ctx, mkMsg(t, "A", "c-1"))
	_ = msgR.Append(ctx, mkReply(t, "A-r1", "A", "c-1", base.Add(time.Minute)))
	_ = msgR.Append(ctx, mkReply(t, "A-r2", "A", "c-1", last))
	_ = msgR.Append(ctx, mkMsg(t, "B", "c-1")) // no replies → absent from the map

	digests, err := msgR.ThreadReplyDigests(ctx, "c-1")
	if err != nil {
		t.Fatal(err)
	}
	if digests["A"].ReplyCount != 2 {
		t.Fatalf("A reply count = %d want 2", digests["A"].ReplyCount)
	}
	if digests["A"].LastActivityAt != last.Format(time.RFC3339Nano) {
		t.Fatalf("A last activity = %q want %q", digests["A"].LastActivityAt, last.Format(time.RFC3339Nano))
	}
	// v2.9.1 P3: LastReplyID = MAX(reply id) — the latest reply, for the per-user
	// has-new-activity compare. Ids "A-r1" < "A-r2" lexicographically.
	if digests["A"].LastReplyID != "A-r2" {
		t.Fatalf("A last reply id = %q want %q", digests["A"].LastReplyID, "A-r2")
	}
	if _, ok := digests["B"]; ok {
		t.Fatalf("B has no replies; should be absent, got %v", digests)
	}
}

// TopLevelOnly filter excludes replies from FindByConversationID (the main flow).
func TestMessageRepo_FindByConversationID_TopLevelOnly(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	ctx := context.Background()
	_ = convR.Save(ctx, mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	base := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	_ = msgR.Append(ctx, mkMsg(t, "A", "c-1"))
	_ = msgR.Append(ctx, mkReply(t, "A-r1", "A", "c-1", base.Add(time.Minute)))
	_ = msgR.Append(ctx, mkMsg(t, "C", "c-1"))

	got, err := msgR.FindByConversationID(ctx, "c-1", conversation.MessageFilter{TopLevelOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, string(m.ID()))
	}
	for _, id := range ids {
		if id == "A-r1" {
			t.Fatalf("reply A-r1 must be excluded from top-level list, got %v", ids)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("top-level list = %v want [A C]", ids)
	}
}
