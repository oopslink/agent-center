package service

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
)

// guardProj builds a WakeProjector from the fixture's repos with a wake-chain
// Guard wired in (the production path injects this singleton; the default fixture
// leaves it nil → ungated).
func (f *wakeFixture) guardProj(g *wakeguard.Guard) *WakeProjector {
	return NewWakeProjector(WakeProjectorDeps{
		DB:         f.db,
		WorkItems:  f.workItems,
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

// TestWakeProjector_WakeGuard_CycleBreaks_ABA (T227) proves the guard is wired
// into the real wake path: an agent A↔B round-trip self-extinguishes once the
// pair trips the cycle gate — the run-real NO-GO this fix closes.
func TestWakeProjector_WakeGuard_CycleBreaks_ABA(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "A", "WA")
	f.saveAgent(t, "B", "WB")
	f.saveWorkItem(t, "wi-a", "A", "pm://tasks/TA", agent.WorkItemWaitingInput)
	f.saveWorkItem(t, "wi-b", "B", "pm://tasks/TB", agent.WorkItemWaitingInput)
	f.saveTaskConv(t, "conv-A", "TA")
	f.saveTaskConv(t, "conv-B", "TB")

	// CycleN=2: the pair (A,B) may hop twice, the 3rd round-trip trips the breaker.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 2, RatePerMin: 100, TokenBudget: 100,
	}))

	// hop 1: A → B's task conversation wakes B.
	mustProject(t, p, f, "EV1", "conv-B", "TB", "m1", "agent:A", "ping")
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("hop1 should wake B once, got %d", got)
	}
	// hop 2: B → A wakes A.
	mustProject(t, p, f, "EV2", "conv-A", "TA", "m2", "agent:B", "pong")
	if got := len(f.commandsFor(t, "WA")); got != 1 {
		t.Fatalf("hop2 should wake A once, got %d", got)
	}
	// hop 3: A → B again — pair (A,B) already has 2 edges in the window → cycle
	// breaker suppresses this wake (B is NOT woken a second time).
	mustProject(t, p, f, "EV3", "conv-B", "TB", "m3", "agent:A", "ping again")
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("hop3 should be suppressed by cycle gate (B stays at 1 wake), got %d", got)
	}
}

// TestWakeProjector_WakeGuard_HumanBypasses (T227) proves a human sender is never
// gated — human @mention/directed wakes must always deliver.
func TestWakeProjector_WakeGuard_HumanBypasses(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "B", "WB")
	f.saveWorkItem(t, "wi-b", "B", "pm://tasks/TB", agent.WorkItemWaitingInput)
	f.saveTaskConv(t, "conv-B", "TB")

	// A guard that would deny everything for agents (rate 0) — humans bypass it.
	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 0, CycleWindow: time.Minute, CycleN: 1, RatePerMin: 0, TokenBudget: 0,
	}))
	mustProject(t, p, f, "EV1", "conv-B", "TB", "m1", "user:alice", "hi B")
	if got := len(f.commandsFor(t, "WB")); got != 1 {
		t.Fatalf("human sender must wake (bypass gates), got %d", got)
	}
}

// TestWakeProjector_WakeGuard_RateLimitsAgent (T227) proves the per-target rate
// gate: a target woken by agents more than Rate/min has the excess suppressed.
func TestWakeProjector_WakeGuard_RateLimitsAgent(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "B", "WB")
	f.saveWorkItem(t, "wi-b", "B", "pm://tasks/TB", agent.WorkItemWaitingInput)
	f.saveTaskConv(t, "conv-B", "TB")

	p := f.guardProj(wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 100, CycleWindow: time.Hour, CycleN: 100, RatePerMin: 2, TokenBudget: 100,
	}))
	// 3 distinct agent senders wake B within a minute; only the first 2 pass.
	mustProject(t, p, f, "EV1", "conv-B", "TB", "m1", "agent:A", "1")
	mustProject(t, p, f, "EV2", "conv-B", "TB", "m2", "agent:C", "2")
	mustProject(t, p, f, "EV3", "conv-B", "TB", "m3", "agent:D", "3")
	if got := len(f.commandsFor(t, "WB")); got != 2 {
		t.Fatalf("rate gate should cap B at 2 agent-wakes/min, got %d", got)
	}
}

func mustProject(t *testing.T, p *WakeProjector, f *wakeFixture, id, convID, taskID, msgID, sender, text string) {
	t.Helper()
	if err := p.Project(f.ctx, messageAddedEvent(id, convID, taskID, msgID, sender, text)); err != nil {
		t.Fatalf("Project %s: %v", id, err)
	}
}
