package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	disqlite "github.com/oopslink/agent-center/internal/discussion/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// p10Stack is the v2 Conversation BC integration harness for F3:
// CV1 channel + CV2b participants + CV3 carry-over + CV4 派生入口
// + Discussion BC end-to-end.
type p10Stack struct {
	db       *sql.DB
	clk      *clock.FakeClock
	gen      idgen.Generator
	sink     *observability.EventSink
	er       *obsqlite.EventRepo
	convRepo conversation.ConversationRepository
	msgRepo  conversation.MessageRepository
	writer   *convservice.MessageWriter
	channelMgmt *convservice.ChannelManagementService
	pmgmt       *convservice.ParticipantManagementService
	carryOver   *convservice.CarryOverService
	derivation  *convservice.MessageDerivationService
	issueRepo   discussion.IssueRepository
	issueLife   *disservice.IssueLifecycleService
}

// adapter shims that match cli/derivation_shims.go for use in
// integration tests (we don't import the cli package).
type p10IssueOpener struct{ svc *disservice.IssueLifecycleService }

func (s *p10IssueOpener) OpenFromConversation(ctx context.Context, in convservice.OpenFromConversationInput) (convservice.OpenFromConversationResult, error) {
	res, err := s.svc.Open(ctx, disservice.OpenIssueCommand{
		ProjectID: in.ProjectID, Title: in.Title, Description: in.Description,
		OpenedByIdentityID: string(in.OpenedBy),
		Origin:             discussion.OriginDerivedFromConversation,
		Actor:              in.Actor,
	})
	if err != nil {
		return convservice.OpenFromConversationResult{}, err
	}
	return convservice.OpenFromConversationResult{
		IssueID: string(res.IssueID), ConversationID: res.ConversationID, EventID: res.EventID,
	}, nil
}

func setupP10Stack(t *testing.T) *p10Stack {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(ctx, db)
	sink := observability.NewEventSink(er, er, gen, clk)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)
	refRepo := convsqlite.NewReferenceRepo(db)
	writer := convservice.NewMessageWriter(db, convRepo, msgRepo, sink, gen, clk)
	channelMgmt := convservice.NewChannelManagementService(db, convRepo, sink, gen, clk)
	pmgmt := convservice.NewParticipantManagementService(db, convRepo, sink, clk)
	carryOver := convservice.NewCarryOverService(db, convRepo, msgRepo, refRepo, sink, gen, clk)
	issueRepo := disqlite.NewIssueRepo(db)
	convOpener := disservice.NewIssueConversationOpener(convRepo, sink, gen, clk)
	issueLife := disservice.NewIssueLifecycleService(db, issueRepo, convOpener, writer, sink, gen, clk)
	derivation := convservice.NewMessageDerivationService(db, convRepo, msgRepo, carryOver,
		&p10IssueOpener{svc: issueLife}, nil, sink, clk)
	return &p10Stack{
		db: db, clk: clk, gen: gen, sink: sink, er: er,
		convRepo: convRepo, msgRepo: msgRepo,
		writer: writer, channelMgmt: channelMgmt, pmgmt: pmgmt,
		carryOver: carryOver, derivation: derivation,
		issueRepo: issueRepo, issueLife: issueLife,
	}
}

