package service

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
	"github.com/oopslink/agent-center/internal/conversation"
)

// guardProj builds a WakeProjector from the fixture's repos with a wake-chain
// Guard wired in (the production path injects this singleton; the default fixture
// leaves it nil → ungated).
func (f *wakeFixture) guardProj(g *wakeguard.Guard) *WakeProjector {
	return NewWakeProjector(WakeProjectorDeps{
		DB:         f.db,
		Agents:     f.agents,
		ControlLog: f.control,
		Applied:    f.applied,
		Clock:      f.clk,
		ConvRepo:   f.convs,
		MsgRepo:    f.msgs,
		ReadState:  f.readState,
		WakeGuard:  g,
	})
}

// dmHop projects an agent-sender DM message into convID. v2.14.0 F7: the
// AgentWorkItem-keyed task wake was removed, so the wake-chain guard is now
// exercised over the surviving agent→agent DM wake path (T289) — an agent posting
// to a DM wakes its peer, gated by the four-gate circuit breaker.
func (f *wakeFixture) dmHop(t *testing.T, p *WakeProjector, id, convID, msgID, sender, text string) {
	t.Helper()
	if err := p.Project(f.ctx, convMessageEvent(id, convID, msgID, sender, text)); err != nil {
		t.Fatalf("Project %s: %v", id, err)
	}
}

// TestWakeProjector_WakeGuard_CycleBreaks_ABA (T227) proves the guard is wired
// into the real wake path: an agent A↔B round-trip self-extinguishes once the
// pair trips the cycle gate — the run-real NO-GO this fix closes.
func TestWakeProjector_WakeGuard_CycleBreaks_ABA(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "A", "WA")
	f.saveRunningAgent(t, "B", "WB")
	f.saveConv(t, "dm-AB", conversation.ConversationKindDM, "", agentPart("A"), agentPart("B"))

	// CycleN=2: the pair (A,B) may hop twice, the 3rd round-trip trips the breaker.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 2, RatePerMin: 100, TokenBudget: 100,
	}))

	// hop 1: A → DM wakes B.
	f.dmHop(t, p, "EV1", "dm-AB", "m1", "agent:A", "ping")
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("hop1 should wake B once, got %d", got)
	}
	// hop 2: B → DM wakes A.
	f.dmHop(t, p, "EV2", "dm-AB", "m2", "agent:B", "pong")
	if got := len(f.commandsFor(t, "WA")); got != 1 {
		t.Fatalf("hop2 should wake A once, got %d", got)
	}
	// hop 3: A → B again — pair (A,B) already has 2 edges in the window → cycle
	// breaker suppresses this wake (B is NOT woken a second time).
	f.dmHop(t, p, "EV3", "dm-AB", "m3", "agent:A", "ping again")
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("hop3 should be suppressed by cycle gate (B stays at 1 wake), got %d", got)
	}
}

// TestWakeProjector_WakeGuard_HumanBypasses (T227) proves a human sender is never
// gated — human @mention/directed wakes must always deliver.
func TestWakeProjector_WakeGuard_HumanBypasses(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "B", "WB")
	f.saveConv(t, "dm-B", conversation.ConversationKindDM, "", agentPart("B"), userPart("alice"))

	// A guard that would deny everything for agents (rate 0) — humans bypass it.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 0, CycleWindow: time.Minute, CycleN: 1, RatePerMin: 0, TokenBudget: 0,
	}))
	f.dmHop(t, p, "EV1", "dm-B", "m1", "user:alice", "hi B")
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("human sender must wake (bypass gates), got %d", got)
	}
}

// TestWakeProjector_WakeGuard_RateLimitsAgent (T227) proves the per-target rate
// gate: a target woken by agents more than Rate/min has the excess suppressed.
func TestWakeProjector_WakeGuard_RateLimitsAgent(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "B", "WB")
	f.saveRunningAgent(t, "A", "WA")
	f.saveRunningAgent(t, "C", "WC")
	f.saveRunningAgent(t, "D", "WD")
	// B shares a DM with each of A, C, D — each agent-sender wakes B.
	f.saveConv(t, "dm-AB", conversation.ConversationKindDM, "", agentPart("A"), agentPart("B"))
	f.saveConv(t, "dm-CB", conversation.ConversationKindDM, "", agentPart("C"), agentPart("B"))
	f.saveConv(t, "dm-DB", conversation.ConversationKindDM, "", agentPart("D"), agentPart("B"))

	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: time.Hour, CycleN: 100, RatePerMin: 2, TokenBudget: 100,
	}))
	// 3 distinct agent senders wake B within a minute; only the first 2 pass.
	f.dmHop(t, p, "EV1", "dm-AB", "m1", "agent:A", "1")
	f.dmHop(t, p, "EV2", "dm-CB", "m2", "agent:C", "2")
	f.dmHop(t, p, "EV3", "dm-DB", "m3", "agent:D", "3")
	if got := len(f.commandsFor(t, "WB")); got != 2 {
		t.Fatalf("rate gate should cap B at 2 agent-wakes/min, got %d", got)
	}
}

