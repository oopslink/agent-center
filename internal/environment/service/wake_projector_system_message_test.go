package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/outbox"
)

// quick-fix (@oopslink, system-msg-activity): a system-authored TEXT message that
// @mentions an agent MUST be delivered to it exactly like a normal message — the
// same agent.converse inject that enters the agent's context and records a
// "Received" activity (message_delivered). Before the fix the WakeProjector woke
// ONLY user:/agent: senders, so a system MESSAGE (e.g. a plan "task ready"
// @mention) never entered context and left no receive record. These tests lock in
// the new behavior + its guards (content_kind gate, @mention gate, not-running
// silent-skip). DO NOT weaken back to "system never wakes".

// sysMessageEvent builds a conversation.message_added outbox event for a SYSTEM
// sender with an explicit content_kind (the field that gates text-message vs
// notification-chrome delivery).
func sysMessageEvent(id, convID, msgID, contentKind, text string) outbox.Event {
	pl, err := json.Marshal(map[string]string{
		"conversation_id": convID,
		"message_id":      msgID,
		"sender":          "system",
		"text":            text,
		"content_kind":    contentKind,
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

// saveStoppedAgent persists an agent that is NOT running (never Start()ed), so the
// not-running delivery branch can be exercised.
func (f *wakeFixture) saveStoppedAgent(t *testing.T, agentID, workerID string) {
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
		t.Fatalf("save stopped agent: %v", err)
	}
}

// A system TEXT message @mentioning a running participant agent enqueues an
// agent.converse (delivered like a normal message) — this is the core fix.
func TestWakeProjector_SystemTextMessage_Mention_EnqueuesConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "plan-1", conversation.ConversationKindPlan, "Sprint", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	// PostMention-style system message: "@Helper your task T7 is ready …".
	e := sysMessageEvent("EV1", "plan-1", "m-ready", "text", "@Helper your task T7 is ready — all upstream dependencies are done.")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse for a system text @mention, got %d (%v)", len(cmds), cmds)
	}
	pl := cmds[0].Payload()
	// The converse carries the system sender + the message, and anchors MessageID on
	// the POSTED message id (m-ready) — the same anchor the sanctioned system→agent
	// paths use, so their converse dedups against this one at the ControlLog.
	for _, want := range []string{`"sender_ref":"system"`, `"message_id":"m-ready"`, `your task T7 is ready`} {
		if !strings.Contains(pl, want) {
			t.Errorf("converse payload missing %q: %s", want, pl)
		}
	}
}

// System notification CHROME (content_kind=system, e.g. the "@X is not running"
// notice) must NOT be delivered as a message — only real text messages are.
func TestWakeProjector_SystemChrome_NoConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "plan-1", conversation.ConversationKindPlan, "Sprint", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	// content_kind=system → chrome, even though it names @Helper.
	e := sysMessageEvent("EV1", "plan-1", "m-chrome", "system", "@Helper is not running and won't reply until it is started.")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("system notification chrome must NOT enqueue a converse, got %d", len(cmds))
	}
}

// A system text message with no @mention of the agent in a group-like (plan) conv
// wakes no one — the @mention gate still applies to system senders.
func TestWakeProjector_SystemTextMessage_NoMention_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "plan-1", conversation.ConversationKindPlan, "Sprint", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	e := sysMessageEvent("EV1", "plan-1", "m1", "text", "a system note that pings nobody")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("un-mentioned system message must not wake, got %d", len(cmds))
	}
}

// A system text message targeting a STOPPED agent skips silently — no converse and
// NO "@X is not running" chrome (the durable message stays for the agent's next
// run; the notice is only for human/agent DM/channel senders).
func TestWakeProjector_SystemTextMessage_StoppedAgent_SilentSkip(t *testing.T) {
	f := newWakeFixture(t)
	f.saveStoppedAgent(t, "AG1", "W1")
	f.saveConv(t, "plan-1", conversation.ConversationKindPlan, "Sprint", agentPart("AG1"), userPart("bob"))
	var sysNotes []string
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, &sysNotes)

	e := sysMessageEvent("EV1", "plan-1", "m1", "text", "@Helper your task T7 is ready.")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("stopped agent: system message must not enqueue a converse, got %d", len(cmds))
	}
	if len(sysNotes) != 0 {
		t.Fatalf("stopped agent: system message must NOT post a 'not running' notice, got %v", sysNotes)
	}
}

// A human DM/channel message to a STOPPED agent still posts the "not running"
// notice (unchanged behavior — the silent-skip is system-sender-only).
func TestWakeProjector_HumanMessage_StoppedAgent_StillNotifies(t *testing.T) {
	f := newWakeFixture(t)
	f.saveStoppedAgent(t, "AG1", "W1")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), userPart("bob"))
	var sysNotes []string
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, &sysNotes)

	if err := p.Project(f.ctx, convMessageEvent("EV1", "dm-1", "m1", "user:bob", "hello?")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(sysNotes) != 1 || !strings.Contains(sysNotes[0], "is not running") {
		t.Fatalf("human message to stopped agent must post the not-running notice, got %v", sysNotes)
	}
}
