package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/observability"
)

// fakeIssueOpener and fakeTaskCreator mock the cross-BC ports. They each
// receive the call, create a stub child conversation in the same SQLite,
// and return its id so carry-over Materialise can be exercised.
type fakeIssueOpener struct {
	w         *MessageWriter
	convKind  conversation.ConversationKind
	lastInput OpenFromConversationInput
}

func (f *fakeIssueOpener) OpenFromConversation(ctx context.Context, in OpenFromConversationInput) (OpenFromConversationResult, error) {
	f.lastInput = in
	id := conversation.ConversationID(f.w.idgen.NewULID())
	conv, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: id, Kind: f.convKind,
		Name: in.Title, CreatedBy: in.OpenedBy, OpenedAt: f.w.clock.Now(),
	})
	if err := f.w.convRepo.Save(ctx, conv); err != nil {
		return OpenFromConversationResult{}, err
	}
	return OpenFromConversationResult{
		IssueID: "ISSUE-" + in.Title, ConversationID: id, EventID: "EV-ISSUE",
	}, nil
}

type fakeTaskCreator struct {
	w         *MessageWriter
	lastInput CreateFromConversationInput
}

func (f *fakeTaskCreator) CreateFromConversation(ctx context.Context, in CreateFromConversationInput) (CreateFromConversationResult, error) {
	f.lastInput = in
	id := conversation.ConversationID(f.w.idgen.NewULID())
	conv, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: id, Kind: conversation.ConversationKindTask,
		Name: in.Title, CreatedBy: in.CreatedBy, OpenedAt: f.w.clock.Now(),
	})
	if err := f.w.convRepo.Save(ctx, conv); err != nil {
		return CreateFromConversationResult{}, err
	}
	return CreateFromConversationResult{
		TaskID: "TASK-" + in.Title, ConversationID: id, EventID: "EV-TASK",
	}, nil
}

func setupDerivation(t *testing.T) (*MessageWriter, *MessageDerivationService, *ChannelManagementService, *ParticipantManagementService, *fakeIssueOpener, *fakeTaskCreator) {
	t.Helper()
	db, w := setupRaw(t)
	refRepo := convsqlite.NewReferenceRepo(db)
	co := NewCarryOverService(db, w.convRepo, w.msgRepo, refRepo, w.sink, w.idgen, w.clock)
	chSvc := NewChannelManagementService(db, w.convRepo, w.sink, w.idgen, w.clock)
	pSvc := NewParticipantManagementService(db, w.convRepo, w.sink, w.clock)
	io := &fakeIssueOpener{w: w, convKind: conversation.ConversationKindIssue}
	tc := &fakeTaskCreator{w: w}
	d := NewMessageDerivationService(db, w.convRepo, w.msgRepo, co, io, tc, w.sink, w.clock)
	return w, d, chSvc, pSvc, io, tc
}

func seedChannelWithMsgs(t *testing.T, w *MessageWriter, ch *ChannelManagementService, name string, msgCount int) (conversation.ConversationID, []conversation.MessageID) {
	t.Helper()
	res, _ := ch.CreateChannel(context.Background(), CreateChannelCommand{
		Name: name, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	var ids []conversation.MessageID
	for i := 0; i < msgCount; i++ {
		m, _ := conversation.NewMessage(conversation.NewMessageInput{
			ID: conversation.MessageID(w.idgen.NewULID()), ConversationID: res.ConversationID,
			SenderIdentityID: "user:hayang", ContentKind: conversation.MessageContentText,
			Content: "x", Direction: conversation.DirectionInbound, PostedAt: w.clock.Now(),
		})
		_ = w.msgRepo.Append(context.Background(), m)
		ids = append(ids, m.ID())
	}
	return res.ConversationID, ids
}

func TestDeriveIssue_Happy(t *testing.T) {
	w, d, ch, _, io, _ := setupDerivation(t)
	cid, msgIDs := seedChannelWithMsgs(t, w, ch, "ch", 2)
	res, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: cid,
		SourceMessageIDs:     msgIDs,
		ProjectID:            "p-1", Title: "X",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IssueID == "" || res.ChildConversationID == "" {
		t.Fatalf("got %+v", res)
	}
	if res.ReferenceCount != 2 {
		t.Fatalf("ref count: %d", res.ReferenceCount)
	}
	if io.lastInput.ProjectID != "p-1" {
		t.Fatal()
	}
}

func TestDeriveIssue_SourceNotActive(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, msgIDs := seedChannelWithMsgs(t, w, ch, "x", 1)
	_, _ = ch.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "x", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: cid, SourceMessageIDs: msgIDs,
		ProjectID: "p-1", Title: "Y",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if !errors.Is(err, ErrDerivationSourceNotActive) {
		t.Fatalf("got %v", err)
	}
}

func TestDeriveIssue_NotParticipant(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, msgIDs := seedChannelWithMsgs(t, w, ch, "y", 1)
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: cid, SourceMessageIDs: msgIDs,
		ProjectID: "p-1", Title: "Z",
		CreatedBy: "user:stranger", Actor: "user:stranger",
	})
	if !errors.Is(err, ErrDerivationCallerNotParticipant) {
		t.Fatalf("got %v", err)
	}
}