// TestP10_F3_FullCV4Story walks the v2 user story end-to-end:
//
//  1. user identity + agent identity registered (system identity is
//     auto-provisioned by harness setup)
//  2. user creates a channel (creator becomes owner participant)
//  3. user invites agent into the channel
//  4. user sends 3 messages
//  5. user derives an Issue from 2 of the messages (CV4)
//  6. carry-over refs land in the issue's conversation
//  7. reverse lookup (CV3 message refs) finds the child conversation
//
// Asserts emit ledger contains all the expected events.
func TestP10_F3_FullCV4Story(t *testing.T) {
	s := setupP10Stack(t)
	ctx := context.Background()

	// 1. user creates a channel.
	chRes, err := s.channelMgmt.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "platform", Description: "platform work",
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := s.convRepo.FindByID(ctx, chRes.ConversationID)
	if !conv.HasActiveParticipant("user:hayang") {
		t.Fatal("user:hayang should be the owner participant")
	}

	// 3. user invites agent.
	if _, err := s.pmgmt.Invite(ctx, convservice.InviteCommand{
		ConversationName: "platform",
		IdentityID:       conversation.IdentityRef("agent:fixer"),
		Role:             "member",
		InvitedBy:        conversation.IdentityRef("user:hayang"),
		Actor:            observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	conv, _ = s.convRepo.FindByID(ctx, chRes.ConversationID)
	if !conv.HasActiveParticipant("agent:fixer") {
		t.Fatal("agent should be active after invite")
	}

	// 4. user sends 3 messages.
	var msgIDs []conversation.MessageID
	for _, body := range []string{"problem statement", "rationale A", "rationale B"} {
		amRes, err := s.writer.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   chRes.ConversationID,
			SenderIdentityID: conversation.IdentityRef("user:hayang"),
			ContentKind:      conversation.MessageContentText,
			Content:          body,
			Direction:        conversation.DirectionInbound,
			Actor:            observability.Actor("user:hayang"),
		})
		if err != nil {
			t.Fatal(err)
		}
		msgIDs = append(msgIDs, amRes.MessageID)
	}

	// 5. user derives an Issue from 2 messages.
	dRes, err := s.derivation.DeriveIssue(ctx, convservice.DeriveIssueCommand{
		SourceConversationID: chRes.ConversationID,
		SourceMessageIDs:     []conversation.MessageID{msgIDs[1], msgIDs[2]},
		ProjectID:            "p-1", Title: "Implement X",
		Description: "Carry over rationale",
		CreatedBy:   conversation.IdentityRef("user:hayang"),
		Actor:       observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if dRes.IssueID == "" {
		t.Fatal("expected issue id")
	}
	if dRes.ChildConversationID == "" {
		t.Fatal("expected new issue conversation")
	}
	if dRes.ReferenceCount != 2 {
		t.Fatalf("reference_count: got %d want 2", dRes.ReferenceCount)
	}

	// 6. Verify Issue + its conversation persisted with conversation_id set.
	issue, err := s.issueRepo.FindByID(ctx, discussion.IssueID(dRes.IssueID))
	if err != nil {
		t.Fatal(err)
	}
	if issue.Origin() != discussion.OriginDerivedFromConversation {
		t.Fatalf("origin: got %s want derived_from_conversation", issue.Origin())
	}
	if issue.ConversationID() != dRes.ChildConversationID {
		t.Fatalf("issue.conversation_id mismatch")
	}
	childConv, err := s.convRepo.FindByID(ctx, dRes.ChildConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if childConv.Kind() != conversation.ConversationKindIssue {
		t.Fatalf("child conv kind: %s", childConv.Kind())
	}

	// 7. Reverse CV3 lookup: msgIDs[1] should point to childConv.
	refs, err := s.carryOver.FindBySourceMsg(ctx, msgIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ChildConversationID != dRes.ChildConversationID {
		t.Fatalf("reverse lookup: %v", refs)
	}

	// 8. Forward CV3 lookup: childConv refs.
	childRefs, err := s.carryOver.FindByChildConv(ctx, dRes.ChildConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(childRefs) != 2 {
		t.Fatalf("forward lookup: got %d want 2", len(childRefs))
	}

	// 9. Verify event ledger has the full chain.
	expected := map[observability.EventType]bool{
		"conversation.opened":                        true,
		"conversation.participant_joined":            true,
		"conversation.message_added":                 true,
		"issue.opened":                               true,
		"conversation.message_references_added":      true,
	}
	events, err := s.er.Find(ctx, observability.EventQueryFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	got := map[observability.EventType]int{}
	for _, e := range events {
		got[e.Type()]++
	}
	for ev := range expected {
		if got[ev] == 0 {
			t.Errorf("missing event type %s in ledger (saw: %v)", ev, got)
		}
	}
}

// TestP10_F3_DeriveBlocksWhenSourceArchived guards CV4 validation:
// archive source channel → DeriveIssue must return ErrSourceNotActive.
func TestP10_F3_DeriveBlocksWhenSourceArchived(t *testing.T) {
	s := setupP10Stack(t)
	ctx := context.Background()
	chRes, _ := s.channelMgmt.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "deadch", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, _ = s.channelMgmt.ArchiveChannel(ctx, convservice.ArchiveChannelCommand{
		Name: "deadch", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	_, err := s.derivation.DeriveIssue(ctx, convservice.DeriveIssueCommand{
		SourceConversationID: chRes.ConversationID,
		ProjectID:            "p-1", Title: "Late",
		CreatedBy:            "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected error from archived source")
	}
}

// TestP10_F3_DeriveBlocksWhenCallerNotParticipant guards the channel-kind
// participant rule.
func TestP10_F3_DeriveBlocksWhenCallerNotParticipant(t *testing.T) {
	s := setupP10Stack(t)
	ctx := context.Background()
	chRes, _ := s.channelMgmt.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "exclusive", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, err := s.derivation.DeriveIssue(ctx, convservice.DeriveIssueCommand{
		SourceConversationID: chRes.ConversationID,
		ProjectID:            "p-1", Title: "Try",
		CreatedBy:            "user:stranger", Actor: "user:stranger",
	})
	if err == nil {
		t.Fatal("expected ErrDerivationCallerNotParticipant")
	}
}
