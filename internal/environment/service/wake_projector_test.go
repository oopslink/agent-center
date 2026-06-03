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
)

type wakeFixture struct {
	proj      *WakeProjector
	control   *environment.ControlLog
	eventsR   *envsql.ControlEventRepo
	workItems *agentsql.WorkItemRepo
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
	workItems := agentsql.NewWorkItemRepo(db)
	agents := agentsql.NewAgentRepo(db)
	convs := convsql.NewConversationRepo(db)
	msgs := convsql.NewMessageRepo(db)
	readState := convsql.NewReadStateRepo(db)
	proj := NewWakeProjector(WakeProjectorDeps{
		DB:         db,
		WorkItems:  workItems,
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
		workItems: workItems, agents: agents,
		convs: convs, msgs: msgs, readState: readState, gen: gen,
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

func awaitingInputEvent(id, agentID, workItemID, taskID, convID string) outbox.Event {
	pl, err := json.Marshal(map[string]string{
		"agent_id":        agentID,
		"work_item_id":    workItemID,
		"task_ref":        "pm://tasks/" + taskID,
		"conversation_id": convID,
	})
	if err != nil {
		panic(err)
	}
	return outbox.Event{
		ID:        id,
		EventType: EvtAgentAwaitingInput,
		Payload:   string(pl),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
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

// saveWorkItem persists an AgentWorkItem on taskRef, advanced to the given
// status (queued→active→waiting_input as needed).
func (f *wakeFixture) saveWorkItem(t *testing.T, id, agentID, taskRef string, status agent.WorkItemStatus) {
	t.Helper()
	wi, err := agent.NewWorkItem(agent.NewWorkItemInput{
		ID: id, AgentID: agent.AgentID(agentID), TaskRef: taskRef, CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new work item: %v", err)
	}
	switch status {
	case agent.WorkItemQueued:
		// already queued
	case agent.WorkItemActive:
		if err := wi.Activate(f.clk.Now()); err != nil {
			t.Fatal(err)
		}
	case agent.WorkItemWaitingInput:
		if err := wi.Activate(f.clk.Now()); err != nil {
			t.Fatal(err)
		}
		if err := wi.WaitInput(f.clk.Now()); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported status %q", status)
	}
	if err := f.workItems.Save(f.ctx, wi); err != nil {
		t.Fatalf("save work item: %v", err)
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

func TestWakeProjector_WaitingInput_EnqueuesWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "user:bob", "please continue")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}

	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 wake command, got %d", len(cmds))
	}
	c := cmds[0]
	if c.CommandType() != "agent.wake" {
		t.Fatalf("command_type = %q, want agent.wake", c.CommandType())
	}
	if c.IdempotencyKey() != "agent.wake:wi-1:msg-1" {
		t.Fatalf("idempotency_key = %q", c.IdempotencyKey())
	}
	if !strings.Contains(c.Payload(), `"agent_id":"AG1"`) {
		t.Fatalf("payload missing agent_id: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"work_item_id":"wi-1"`) {
		t.Fatalf("payload missing work_item_id: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"message_id":"msg-1"`) {
		t.Fatalf("payload missing message_id: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"message_text":"please continue"`) {
		t.Fatalf("payload missing message_text: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"task_ref":"pm://tasks/T1"`) {
		t.Fatalf("payload missing task_ref: %s", c.Payload())
	}
}

func TestWakeProjector_SelfSender_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	// Sender IS the agent owning the WorkItem → self-exclusion.
	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "agent:AG1", "my own question")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("self-sender must enqueue NO wake, got %d", len(cmds))
	}
}

func TestWakeProjector_ActiveWorkItem_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	// active (not waiting_input) → e-i immediate-only, no wake.
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemActive)

	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "user:bob", "hi")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("active WorkItem must not wake (e-i immediate-only), got %d", len(cmds))
	}
}

func TestWakeProjector_IdempotentRedelivery(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "user:bob", "go")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("re-delivery must not duplicate: want 1 command, got %d", len(cmds))
	}
}

