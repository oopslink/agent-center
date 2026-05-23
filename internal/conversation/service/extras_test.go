package service

import (
	"context"
	"database/sql"
	"errors"
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

func setupRaw(t *testing.T) (*sql.DB, *MessageWriter) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	conv := convsqlite.NewConversationRepo(db)
	msg := convsqlite.NewMessageRepo(db)
	return db, NewMessageWriter(db, conv, msg, sink, gen, fc)
}

func TestNewMessageWriter_NilClock(t *testing.T) {
	db, _ := persistence.Open(persistence.MemoryDSN())
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	gen := idgen.NewGenerator(clock.SystemClock{})
	sink := observability.NewEventSink(er, er, gen, clock.SystemClock{})
	w := NewMessageWriter(db, convsqlite.NewConversationRepo(db), convsqlite.NewMessageRepo(db), sink, gen, nil)
	if w == nil {
		t.Fatal()
	}
}

func TestOpenConversation_ChannelNameRequired(t *testing.T) {
	_, w := setupRaw(t)
	_, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindChannel,
		// Name missing
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestArchive_VersionConflict(t *testing.T) {
	_, w := setupRaw(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM,
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	_, err := w.Archive(context.Background(), ArchiveCommand{
		ConversationID: res.ConversationID, Version: 99,
		ArchivedBy: conversation.IdentityRef("user:hayang"),
		Actor:      observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestClose_NotFound(t *testing.T) {
	_, w := setupRaw(t)
	_, err := w.Close(context.Background(), CloseCommand{
		ConversationID: "nope", Version: 1,
		Reason: "r", Message: "m", Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}
