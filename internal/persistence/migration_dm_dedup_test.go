package persistence

import (
	"context"
	"testing"
)

// TestMigration0066_DMDedup_MergesDuplicates is the T288 cleanup acceptance: on a
// NON-empty DB that already holds duplicate DMs (the bug), migration 0066 must
// backfill dm_key, MERGE the duplicates into the earliest (canonical) DM —
// repointing messages + read-state — delete the redundant DMs, and enforce the
// partial unique index so no new duplicate can be inserted.
func TestMigration0066_DMDedup_MergesDuplicates(t *testing.T) {
	ctx := context.Background()
	db, err := Open(MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mig := NewMigrator(db)
	// Bring the DB to v65 — the schema BEFORE 0066 (no dm_key column), so we can seed
	// the pre-existing duplicate DMs exactly as the buggy code produced them.
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mig.Down(ctx, 65); err != nil {
		t.Fatalf("down to 65: %v", err)
	}

	// {user:h, agent:pd} — order-independent key "agent:pd\x1fuser:h".
	parts := `[{"identity_id":"user:h","role":"owner","joined_at":"t","joined_by":"user:h"},` +
		`{"identity_id":"agent:pd","role":"member","joined_at":"t","joined_by":"user:h"}]`
	seedDM := func(id, org, partsJSON, createdAt string) {
		if _, err := db.ExecContext(ctx, `INSERT INTO conversations
			(id, kind, participants, created_by, status, opened_at, created_at, updated_at, version, organization_id)
			VALUES (?, 'dm', ?, 'user:h', 'active', ?, ?, ?, 1, ?)`,
			id, partsJSON, createdAt, createdAt, createdAt, org); err != nil {
			t.Fatalf("seed dm %s: %v", id, err)
		}
	}
	seedMsg := func(id, convID string) {
		if _, err := db.ExecContext(ctx, `INSERT INTO messages
			(id, conversation_id, sender_identity_id, content_kind, content, direction, posted_at, created_at)
			VALUES (?, ?, 'user:h', 'text', 'hi', 'inbound', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			id, convID); err != nil {
			t.Fatalf("seed msg %s: %v", id, err)
		}
	}
	seedRead := func(conv string) {
		if _, err := db.ExecContext(ctx, `INSERT INTO user_conversation_read_state
			(user_id, conversation_id, last_seen_message_id, updated_at, version)
			VALUES ('user:h', ?, 'x', '2026-01-01T00:00:00Z', 1)`, conv); err != nil {
			t.Fatalf("seed read %s: %v", conv, err)
		}
	}

	// Two duplicate DMs for the same pair (canonical = earliest created_at), each with
	// a message + a read-state row (the read rows collide on the canonical PK on merge).
	seedDM("dm-old", "org1", parts, "2026-01-01T00:00:00Z")
	seedDM("dm-new", "org1", parts, "2026-02-01T00:00:00Z")
	seedMsg("m-old", "dm-old")
	seedMsg("m-new", "dm-new")
	seedRead("dm-old")
	seedRead("dm-new")
	// A non-duplicate DM for a different pair — must be left untouched.
	soloParts := `[{"identity_id":"user:h","role":"owner","joined_at":"t","joined_by":"user:h"},` +
		`{"identity_id":"agent:x","role":"member","joined_at":"t","joined_by":"user:h"}]`
	seedDM("dm-solo", "org1", soloParts, "2026-01-03T00:00:00Z")

	// Apply 0066.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("up to 66: %v", err)
	}

	scalar := func(q string, args ...any) string {
		var s any
		if err := db.QueryRowContext(ctx, q, args...).Scan(&s); err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		if s == nil {
			return ""
		}
		switch v := s.(type) {
		case string:
			return v
		case []byte:
			return string(v)
		default:
			return ""
		}
	}
	count := func(q string, args ...any) int {
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", q, err)
		}
		return n
	}

	// The duplicate is gone; the earliest survives; the unrelated DM survives.
	if n := count(`SELECT COUNT(*) FROM conversations WHERE id='dm-new'`); n != 0 {
		t.Fatalf("duplicate dm-new should be deleted, found %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM conversations WHERE id='dm-old'`); n != 1 {
		t.Fatalf("canonical dm-old should survive, found %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM conversations WHERE id='dm-solo'`); n != 1 {
		t.Fatalf("unrelated dm-solo must be untouched, found %d", n)
	}

	// dm_key backfilled to the canonical (sorted, US-joined) key.
	if got := scalar(`SELECT dm_key FROM conversations WHERE id='dm-old'`); got != "agent:pd\x1fuser:h" {
		t.Fatalf("dm-old dm_key = %q, want canonical pair key", got)
	}

	// Both messages now live on the canonical DM (no message lost).
	if got := scalar(`SELECT conversation_id FROM messages WHERE id='m-new'`); got != "dm-old" {
		t.Fatalf("m-new should be repointed to dm-old, got %q", got)
	}
	if got := scalar(`SELECT conversation_id FROM messages WHERE id='m-old'`); got != "dm-old" {
		t.Fatalf("m-old should stay on dm-old, got %q", got)
	}

	// read-state collapsed onto the canonical (no orphan rows pointing at dm-new).
	if n := count(`SELECT COUNT(*) FROM user_conversation_read_state WHERE conversation_id='dm-new'`); n != 0 {
		t.Fatalf("read-state on deleted dm-new should be gone, found %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM user_conversation_read_state WHERE conversation_id='dm-old'`); n != 1 {
		t.Fatalf("canonical read-state row expected, found %d", n)
	}

	// The unique index now BLOCKS a new duplicate (the regression guard).
	_, err = db.ExecContext(ctx, `INSERT INTO conversations
		(id, kind, participants, dm_key, created_by, status, opened_at, created_at, updated_at, version, organization_id)
		VALUES ('dm-dupe', 'dm', ?, 'agent:pd`+"\x1f"+`user:h', 'user:h', 'active', 't', 't', 't', 1, 'org1')`, parts)
	if err == nil {
		t.Fatal("inserting a second non-archived DM with the same (org, dm_key) must violate the unique index")
	}
}
