package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// wakeFixture wires a MessageWriter with the cross-BC outbox attached + an
// outbox repo to assert the emitted wake-trigger events. A shared in-memory DB
// backs all three so the same-tx emission round-trips.
type wakeFixture struct {
	w        *MessageWriter
	convRepo conversation.ConversationRepository
	outboxR  *outboxsql.OutboxRepo
	clk      *clock.FakeClock
	ctx      context.Context
}

func newWakeFixture(t *testing.T) *wakeFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	cr := convsqlite.NewConversationRepo(db)
	mg := convsqlite.NewMessageRepo(db)
	outboxR := outboxsql.NewOutboxRepo(db)
	w := NewMessageWriter(db, cr, mg, sink, gen, fc).WithOutbox(outboxR)
	return &wakeFixture{w: w, convRepo: cr, outboxR: outboxR, clk: fc, ctx: context.Background()}
}

// saveConv builds + saves an active conversation of the given kind/owner_ref.
func (f *wakeFixture) saveConv(t *testing.T, id conversation.ConversationID, kind conversation.ConversationKind, owner conversation.OwnerRef, name string) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:        id,
		Kind:      kind,
		OwnerRef:  owner,
		Name:      name,
		CreatedBy: conversation.IdentityRef("user:alice"),
		OpenedAt:  f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	if err := f.convRepo.Save(f.ctx, c); err != nil {
		t.Fatalf("save conversation: %v", err)
	}
}

// messageAddedEvents returns the unprocessed outbox events of the wake-trigger
// type.
func (f *wakeFixture) messageAddedEvents(t *testing.T) []outbox.Event {
	t.Helper()
	evs, err := f.outboxR.FetchUnprocessed(f.ctx, 100)
	if err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	var out []outbox.Event
	for _, e := range evs {
		if e.EventType == EvtConversationMessageAdded {
			out = append(out, e)
		}
	}
	return out
}

func TestAddMessage_TaskConversation_EmitsWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-task-1")
	f.saveConv(t, convID, conversation.ConversationKindTask, conversation.NewTaskOwnerRef("T1"), "")

	res, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "please continue",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	evs := f.messageAddedEvents(t)
	if len(evs) != 1 {
		t.Fatalf("want 1 wake outbox event, got %d", len(evs))
	}
	p := evs[0].Payload
	if !strings.Contains(p, `"owner_ref":"pm://tasks/T1"`) {
		t.Fatalf("payload missing owner_ref: %s", p)
	}
	if !strings.Contains(p, `"conversation_id":"conv-task-1"`) {
		t.Fatalf("payload missing conversation_id: %s", p)
	}
	if !strings.Contains(p, `"message_id":"`+string(res.MessageID)+`"`) {
		t.Fatalf("payload missing message_id: %s", p)
	}
	if !strings.Contains(p, `"sender":"user:bob"`) {
		t.Fatalf("payload missing sender: %s", p)
	}
	if !strings.Contains(p, `"text":"please continue"`) {
		t.Fatalf("payload missing text: %s", p)
	}
}

// v2.9.1 Thread P2 (§5.1 verify-not-trust — confirm, don't trust): a thread REPLY
// is an ordinary same-conversation message, so it MUST emit the wake-trigger outbox
// event exactly like any message — i.e. @agent-in-thread wakes via the existing
// v2.9 mention-wake mechanism, with no upstream event-emit gap. The emit gate is
// parent-agnostic; this class-guard would catch any regression that made replies
// silently skip the wake.
func TestAddMessage_ThreadReply_EmitsWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-task-thread")
	f.saveConv(t, convID, conversation.ConversationKindTask, conversation.NewTaskOwnerRef("T9"), "")

	root, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID: convID, SenderIdentityID: "user:bob",
		ContentKind: conversation.MessageContentText, Content: "root", Direction: conversation.DirectionInbound,
		Actor: observability.Actor("user:bob"),
	})
	if err != nil {
		t.Fatalf("AddMessage root: %v", err)
	}
	reply, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID: convID, SenderIdentityID: "user:carol",
		ContentKind: conversation.MessageContentText, Content: "@agent ping in thread",
		Direction:       conversation.DirectionInbound,
		ParentMessageID: root.MessageID, // <-- thread reply
		Actor:           observability.Actor("user:carol"),
	})
	if err != nil {
		t.Fatalf("AddMessage reply: %v", err)
	}

	evs := f.messageAddedEvents(t)
	if len(evs) != 2 {
		t.Fatalf("want 2 wake events (root + thread reply), got %d", len(evs))
	}
	found := false
	for _, e := range evs {
		if strings.Contains(e.Payload, `"message_id":"`+string(reply.MessageID)+`"`) {
			found = true
			if !strings.Contains(e.Payload, `"text":"@agent ping in thread"`) {
				t.Fatalf("reply wake payload wrong: %s", e.Payload)
			}
		}
	}
	if !found {
		t.Fatalf("thread reply did not emit a wake event; the @agent-in-thread wake would not fire")
	}
}

