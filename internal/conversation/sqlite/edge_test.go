package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

func TestConversationRepo_FindByID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	_ = db.Close()
	_, err := repo.FindByID(context.Background(), "C")
	if err == nil {
		t.Fatal()
	}
}

func TestConversationRepo_Find_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	_ = db.Close()
	_, err := repo.Find(context.Background(), conversation.ConversationFilter{})
	if err == nil {
		t.Fatal()
	}
}

func TestConversationRepo_FindByChannelAndThreadKey_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	_ = db.Close()
	_, err := repo.FindByChannelAndThreadKey(context.Background(), "f", "t")
	if err == nil {
		t.Fatal()
	}
}

func TestMessageRepo_FindByID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewMessageRepo(db)
	_ = db.Close()
	_, err := repo.FindByID(context.Background(), "M")
	if err == nil {
		t.Fatal()
	}
}

func TestMessageRepo_FindByConversationID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewMessageRepo(db)
	_ = db.Close()
	_, err := repo.FindByConversationID(context.Background(), "C", conversation.MessageFilter{})
	if err == nil {
		t.Fatal()
	}
}

func TestMessageRepo_FindByVendorMsgRef_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewMessageRepo(db)
	_ = db.Close()
	_, err := repo.FindByVendorMsgRef(context.Background(), "v")
	if err == nil {
		t.Fatal()
	}
}

func TestParseNullTime_Conv_Empty(t *testing.T) {
	out, err := parseNullTime(sql.NullString{Valid: false})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Fatal()
	}
}

func TestParseNullTime_Conv_BadValue(t *testing.T) {
	_, err := parseNullTime(sql.NullString{String: "not-a-time", Valid: true})
	if err == nil {
		t.Fatal()
	}
}

func TestIsUniqueConstraint_Variants(t *testing.T) {
	if isUniqueConstraint(nil) {
		t.Fatal()
	}
	if !isUniqueConstraint(errors.New("UNIQUE constraint failed")) {
		t.Fatal()
	}
	if !isUniqueConstraint(errors.New("constraint failed: UNIQUE")) {
		t.Fatal()
	}
	if isUniqueConstraint(errors.New("unrelated")) {
		t.Fatal()
	}
}

func TestNullTimePtrFromTime_Valid(t *testing.T) {
	// closed conversation case (use=true) returns formatted string.
	got := nullTimePtrFromTime(testNow(), true)
	if got == nil {
		t.Fatal()
	}
}

func TestNullTimePtrFromTime_Nil(t *testing.T) {
	got := nullTimePtrFromTime(testNow(), false)
	if got != nil {
		t.Fatal()
	}
}

func TestNullTimePtr_NilTime(t *testing.T) {
	if nullTimePtr(nil) != nil {
		t.Fatal()
	}
}

func testNow() (t time.Time) {
	return time.Now()
}
