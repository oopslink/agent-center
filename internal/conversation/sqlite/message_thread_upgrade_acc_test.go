package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

// Tester lane — v2.9.1 Thread P1 prod-upgrade real-DB acceptance.
//
// The deterministic round-trip test migrates an EMPTY schema 56→57. The live
// instance, however, upgrades a DB that already holds message rows. This guard
// reproduces that real upgrade path on a real migrator + real repo (zero
// footprint: in-memory DB) and asserts the institutional invariant: a 0057
// ADD COLUMN upgrade leaves pre-existing messages valid and treats them as
// thread ROOTS (NULL refs), and new replies thread off them correctly. This
// guards the "upgrade breaks existing data / legacy rows aren't valid roots"
// regression class.
func TestUpgradeAcc_Migrate0057_PreservesLegacyMessagesAsRoots(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mig := persistence.NewMigrator(db)
	// Reach the pre-feature schema exactly as the live instance was before v2.9.1:
	// migrate to latest, then DOWN to 56 (drops the thread columns).
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := mig.Down(ctx, 56); err != nil {
		t.Fatalf("Down to 56: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 56 {
		t.Fatalf("pre-upgrade version = %d want 56", v)
	}

	// Seed legacy messages using ONLY the v56 column set (no thread refs) — exactly
	// what real prod rows look like before the upgrade.
	const conv = "conv-legacy"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ctxRefsJSON, _ := conversation.MarshalContextRefsJSON(conversation.ContextRefs{})
	attsJSON, _ := conversation.MarshalAttachmentsJSON(nil)
	legacy := []string{"msg-legacy-1", "msg-legacy-2"}
	for _, id := range legacy {
		_, err := db.ExecContext(ctx, `INSERT INTO messages
			(id, conversation_id, sender_identity_id, content_kind, content,
			 direction, input_request_ref, context_refs, attachments, posted_at, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			id, conv, "user:hayang", "text", "legacy content",
			"inbound", "", ctxRefsJSON, attsJSON, now, now)
		if err != nil {
			t.Fatalf("seed legacy %s: %v", id, err)
		}
	}

	// THE UPGRADE: 56 → 57 on a DB that already holds rows.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("upgrade Up 56→57: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 57 {
		t.Fatalf("post-upgrade version = %d want 57", v)
	}

	repo := NewMessageRepo(db)

	// 1. Legacy rows survive and rehydrate as valid thread ROOTS (NULL refs).
	for _, id := range legacy {
		m, err := repo.FindByID(ctx, conversation.MessageID(id))
		if err != nil {
			t.Fatalf("legacy %s lost after upgrade: %v", id, err)
		}
		if !m.IsThreadRoot() {
			t.Errorf("legacy %s not a thread root after upgrade (parent=%q root=%q)",
				id, m.ParentMessageID(), m.RootMessageID())
		}
		if m.Content() != "legacy content" {
			t.Errorf("legacy %s content corrupted: %q", id, m.Content())
		}
	}

	// 2. FindThread treats a legacy message as a root (itself, no replies yet).
	thread, err := repo.FindThread(ctx, conv, "msg-legacy-1")
	if err != nil {
		t.Fatalf("FindThread on legacy root: %v", err)
	}
	if len(thread) != 1 || thread[0].ID() != "msg-legacy-1" {
		t.Fatalf("legacy root thread = %d msgs want 1 (the root itself)", len(thread))
	}

	// 3. A NEW reply threads off the legacy root via the real repo (depth-1).
	parent, _ := conversation.ResolveReplyPlacement(thread[0])
	reply, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: "msg-reply-1", ConversationID: conv, SenderIdentityID: "user:hayang",
		ContentKind: conversation.MessageContentText, Content: "reply to legacy",
		Direction: conversation.DirectionInbound,
		ParentMessageID: parent, RootMessageID: parent, PostedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("build reply: %v", err)
	}
	if err := repo.Append(ctx, reply); err != nil {
		t.Fatalf("append reply to legacy root: %v", err)
	}

	thread, _ = repo.FindThread(ctx, conv, "msg-legacy-1")
	if len(thread) != 2 || thread[0].ID() != "msg-legacy-1" || thread[1].ID() != "msg-reply-1" {
		t.Fatalf("after reply, thread = %v want [root, reply] ordered", threadIDs(thread))
	}
	counts, _ := repo.ThreadReplyCounts(ctx, conv)
	if counts["msg-legacy-1"] != 1 {
		t.Errorf("legacy root reply count = %d want 1", counts["msg-legacy-1"])
	}

	// 4. Down-migration off the upgraded-with-data DB stays reversible (rollback path).
	if err := mig.Down(ctx, 56); err != nil {
		t.Fatalf("rollback 57→56 with data present: %v", err)
	}
}

func threadIDs(ms []*conversation.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m.ID())
	}
	return out
}
