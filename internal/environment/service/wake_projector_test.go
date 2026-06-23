package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

type wakeFixture struct {
	proj      *WakeProjector
	control   *environment.ControlLog
	eventsR   *envsql.ControlEventRepo
	agents    *agentsql.AgentRepo
	convs     *convsql.ConversationRepo
	msgs      *convsql.MessageRepo
	readState *convsql.ReadStateRepo
	gen       idgen.Generator
	clk       *clock.FakeClock
	ctx       context.Context
	db        *sql.DB             // v2.7 #185: for building projector variants in tests
	applied   outbox.AppliedStore // v2.7 #185: ditto
}

func newWakeFixture(t *testing.T) *wakeFixture {
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
	eventsR := envsql.NewControlEventRepo(db)
	control := environment.NewControlLog(eventsR, gen, clk)
	applied := outboxsql.NewAppliedRepo(db)
	agents := agentsql.NewAgentRepo(db)
	convs := convsql.NewConversationRepo(db)
	msgs := convsql.NewMessageRepo(db)
	readState := convsql.NewReadStateRepo(db)
	proj := NewWakeProjector(WakeProjectorDeps{
		DB:         db,
		Agents:     agents,
		ControlLog: control,
		Applied:    applied,
		Clock:      clk,
		ConvRepo:   convs,
		MsgRepo:    msgs,
		ReadState:  readState,
	})
	return &wakeFixture{
		proj: proj, control: control, eventsR: eventsR,
		agents: agents,
		convs:  convs, msgs: msgs, readState: readState, gen: gen,
		clk: clk, ctx: context.Background(),
		db: db, applied: applied,
	}
}

// saveTaskConv persists a task-owned conversation (owner_ref pm://tasks/{taskID}).
func (f *wakeFixture) saveTaskConv(t *testing.T, convID, taskID string) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID(convID),
		Kind:           conversation.ConversationKindTask,
		OwnerRef:       conversation.NewTaskOwnerRef(taskID),
		Name:           "task " + taskID,
		OrganizationID: "org-1",
		CreatedBy:      conversation.IdentityRef("user:alice"),
		OpenedAt:       f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new conv: %v", err)
	}
	if err := f.convs.Save(f.ctx, c); err != nil {
		t.Fatalf("save conv: %v", err)
	}
}

// addMsg appends a message with a deterministic, monotonically-increasing
// posted_at (each call +1s) so ULID ids + posted_at both advance in call order.
func (f *wakeFixture) addMsg(t *testing.T, convID, sender, content string) string {
	t.Helper()
	f.clk.Advance(time.Second)
	id := f.gen.NewULID()
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(id),
		ConversationID:   conversation.ConversationID(convID),
		SenderIdentityID: conversation.IdentityRef(sender),
		ContentKind:      conversation.MessageContentText,
		Content:          content,
		Direction:        conversation.DirectionInbound,
		PostedAt:         f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new msg: %v", err)
	}
	if err := f.msgs.Append(f.ctx, m); err != nil {
		t.Fatalf("append msg: %v", err)
	}
	return id
}

// setCursor seeds the agent participant's read-state cursor at lastSeen.
func (f *wakeFixture) setCursor(t *testing.T, agentID, convID, lastSeen string) {
	t.Helper()
	if err := f.readState.Upsert(f.ctx, &conversation.UserConversationReadState{
		UserID:            conversation.IdentityRef("agent:" + agentID),
		ConversationID:    conversation.ConversationID(convID),
		LastSeenMessageID: conversation.MessageID(lastSeen),
		UpdatedAt:         f.clk.Now(),
	}); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
}

func (f *wakeFixture) commandsFor(t *testing.T, workerID string) []*environment.WorkerControlEvent {
	t.Helper()
	cmds, err := f.control.CommandsAfter(f.ctx, environment.WorkerID(workerID), 0)
	if err != nil {
		t.Fatalf("CommandsAfter: %v", err)
	}
	return cmds
}

// saveAgent persists an Agent bound to workerID.
func (f *wakeFixture) saveAgent(t *testing.T, agentID, workerID string) {
	t.Helper()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID:             agent.AgentID(agentID),
		OrganizationID: "org-1",
		Profile:        agent.Profile{Name: "A " + agentID},
		WorkerID:       workerID,
		CreatedBy:      agent.IdentityRef("user:alice"),
		CreatedAt:      f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	if err := f.agents.Save(f.ctx, a); err != nil {
		t.Fatalf("save agent: %v", err)
	}
}

