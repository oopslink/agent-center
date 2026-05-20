package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

type suite struct {
	db        *sql.DB
	conv      *convsqlite.ConversationRepo
	msg       *convsqlite.MessageRepo
	event     *obsqlite.EventRepo
	sink      *observability.EventSink
	writer    *MessageWriter
	clock     *clock.FakeClock
}

func setupSuite(t *testing.T) *suite {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, fc)
	cr := convsqlite.NewConversationRepo(db)
	mr := convsqlite.NewMessageRepo(db)
	return &suite{
		db: db, conv: cr, msg: mr, event: er, sink: sink, clock: fc,
		writer: NewMessageWriter(db, cr, mr, sink, gen, fc),
	}
}

func TestOpen_Happy(t *testing.T) {
	s := setupSuite(t)
	res, err := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Title: "T", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID == "" || res.EventID == "" {
		t.Fatal()
	}
	got, _ := s.conv.FindByID(context.Background(), res.ConversationID)
	if got.Kind() != conversation.ConversationKindDM {
		t.Fatal()
	}
	events, _ := s.event.Find(context.Background(), observability.EventQueryFilter{})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type() != "conversation.opened" {
		t.Fatalf("type: %s", events[0].Type())
	}
}

func TestOpen_RejectsTaskKind(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindTask, Actor: "user:hayang",
	})
	if !errors.Is(err, conversation.ErrConversationInvalidKind) {
		t.Fatalf("got %v", err)
	}
}

func TestOpen_RejectsIssueKind(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindIssue, Actor: "user:hayang",
	})
	if !errors.Is(err, conversation.ErrConversationInvalidKind) {
		t.Fatalf("got %v", err)
	}
}

func TestOpen_RejectsBogusKind(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: "bogus", Actor: "user:hayang",
	})
	if !errors.Is(err, conversation.ErrConversationInvalidKind) {
		t.Fatalf("got %v", err)
	}
}

func TestOpen_RejectsBadActor(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestAddMessage_Happy(t *testing.T) {
	s := setupSuite(t)
	openRes, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:hayang",
	})
	res, err := s.writer.AddMessage(context.Background(), AddMessageCommand{
		ConversationID:   openRes.ConversationID,
		SenderIdentityID: "user:hayang",
		ContentKind:      conversation.MessageContentText,
		Content:          "hi",
		Direction:        conversation.DirectionInbound,
		Actor:            "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageID == "" || res.EventID == "" {
		t.Fatal()
	}
	events, _ := s.event.Find(context.Background(), observability.EventQueryFilter{})
	if len(events) != 2 {
		t.Fatalf("expected 2 events (opened + message_added), got %d", len(events))
	}
	found := false
	for _, e := range events {
		if e.Type() == "conversation.message_added" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing message_added event")
	}
}

func TestAddMessage_ToClosedConversation(t *testing.T) {
	s := setupSuite(t)
	openRes, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:x",
	})
	_, _ = s.writer.Close(context.Background(), CloseCommand{
		ConversationID: openRes.ConversationID, Version: 1,
		Reason: "done", Message: "x", Actor: "user:x",
	})
	_, err := s.writer.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: openRes.ConversationID, SenderIdentityID: "user:x",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		Actor: "user:x",
	})
	if !errors.Is(err, conversation.ErrConversationClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestAddMessage_NotFound(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.AddMessage(context.Background(), AddMessageCommand{
		ConversationID:   "C-NEVER",
		SenderIdentityID: "user:x",
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Actor:            "user:x",
	})
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAddMessage_VendorRefDup(t *testing.T) {
	s := setupSuite(t)
	openRes, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:x",
	})
	cmd := AddMessageCommand{
		ConversationID:   openRes.ConversationID,
		SenderIdentityID: "user:x",
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		VendorMsgRef:     "v-1",
		Actor:            "user:x",
	}
	_, err := s.writer.AddMessage(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.writer.AddMessage(context.Background(), cmd)
	if !errors.Is(err, conversation.ErrMessageDuplicate) {
		t.Fatalf("got %v", err)
	}
}

func TestAddMessage_BadActor(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: "C", SenderIdentityID: "user:x",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		Actor: "foo:bar",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestAddMessage_BadSender(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: "C", SenderIdentityID: "bogus:x",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		Actor: "user:x",
	})
	if !errors.Is(err, conversation.ErrMessageInvalidSender) {
		t.Fatalf("got %v", err)
	}
}

