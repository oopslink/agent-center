package service

import (
	"context"
	"errors"
	"fmt"
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

func setup(t *testing.T) *MessageWriter {
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
	return NewMessageWriter(db, conv, msg, sink, gen, fc)
}

func TestOpenConversation_DMHappy(t *testing.T) {
	w := setup(t)
	res, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind:         conversation.ConversationKindDM,
		CreatedBy:    conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID == "" || res.EventID == "" {
		t.Fatalf("got %+v", res)
	}
}

// dmFixtureParticipants is the canonical 2-party DM participant set for tests
// (T344: OpenConversation rejects a DM with <2 distinct active participants).
func dmFixtureParticipants() []conversation.ParticipantElement {
	return []conversation.ParticipantElement{
		{IdentityID: "user:hayang", Role: "owner", JoinedAt: "t", JoinedBy: "user:hayang"},
		{IdentityID: "agent:agent-peer", Role: "member", JoinedAt: "t", JoinedBy: "user:hayang"},
	}
}

// T344: a DM with a single (or zero) active participant is rejected — the bug that
// let the reminder deliverer mint stray single-party DMs.
func TestOpenConversation_DM_RequiresTwoParticipants(t *testing.T) {
	w := setup(t)
	for _, parts := range [][]conversation.ParticipantElement{
		nil,
		{{IdentityID: "agent:agent-solo", Role: "member", JoinedAt: "t", JoinedBy: "system"}},
	} {
		_, err := w.OpenConversation(context.Background(), OpenCommand{
			Kind:         conversation.ConversationKindDM,
			CreatedBy:    conversation.IdentityRef("system"),
			Participants: parts,
			Actor:        observability.Actor("system"),
		})
		if !errors.Is(err, conversation.ErrConversationDMParticipants) {
			t.Fatalf("participants=%v: want ErrConversationDMParticipants, got %v", parts, err)
		}
	}
}

func TestOpenConversation_ChannelHappy(t *testing.T) {
	w := setup(t)
	_, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind:      conversation.ConversationKindChannel,
		Name:      "general",
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenConversation_BadKind(t *testing.T) {
	w := setup(t)
	_, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind:      "weird",
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationInvalidKind) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenConversation_TaskKindRejected(t *testing.T) {
	w := setup(t)
	_, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind:      conversation.ConversationKindTask,
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationInvalidKind) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenConversation_BadActor(t *testing.T) {
	w := setup(t)
	_, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind:      conversation.ConversationKindDM,
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor(""),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestAddMessage_Happy(t *testing.T) {
	w := setup(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind:         conversation.ConversationKindDM,
		CreatedBy:    conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	got, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID:   res.ConversationID,
		SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hi",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID == "" || got.EventID == "" {
		t.Fatal()
	}
}

func TestAddMessage_NotFound(t *testing.T) {
	w := setup(t)
	_, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID:   "nope",
		SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Content:          "x",
		Actor:            observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAddMessage_ToArchived(t *testing.T) {
	w := setup(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind:         conversation.ConversationKindDM,
		CreatedBy:    conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	_, err := w.Archive(context.Background(), ArchiveCommand{
		ConversationID: res.ConversationID,
		Version:        1,
		ArchivedBy:     conversation.IdentityRef("user:hayang"),
		Actor:          observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID:   res.ConversationID,
		SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Content:          "x",
		Actor:            observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationArchived) {
		t.Fatalf("got %v", err)
	}
}

func TestAddMessage_ToClosed(t *testing.T) {
	w := setup(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind:         conversation.ConversationKindDM,
		CreatedBy:    conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	_, _ = w.Close(context.Background(), CloseCommand{
		ConversationID: res.ConversationID, Version: 1,
		Reason: "done", Message: "wrapped", Actor: observability.Actor("user:hayang"),
	})
	_, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: res.ConversationID, SenderIdentityID: "user:hayang",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		Content: "x", Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestAddMessage_BadSender(t *testing.T) {
	w := setup(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, CreatedBy: conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	_, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: res.ConversationID, SenderIdentityID: "",
		ContentKind: conversation.MessageContentText, Direction: conversation.DirectionInbound,
		Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrMessageInvalidSender) {
		t.Fatalf("got %v", err)
	}
}

func TestClose_Happy(t *testing.T) {
	w := setup(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, CreatedBy: conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	evID, err := w.Close(context.Background(), CloseCommand{
		ConversationID: res.ConversationID, Version: 1,
		Reason: "done", Message: "wrapped", Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if evID == "" {
		t.Fatal()
	}
}

func TestClose_RequiresReasonMessage(t *testing.T) {
	w := setup(t)
	_, err := w.Close(context.Background(), CloseCommand{
		ConversationID: "x", Version: 1, Actor: observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestClose_VersionConflict(t *testing.T) {
	w := setup(t)
	res, _ := w.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, CreatedBy: conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	_, err := w.Close(context.Background(), CloseCommand{
		ConversationID: res.ConversationID, Version: 99,
		Reason: "r", Message: "m", Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestArchive_BadActor(t *testing.T) {
	w := setup(t)
	_, err := w.Archive(context.Background(), ArchiveCommand{
		ConversationID: "x", Version: 1, ArchivedBy: "user:h", Actor: observability.Actor(""),
	})
	if err == nil {
		t.Fatal()
	}
	_, err = w.Archive(context.Background(), ArchiveCommand{
		ConversationID: "x", Version: 1, ArchivedBy: "", Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal()
	}
}

// TestAddMessage_WithAttachments verifies #133 write-wire: AddMessageCommand.Attachments
// are persisted on the Message and round-trip via the repo (URI/filename/mime/size).
func TestAddMessage_WithAttachments(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)
	w := NewMessageWriter(db, convRepo, msgRepo, sink, gen, fc)

	res, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, CreatedBy: conversation.IdentityRef("user:hayang"),
		Participants: dmFixtureParticipants(),
		Actor:        observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	atts := []conversation.MessageAttachment{
		{URI: "ac://files/01ARZ3NDEKTSV4RRFFQ69G5FAV", Filename: "design.png", MimeType: "image/png", Size: 2048},
	}
	got, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: res.ConversationID, SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind: conversation.MessageContentText, Content: "see attached",
		Direction: conversation.DirectionInbound, Attachments: atts,
		Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := msgRepo.FindByID(context.Background(), got.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	rt := m.Attachments()
	if len(rt) != 1 || rt[0].URI != atts[0].URI || rt[0].Filename != "design.png" || rt[0].MimeType != "image/png" || rt[0].Size != 2048 {
		t.Fatalf("attachments not persisted/round-tripped: %+v", rt)
	}

	// A plain message (no attachments) round-trips with none.
	got2, _ := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: res.ConversationID, SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind: conversation.MessageContentText, Content: "plain",
		Direction: conversation.DirectionInbound, Actor: observability.Actor("user:hayang"),
	})
	m2, _ := msgRepo.FindByID(context.Background(), got2.MessageID)
	if len(m2.Attachments()) != 0 {
		t.Fatalf("plain message should have no attachments, got %+v", m2.Attachments())
	}
}

// --- v2.9.1 Thread (P1): AddMessage parent → derived root/thread ---

// setupWithRepos returns a writer plus the message repo so a test can inspect
// the persisted thread fields (mirrors TestAddMessage_WithAttachments).
func setupWithRepos(t *testing.T) (*MessageWriter, *convsqlite.MessageRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)
	return NewMessageWriter(db, convRepo, msgRepo, sink, gen, fc), msgRepo
}

// dmPeerSeq makes each openDM() call a DISTINCT 1:1 pair (distinct dm_key) so
// tests that open two DMs get two conversations (the dedup index would otherwise
// collapse identical pairs). T344: a DM now requires ≥2 distinct active parties.
var dmPeerSeq int

func openDM(t *testing.T, w *MessageWriter) conversation.ConversationID {
	t.Helper()
	dmPeerSeq++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := w.OpenConversation(context.Background(), OpenCommand{
		Kind: conversation.ConversationKindDM, CreatedBy: conversation.IdentityRef("user:hayang"),
		Participants: []conversation.ParticipantElement{
			{IdentityID: "user:hayang", Role: "owner", JoinedAt: now, JoinedBy: "user:hayang"},
			{IdentityID: conversation.IdentityRef(fmt.Sprintf("agent:agent-peer%d", dmPeerSeq)), Role: "member", JoinedAt: now, JoinedBy: "user:hayang"},
		},
		Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.ConversationID
}

func addMsg(t *testing.T, w *MessageWriter, conv conversation.ConversationID, parent conversation.MessageID) conversation.MessageID {
	t.Helper()
	res, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: conv, SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind: conversation.MessageContentText, Content: "x", Direction: conversation.DirectionInbound,
		ParentMessageID: parent, Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.MessageID
}

// A reply onto a root derives parent == root == the root's id.
func TestAddMessage_ThreadReply_DerivesRootAndParent(t *testing.T) {
	w, msgRepo := setupWithRepos(t)
	conv := openDM(t, w)
	rootID := addMsg(t, w, conv, "")
	replyID := addMsg(t, w, conv, rootID)

	reply, err := msgRepo.FindByID(context.Background(), replyID)
	if err != nil {
		t.Fatal(err)
	}
	if reply.ParentMessageID() != rootID || reply.RootMessageID() != rootID {
		t.Fatalf("got parent=%q root=%q, want %q/%q", reply.ParentMessageID(), reply.RootMessageID(), rootID, rootID)
	}
	if reply.ThreadID() != rootID {
		t.Fatalf("reply thread id %q want %q", reply.ThreadID(), rootID)
	}
}

// Replying to a REPLY merges into the same thread — parent/root are redirected to
// the root, so no 2nd level is ever created (Slack-style depth-1).
func TestAddMessage_ThreadReply_ToReply_MergesToRoot(t *testing.T) {
	w, msgRepo := setupWithRepos(t)
	conv := openDM(t, w)
	rootID := addMsg(t, w, conv, "")
	reply1 := addMsg(t, w, conv, rootID)
	reply2 := addMsg(t, w, conv, reply1) // reply to a reply

	got, err := msgRepo.FindByID(context.Background(), reply2)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentMessageID() != rootID || got.RootMessageID() != rootID {
		t.Fatalf("reply-to-reply must redirect to root: got parent=%q root=%q, want %q", got.ParentMessageID(), got.RootMessageID(), rootID)
	}
}

// Replying to a non-existent parent is rejected.
func TestAddMessage_ThreadReply_ParentNotFound(t *testing.T) {
	w, _ := setupWithRepos(t)
	conv := openDM(t, w)
	_, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: conv, SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind: conversation.MessageContentText, Content: "x", Direction: conversation.DirectionInbound,
		ParentMessageID: "ghost", Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("got %v", err)
	}
}

// Org/conversation isolation: a reply cannot target a parent that lives in a
// DIFFERENT conversation (conversations are org-scoped, so this also blocks
// cross-org thread stitching — §5.7 existence-non-disclosure at the edge).
func TestAddMessage_ThreadReply_ParentInOtherConversation(t *testing.T) {
	w, _ := setupWithRepos(t)
	convA := openDM(t, w)
	convB := openDM(t, w)
	rootInA := addMsg(t, w, convA, "")

	_, err := w.AddMessage(context.Background(), AddMessageCommand{
		ConversationID: convB, SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind: conversation.MessageContentText, Content: "x", Direction: conversation.DirectionInbound,
		ParentMessageID: rootInA, Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrMessageParentMismatch) {
		t.Fatalf("got %v", err)
	}
}

// v2.7 #187: conversation ids are user-facing "<kind>-<8hex>" for channel/DM;
// task (and other internal) conversations keep the ULID (navigated via owner_ref).
func TestNewConversationID_PrefixByKind(t *testing.T) {
	gen := idgen.NewGenerator(clock.SystemClock{})
	if got := newConversationID(gen, conversation.ConversationKindChannel); !strings.HasPrefix(string(got), "channel-") {
		t.Errorf("channel id %q want channel- prefix", got)
	}
	if got := newConversationID(gen, conversation.ConversationKindDM); !strings.HasPrefix(string(got), "dm-") {
		t.Errorf("dm id %q want dm- prefix", got)
	}
	if got := newConversationID(gen, conversation.ConversationKindTask); !idgen.IsValid(string(got)) {
		t.Errorf("task conversation id %q want internal ULID (no prefix)", got)
	}
}