// planCreatorWakeEvent builds an EvtPlanCreatorFailureWake outbox event (v2.9 P3).
func planCreatorWakeEvent(id, creatorRef, convID, msgID, planID, taskID string) outbox.Event {
	pl, err := json.Marshal(map[string]string{
		"creator_ref":     creatorRef,
		"conversation_id": convID,
		"message_id":      msgID,
		"plan_id":         planID,
		"task_id":         taskID,
		"organization_id": "org-1",
	})
	if err != nil {
		panic(err)
	}
	return outbox.Event{
		ID:        id,
		EventType: pmservice.EvtPlanCreatorFailureWake,
		Payload:   string(pl),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

func messageAddedEvent(id, convID, taskID, msgID, sender, text string) outbox.Event {
	return messageAddedEventOwner(id, convID, "pm://tasks/"+taskID, msgID, sender, text)
}

// messageAddedEventOwner builds a message_added event with an explicit owner_ref
// (v2.7 #185: empty/non-task owner_ref routes to the DM/channel path).
func messageAddedEventOwner(id, convID, ownerRef, msgID, sender, text string) outbox.Event {
	pl, err := json.Marshal(map[string]string{
		"conversation_id": convID,
		"owner_ref":       ownerRef,
		"message_id":      msgID,
		"sender":          sender,
		"text":            text,
	})
	if err != nil {
		panic(err)
	}
	return outbox.Event{
		ID:        id,
		EventType: convservice.EvtConversationMessageAdded,
		Payload:   string(pl),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

// F4 (v2.14.0 F7): a human @mention on a TASK conversation drives the
// conversational wake (the AgentWorkItem-keyed task wake was removed). When the
// triggering message is a thread reply, its root_message_id must propagate through
// the projector into the agent.converse command payload so the agent replies
// in-thread.
func TestWakeProjector_ThreadReply_CarriesRootIntoConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "conv-1", conversation.ConversationKindTask, "task T1", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	pl, _ := json.Marshal(map[string]string{
		"conversation_id": "conv-1",
		"owner_ref":       "pm://tasks/T1",
		"message_id":      "msg-reply",
		"sender":          "user:bob",
		"text":            "@Helper in thread",
		"root_message_id": "msg-root",
	})
	e := outbox.Event{ID: "EVF4", EventType: convservice.EvtConversationMessageAdded, Payload: string(pl), CreatedAt: time.Unix(1_700_000_000, 0).UTC()}
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse command, got %d (%v)", len(cmds), cmds)
	}
	if !strings.Contains(cmds[0].Payload(), `"root_message_id":"msg-root"`) {
		t.Fatalf("converse command must carry root_message_id, got: %s", cmds[0].Payload())
	}
}

// A human @mention on a TASK conversation wakes the participant agent with an
// agent.converse (the surviving conversational wake; the WorkItem task wake is gone).
func TestWakeProjector_TaskConv_Mention_EnqueuesConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "conv-1", conversation.ConversationKindTask, "task T1", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "user:bob", "@Helper please continue")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse on task-conv @mention, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0].Payload(), `"conv_kind":"task"`) {
		t.Fatalf("payload missing conv_kind=task: %s", cmds[0].Payload())
	}
}

// An AGENT sender on a TASK conversation wakes no one (the #185 human-only
// loop-break holds for group-like kinds; only DMs open the guarded agent path).
func TestWakeProjector_TaskConv_AgentSender_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "conv-1", conversation.ConversationKindTask, "task T1", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "agent:AG1", "@Helper my own note")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("agent sender on a task conv must enqueue NO wake, got %d", len(cmds))
	}
}

func TestWakeProjector_TaskConv_IdempotentRedelivery(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "conv-1", conversation.ConversationKindTask, "task T1", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "user:bob", "@Helper go")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("re-delivery must not duplicate: want 1 command, got %d", len(cmds))
	}
}

func TestWakeProjector_NonTaskOwner_NoOp(t *testing.T) {
	f := newWakeFixture(t)
	e := outbox.Event{
		ID:        "EV1",
		EventType: convservice.EvtConversationMessageAdded,
		Payload:   `{"conversation_id":"c","owner_ref":"id://organizations/org-1","message_id":"m","sender":"user:bob","text":"hi"}`,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("non-task owner must be a no-op, got %d", len(cmds))
	}
}