func TestClose_Happy(t *testing.T) {
	s := setupSuite(t)
	open, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:x",
	})
	_, err := s.writer.Close(context.Background(), CloseCommand{
		ConversationID: open.ConversationID, Version: 1,
		Reason: "done", Message: "ok", Actor: "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.conv.FindByID(context.Background(), open.ConversationID)
	if got.Status() != conversation.ConversationClosed {
		t.Fatal()
	}
}

func TestClose_VersionConflict(t *testing.T) {
	s := setupSuite(t)
	open, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:x",
	})
	_, err := s.writer.Close(context.Background(), CloseCommand{
		ConversationID: open.ConversationID, Version: 99,
		Reason: "x", Message: "y", Actor: "user:x",
	})
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestClose_RequiresReasonMessage(t *testing.T) {
	s := setupSuite(t)
	open, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:x",
	})
	_, err := s.writer.Close(context.Background(), CloseCommand{
		ConversationID: open.ConversationID, Version: 1, Actor: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestClose_BadActor(t *testing.T) {
	s := setupSuite(t)
	_, err := s.writer.Close(context.Background(), CloseCommand{
		ConversationID: "C", Version: 1,
		Reason: "x", Message: "y", Actor: "foo:bar",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestAddMessage_RollbackOnSinkFailure(t *testing.T) {
	s := setupSuite(t)
	openRes, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:x",
	})
	// Replace sink with failing repo
	failing := &failingRepo{}
	sink := observability.NewEventSink(failing, s.event, idgen.NewGenerator(s.clock), s.clock)
	writer := NewMessageWriter(s.db, s.conv, s.msg, sink, idgen.NewGenerator(s.clock), s.clock)
	_, err := writer.AddMessage(context.Background(), AddMessageCommand{
		ConversationID:   openRes.ConversationID,
		SenderIdentityID: "user:x",
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Actor:            "user:x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// Message must not exist.
	msgs, _ := s.msg.FindByConversationID(context.Background(), openRes.ConversationID, conversation.MessageFilter{})
	if len(msgs) != 0 {
		t.Fatalf("expected rollback, got %d messages", len(msgs))
	}
}

type failingRepo struct{}

func (failingRepo) Append(ctx context.Context, e *observability.Event) error {
	return errors.New("simulated failure")
}
func (failingRepo) FindByID(ctx context.Context, _ observability.EventID) (*observability.Event, error) {
	return nil, errors.New("x")
}
func (failingRepo) Find(ctx context.Context, _ observability.EventQueryFilter) ([]*observability.Event, error) {
	return nil, nil
}

func TestAddMessage_EmitPayloadShape(t *testing.T) {
	s := setupSuite(t)
	openRes, _ := s.writer.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, Actor: "user:x",
	})
	addRes, _ := s.writer.AddMessage(context.Background(), AddMessageCommand{
		ConversationID:   openRes.ConversationID,
		SenderIdentityID: "user:x",
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Actor:            "user:x",
	})
	got, _ := s.event.FindByID(context.Background(), addRes.EventID)
	if got.Type() != "conversation.message_added" {
		t.Fatal()
	}
	payload := got.Payload()
	if payload["message_id"] != string(addRes.MessageID) {
		t.Fatalf("payload missing message_id: %v", payload)
	}
	if !strings.Contains(payload["direction"].(string), "inbound") {
		t.Fatal()
	}
}
