package wakeguard

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.MaxDepth != 4 || c.CycleN != 3 || c.RatePerMin != 10 || c.CycleWindow != 5*time.Minute {
		t.Errorf("defaults drifted: %+v", c)
	}
}

func TestHumanActor_BypassesAllGates(t *testing.T) {
	g := NewGuard(Config{MaxDepth: 1, CycleWindow: time.Minute, CycleN: 1, RatePerMin: 1, TokenBudget: 0})
	// A chain that would trip depth AND cost AND rate — human bypasses all.
	chain := WakeChain{Depth: 99, TokenBudgetRemaining: 0, ActorKind: ActorHuman}
	for i := 0; i < 5; i++ {
		tr := g.Evaluate("A", "B", chain, t0)
		if !tr.Allowed || tr.Gate != GateNone {
			t.Fatalf("human wake %d denied: gate=%s reason=%s", i, tr.Gate, tr.Reason)
		}
	}
}

func TestDepthGate(t *testing.T) {
	g := NewGuard(Config{MaxDepth: 4, CycleWindow: time.Minute, CycleN: 100, RatePerMin: 100, TokenBudget: 100})
	// depth 3 → next hop is 4 (== max) → allowed.
	if tr := g.Evaluate("A", "B", WakeChain{Depth: 3, TokenBudgetRemaining: 100, ActorKind: ActorAgent}, t0); !tr.Allowed {
		t.Fatalf("depth 3→4 should be allowed: %s", tr.Reason)
	}
	// depth 4 → next hop is 5 (> max) → denied by depth.
	tr := g.Evaluate("A", "C", WakeChain{Depth: 4, TokenBudgetRemaining: 100, ActorKind: ActorAgent}, t0)
	if tr.Allowed || tr.Gate != GateDepth {
		t.Fatalf("depth 4→5 should be denied by depth: allowed=%v gate=%s", tr.Allowed, tr.Gate)
	}
}

func TestCostGate(t *testing.T) {
	g := NewGuard(DefaultConfig())
	tr := g.Evaluate("A", "B", WakeChain{Depth: 0, TokenBudgetRemaining: 0, ActorKind: ActorAgent}, t0)
	if tr.Allowed || tr.Gate != GateCost {
		t.Fatalf("zero budget should be denied by cost: allowed=%v gate=%s", tr.Allowed, tr.Gate)
	}
}

func TestCycleGate_ABA(t *testing.T) {
	// CycleN=3: the pair (A,B) may hop 3 times in the window, the 4th trips cycle.
	g := NewGuard(Config{MaxDepth: 100, CycleWindow: 5 * time.Minute, CycleN: 3, RatePerMin: 100, TokenBudget: 100})
	chain := WakeChain{Depth: 0, TokenBudgetRemaining: 100, ActorKind: ActorAgent}
	// A→B, B→A, A→B all same unordered pair, within the window.
	steps := [][2]string{{"A", "B"}, {"B", "A"}, {"A", "B"}}
	for i, s := range steps {
		if tr := g.Evaluate(s[0], s[1], chain, t0.Add(time.Duration(i)*time.Second)); !tr.Allowed {
			t.Fatalf("hop %d should be allowed: %s", i, tr.Reason)
		}
	}
	// 4th round-trip on the same pair → cycle breaker.
	tr := g.Evaluate("B", "A", chain, t0.Add(3*time.Second))
	if tr.Allowed || tr.Gate != GateCycle {
		t.Fatalf("4th A↔B hop should be denied by cycle: allowed=%v gate=%s", tr.Allowed, tr.Gate)
	}
	// After the window passes, the pair is clear again.
	if tr := g.Evaluate("A", "B", chain, t0.Add(6*time.Minute)); !tr.Allowed {
		t.Errorf("after window the pair should be clear: %s", tr.Reason)
	}
}

func TestRateGate(t *testing.T) {
	// RatePerMin=3: a target may be agent-woken 3×/min; the 4th in the window trips.
	g := NewGuard(Config{MaxDepth: 100, CycleWindow: time.Hour, CycleN: 100, RatePerMin: 3, TokenBudget: 100})
	chain := WakeChain{Depth: 0, TokenBudgetRemaining: 100, ActorKind: ActorAgent}
	// Different senders, SAME target B, within a minute.
	for i, from := range []string{"A", "C", "D"} {
		if tr := g.Evaluate(from, "B", chain, t0.Add(time.Duration(i)*time.Second)); !tr.Allowed {
			t.Fatalf("rate hop %d should be allowed: %s", i, tr.Reason)
		}
	}
	tr := g.Evaluate("E", "B", chain, t0.Add(4*time.Second))
	if tr.Allowed || tr.Gate != GateRate {
		t.Fatalf("4th wake to B in a minute should be denied by rate: allowed=%v gate=%s", tr.Allowed, tr.Gate)
	}
	// A different target is unaffected.
	if tr := g.Evaluate("E", "Z", chain, t0.Add(4*time.Second)); !tr.Allowed {
		t.Errorf("different target should be allowed: %s", tr.Reason)
	}
	// After a minute B's bucket refills.
	if tr := g.Evaluate("E", "B", chain, t0.Add(61*time.Second)); !tr.Allowed {
		t.Errorf("after a minute B should be wakeable again: %s", tr.Reason)
	}
}

func TestExtend(t *testing.T) {
	c := NewRootChain("msg-1", ActorAgent, 16)
	c1 := c.Extend("B")
	if c1.Depth != 1 || c1.TokenBudgetRemaining != 15 || !c1.Members["B"] || c1.RootMessageID != "msg-1" {
		t.Errorf("extend: %+v", c1)
	}
	c2 := c1.Extend("C")
	if c2.Depth != 2 || c2.TokenBudgetRemaining != 14 || !c2.Members["B"] || !c2.Members["C"] {
		t.Errorf("extend 2: %+v", c2)
	}
	// Extending must not mutate the parent's members.
	if c1.Members["C"] {
		t.Errorf("Extend mutated parent members")
	}
}

func TestDenyRecordsNothing(t *testing.T) {
	// A denied wake must not consume rate/cycle budget (only allowed hops do).
	g := NewGuard(Config{MaxDepth: 0, CycleWindow: time.Minute, CycleN: 1, RatePerMin: 1, TokenBudget: 100})
	// depth 0 → next hop 1 > MaxDepth 0 → denied by depth, before rate/cycle.
	tr := g.Evaluate("A", "B", WakeChain{Depth: 0, TokenBudgetRemaining: 100, ActorKind: ActorAgent}, t0)
	if tr.Allowed || tr.Gate != GateDepth {
		t.Fatalf("want depth deny: %+v", tr)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.rate) != 0 || len(g.cycleEdges) != 0 {
		t.Errorf("denied wake recorded state: rate=%v cycle=%v", g.rate, g.cycleEdges)
	}
}
