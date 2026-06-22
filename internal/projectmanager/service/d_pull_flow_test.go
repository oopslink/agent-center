package service

// PR7 — #278 D pull-model integration suite, dual-stream slice (mechanism layer).
//
// The work-item pull-flow half of this suite was removed with the AgentWorkItem
// domain (assign→queued WI→pull→task-sync→reconciler). What survives is the
// agent's SECOND inbound stream: besides pulling work, an agent reads conversation
// messages addressed to it via get_my_unread (DM-all + channel-@mention only), and
// mark_seen dedups. Wired on the conversation BC (AgentInboxService +
// ReadStateService), mirroring the A6 unit-test scope at the integration layer.

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	conversation "github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func pr7InboxSetup(t *testing.T) (*convservice.AgentInboxService, *convservice.ReadStateService, *convsql.ConversationRepo, *convsql.MessageRepo, *clock.FakeClock, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	convRepo := convsql.NewConversationRepo(db)
	msgRepo := convsql.NewMessageRepo(db)
	rsRepo := convsql.NewReadStateRepo(db)
	inbox := convservice.NewAgentInboxService(db, convRepo, rsRepo)
	rs := convservice.NewReadStateService(db, rsRepo, msgRepo, sink, clk)
	return inbox, rs, convRepo, msgRepo, clk, context.Background()
}

func pr7SeedConv(t *testing.T, repo *convsql.ConversationRepo, clk *clock.FakeClock, id conversation.ConversationID, kind conversation.ConversationKind, org string, participants ...conversation.IdentityRef) {
	t.Helper()
	parts := make([]conversation.ParticipantElement, 0, len(participants))
	for _, p := range participants {
		parts = append(parts, conversation.ParticipantElement{
			IdentityID: p, Role: "member", JoinedAt: clk.Now().Format(time.RFC3339Nano), JoinedBy: "user:alice",
		})
	}
	name := ""
	if kind == conversation.ConversationKindChannel {
		name = "ch-" + string(id)
	}
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: id, Kind: kind, Name: name, CreatedBy: "user:alice",
		OpenedAt: clk.Now(), Participants: parts, OrganizationID: org,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

func pr7SeedMsg(t *testing.T, repo *convsql.MessageRepo, clk *clock.FakeClock, id conversation.MessageID, conv conversation.ConversationID, sender conversation.IdentityRef, content string) {
	t.Helper()
	clk.Advance(time.Millisecond)
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: id, ConversationID: conv, SenderIdentityID: sender,
		ContentKind: conversation.MessageContentText, Content: content,
		Direction: conversation.DirectionInbound, PostedAt: clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func TestPR7_DualStream(t *testing.T) {
	inbox, rs, convRepo, msgRepo, clk, ctx := pr7InboxSetup(t)
	const agent = conversation.IdentityRef("agent:bot-1")
	refs := []conversation.IdentityRef{agent}

	// DM(agent+user): every DM message is unread for the agent.
	pr7SeedConv(t, convRepo, clk, "dm-1", conversation.ConversationKindDM, "org-1", agent, "user:alice")
	pr7SeedMsg(t, msgRepo, clk, "dm-1-msg-1", "dm-1", "user:alice", "hey bot, ping")
	// Channel(agent participates): @mention is unread; plain chatter is NOT.
	pr7SeedConv(t, convRepo, clk, "ch-1", conversation.ConversationKindChannel, "org-1", agent, "user:alice")
	pr7SeedMsg(t, msgRepo, clk, "ch-1-msg-1", "ch-1", "user:alice", "morning everyone")
	pr7SeedMsg(t, msgRepo, clk, "ch-1-msg-2", "ch-1", "user:alice", "@Bot please review")

	idset := func(items []convservice.UnreadItem) map[conversation.MessageID]bool {
		m := map[conversation.MessageID]bool{}
		for _, it := range items {
			m[it.MessageID] = true
		}
		return m
	}

	got, err := inbox.ListUnreadForIdentity(ctx, refs, "org-1", "Bot")
	if err != nil {
		t.Fatal(err)
	}
	ids := idset(got)
	if !ids["dm-1-msg-1"] {
		t.Fatal("dual-stream: DM message must be unread for the agent")
	}
	if !ids["ch-1-msg-2"] {
		t.Fatal("dual-stream: channel @mention must be unread for the agent")
	}
	if ids["ch-1-msg-1"] {
		t.Fatal("dual-stream: channel non-mention chatter must NOT surface in get_my_unread")
	}

	// mark_seen the DM → it dedups out of the next get_my_unread.
	if _, err := rs.MarkSeen(ctx, convservice.MarkSeenCommand{
		UserID: agent, ConversationID: "dm-1", LastSeenMessageID: "dm-1-msg-1", Actor: observability.Actor(agent),
	}); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}
	if idset(mustUnread(t, inbox, ctx, refs))["dm-1-msg-1"] {
		t.Fatal("dual-stream: after mark_seen the DM message must no longer be unread (dedup)")
	}
}

func mustUnread(t *testing.T, inbox *convservice.AgentInboxService, ctx context.Context, refs []conversation.IdentityRef) []convservice.UnreadItem {
	t.Helper()
	got, err := inbox.ListUnreadForIdentity(ctx, refs, "org-1", "Bot")
	if err != nil {
		t.Fatal(err)
	}
	return got
}
