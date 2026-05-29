package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TestSave_ExecError exercises the non-unique INSERT error branch via a
// TEMP TRIGGER.
func TestConversationRepo_Save_ExecError(t *testing.T) {
	r := setupDB(t)
	_, err := r.db.Exec(`CREATE TEMP TRIGGER ban_conv_insert BEFORE INSERT ON conversations BEGIN
		SELECT RAISE(ABORT, 'forbidden');
	END`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Exec(`DROP TRIGGER IF EXISTS ban_conv_insert`)
	err = r.Save(context.Background(), mkConv(t, "c-x", conversation.ConversationKindDM, ""))
	if err == nil {
		t.Fatal()
	}
	if errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatal("non-unique error must not map to AlreadyExists")
	}
}

func TestConversationRepo_FindByParent_QueryError(t *testing.T) {
	r := setupDB(t)
	r.db.Close()
	_, err := r.FindByParent(context.Background(), "x")
	if err == nil {
		t.Fatal()
	}
}

func TestConversationRepo_UpdateStatus_NonConflictError(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	_, err := r.db.Exec(`CREATE TEMP TRIGGER ban_conv_update BEFORE UPDATE ON conversations BEGIN
		SELECT RAISE(ABORT, 'forbidden');
	END`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Exec(`DROP TRIGGER IF EXISTS ban_conv_update`)
	err = r.UpdateStatus(context.Background(), "c-1",
		conversation.ConversationActive, conversation.ConversationClosed, 1,
		"r", "m", time.Now())
	if err == nil {
		t.Fatal()
	}
}

func TestConversationRepo_UpdateArchive_NonConflictError(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	_, err := r.db.Exec(`CREATE TEMP TRIGGER ban_conv_update2 BEFORE UPDATE ON conversations BEGIN
		SELECT RAISE(ABORT, 'nope');
	END`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Exec(`DROP TRIGGER IF EXISTS ban_conv_update2`)
	err = r.UpdateArchive(context.Background(), "c-1", 1, "user:h", time.Now())
	if err == nil {
		t.Fatal()
	}
}

func TestConversationRepo_UpdateParticipants_NonConflictError(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindProjectChannel, "n")
	_ = r.Save(context.Background(), c)
	_, err := r.db.Exec(`CREATE TEMP TRIGGER ban_conv_update3 BEFORE UPDATE ON conversations BEGIN
		SELECT RAISE(ABORT, 'nope');
	END`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Exec(`DROP TRIGGER IF EXISTS ban_conv_update3`)
	err = r.UpdateParticipants(context.Background(), "c-1", nil, 1, time.Now())
	if err == nil {
		t.Fatal()
	}
}

// TestScanConversation_BadCreatedAt + BadUpdatedAt covers the time-parse
// branches for created_at / updated_at.
func TestScanConversation_BadCreatedAt(t *testing.T) {
	r := setupDB(t)
	_, err := r.db.Exec(`INSERT INTO conversations (id, kind, status, opened_at, participants, created_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"c-bad-ct", "dm", "active",
		time.Now().UTC().Format(time.RFC3339Nano), "[]", "system",
		"not-a-time", time.Now().UTC().Format(time.RFC3339Nano), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByID(context.Background(), "c-bad-ct"); err == nil {
		t.Fatal()
	}
}

func TestScanConversation_BadUpdatedAt(t *testing.T) {
	r := setupDB(t)
	_, err := r.db.Exec(`INSERT INTO conversations (id, kind, status, opened_at, participants, created_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"c-bad-ut", "dm", "active",
		time.Now().UTC().Format(time.RFC3339Nano), "[]", "system",
		time.Now().UTC().Format(time.RFC3339Nano), "not-a-time", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByID(context.Background(), "c-bad-ut"); err == nil {
		t.Fatal()
	}
}

func TestScanConversation_BadClosedAt(t *testing.T) {
	r := setupDB(t)
	_, err := r.db.Exec(`INSERT INTO conversations (id, kind, status, opened_at, closed_at, participants, created_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"c-bad-cl", "dm", "closed",
		time.Now().UTC().Format(time.RFC3339Nano), "not-a-time",
		"[]", "system",
		time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByID(context.Background(), "c-bad-cl"); err == nil {
		t.Fatal()
	}
}

func TestScanConversation_BadParticipantsJSON(t *testing.T) {
	r := setupDB(t)
	_, err := r.db.Exec(`INSERT INTO conversations (id, kind, status, opened_at, participants, created_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"c-bad-p", "dm", "active",
		time.Now().UTC().Format(time.RFC3339Nano), "{not json", "system",
		time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByID(context.Background(), "c-bad-p"); err == nil {
		t.Fatal()
	}
}

func TestMessageRepo_FindByConversationID_Since(t *testing.T) {
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
	since := now.Add(time.Second + 500*time.Millisecond) // expects to skip msg 0 + 1
	got, _ := msgR.FindByConversationID(context.Background(), "c-1", conversation.MessageFilter{Since: &since})
	if len(got) != 1 {
		t.Fatalf("expected 1 msg, got %d", len(got))
	}
}

func TestMessageRepo_FindByConversationID_Limit(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	for i := 0; i < 5; i++ {
		m, _ := conversation.NewMessage(conversation.NewMessageInput{
			ID: conversation.MessageID(string(rune('a' + i))), ConversationID: "c-1",
			SenderIdentityID: "user:h", ContentKind: conversation.MessageContentText,
			Direction: conversation.DirectionInbound, PostedAt: time.Now().Add(time.Duration(i) * time.Second),
		})
		_ = msgR.Append(context.Background(), m)
	}
	got, _ := msgR.FindByConversationID(context.Background(), "c-1", conversation.MessageFilter{Limit: 2})
	if len(got) != 2 {
		t.Fatalf("expected 2 msgs, got %d", len(got))
	}
}

func TestMessageRepo_QueryError(t *testing.T) {
	_, msgR := setupMsgDB(t)
	msgR.db.Close()
	_, err := msgR.FindByConversationID(context.Background(), "x", conversation.MessageFilter{})
	if err == nil {
		t.Fatal()
	}
}

func TestMessageRepo_FindRecent_DefaultsToZero(t *testing.T) {
	convR, msgR := setupMsgDB(t)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	got, err := msgR.FindRecent(context.Background(), "c-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal()
	}
}

func TestScanMessage_BadCreatedAt(t *testing.T) {
	_, msgR := setupMsgDB(t)
	convR := NewConversationRepo(msgR.db)
	_ = convR.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindDM, ""))
	_, err := msgR.db.Exec(`INSERT INTO messages (id, conversation_id, sender_identity_id, content_kind, content, direction, posted_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-bad", "c-1", "user:h", "text", "x", "inbound",
		time.Now().UTC().Format(time.RFC3339Nano), "not-a-time")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := msgR.FindByID(context.Background(), "m-bad"); err == nil {
		t.Fatal()
	}
}

func TestReferenceRepo_FindBySource_QueryError(t *testing.T) {
	r := setupRefRepo(t)
	r.db.Close()
	if _, err := r.FindBySourceMsgID(context.Background(), "x"); err == nil {
		t.Fatal()
	}
}

func TestReferenceRepo_FindByChild_QueryError(t *testing.T) {
	r := setupRefRepo(t)
	r.db.Close()
	if _, err := r.FindByChildConvID(context.Background(), "x"); err == nil {
		t.Fatal()
	}
}

func TestScanRefs_BadCreatedAt(t *testing.T) {
	r := setupRefRepo(t)
	_, err := r.db.Exec(`INSERT INTO conversation_message_reference (id, child_conversation_id, source_conversation_id, source_message_id, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"r-bad", "child-1", "src-1", "msg-1", "user:h", "not-a-time")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByChildConvID(context.Background(), "child-1"); err == nil {
		t.Fatal()
	}
}

// TestReferenceRepo_Save_ExecError exercises the non-unique INSERT error
// path via TEMP TRIGGER.
func TestReferenceRepo_Save_ExecError(t *testing.T) {
	r := setupRefRepo(t)
	_, err := r.db.Exec(`CREATE TEMP TRIGGER ban_ref_insert BEFORE INSERT ON conversation_message_reference BEGIN
		SELECT RAISE(ABORT, 'forbidden');
	END`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Exec(`DROP TRIGGER IF EXISTS ban_ref_insert`)
	err = r.Save(context.Background(), []*conversation.ConversationMessageReference{
		mkRef("r-1", "child-1", "src-1", "msg-1"),
	})
	if err == nil || errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("expected generic error, got %v", err)
	}
}

func TestCasConflict_QueryError(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	// Close DB to force the inner SELECT to fail when CAS finds no rows.
	r.db.Close()
	// UpdateStatus will hit casConflict's inner SELECT path which will
	// fail because the DB is closed.
	err := r.UpdateStatus(context.Background(), "c-1",
		conversation.ConversationActive, conversation.ConversationClosed, 99,
		"r", "m", time.Now())
	if err == nil {
		t.Fatal()
	}
	if errors.Is(err, conversation.ErrConversationNotFound) || errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("expected raw DB error, got %v", err)
	}
}

// Persist a no-op suppression for currently unused vars.
var _ = persistence.MemoryDSN
