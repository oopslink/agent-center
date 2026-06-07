package service

import (
	"context"
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

// v2.8.1 #278 D PR4b dual-stream: AgentInboxService.ListUnreadForIdentity is the
// read side of get_my_unread — directed-at = ALL unread in DMs the agent is in +
// only @mentions in channels it participates in; own messages + non-participant +
// cross-org conversations excluded; read-state last_seen respected.

type inboxFixture struct {
	inbox    *AgentInboxService
	rs       *ReadStateService
	convRepo *convsqlite.ConversationRepo
	msgRepo  *convsqlite.MessageRepo
	clock    *clock.FakeClock
}

func setupInbox(t *testing.T) *inboxFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC))
	conv := convsqlite.NewConversationRepo(db)
	msg := convsqlite.NewMessageRepo(db)
	rs := convsqlite.NewReadStateRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(fc), fc)
	return &inboxFixture{
		inbox:    NewAgentInboxService(db, conv, rs),
		rs:       NewReadStateService(db, rs, msg, sink, fc),
		convRepo: conv,
		msgRepo:  msg,
		clock:    fc,
	}
}

func (f *inboxFixture) seedConv(t *testing.T, id conversation.ConversationID, kind conversation.ConversationKind, org string, participants ...conversation.IdentityRef) {
	t.Helper()
	parts := make([]conversation.ParticipantElement, 0, len(participants))
	for _, p := range participants {
		parts = append(parts, conversation.ParticipantElement{
			IdentityID: p, Role: "member", JoinedAt: f.clock.Now().Format(time.RFC3339Nano), JoinedBy: "user:alice",
		})
	}
	name := ""
	if kind == conversation.ConversationKindChannel {
		name = "ch-" + string(id)
	}
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: id, Kind: kind, Name: name, CreatedBy: "user:alice",
		OpenedAt: f.clock.Now(), Participants: parts, OrganizationID: org,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.convRepo.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

func (f *inboxFixture) seedMsg(t *testing.T, id conversation.MessageID, conv conversation.ConversationID, sender conversation.IdentityRef, content string) {
	t.Helper()
	f.clock.Advance(time.Millisecond)
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: id, ConversationID: conv, SenderIdentityID: sender,
		ContentKind: conversation.MessageContentText, Content: content,
		Direction: conversation.DirectionInbound, PostedAt: f.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.msgRepo.Append(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func TestAgentInbox_ListUnreadForIdentity(t *testing.T) {
	f := setupInbox(t)
	ctx := context.Background()
	const agent = conversation.IdentityRef("agent:bot-1")

	// DM (agent is a participant): all unread surface, EXCEPT the agent's own.
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", agent, "user:alice")
	f.seedMsg(t, "dm-1-msg-1", "dm-1", "user:alice", "hey bot, ping")
	f.seedMsg(t, "dm-1-msg-2", "dm-1", agent, "on it") // own → excluded
	f.seedMsg(t, "dm-1-msg-3", "dm-1", "user:alice", "thanks")

	// Channel (agent is a participant): only @mentions surface.
	f.seedConv(t, "ch-1", conversation.ConversationKindChannel, "org-1", agent, "user:alice")
	f.seedMsg(t, "ch-1-msg-1", "ch-1", "user:alice", "morning everyone") // no mention → excluded
	f.seedMsg(t, "ch-1-msg-2", "ch-1", "user:alice", "@Bot please review") // mention → included

	// Channel the agent is NOT in (mentions it, but non-participant) → excluded.
	f.seedConv(t, "ch-2", conversation.ConversationKindChannel, "org-1", "user:alice")
	f.seedMsg(t, "ch-2-msg-1", "ch-2", "user:alice", "@Bot over here")

	// DM in a DIFFERENT org → excluded.
	f.seedConv(t, "dm-x", conversation.ConversationKindDM, "org-2", agent, "user:eve")
	f.seedMsg(t, "dm-x-msg-1", "dm-x", "user:eve", "cross-org ping")

	got, err := f.inbox.ListUnreadForIdentity(ctx, agent, "org-1", "Bot")
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := map[conversation.MessageID]bool{}
	for _, it := range got {
		gotIDs[it.MessageID] = true
	}
	// Expect: dm-1-msg-1, dm-1-msg-3 (DM all, own excluded), ch-1-msg-2 (mention).
	want := []conversation.MessageID{"dm-1-msg-1", "dm-1-msg-3", "ch-1-msg-2"}
	if len(got) != len(want) {
		t.Fatalf("got %d items %v, want %d %v", len(got), keysOf(gotIDs), len(want), want)
	}
	for _, w := range want {
		if !gotIDs[w] {
			t.Errorf("missing expected unread %s (got %v)", w, keysOf(gotIDs))
		}
	}
	// Negative: own / non-mention / non-participant / cross-org never appear.
	for _, bad := range []conversation.MessageID{"dm-1-msg-2", "ch-1-msg-1", "ch-2-msg-1", "dm-x-msg-1"} {
		if gotIDs[bad] {
			t.Errorf("unexpected unread %s should be excluded", bad)
		}
	}

	// read-state last_seen: mark dm-1 seen through msg-1 → only msg-3 unread there.
	if _, err := f.rs.MarkSeen(ctx, MarkSeenCommand{
		UserID: agent, ConversationID: "dm-1", LastSeenMessageID: "dm-1-msg-1", Actor: observability.Actor(agent),
	}); err != nil {
		t.Fatal(err)
	}
	got2, err := f.inbox.ListUnreadForIdentity(ctx, agent, "org-1", "Bot")
	if err != nil {
		t.Fatal(err)
	}
	got2IDs := map[conversation.MessageID]bool{}
	for _, it := range got2 {
		got2IDs[it.MessageID] = true
	}
	if got2IDs["dm-1-msg-1"] {
		t.Errorf("dm-1-msg-1 is now seen, must not be unread")
	}
	if !got2IDs["dm-1-msg-3"] || !got2IDs["ch-1-msg-2"] {
		t.Errorf("dm-1-msg-3 + ch-1-msg-2 still unread, got %v", keysOf(got2IDs))
	}
}

func keysOf(m map[conversation.MessageID]bool) []conversation.MessageID {
	out := make([]conversation.MessageID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
