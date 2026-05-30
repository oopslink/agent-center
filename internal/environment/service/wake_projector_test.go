package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
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
	clk       *clock.FakeClock
	ctx       context.Context
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
	proj := NewWakeProjector(WakeProjectorDeps{
		DB:         db,
		WorkItems:  workItems,
		Agents:     agents,
		ControlLog: control,
		Applied:    applied,
		Clock:      clk,
	})
	return &wakeFixture{
		proj: proj, control: control, eventsR: eventsR,
		workItems: workItems, agents: agents, clk: clk, ctx: context.Background(),
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
	pl, err := json.Marshal(map[string]string{
		"conversation_id": convID,
		"owner_ref":       "pm://tasks/" + taskID,
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

func TestWakeProjector_Name(t *testing.T) {
	f := newWakeFixture(t)
	if f.proj.Name() != "conv-agent-wake" {
		t.Fatalf("Name = %q", f.proj.Name())
	}
}