func TestWakeProjector_UnknownEvent_NoOp(t *testing.T) {
	f := newWakeFixture(t)
	e := outbox.Event{ID: "EV1", EventType: "some.other.event", Payload: `{}`}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("unknown event must be a no-op, got %d", len(cmds))
	}
}

// --- v2.9 P3 plan-creator-wake (pm.plan.creator_failure_wake) -----------------

// TestWakeProjector_PlanCreatorWake_EnqueuesConverse is the P3 headline: an
// EvtPlanCreatorFailureWake event → the WakeProjector enqueues ONE agent.converse
// for the agent-creator on the plan conversation, keyed by the failure @mention id
// (so a replay never double-wakes). This is the DIRECT system wake the orchestrator
// triggers because a system @mention can never wake an agent (#220 / #185).
func TestWakeProjector_PlanCreatorWake_EnqueuesConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")

	e := planCreatorWakeEvent("EVW1", "agent:BOT", "plan-conv-1", "failmsg-1", "plan-1", "T9")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}

	cmds := f.commandsFor(t, "W7")
	if len(cmds) != 1 {
		t.Fatalf("want 1 converse command, got %d", len(cmds))
	}
	c := cmds[0]
	if c.CommandType() != "agent.converse" {
		t.Fatalf("command_type = %q, want agent.converse", c.CommandType())
	}
	if c.IdempotencyKey() != "agent.converse:plan-conv-1:failmsg-1:BOT" {
		t.Fatalf("idempotency_key = %q, want agent.converse:plan-conv-1:failmsg-1:BOT", c.IdempotencyKey())
	}
	if !strings.Contains(c.Payload(), `"agent_id":"BOT"`) {
		t.Fatalf("payload missing agent_id: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"conversation_id":"plan-conv-1"`) {
		t.Fatalf("payload missing conversation_id: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"message_id":"failmsg-1"`) {
		t.Fatalf("payload missing failure message_id: %s", c.Payload())
	}
}

// TestWakeProjector_PlanCreatorWake_ReplayOnce feeds the SAME wake event twice →
// exactly ONE agent.converse (the same-tx AppliedStore dedups the redelivery, and
// the converse idempotency key would dedup at the ControlLog regardless).
func TestWakeProjector_PlanCreatorWake_ReplayOnce(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")

	e := planCreatorWakeEvent("EVW1", "agent:BOT", "plan-conv-1", "failmsg-1", "plan-1", "T9")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2: %v", err)
	}
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 1 {
		t.Fatalf("replay must not duplicate the converse: want 1, got %d", len(cmds))
	}
}

// TestWakeProjector_PlanCreatorWake_StoppedAgent_NoConverse asserts a STOPPED
// agent-creator is skipped (no converse, no error) — the failure @mention already
// sits in the plan conversation for it to read when it next runs.
func TestWakeProjector_PlanCreatorWake_StoppedAgent_NoConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "BOT", "W7") // NewAgent → LifecycleStopped

	e := planCreatorWakeEvent("EVW1", "agent:BOT", "plan-conv-1", "failmsg-1", "plan-1", "T9")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project must not fail on a stopped agent: %v", err)
	}
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 0 {
		t.Fatalf("stopped agent-creator must enqueue no converse, got %d", len(cmds))
	}
}

// TestWakeProjector_PlanCreatorWake_UnresolvedAgent_NoError asserts an unresolvable
// agent-creator is skipped (no converse, no error) — a missing wake target must not
// stall the projector.
func TestWakeProjector_PlanCreatorWake_UnresolvedAgent_NoError(t *testing.T) {
	f := newWakeFixture(t)
	// No agent saved → resolveAgent fails → skip + no error.
	e := planCreatorWakeEvent("EVW1", "agent:GHOST", "plan-conv-1", "failmsg-1", "plan-1", "T9")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project must not fail on an unresolved agent-creator: %v", err)
	}
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 0 {
		t.Fatalf("unresolved agent-creator must enqueue nothing, got %d", len(cmds))
	}
}

func TestWakeProjector_Name(t *testing.T) {
	f := newWakeFixture(t)
	if f.proj.Name() != "conv-agent-wake" {
		t.Fatalf("Name = %q", f.proj.Name())
	}
}
