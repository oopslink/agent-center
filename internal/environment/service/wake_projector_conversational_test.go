package service

// v2.7 #185 (FINDING-H): DM/channel → agent conversational-wake tests.

import (
	"context"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/outbox"
)

// --- pure: @mention token matching ------------------------------------------

func TestMentionTokenPresent(t *testing.T) {
	cases := []struct {
		text, needle string
		want         bool
	}{
		{"hey @bot can you help", "@bot", true},  // bounded by space
		{"@bot", "@bot", true},                   // whole string
		{"ping @bot.", "@bot", true},             // bounded by punctuation
		{"see @bottom shelf", "@bot", false},     // @bot ≠ @bottom (word boundary)
		{"email bot@host", "@bot", false},        // not a leading-@ mention
		{"no mention here", "@bot", false},       // absent
		{"cc @bot and @bot again", "@bot", true}, // multiple
		{"@bot-2 deploy", "@bot-2", true},        // hyphen in name
	}
	for _, c := range cases {
		if got := mentionTokenPresent(c.text, c.needle); got != c.want {
			t.Errorf("mentionTokenPresent(%q,%q)=%v want %v", c.text, c.needle, got, c.want)
		}
	}
}

// --- helpers ----------------------------------------------------------------

// saveRunningAgent persists an agent already in the Running lifecycle (Start()
// from the default Stopped) so the converse path enqueues rather than notifying.
func (f *wakeFixture) saveRunningAgent(t *testing.T, agentID, workerID string) {
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
	if err := a.Start(f.clk.Now()); err != nil {
		t.Fatalf("start agent: %v", err)
	}
	if err := f.agents.Save(f.ctx, a); err != nil {
		t.Fatalf("save running agent: %v", err)
	}
}

// saveConv persists a DM/Channel conversation with the given participants.
func (f *wakeFixture) saveConv(t *testing.T, convID string, kind conversation.ConversationKind, name string, participants ...conversation.ParticipantElement) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID(convID),
		Kind:           kind,
		Name:           name,
		OrganizationID: "org-1",
		CreatedBy:      conversation.IdentityRef("user:alice"),
		OpenedAt:       f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new conv: %v", err)
	}
	c.SetParticipants(participants, f.clk.Now())
	if err := f.convs.Save(f.ctx, c); err != nil {
		t.Fatalf("save conv: %v", err)
	}
}

func agentPart(id string) conversation.ParticipantElement {
	return conversation.ParticipantElement{IdentityID: conversation.IdentityRef("agent:" + id), Role: "member", JoinedAt: "t"}
}
func userPart(id string) conversation.ParticipantElement {
	return conversation.ParticipantElement{IdentityID: conversation.IdentityRef("user:" + id), Role: "member", JoinedAt: "t"}
}

// convMessageEvent builds a conversation.message_added outbox event for a
// NON-task conversation (empty owner_ref → routes to the #185 path).
func convMessageEvent(id, convID, msgID, sender, text string) outbox.Event {
	return messageAddedEventOwner(id, convID, "", msgID, sender, text)
}

// projWith builds a projector over the fixture's repos plus #185 conversational
// deps (display-name resolver + a recording system-notifier).
func (f *wakeFixture) projWith(displayName map[string]string, sysNotes *[]string) *WakeProjector {
	return NewWakeProjector(WakeProjectorDeps{
		DB: f.db, WorkItems: f.workItems, Agents: f.agents,
		ControlLog: f.control, Applied: f.applied, Clock: f.clk,
		ConvRepo: f.convs, MsgRepo: f.msgs, ReadState: f.readState,
		DisplayName: func(_ context.Context, ref string) (string, bool) {
			n, ok := displayName[ref]
			return n, ok
		},
		SystemNotify: func(_ context.Context, convID, text string) error {
			if sysNotes != nil {
				*sysNotes = append(*sysNotes, convID+": "+text)
			}
			return nil
		},
	})
}

// --- DM ---------------------------------------------------------------------

func TestWakeProjector_DM_FromHuman_EnqueuesConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(nil, nil)

	if err := p.Project(f.ctx, convMessageEvent("EV1", "dm-1", "m1", "user:bob", "hello agent")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse, got %d (%v)", len(cmds), cmds)
	}
	pl := cmds[0].Payload()
	for _, want := range []string{`"agent_id":"AG1"`, `"conversation_id":"dm-1"`, `"conv_kind":"dm"`, `"message_text":"hello agent"`} {
		if !strings.Contains(pl, want) {
			t.Errorf("payload missing %s: %s", want, pl)
		}
	}
}

func TestWakeProjector_DM_FromAgent_NoWake_LoopBreak(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	// Two agents in a DM-like conv; an AGENT's message must wake NEITHER (the
	// structural loop-break: only user: senders wake agents).
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), agentPart("AG2"))
	p := f.projWith(nil, nil)

	if err := p.Project(f.ctx, convMessageEvent("EV1", "dm-1", "m1", "agent:AG2", "hi from agent")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("agent-sender must not wake any agent (loop-break), got %d on W1", len(c1))
	}
}

// --- Channel ----------------------------------------------------------------

func TestWakeProjector_Channel_Mention_EnqueuesConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	// @Helper (case-insensitive) → wakes AG1.
	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "user:bob", "hey @helper please look")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse on @mention, got %d", len(cmds))
	}
}

func TestWakeProjector_Channel_NoMention_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	// No @mention → no wake (channel is @mention-gated).
	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "user:bob", "just chatting, nobody pinged")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("un-mentioned channel message must not wake, got %d", len(cmds))
	}
}

// --- stopped agent → visible system notice (no silent black hole) -----------

func TestWakeProjector_DM_StoppedAgent_SystemNotice(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1") // default Stopped (not started)
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), userPart("bob"))
	var notes []string
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, &notes)

	if err := p.Project(f.ctx, convMessageEvent("EV1", "dm-1", "m1", "user:bob", "you there?")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("stopped agent must not get a converse command, got %d", len(cmds))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "dm-1:") || !strings.Contains(notes[0], "not running") {
		t.Fatalf("want a 'not running' system notice on dm-1, got %v", notes)
	}
}
