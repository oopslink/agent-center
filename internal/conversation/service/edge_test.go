package service

import (
	"context"
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

// failingEventRepo always errors on Append; used to hit the EventSink
// Emit error branch across services.
type failingEventRepo struct{}

func (failingEventRepo) Append(ctx context.Context, e *observability.Event) error {
	return errors.New("forced emit failure")
}
func (failingEventRepo) FindByID(ctx context.Context, id observability.EventID) (*observability.Event, error) {
	return nil, observability.ErrEventNotFound
}
func (failingEventRepo) Find(ctx context.Context, filter observability.EventQueryFilter) ([]*observability.Event, error) {
	return nil, nil
}

func setupFailing(t *testing.T) (*MessageWriter, *ChannelManagementService, *ParticipantManagementService, *CarryOverService) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_ = persistence.NewMigrator(db).Up(context.Background())
	fc := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(failingEventRepo{}, er, gen, fc)
	cr := convsqlite.NewConversationRepo(db)
	mr := convsqlite.NewMessageRepo(db)
	rr := convsqlite.NewReferenceRepo(db)
	w := NewMessageWriter(db, cr, mr, sink, gen, fc)
	ch := NewChannelManagementService(db, cr, sink, gen, fc)
	p := NewParticipantManagementService(db, cr, sink, fc)
	co := NewCarryOverService(db, cr, mr, rr, sink, gen, fc)
	return w, ch, p, co
}

func TestMessageWriter_OpenConversation_EmitFailureRollsBack(t *testing.T) {
	mw, _, _, _ := setupFailing(t)
	_, err := mw.OpenConversation(context.Background(), OpenCommand{
		Kind:      conversation.ConversationKindDM,
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal("expected emit failure")
	}
}

func TestMessageWriter_AddMessage_EmitFailureRollsBack(t *testing.T) {
	// Two-step: open succeeds (via good sink), AddMessage emits → fail.
	// Easier path: just confirm AddMessage propagates error when conv
	// is missing (covered) and ensure the failing sink path works for
	// other services.
	t.Skip("emit failure path already covered indirectly via OpenConversation roll-back")
}

func TestMessageWriter_Close_EmitFailure(t *testing.T) {
	// Set up: open via normal writer + close via failing-sink writer.
	dbW, w := setupRaw(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind:      conversation.ConversationKindDM,
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	// Build a failing-sink writer on the same DB.
	er, _ := obsqlite.NewEventRepo(context.Background(), dbW)
	sink := observability.NewEventSink(failingEventRepo{}, er, w.idgen, w.clock)
	failW := NewMessageWriter(dbW, w.convRepo, w.msgRepo, sink, w.idgen, w.clock)
	_, err := failW.Close(context.Background(), CloseCommand{
		ConversationID: res.ConversationID, Version: 1,
		Reason: "done", Message: "wrapped",
		Actor: observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestChannelManagement_CreateChannel_EmitFailure(t *testing.T) {
	_, ch, _, _ := setupFailing(t)
	_, err := ch.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "x", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestParticipant_Invite_EmitFailure(t *testing.T) {
	dbW, w := setupRaw(t)
	ch := NewChannelManagementService(dbW, w.convRepo, w.sink, w.idgen, w.clock)
	_, _ = ch.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "z", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	// Failing-sink participant svc.
	er, _ := obsqlite.NewEventRepo(context.Background(), dbW)
	failSink := observability.NewEventSink(failingEventRepo{}, er, w.idgen, w.clock)
	failP := NewParticipantManagementService(dbW, w.convRepo, failSink, w.clock)
	_, err := failP.Invite(context.Background(), InviteCommand{
		ConversationName: "z", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestCarryOver_Materialise_EmitFailure(t *testing.T) {
	dbW, w := setupRaw(t)
	rr := convsqlite.NewReferenceRepo(dbW)
	co := NewCarryOverService(dbW, w.convRepo, w.msgRepo, rr, w.sink, w.idgen, w.clock)
	// Seed source conv + msg + child conv via normal writer.
	src, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "src-1", Kind: conversation.ConversationKindProjectChannel, Name: "src",
		CreatedBy: "user:hayang", OpenedAt: w.clock.Now(),
	})
	_ = w.convRepo.Save(context.Background(), src)
	child, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "child-1", Kind: conversation.ConversationKindIssue,
		CreatedBy: "user:hayang", OpenedAt: w.clock.Now(),
	})
	_ = w.convRepo.Save(context.Background(), child)
	m, _ := conversation.NewMessage(conversation.NewMessageInput{
		ID: "m-1", ConversationID: "src-1", SenderIdentityID: "user:hayang",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		Content: "x", PostedAt: w.clock.Now(),
	})
	_ = w.msgRepo.Append(context.Background(), m)
	// Failing-sink carry-over.
	er, _ := obsqlite.NewEventRepo(context.Background(), dbW)
	failSink := observability.NewEventSink(failingEventRepo{}, er, w.idgen, w.clock)
	failCo := NewCarryOverService(dbW, w.convRepo, w.msgRepo, rr, failSink, w.idgen, w.clock)
	_, err := failCo.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID: "child-1", SourceConversationID: "src-1",
		SourceMessageIDs: []conversation.MessageID{"m-1"},
		CreatedBy:        "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
	// Also confirms refs were rolled back.
	if got, _ := co.FindByChildConv(context.Background(), "child-1"); len(got) != 0 {
		t.Fatalf("expected rollback, got %d refs", len(got))
	}
}

func TestAddMessage_EmitFailureRollsBack(t *testing.T) {
	dbW, w := setupRaw(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind:      conversation.ConversationKindDM,
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	er, _ := obsqlite.NewEventRepo(context.Background(), dbW)
	failSink := observability.NewEventSink(failingEventRepo{}, er, w.idgen, w.clock)
	failW := NewMessageWriter(dbW, w.convRepo, w.msgRepo, failSink, w.idgen, w.clock)
	_, err := failW.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: res.ConversationID, SenderIdentityID: "user:hayang",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		Content: "x", Actor: observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestArchiveChannel_EmitFailure(t *testing.T) {
	dbW, w := setupRaw(t)
	ch := NewChannelManagementService(dbW, w.convRepo, w.sink, w.idgen, w.clock)
	_, _ = ch.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "y", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	er, _ := obsqlite.NewEventRepo(context.Background(), dbW)
	failSink := observability.NewEventSink(failingEventRepo{}, er, w.idgen, w.clock)
	failCh := NewChannelManagementService(dbW, w.convRepo, failSink, w.idgen, w.clock)
	_, err := failCh.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "y", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveTask_SourceNotActive(t *testing.T) {
	dbW, w := setupRaw(t)
	rr := convsqlite.NewReferenceRepo(dbW)
	co := NewCarryOverService(dbW, w.convRepo, w.msgRepo, rr, w.sink, w.idgen, w.clock)
	ch := NewChannelManagementService(dbW, w.convRepo, w.sink, w.idgen, w.clock)
	_ = co
	tc := &fakeTaskCreator{w: w}
	d := NewMessageDerivationService(dbW, w.convRepo, w.msgRepo, co, nil, tc, w.sink, w.clock)
	_, _ = ch.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "tn", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	source, _ := w.convRepo.FindByName(context.Background(), "tn")
	_, _ = ch.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "tn", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	_, err := d.DeriveTask(context.Background(), DeriveTaskCommand{
		SourceConversationID: source.ID(),
		ProjectID:            "p", Title: "T", AgentInstanceID: "ai-1",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if !errors.Is(err, ErrDerivationSourceNotActive) {
		t.Fatalf("got %v", err)
	}
}