// TestWakeProjector_WakeGuard_DepthBreaks_Chain (T227) proves the depth ① gate
// fires on the REAL wake path: a non-repeating chain A→B→C→D→E grows depth across
// deliveries (the Guard carries the chain per agent), so the hop past MaxDepth is
// suppressed — the gap the pre-fix per-wake-root wiring left open (depth was always 0).
func TestWakeProjector_WakeGuard_DepthBreaks_Chain(t *testing.T) {
	f := newWakeFixture(t)
	for _, id := range []string{"A", "B", "C", "D", "E"} {
		f.saveRunningAgent(t, id, "W"+id)
	}
	// DM links along the chain: A→B, B→C, C→D, D→E.
	f.saveConv(t, "dm-AB", conversation.ConversationKindDM, "", agentPart("A"), agentPart("B"))
	f.saveConv(t, "dm-BC", conversation.ConversationKindDM, "", agentPart("B"), agentPart("C"))
	f.saveConv(t, "dm-CD", conversation.ConversationKindDM, "", agentPart("C"), agentPart("D"))
	f.saveConv(t, "dm-DE", conversation.ConversationKindDM, "", agentPart("D"), agentPart("E"))

	// MaxDepth=3: hops to depth 1,2,3 pass; depth 4 trips. Cycle/rate/budget held high.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 3, CycleWindow: 5 * time.Minute, CycleN: 100, RatePerMin: 100, TokenBudget: 100,
	}))
	// A→B (depth1), B→C (depth2), C→D (depth3) — all delivered.
	f.dmHop(t, p, "EV1", "dm-AB", "m1", "agent:A", "to B")
	f.dmHop(t, p, "EV2", "dm-BC", "m2", "agent:B", "to C")
	f.dmHop(t, p, "EV3", "dm-CD", "m3", "agent:C", "to D")
	for _, w := range []string{"WB", "WC", "WD"} {
		if got := len(f.commandsFor(t, w)); got != 1 {
			t.Fatalf("%s should be woken once along the chain, got %d", w, got)
		}
	}
	// D→E would be depth 4 > MaxDepth 3 → depth gate suppresses it (E not woken).
	f.dmHop(t, p, "EV4", "dm-DE", "m4", "agent:D", "to E")
	if got := len(f.commandsFor(t, "WE")); got != 0 {
		t.Fatalf("D→E (depth 4) must be suppressed by depth gate, E woken %d times", got)
	}
}

// TestWakeProjector_WakeGuard_CostBreaks_Chain (T227) proves the cost ④ gate fires
// on the real path: a per-chain token budget is spent down across hops, so a chain
// longer than the budget self-extinguishes even with no repeated pair.
func TestWakeProjector_WakeGuard_CostBreaks_Chain(t *testing.T) {
	f := newWakeFixture(t)
	for _, id := range []string{"A", "B", "C", "D"} {
		f.saveRunningAgent(t, id, "W"+id)
	}
	f.saveConv(t, "dm-AB", conversation.ConversationKindDM, "", agentPart("A"), agentPart("B"))
	f.saveConv(t, "dm-BC", conversation.ConversationKindDM, "", agentPart("B"), agentPart("C"))
	f.saveConv(t, "dm-CD", conversation.ConversationKindDM, "", agentPart("C"), agentPart("D"))

	// TokenBudget=2: root→B spends to 1, B→C to 0, C→D denied by cost. Depth cap high.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 100, RatePerMin: 100, TokenBudget: 2,
	}))
	f.dmHop(t, p, "EV1", "dm-AB", "m1", "agent:A", "to B")
	f.dmHop(t, p, "EV2", "dm-BC", "m2", "agent:B", "to C")
	for _, w := range []string{"WB", "WC"} {
		if got := len(f.commandsFor(t, w)); got != 1 {
			t.Fatalf("%s should be woken once before the budget runs out, got %d", w, got)
		}
	}
	// C carries budget 0 → C→D denied by cost (D not woken).
	f.dmHop(t, p, "EV3", "dm-CD", "m3", "agent:C", "to D")
	if got := len(f.commandsFor(t, "WD")); got != 0 {
		t.Fatalf("C→D (budget exhausted) must be suppressed by cost gate, D woken %d times", got)
	}
}
