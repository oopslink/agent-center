package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
	"github.com/oopslink/agent-center/internal/conversation"
)

// T333: an agent @mentioning another agent in a TASK / CHANNEL / ISSUE / PLAN
// conversation must wake the @mentioned agent — previously only DM agent→agent
// wake was plumbed (T289), and the task-conv conversational @mention wake (#220)
// was hard-gated human-only. This extends the agent-sender @mention wake to the
// group-like kinds, ALWAYS through the wake-chain four-gate guard, while keeping
// the guardrails: only an explicit @display_name wakes (never @all from an agent),
// the sender never wakes itself, system senders never wake, and with no guard
// wired the safe human-only default holds.

// guardProjT333 builds a projector with BOTH the wake-chain guard and a
// display-name resolver wired — the combination the group-kind agent→agent
// @mention path needs (the guard authorizes the hop, the resolver matches the
// @display_name). guardProj alone has no resolver; projWith alone has no guard.
func (f *wakeFixture) guardProjT333(g *wakeguard.Guard, displayName map[string]string) *WakeProjector {
	return NewWakeProjector(WakeProjectorDeps{
		DB: f.db, WorkItems: f.workItems, Agents: f.agents,
		ControlLog: f.control, Applied: f.applied, Clock: f.clk,
		ConvRepo: f.convs, MsgRepo: f.msgs, ReadState: f.readState,
		WakeGuard: g,
		DisplayName: func(_ context.Context, ref string) (string, bool) {
			n, ok := displayName[ref]
			return n, ok
		},
	})
}

func genGuard() *wakeguard.Guard {
	return wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 100, RatePerMin: 100, TokenBudget: 100,
	})
}

// (1) agent → agent @mention in a TASK conversation wakes the target once; the
// sender is never woken on its own message.
func TestWakeProjector_T333_Task_AgentMention_WakesTarget(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "task-1", conversation.ConversationKindTask, "", agentPart("AG1"), agentPart("AG2"))
	p := f.guardProjT333(genGuard(), map[string]string{"agent:AG1": "AG1", "agent:AG2": "AG2"})

	ev := messageAddedEventOwner("EV1", "task-1", string(conversation.NewTaskOwnerRef("T1")), "m1", "agent:AG2", "@AG1 please verify")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 1 || c1[0].CommandType() != "agent.converse" {
		t.Fatalf("task @mention should wake AG1 with 1 agent.converse, got %d (%v)", len(c1), c1)
	}
	if c2 := f.commandsFor(t, "W2"); len(c2) != 0 {
		t.Fatalf("sender AG2 must not wake itself, got %d on W2", len(c2))
	}
}

// (2) agent → agent @mention in a CHANNEL wakes the target.
func TestWakeProjector_T333_Channel_AgentMention_WakesTarget(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("AG1"), agentPart("AG2"))
	p := f.guardProjT333(genGuard(), map[string]string{"agent:AG1": "AG1", "agent:AG2": "AG2"})

	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "agent:AG2", "@AG1 heads up")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 1 || c1[0].CommandType() != "agent.converse" {
		t.Fatalf("channel @mention should wake AG1 once, got %d (%v)", len(c1), c1)
	}
}

// (2b) agent → agent @mention in an ISSUE conversation wakes the target.
func TestWakeProjector_T333_Issue_AgentMention_WakesTarget(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", agentPart("AG1"), agentPart("AG2"))
	p := f.guardProjT333(genGuard(), map[string]string{"agent:AG1": "AG1", "agent:AG2": "AG2"})

	if err := p.Project(f.ctx, convMessageEvent("EV1", "iss-1", "m1", "agent:AG2", "@AG1 can you accept this")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 1 || c1[0].CommandType() != "agent.converse" {
		t.Fatalf("issue @mention should wake AG1 once, got %d (%v)", len(c1), c1)
	}
}

// (3) A→B→A ping-pong in a channel self-extinguishes once the pair trips the
// cycle gate — the group-kind agent→agent wake cannot run away.
func TestWakeProjector_T333_Channel_PingPong_CycleBreaks(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "A", "WA")
	f.saveRunningAgent(t, "B", "WB")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("A"), agentPart("B"))
	// CycleN=2: the pair may hop twice; the 3rd round-trip trips the breaker.
	p := f.guardProjT333(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 2, RatePerMin: 100, TokenBudget: 100,
	}), map[string]string{"agent:A": "Alpha", "agent:B": "Beta"})

	// hop 1: A → B wakes B.
	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "agent:A", "@Beta ping")); err != nil {
		t.Fatalf("hop1: %v", err)
	}
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("hop1 should wake B once, got %d", got)
	}
	// hop 2: B → A wakes A.
	if err := p.Project(f.ctx, convMessageEvent("EV2", "chan-1", "m2", "agent:B", "@Alpha pong")); err != nil {
		t.Fatalf("hop2: %v", err)
	}
	if got := len(f.commandsFor(t, "WA")); got != 1 {
		t.Fatalf("hop2 should wake A once, got %d", got)
	}
	// hop 3: A → B again — pair already has 2 edges in the window → suppressed.
	if err := p.Project(f.ctx, convMessageEvent("EV3", "chan-1", "m3", "agent:A", "@Beta again")); err != nil {
		t.Fatalf("hop3: %v", err)
	}
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("hop3 should be suppressed by cycle gate (B stays at 1), got %d", got)
	}
}

