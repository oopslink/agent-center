package service

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
)

// T289: agent↔agent DM wake. The send side shipped in T291 but the wake side was
// never plumbed — projectConversationMessage hard-returned for every non-user:
// sender, so an agent's DM never woke its peer. The fix lets an AGENT sender wake
// its DM peer, but ONLY when the wake-chain four-gate guard is wired (the circuit
// breaker is what makes agent→agent waking safe) and ONLY in a DM kind.

// A guarded agent→agent DM wakes the peer exactly once, and never wakes the sender
// itself (self-exclusion).
func TestWakeProjector_DM_AgentToAgent_Guarded_WakesPeer(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), agentPart("AG2"))
	// Generous limits — the first hop always passes.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 2, RatePerMin: 100, TokenBudget: 100,
	}))

	if err := p.Project(f.ctx, convMessageEvent("EV1", "dm-1", "m1", "agent:AG2", "ping peer")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	// AG1 (the peer) is woken once.
	if c1 := f.commandsFor(t, "W1"); len(c1) != 1 || c1[0].CommandType() != "agent.converse" {
		t.Fatalf("peer AG1 should get 1 agent.converse, got %d (%v)", len(c1), c1)
	}
	// AG2 (the sender) is NOT woken on its own message (self-exclusion).
	if c2 := f.commandsFor(t, "W2"); len(c2) != 0 {
		t.Fatalf("sender AG2 must not wake itself, got %d on W2", len(c2))
	}
}

// Without a wake guard wired, an agent→agent DM still does NOT wake (the safe
// #185 default holds — no unprotected ping-pong). Complements the guarded case.
func TestWakeProjector_DM_AgentToAgent_NoGuard_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), agentPart("AG2"))
	p := f.projWith(nil, nil) // no WakeGuard

	if err := p.Project(f.ctx, convMessageEvent("EV1", "dm-1", "m1", "agent:AG2", "ping peer")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("no guard wired → agent DM must not wake, got %d on W1", len(c1))
	}
}

// An A↔B DM ping-pong self-extinguishes once the pair trips the cycle gate, exactly
// like the task-path circuit breaker — the wake side cannot run away.
func TestWakeProjector_DM_AgentPingPong_CycleBreaks(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), agentPart("AG2"))
	// CycleN=2: the pair may hop twice; the 3rd round-trip trips the breaker.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 2, RatePerMin: 100, TokenBudget: 100,
	}))

	// hop 1: AG2 → AG1 wakes AG1.
	if err := p.Project(f.ctx, convMessageEvent("EV1", "dm-1", "m1", "agent:AG2", "ping")); err != nil {
		t.Fatalf("hop1: %v", err)
	}
	if got := len(f.commandsFor(t, "W1")); got != 1 {
		t.Fatalf("hop1 should wake AG1 once, got %d", got)
	}
	// hop 2: AG1 → AG2 wakes AG2.
	if err := p.Project(f.ctx, convMessageEvent("EV2", "dm-1", "m2", "agent:AG1", "pong")); err != nil {
		t.Fatalf("hop2: %v", err)
	}
	if got := len(f.commandsFor(t, "W2")); got != 1 {
		t.Fatalf("hop2 should wake AG2 once, got %d", got)
	}
	// hop 3: AG2 → AG1 again — pair already has 2 edges in the window → suppressed.
	if err := p.Project(f.ctx, convMessageEvent("EV3", "dm-1", "m3", "agent:AG2", "ping again")); err != nil {
		t.Fatalf("hop3: %v", err)
	}
	if got := len(f.commandsFor(t, "W1")); got != 1 {
		t.Fatalf("hop3 should be suppressed by cycle gate (AG1 stays at 1), got %d", got)
	}
}

// Minimal surface: even with a guard wired, an AGENT sender in a CHANNEL wakes no
// one (T289 only opens the DM path). The #185 loop-break still holds for groups.
func TestWakeProjector_Channel_AgentSender_Guarded_StillNoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveRunningAgent(t, "AG2", "W2")
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general", agentPart("AG1"), agentPart("AG2"))
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 100, RatePerMin: 100, TokenBudget: 100,
	}))

	if err := p.Project(f.ctx, convMessageEvent("EV1", "chan-1", "m1", "agent:AG2", "@AG1 heads up")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c1 := f.commandsFor(t, "W1"); len(c1) != 0 {
		t.Fatalf("agent sender in a channel must wake no one, got %d on W1", len(c1))
	}
}
