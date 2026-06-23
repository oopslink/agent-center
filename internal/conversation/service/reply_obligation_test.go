package service

import (
	"context"
	"fmt"
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

// T341: ReplyObligationService.OutstandingForIdentity is the detection half of
// the reply-guardrail. directed = DM (all) + @mention (channel/task/issue/plan);
// human AND agent authors both create an obligation (system excluded); discharge
// = an agent reply to the SAME conversation after the trigger; perceived = cursor
// past it OR deliverable ≥ idleGrace.

const (
	obTTL   = time.Hour
	obGrace = 30 * time.Second
)

type oblFixture struct {
	svc      *ReplyObligationService
	rs       *ReadStateService
	convRepo *convsqlite.ConversationRepo
	msgRepo  *convsqlite.MessageRepo
	rsRepo   *convsqlite.ReadStateRepo
	clock    *clock.FakeClock
}

func setupObl(t *testing.T) *oblFixture {
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
	return &oblFixture{
		svc:      NewReplyObligationService(db, conv, rs),
		rs:       NewReadStateService(db, rs, msg, sink, fc),
		convRepo: conv,
		msgRepo:  msg,
		rsRepo:   rs,
		clock:    fc,
	}
}

func (f *oblFixture) seedConv(t *testing.T, id conversation.ConversationID, kind conversation.ConversationKind, org string, participants ...conversation.IdentityRef) {
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

// seedMsg appends a message and returns its posted_at.
func (f *oblFixture) seedMsg(t *testing.T, id conversation.MessageID, conv conversation.ConversationID, sender conversation.IdentityRef, content string) time.Time {
	t.Helper()
	f.clock.Advance(time.Millisecond)
	at := f.clock.Now()
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: id, ConversationID: conv, SenderIdentityID: sender,
		ContentKind: conversation.MessageContentText, Content: content,
		Direction: conversation.DirectionInbound, PostedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.msgRepo.Append(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return at
}

func (f *oblFixture) markSeen(t *testing.T, who conversation.IdentityRef, conv conversation.ConversationID, upto conversation.MessageID) {
	t.Helper()
	if _, err := f.rs.MarkSeen(context.Background(), MarkSeenCommand{
		UserID: who, ConversationID: conv, LastSeenMessageID: upto,
		Actor: observability.Actor(who),
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *oblFixture) outstanding(t *testing.T, agent conversation.IdentityRef, org, name string, now time.Time) []ReplyObligation {
	t.Helper()
	obs, err := f.svc.OutstandingForIdentity(context.Background(), []conversation.IdentityRef{agent}, org, name, obTTL, obGrace, now)
	if err != nil {
		t.Fatal(err)
	}
	return obs
}

const bot = conversation.IdentityRef("agent:bot-1")

// Core bug: a human DM perceived (mark_seen'd) but never replied to is an
// outstanding obligation — mark_seen alone does NOT discharge (§5-③).
func TestObl_HumanDM_MarkSeenNoReply_IsOutstanding(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	at := f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "please look at this")
	f.markSeen(t, bot, "dm-1", "dm-1-m1") // read, but no reply

	obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Second))
	if len(obs) != 1 {
		t.Fatalf("want 1 obligation, got %d", len(obs))
	}
	if obs[0].TriggerMessageID != "dm-1-m1" || obs[0].ActorKind != conversation.ActorKindHuman {
		t.Fatalf("unexpected obligation: %+v", obs[0])
	}
}

// A reply (any agent post to the same conversation after the trigger) discharges.
func TestObl_RepliedAfter_Discharged(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	at := f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "ping")
	f.seedMsg(t, "dm-1-m2", "dm-1", bot, "on it")

	if obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Second)); len(obs) != 0 {
		t.Fatalf("reply should discharge, got %d obligations", len(obs))
	}
}

// agent→agent directed messages ALSO create an obligation (§5-① revised), tagged
// ActorKind=agent so the enforcement layer gates it through the wake-guardrail.
func TestObl_AgentAuthoredMention_IsOutstanding_TaggedAgent(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "ch-1", conversation.ConversationKindChannel, "org-1", bot, "agent:bot-2")
	at := f.seedMsg(t, "ch-1-m1", "ch-1", "agent:bot-2", "hey @Bot can you confirm")

	obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Minute))
	if len(obs) != 1 || obs[0].ActorKind != conversation.ActorKindAgent {
		t.Fatalf("want 1 agent-authored obligation, got %+v", obs)
	}
}

// system-authored messages never create an obligation.
func TestObl_SystemAuthored_NoObligation(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "ch-1", conversation.ConversationKindChannel, "org-1", bot, "user:alice")
	at := f.seedMsg(t, "ch-1-m1", "ch-1", "system", "@Bot system notice")
	if obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Minute)); len(obs) != 0 {
		t.Fatalf("system author should not create obligation, got %d", len(obs))
	}
}

// In a channel only an @mention of the agent is directed; a non-mention is not.
func TestObl_Channel_NonMention_NoObligation(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "ch-1", conversation.ConversationKindChannel, "org-1", bot, "user:alice")
	at := f.seedMsg(t, "ch-1-m1", "ch-1", "user:alice", "general chatter, nobody pinged")
	if obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Minute)); len(obs) != 0 {
		t.Fatalf("non-mention channel message should not create obligation, got %d", len(obs))
	}
}

