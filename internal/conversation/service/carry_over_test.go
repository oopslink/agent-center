package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/observability"
)

func setupCarryOver(t *testing.T) (*MessageWriter, *CarryOverService, *convsqlite.MessageRepo, *convsqlite.ReferenceRepo) {
	t.Helper()
	db, w := setupRaw(t)
	refRepo := convsqlite.NewReferenceRepo(db)
	co := NewCarryOverService(db, w.convRepo, w.msgRepo, refRepo, w.sink, w.idgen, w.clock)
	mr := w.msgRepo.(*convsqlite.MessageRepo)
	return w, co, mr, refRepo
}

func seedConvAndMsgs(t *testing.T, w *MessageWriter, kind conversation.ConversationKind, count int) (conversation.ConversationID, []conversation.MessageID) {
	t.Helper()
	conv, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID:        conversation.ConversationID(w.idgen.NewULID()),
		Kind:      kind,
		Name:      "x",
		CreatedBy: conversation.IdentityRef("user:hayang"),
		OpenedAt:  w.clock.Now(),
	})
	_ = w.convRepo.Save(context.Background(), conv)
	var ids []conversation.MessageID
	for i := 0; i < count; i++ {
		m, _ := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(w.idgen.NewULID()),
			ConversationID:   conv.ID(),
			SenderIdentityID: "user:hayang",
			ContentKind:      conversation.MessageContentText,
			Content:          "msg",
			Direction:        conversation.DirectionInbound,
			PostedAt:         w.clock.Now().Add(time.Duration(i) * time.Second),
		})
		_ = w.msgRepo.Append(context.Background(), m)
		ids = append(ids, m.ID())
	}
	return conv.ID(), ids
}

func TestCarryOver_Materialise_Happy(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	sourceID, msgIDs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 3)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	res, err := co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID:  childID,
		SourceConversationID: sourceID,
		SourceMessageIDs:     msgIDs,
		CreatedBy:            conversation.IdentityRef("user:hayang"),
		Actor:                observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.References) != 3 {
		t.Fatalf("got %d refs", len(res.References))
	}
	if res.EventID == "" {
		t.Fatal()
	}
}

func TestCarryOver_FindByChildConv(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	sourceID, msgIDs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 2)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	_, _ = co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID:  childID,
		SourceConversationID: sourceID,
		SourceMessageIDs:     msgIDs,
		CreatedBy:            "user:hayang", Actor: "user:hayang",
	})
	got, err := co.FindByChildConv(context.Background(), childID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}

func TestCarryOver_FindBySourceMsg(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	sourceID, msgIDs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 1)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	_, _ = co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID: childID, SourceConversationID: sourceID,
		SourceMessageIDs: msgIDs, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	got, err := co.FindBySourceMsg(context.Background(), msgIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ChildConversationID != childID {
		t.Fatalf("got %v", got)
	}
}

func TestCarryOver_EmptyMessages(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	sourceID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 0)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	res, err := co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID: childID, SourceConversationID: sourceID,
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.References != nil || res.EventID != "" {
		t.Fatalf("expected no-op, got %+v", res)
	}
}

func TestCarryOver_MessageInWrongConv(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	sourceID, msgIDs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 1)
	otherID, otherMsgs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 1)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	_, err := co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID:  childID,
		SourceConversationID: sourceID,
		SourceMessageIDs:     []conversation.MessageID{msgIDs[0], otherMsgs[0]},
		CreatedBy:            "user:hayang", Actor: "user:hayang",
	})
	if !errors.Is(err, ErrCarryOverSourceMsgNotInConv) {
		t.Fatalf("got %v", err)
	}
	_ = otherID
}

func TestCarryOver_ChildConvNotFound(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	sourceID, msgIDs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 1)
	_, err := co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID:  "nope",
		SourceConversationID: sourceID,
		SourceMessageIDs:     msgIDs,
		CreatedBy:            "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestCarryOver_SourceConvNotFound(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	_, err := co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID:  childID,
		SourceConversationID: "nope",
		SourceMessageIDs:     []conversation.MessageID{"m-1"},
		CreatedBy:            "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestCarryOver_DuplicateRef(t *testing.T) {
	w, co, _, _ := setupCarryOver(t)
	sourceID, msgIDs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 1)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	_, _ = co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID: childID, SourceConversationID: sourceID,
		SourceMessageIDs: msgIDs, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, err := co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID: childID, SourceConversationID: sourceID,
		SourceMessageIDs: msgIDs, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestCarryOver_MissingArgs(t *testing.T) {
	_, co, _, _ := setupCarryOver(t)
	cases := []MaterialiseCommand{
		{}, // empty
		{ChildConversationID: "x"}, // missing source
		{ChildConversationID: "x", SourceConversationID: "y"}, // no msgs OK (early return) — skip
	}
	for i, c := range cases[:2] {
		c.Actor = ""
		c.CreatedBy = ""
		if _, err := co.Materialise(context.Background(), c); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestNewCarryOverService_NilClock(t *testing.T) {
	db, w := setupRaw(t)
	refRepo := convsqlite.NewReferenceRepo(db)
	co := NewCarryOverService(db, w.convRepo, w.msgRepo, refRepo, w.sink, w.idgen, nil)
	if co == nil {
		t.Fatal()
	}
}

func TestReferenceRepo_DeleteByChildConvID(t *testing.T) {
	w, co, _, repo := setupCarryOver(t)
	sourceID, msgIDs := seedConvAndMsgs(t, w, conversation.ConversationKindChannel, 1)
	childID, _ := seedConvAndMsgs(t, w, conversation.ConversationKindIssue, 0)
	_, _ = co.Materialise(context.Background(), MaterialiseCommand{
		ChildConversationID: childID, SourceConversationID: sourceID,
		SourceMessageIDs: msgIDs, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err := repo.DeleteByChildConvID(context.Background(), childID); err != nil {
		t.Fatal(err)
	}
	got, _ := co.FindByChildConv(context.Background(), childID)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestReferenceRepo_SaveEmpty(t *testing.T) {
	_, _, _, repo := setupCarryOver(t)
	if err := repo.Save(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceRepo_SaveNilInBatch(t *testing.T) {
	_, _, _, repo := setupCarryOver(t)
	err := repo.Save(context.Background(), []*conversation.ConversationMessageReference{nil})
	if err == nil {
		t.Fatal()
	}
}