// F4: the wake outbox payload of a thread reply must carry root_message_id so the
// woken agent can reply IN the thread (parent=root). A top-level message carries none.
func TestAddMessage_ThreadReply_WakePayloadCarriesRoot(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-task-root")
	f.saveConv(t, convID, conversation.ConversationKindTask, conversation.NewTaskOwnerRef("T7"), "")

	root, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID: convID, SenderIdentityID: "user:bob",
		ContentKind: conversation.MessageContentText, Content: "root", Direction: conversation.DirectionInbound,
		Actor: observability.Actor("user:bob"),
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID: convID, SenderIdentityID: "user:carol",
		ContentKind: conversation.MessageContentText, Content: "in thread", Direction: conversation.DirectionInbound,
		ParentMessageID: root.MessageID, Actor: observability.Actor("user:carol"),
	})
	if err != nil {
		t.Fatal(err)
	}

	var rootEv, replyEv string
	for _, e := range f.messageAddedEvents(t) {
		if strings.Contains(e.Payload, `"message_id":"`+string(root.MessageID)+`"`) {
			rootEv = e.Payload
		}
		if strings.Contains(e.Payload, `"message_id":"`+string(reply.MessageID)+`"`) {
			replyEv = e.Payload
		}
	}
	if !strings.Contains(replyEv, `"root_message_id":"`+string(root.MessageID)+`"`) {
		t.Fatalf("reply wake payload must carry root_message_id=%s, got: %s", root.MessageID, replyEv)
	}
	if strings.Contains(rootEv, "root_message_id") {
		t.Fatalf("top-level root message payload must NOT carry root_message_id, got: %s", rootEv)
	}
}

// v2.7.1 #227: an issue conversation emits the wake event even with NO agent
// participant, so the WakeProjector runs + can auto-join an @mentioned project
// member (chicken-and-egg: without this, the first issue @mention never emits).
func TestAddMessage_IssueConversation_EmitsWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-issue-1")
	f.saveConv(t, convID, conversation.ConversationKindIssue, conversation.OwnerRef("pm://issues/I1"), "")

	if _, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hey @agent please look",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if evs := f.messageAddedEvents(t); len(evs) != 1 {
		t.Fatalf("issue conversation must emit a wake event (#227), got %d", len(evs))
	}
}

// v2.9 #306 ②: a plan conversation emits the wake event even with NO agent
// participant, so the WakeProjector runs + can broaden to an @mentioned project-
// member agent (symmetric with issues #227). Without this, a non-participant
// project-member @mention in a plan conversation never emits → ② can't fire — the
// run-real gap Tester2 caught (converse=0) while ① [participant] worked because a
// participant conv passes the conversationHasAgentParticipant gate.
func TestAddMessage_PlanConversation_EmitsWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-plan-1")
	f.saveConv(t, convID, conversation.ConversationKindPlan, conversation.OwnerRef("pm://plans/P1"), "")

	if _, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hey @agent please look at this plan",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if evs := f.messageAddedEvents(t); len(evs) != 1 {
		t.Fatalf("plan conversation must emit a wake event (#306 ②), got %d", len(evs))
	}
}

