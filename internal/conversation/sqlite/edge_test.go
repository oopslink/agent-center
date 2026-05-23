package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TestNullHelpers exercises the small null-coalesce helpers (covered
// indirectly elsewhere; this hits the negative branch).
func TestNullHelpers(t *testing.T) {
	if nullString("") != nil {
		t.Fatal()
	}
	if nullString("x") != "x" {
		t.Fatal()
	}
	if nullTimePtr(nil) != nil {
		t.Fatal()
	}
	now := time.Now()
	if nullTimePtr(&now) == nil {
		t.Fatal()
	}
	if nullTimePtrFromTime(now, false) != nil {
		t.Fatal()
	}
	if nullTimePtrFromTime(now, true) == nil {
		t.Fatal()
	}
}

func TestParseNullTime(t *testing.T) {
	got, err := parseNullTime(sql.NullString{})
	if err != nil || got != nil {
		t.Fatalf("got (%v, %v)", got, err)
	}
	got, err = parseNullTime(sql.NullString{Valid: true, String: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || got == nil {
		t.Fatalf("got (%v, %v)", got, err)
	}
	if _, err := parseNullTime(sql.NullString{Valid: true, String: "not-a-time"}); err == nil {
		t.Fatal()
	}
}

func TestIsUniqueConstraint(t *testing.T) {
	if isUniqueConstraint(nil) {
		t.Fatal()
	}
	if !isUniqueConstraint(errors.New("UNIQUE constraint failed: x.y")) {
		t.Fatal()
	}
	if !isUniqueConstraint(errors.New("constraint failed: UNIQUE x")) {
		t.Fatal()
	}
}

func TestConversationRepo_SaveNil(t *testing.T) {
	r := setupDB(t)
	if err := r.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

func TestConversationRepo_Find_StatusFilter(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	st := conversation.ConversationActive
	got, _ := r.Find(context.Background(), conversation.ConversationFilter{Status: &st, Limit: 5})
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
}

func TestConversationRepo_Find_Cursor(t *testing.T) {
	r := setupDB(t)
	_ = r.Save(context.Background(), mkConv(t, "c-a", conversation.ConversationKindDM, ""))
	_ = r.Save(context.Background(), mkConv(t, "c-b", conversation.ConversationKindDM, ""))
	cursor := conversation.ConversationID("c-a")
	got, _ := r.Find(context.Background(), conversation.ConversationFilter{Cursor: &cursor})
	if len(got) != 1 || got[0].ID() != "c-b" {
		t.Fatalf("got %v", got)
	}
}

func TestConversationRepo_UpdateStatus_InvalidStatus(t *testing.T) {
	r := setupDB(t)
	err := r.UpdateStatus(context.Background(), "x", "weird", conversation.ConversationClosed, 1, "r", "m", time.Now())
	if !errors.Is(err, conversation.ErrConversationInvalidStatus) {
		t.Fatalf("got %v", err)
	}
}

// TestScanConversation_BadTime hits the time-parse error branches.
func TestScanConversation_BadTime(t *testing.T) {
	db, _ := persistence.Open(persistence.MemoryDSN())
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	// Insert a row directly with a bogus opened_at.
	_, err := db.Exec(`INSERT INTO conversations (id, kind, status, opened_at, participants, created_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"c-bad", "dm", "active", "not-a-time", "[]", "system",
		time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), 1)
	if err != nil {
		t.Fatal(err)
	}
	r := NewConversationRepo(db)
	if _, err := r.FindByID(context.Background(), "c-bad"); err == nil {
		t.Fatal("expected parse error")
	}
}