// A trigger older than ttl is stale and no longer nudged.
func TestObl_TTLExpired_NoObligation(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	at := f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "old ask")
	f.markSeen(t, bot, "dm-1", "dm-1-m1")
	if obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(2*time.Hour)); len(obs) != 0 {
		t.Fatalf("ttl-expired trigger should not create obligation, got %d", len(obs))
	}
}

// Perceived gate: an unread (not mark_seen'd) trigger within idleGrace is not yet
// an obligation (the agent may not have seen it); past idleGrace it is.
func TestObl_PerceivedGrace(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	at := f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "just arrived")
	// within grace, unread → not yet
	if obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(5*time.Second)); len(obs) != 0 {
		t.Fatalf("unread within grace should not create obligation, got %d", len(obs))
	}
	// past grace, still unread → obligation (woken-but-not-acked)
	if obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(obGrace+time.Second)); len(obs) != 1 {
		t.Fatalf("unread past grace should create obligation, got %d", len(obs))
	}
}

// §5-④: a reply to a DIFFERENT conversation does not discharge the original.
func TestObl_CrossDestinationReply_DoesNotDischarge(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "task-1", conversation.ConversationKindTask, "org-1", bot, "user:alice")
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	at := f.seedMsg(t, "task-1-m1", "task-1", "user:alice", "@Bot status?")
	f.seedMsg(t, "dm-1-m1", "dm-1", bot, "replied in DM instead") // wrong destination

	obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Minute))
	if len(obs) != 1 || obs[0].ConversationID != "task-1" {
		t.Fatalf("cross-destination reply must not discharge task-1, got %+v", obs)
	}
}

// When several human messages are unanswered, the obligation points at the LATEST
// one; replying only to an earlier message does not discharge a later one.
func TestObl_LatestUndischarged(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "first")
	f.seedMsg(t, "dm-1-m2", "dm-1", bot, "ack first")
	at := f.seedMsg(t, "dm-1-m3", "dm-1", "user:alice", "second, newer")
	f.markSeen(t, bot, "dm-1", "dm-1-m3")

	obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Second))
	if len(obs) != 1 || obs[0].TriggerMessageID != "dm-1-m3" {
		t.Fatalf("want obligation at latest unanswered dm-1-m3, got %+v", obs)
	}
}

// The result is bounded by MaxObligations even when more conversations owe a reply.
func TestObl_BoundedByMaxObligations(t *testing.T) {
	f := setupObl(t)
	const n = MaxObligations + 5
	var lastAt time.Time
	for i := 0; i < n; i++ {
		cid := conversation.ConversationID(fmt.Sprintf("ch-%03d", i))
		mid := conversation.MessageID(fmt.Sprintf("ch-%03d-m1", i))
		f.seedConv(t, cid, conversation.ConversationKindChannel, "org-1", bot, "user:alice")
		lastAt = f.seedMsg(t, mid, cid, "user:alice", "@Bot reply please")
		f.markSeen(t, bot, cid, mid)
	}
	obs := f.outstanding(t, bot, "org-1", "Bot", lastAt.Add(time.Second))
	if len(obs) != MaxObligations {
		t.Fatalf("want exactly MaxObligations=%d, got %d", MaxObligations, len(obs))
	}
}

// Input validation: missing refs / bad ref / missing org are rejected.
func TestObl_InputValidation(t *testing.T) {
	f := setupObl(t)
	ctx := context.Background()
	now := f.clock.Now()
	if _, err := f.svc.OutstandingForIdentity(ctx, nil, "org-1", "Bot", obTTL, obGrace, now); err == nil {
		t.Fatal("empty refs should error")
	}
	if _, err := f.svc.OutstandingForIdentity(ctx, []conversation.IdentityRef{"not-a-ref"}, "org-1", "Bot", obTTL, obGrace, now); err == nil {
		t.Fatal("invalid ref should error")
	}
	if _, err := f.svc.OutstandingForIdentity(ctx, []conversation.IdentityRef{bot}, "", "Bot", obTTL, obGrace, now); err == nil {
		t.Fatal("empty org should error")
	}
}

// Non-participant / cross-org conversations are excluded.
func TestObl_NonParticipantAndCrossOrg_Excluded(t *testing.T) {
	f := setupObl(t)
	// bot is NOT a participant here
	f.seedConv(t, "dm-x", conversation.ConversationKindDM, "org-1", "agent:other", "user:alice")
	f.seedMsg(t, "dm-x-m1", "dm-x", "user:alice", "not for bot")
	// right participant but different org
	f.seedConv(t, "dm-y", conversation.ConversationKindDM, "org-2", bot, "user:alice")
	at := f.seedMsg(t, "dm-y-m1", "dm-y", "user:alice", "other org")
	f.markSeen(t, bot, "dm-y", "dm-y-m1")

	if obs := f.outstanding(t, bot, "org-1", "Bot", at.Add(time.Minute)); len(obs) != 0 {
		t.Fatalf("non-participant + cross-org must be excluded, got %d", len(obs))
	}
}