// (4) an agent writing @all wakes NO ONE — @all stays human-only even after T333
// opens agent→agent @mention wake to group kinds (an agent cannot broadcast-storm
// the room; only an explicit @display_name wakes a peer).
func TestWakeProjector_T333_Channel_AgentAtAll_WakesNoOne(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveRunningAgent(t, "AG3", "W3")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general",
		agentPart("AG1"), agentPart("AG2"), agentPart("AG3"))
	p := f.guardProjT333(genGuard(), map[string]string{"agent:AG1": "AG1", "agent:AG2": "AG2", "agent:AG3": "AG3"})

	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "agent:AG2", "@all listen up")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("agent @all must wake no one, AG1 got %d", len(c1))
	}
	if c3 := f.commandsFor(t, "W3"); len(c3) != 0 {
		t.Fatalf("agent @all must wake no one, AG3 got %d", len(c3))
	}
}

// (5a) regression: a HUMAN writing @all in a channel still wakes every candidate
// agent (broadcastAll path unchanged for human senders).
func TestWakeProjector_T333_Channel_HumanAtAll_WakesEveryone(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general",
		agentPart("AG1"), agentPart("AG2"), userPart("bob"))
	p := f.guardProjT333(genGuard(), map[string]string{"agent:AG1": "AG1", "agent:AG2": "AG2"})

	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "user:bob", "@all standup now")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 1 {
		t.Fatalf("human @all should wake AG1 once, got %d", len(c1))
	}
	if c2 := f.commandsFor(t, "W2"); len(c2) != 1 {
		t.Fatalf("human @all should wake AG2 once, got %d", len(c2))
	}
}

// (5b) regression: a SYSTEM sender never wakes (the conversational path only
// fires for human or agent senders).
func TestWakeProjector_T333_Channel_SystemSender_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("AG1"))
	p := f.guardProjT333(genGuard(), map[string]string{"agent:AG1": "AG1"})

	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "system", "@AG1 ignored")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("system sender must wake no one, got %d", len(c1))
	}
}

// (6) with NO wake guard wired, an agent→agent @mention in a channel still does
// NOT wake — the safe #185 human-only default holds (no unprotected ping-pong).
func TestWakeProjector_T333_Channel_AgentMention_NoGuard_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("AG1"), agentPart("AG2"))
	p := f.projWith(map[string]string{"agent:AG1": "AG1"}, nil) // no WakeGuard

	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "agent:AG2", "@AG1 heads up")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("no guard wired → agent @mention must not wake, got %d on W1", len(c1))
	}
}

// (6b) the same no-guard default holds on the TASK conversational path: an agent
// @mention in a task conversation with no guard wired wakes no participant.
func TestWakeProjector_T333_Task_AgentMention_NoGuard_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "task-1", conversation.ConversationKindTask, "", agentPart("AG1"), agentPart("AG2"))
	p := f.projWith(map[string]string{"agent:AG1": "AG1"}, nil) // no WakeGuard

	ev := messageAddedEventOwner("EV1", "task-1", string(conversation.NewTaskOwnerRef("T1")), "m1", "agent:AG2", "@AG1 please verify")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("no guard wired → agent task @mention must not wake, got %d on W1", len(c1))
	}
}

// (7) un-mentioned: an agent message in a channel that names no one wakes no one
// (the @display_name gate still applies to agent senders).
func TestWakeProjector_T333_Channel_AgentNoMention_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("AG1"), agentPart("AG2"))
	p := f.guardProjT333(genGuard(), map[string]string{"agent:AG1": "AG1", "agent:AG2": "AG2"})

	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "agent:AG2", "just thinking out loud")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("un-mentioned agent message must not wake, got %d on W1", len(c1))
	}
}
