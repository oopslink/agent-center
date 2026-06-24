package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

// Tester lane — v2.9.1 Thread P1/P2 prod-upgrade real-DB acceptance.
//
// The round-trip test migrates an EMPTY schema 56→57. The live instance upgrades a
// DB that already holds message rows. This reproduces that real upgrade path on a
// real migrator + real repo (in-memory, zero footprint) and asserts: a 0057
// ADD COLUMN upgrade leaves pre-existing messages valid + treated as thread ROOTS
// (NULL refs), and new replies thread off them (FindThreadReplies / ThreadReplyDigests
// work). Guards the "upgrade breaks existing data / legacy rows aren't valid roots" class.
func TestUpgradeAcc_Migrate0057_PreservesLegacyMessagesAsRoots(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mig := persistence.NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := mig.Down(ctx, 56); err != nil { // back to the pre-feature schema
		t.Fatalf("Down to 56: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 56 {
		t.Fatalf("pre-upgrade version = %d want 56", v)
	}

	// Seed legacy rows using ONLY the v56 column set (no thread refs).
	const conv = "conv-legacy"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ctxRefsJSON, _ := conversation.MarshalContextRefsJSON(conversation.ContextRefs{})
	attsJSON, _ := conversation.MarshalAttachmentsJSON(nil)
	for _, id := range []string{"msg-legacy-1", "msg-legacy-2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO messages
			(id, conversation_id, sender_identity_id, content_kind, content,
			 direction, input_request_ref, context_refs, attachments, posted_at, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			id, conv, "user:hayang", "text", "legacy content",
			"inbound", "", ctxRefsJSON, attsJSON, now, now); err != nil {
			t.Fatalf("seed legacy %s: %v", id, err)
		}
	}

	// THE UPGRADE: 56 → 57 on a DB that already holds rows.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("upgrade Up 56→57: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 79 {
		t.Fatalf("post-upgrade version = %d want 79", v)
	}

	repo := NewMessageRepo(db)

	// 1. Legacy rows survive + rehydrate as valid thread ROOTS (NULL refs).
	for _, id := range []string{"msg-legacy-1", "msg-legacy-2"} {
		m, err := repo.FindByID(ctx, conversation.MessageID(id))
		if err != nil {
			t.Fatalf("legacy %s lost after upgrade: %v", id, err)
		}
		if !m.IsThreadRoot() {
			t.Errorf("legacy %s not a thread root (parent=%q root=%q)", id, m.ParentMessageID(), m.RootMessageID())
		}
		if m.Content() != "legacy content" {
			t.Errorf("legacy %s content corrupted: %q", id, m.Content())
		}
	}

	// 2. A legacy root has no replies yet.
	replies, err := repo.FindThreadReplies(ctx, conv, "msg-legacy-1")
	if err != nil {
		t.Fatalf("FindThreadReplies on legacy root: %v", err)
	}
	if len(replies) != 0 {
		t.Fatalf("legacy root replies = %d want 0", len(replies))
	}

	// 3. A NEW reply threads off the legacy root via the real repo (depth-1).
	parent, _ := conversation.ResolveReplyPlacement(mustFind(t, repo, "msg-legacy-1"))
	reply, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: "msg-reply-1", ConversationID: conv, SenderIdentityID: "user:hayang",
		ContentKind: conversation.MessageContentText, Content: "reply to legacy",
		Direction:       conversation.DirectionInbound,
		ParentMessageID: parent, RootMessageID: parent, PostedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("build reply: %v", err)
	}
	if err := repo.Append(ctx, reply); err != nil {
		t.Fatalf("append reply to legacy root: %v", err)
	}

	replies, _ = repo.FindThreadReplies(ctx, conv, "msg-legacy-1")
	if len(replies) != 1 || replies[0].ID() != "msg-reply-1" {
		t.Fatalf("after reply, replies = %v want [msg-reply-1]", replyIDs(replies))
	}
	digests, _ := repo.ThreadReplyDigests(ctx, conv)
	dg, ok := digests["msg-legacy-1"]
	if !ok || dg.ReplyCount != 1 || dg.LastActivityAt == "" {
		t.Errorf("legacy root digest = %+v want {ReplyCount:1, LastActivityAt:non-empty}", dg)
	}

	// 4. Down-migration off the upgraded-with-data DB stays reversible.
	if err := mig.Down(ctx, 56); err != nil {
		t.Fatalf("rollback 57→56 with data present: %v", err)
	}
}

func mustFind(t *testing.T, repo *MessageRepo, id string) *conversation.Message {
	t.Helper()
	m, err := repo.FindByID(context.Background(), conversation.MessageID(id))
	if err != nil {
		t.Fatalf("FindByID %s: %v", id, err)
	}
	return m
}

func replyIDs(ms []*conversation.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m.ID())
	}
	return out
}