func TestAddMessage_ChannelConversation_NoWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-chan-1")
	f.saveConv(t, convID, conversation.ConversationKindChannel,
		conversation.NewOrgOwnerRef("org-1"), "general")

	if _, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hi channel",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if evs := f.messageAddedEvents(t); len(evs) != 0 {
		t.Fatalf("channel conversation must emit NO wake outbox event, got %d", len(evs))
	}
}

func TestAddMessage_DMConversation_NoWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-dm-1")
	// dm: no owner_ref (empty).
	f.saveConv(t, convID, conversation.ConversationKindDM, "", "")

	if _, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hi dm",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if evs := f.messageAddedEvents(t); len(evs) != 0 {
		t.Fatalf("dm conversation must emit NO wake outbox event, got %d", len(evs))
	}
}

// saveConvWithAgent saves a conversation that includes an active agent
// participant (v2.7 #185).
func (f *wakeFixture) saveConvWithAgent(t *testing.T, id conversation.ConversationID, kind conversation.ConversationKind, owner conversation.OwnerRef, name, agentID string) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:        id,
		Kind:      kind,
		OwnerRef:  owner,
		Name:      name,
		CreatedBy: conversation.IdentityRef("user:alice"),
		OpenedAt:  f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	c.SetParticipants([]conversation.ParticipantElement{
		{IdentityID: conversation.IdentityRef("user:alice"), Role: "owner", JoinedAt: "t"},
		{IdentityID: conversation.IdentityRef("agent:" + agentID), Role: "member", JoinedAt: "t"},
	}, f.clk.Now())
	if err := f.convRepo.Save(f.ctx, c); err != nil {
		t.Fatalf("save conversation: %v", err)
	}
}

// v2.7 #185 FINDING-I: a DM WITH an agent participant MUST emit
// conversation.message_added (the prior task-only gate left the WakeProjector's
// DM/channel branch dead → DM/channel→agent never fired). This exercises the
// real AddMessage→emit path the projector unit tests bypassed.
func TestAddMessage_DMWithAgent_EmitsWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-dm-agent")
	f.saveConvWithAgent(t, convID, conversation.ConversationKindDM, "", "", "AG1")

	if _, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hi agent",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if evs := f.messageAddedEvents(t); len(evs) != 1 {
		t.Fatalf("DM-with-agent must emit 1 conversation.message_added (FINDING-I), got %d", len(evs))
	}
}

// A channel WITH an agent participant also emits (the WakeProjector then gates
// on @mention).
func TestAddMessage_ChannelWithAgent_EmitsWakeOutbox(t *testing.T) {
	f := newWakeFixture(t)
	convID := conversation.ConversationID("conv-chan-agent")
	f.saveConvWithAgent(t, convID, conversation.ConversationKindChannel, conversation.NewOrgOwnerRef("org-1"), "general", "AG1")

	if _, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hey @Helper",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if evs := f.messageAddedEvents(t); len(evs) != 1 {
		t.Fatalf("channel-with-agent must emit 1 conversation.message_added, got %d", len(evs))
	}
}

// A nil outbox dep (default) must not emit and must not break AddMessage.
func TestAddMessage_NilOutbox_NoEmitNoError(t *testing.T) {
	f := newWakeFixture(t)
	// Re-wrap the writer WITHOUT the outbox to exercise the nil path.
	f.w.WithOutbox(nil)
	convID := conversation.ConversationID("conv-task-2")
	f.saveConv(t, convID, conversation.ConversationKindTask, conversation.NewTaskOwnerRef("T2"), "")

	if _, err := f.w.AddMessage(f.ctx, AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef("user:bob"),
		ContentKind:      conversation.MessageContentText,
		Content:          "no outbox wired",
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("user:bob"),
	}); err != nil {
		t.Fatalf("AddMessage with nil outbox: %v", err)
	}
	if evs := f.messageAddedEvents(t); len(evs) != 0 {
		t.Fatalf("nil outbox must emit nothing, got %d", len(evs))
	}
}