func TestWakeProjector_AgentNoWorker_SkipNoError(t *testing.T) {
	f := newWakeFixture(t)
	// No agent saved → FindByID fails → skip + no error (and no command).
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	e := messageAddedEvent("EV1", "conv-1", "T1", "msg-1", "user:bob", "go")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project must not fail on unresolved agent: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("unresolved agent must enqueue nothing, got %d", len(cmds))
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

// --- D2-e-ii batch flush (agent.awaiting_input) ------------------------------

func TestWakeProjector_AwaitingInput_BatchFlushUnread(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	// msg-1, msg-2 (already seen), then unread msg-3(user)/msg-4(agent-self)/msg-5(user).
	_ = f.addMsg(t, "conv-1", "user:bob", "old 1")
	m2 := f.addMsg(t, "conv-1", "user:bob", "old 2")
	m3 := f.addMsg(t, "conv-1", "user:alice", "please continue")
	_ = f.addMsg(t, "conv-1", "agent:AG1", "my own note") // self — excluded
	m5 := f.addMsg(t, "conv-1", "user:carol", "and also this")
	f.setCursor(t, "AG1", "conv-1", m2)

	e := awaitingInputEvent("EV1", "AG1", "wi-1", "T1", "conv-1")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}

	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 batch wake, got %d", len(cmds))
	}
	c := cmds[0]
	if c.IdempotencyKey() != "agent.wake:wi-1:batch:"+m5 {
		t.Fatalf("idempotency_key = %q (want batch:%s)", c.IdempotencyKey(), m5)
	}
	pay := c.Payload()
	if !strings.Contains(pay, "please continue") || !strings.Contains(pay, "and also this") {
		t.Fatalf("merged text missing unread: %s", pay)
	}
	if strings.Contains(pay, "my own note") {
		t.Fatalf("self message must be excluded: %s", pay)
	}
	if strings.Contains(pay, "old 1") || strings.Contains(pay, "old 2") {
		t.Fatalf("already-seen messages must not be re-delivered: %s", pay)
	}
	if !strings.Contains(pay, `"message_id":"`+m5+`"`) {
		t.Fatalf("last delivered id should be %s: %s", m5, pay)
	}
	if !strings.Contains(pay, `"conversation_id":"conv-1"`) {
		t.Fatalf("payload missing conversation_id: %s", pay)
	}
	if !strings.Contains(pay, "[user:alice] please continue") {
		t.Fatalf("merge format should be sender-labeled: %s", pay)
	}
	// ensure m3 came before m5 (posted_at ASC order).
	if strings.Index(pay, m3) > strings.Index(pay, m5) {
		t.Fatalf("merge order not ASC: %s", pay)
	}
}

func TestWakeProjector_AwaitingInput_NoCursor_AllUnread(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	f.addMsg(t, "conv-1", "user:bob", "first")
	last := f.addMsg(t, "conv-1", "user:bob", "second")

	e := awaitingInputEvent("EV1", "AG1", "wi-1", "T1", "conv-1")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("no cursor → all unread; want 1 wake, got %d", len(cmds))
	}
	if cmds[0].IdempotencyKey() != "agent.wake:wi-1:batch:"+last {
		t.Fatalf("idempotency_key = %q", cmds[0].IdempotencyKey())
	}
	if pay := cmds[0].Payload(); !strings.Contains(pay, "first") || !strings.Contains(pay, "second") {
		t.Fatalf("all unread should be delivered: %s", pay)
	}
}

func TestWakeProjector_AwaitingInput_NoUnread_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	last := f.addMsg(t, "conv-1", "user:bob", "seen it")
	f.setCursor(t, "AG1", "conv-1", last) // cursor at the only message → nothing unread

	e := awaitingInputEvent("EV1", "AG1", "wi-1", "T1", "conv-1")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("no unread must enqueue no wake, got %d", len(cmds))
	}
}

func TestWakeProjector_AwaitingInput_OnlySelfUnread_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	f.addMsg(t, "conv-1", "agent:AG1", "self only") // self-excluded → nothing to deliver

	e := awaitingInputEvent("EV1", "AG1", "wi-1", "T1", "conv-1")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("only-self unread must not wake, got %d", len(cmds))
	}
}

func TestWakeProjector_AwaitingInput_NotWaitingInput_Skip(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	// WorkItem is ACTIVE (already woken by an interleaved e-i message).
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemActive)
	f.addMsg(t, "conv-1", "user:bob", "unread but item already active")

	e := awaitingInputEvent("EV1", "AG1", "wi-1", "T1", "conv-1")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("non-waiting_input WorkItem must be skipped, got %d", len(cmds))
	}
}

func TestWakeProjector_AwaitingInput_IdempotentRedelivery(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)
	f.addMsg(t, "conv-1", "user:bob", "go")

	e := awaitingInputEvent("EV1", "AG1", "wi-1", "T1", "conv-1")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("re-delivery must not duplicate batch: want 1, got %d", len(cmds))
	}
}

func TestWakeProjector_Name(t *testing.T) {
	f := newWakeFixture(t)
	if f.proj.Name() != "conv-agent-wake" {
		t.Fatalf("Name = %q", f.proj.Name())
	}
}