func TestDeriveIssue_MsgInWrongConv(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, _ := seedChannelWithMsgs(t, w, ch, "a", 1)
	_, otherMsgs := seedChannelWithMsgs(t, w, ch, "b", 1)
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: cid, SourceMessageIDs: otherMsgs,
		ProjectID: "p-1", Title: "C",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if !errors.Is(err, ErrCarryOverSourceMsgNotInConv) {
		t.Fatalf("got %v", err)
	}
}

func TestDeriveIssue_MissingProject(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, _ := seedChannelWithMsgs(t, w, ch, "n", 0)
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: cid, Title: "Y",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveIssue_MissingTitle(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, _ := seedChannelWithMsgs(t, w, ch, "n2", 0)
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: cid, ProjectID: "p-1",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveIssue_OpenerNotWired(t *testing.T) {
	_, d, _, _, _, _ := setupDerivation(t)
	d.issueOpener = nil
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		CreatedBy: "user:h", Actor: "user:h",
	})
	if !errors.Is(err, ErrDerivationOpenerNotWired) {
		t.Fatalf("got %v", err)
	}
}

func TestDeriveTask_Happy(t *testing.T) {
	w, d, ch, _, _, tc := setupDerivation(t)
	cid, msgIDs := seedChannelWithMsgs(t, w, ch, "t1", 1)
	res, err := d.DeriveTask(context.Background(), DeriveTaskCommand{
		SourceConversationID: cid, SourceMessageIDs: msgIDs,
		ProjectID: "p-1", Title: "TaskT", AgentInstanceID: "AI-1",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskID == "" || res.ChildConversationID == "" || res.ReferenceCount != 1 {
		t.Fatalf("got %+v", res)
	}
	if tc.lastInput.AgentInstanceID != "AI-1" {
		t.Fatal()
	}
}

func TestDeriveTask_MissingAgentInstance(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, _ := seedChannelWithMsgs(t, w, ch, "tt", 0)
	_, err := d.DeriveTask(context.Background(), DeriveTaskCommand{
		SourceConversationID: cid, ProjectID: "p-1", Title: "X",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveTask_MissingProject(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, _ := seedChannelWithMsgs(t, w, ch, "tx", 0)
	_, err := d.DeriveTask(context.Background(), DeriveTaskCommand{
		SourceConversationID: cid, Title: "X", AgentInstanceID: "AI-1",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveTask_MissingTitle(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, _ := seedChannelWithMsgs(t, w, ch, "ty", 0)
	_, err := d.DeriveTask(context.Background(), DeriveTaskCommand{
		SourceConversationID: cid, ProjectID: "p-1", AgentInstanceID: "AI-1",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveTask_CreatorNotWired(t *testing.T) {
	_, d, _, _, _, _ := setupDerivation(t)
	d.taskCreator = nil
	_, err := d.DeriveTask(context.Background(), DeriveTaskCommand{
		CreatedBy: "user:h", Actor: "user:h",
	})
	if !errors.Is(err, ErrDerivationCreatorNotWired) {
		t.Fatalf("got %v", err)
	}
}

func TestDeriveIssue_SourceConvNotFound(t *testing.T) {
	_, d, _, _, _, _ := setupDerivation(t)
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: "nope", ProjectID: "p", Title: "T",
		CreatedBy: "user:h", Actor: "user:h",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveIssue_BadCreatedBy(t *testing.T) {
	_, d, _, _, _, _ := setupDerivation(t)
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: "x", ProjectID: "p", Title: "T",
		CreatedBy: "", Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestDeriveIssue_EmptySourceID(t *testing.T) {
	_, d, _, _, _, _ := setupDerivation(t)
	_, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: "", ProjectID: "p", Title: "T",
		CreatedBy: "user:h", Actor: "user:h",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewMessageDerivationService_NilClock(t *testing.T) {
	db, w := setupRaw(t)
	refRepo := convsqlite.NewReferenceRepo(db)
	co := NewCarryOverService(db, w.convRepo, w.msgRepo, refRepo, w.sink, w.idgen, w.clock)
	d := NewMessageDerivationService(db, w.convRepo, w.msgRepo, co, nil, nil, w.sink, nil)
	if d == nil {
		t.Fatal()
	}
}

func TestDeriveIssue_NoMessages_StillOpensIssue(t *testing.T) {
	w, d, ch, _, _, _ := setupDerivation(t)
	cid, _ := seedChannelWithMsgs(t, w, ch, "nomsg", 0)
	res, err := d.DeriveIssue(context.Background(), DeriveIssueCommand{
		SourceConversationID: cid, ProjectID: "p-1", Title: "T",
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IssueID == "" {
		t.Fatal()
	}
	if res.ReferenceCount != 0 || res.CarryOverEventID != "" {
		t.Fatalf("expected no carry-over, got %+v", res)
	}
}
