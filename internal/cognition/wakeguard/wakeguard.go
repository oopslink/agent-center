// Package wakeguard is the Cognition wake-chain circuit breaker (I7-D1). It
// carries a wake_chain across the agent.wake delivery path and applies four
// independent gates so a runaway agent→agent wake storm self-extinguishes:
//
//	① depth      — chain.Depth > MaxDepth
//	② cycle      — a pair (A,B) round-trips ≥ CycleN within CycleWindow
//	③ rate       — a target agent is woken-by-agent more than Rate per minute
//	④ cost       — a chain's token budget is exhausted (children inherit + decrement)
//
// A HUMAN actor (actor_kind=human, e.g. a person @mentioning an agent) BYPASSES
// all four gates — human intent must always be delivered. Only agent→agent wakes
// are gated. Every decision (allow / which gate broke it) is returned as a Trace
// for observability.
//
// The gate STATE (rate buckets, cycle edges) is runtime, in-memory, and updated
// only when a wake is ALLOWED. The package is clock-injected (no time.Now) so the
// gates are deterministically testable.
package wakeguard

import (
	"sync"
	"time"
)

// ActorKind labels who originated the wake at the root of the chain.
type ActorKind string

const (
	ActorHuman ActorKind = "human"
	ActorAgent ActorKind = "agent"
)

// Gate names the breaker that denied a wake ("" when allowed).
type Gate string

const (
	GateNone  Gate = ""
	GateDepth Gate = "depth"
	GateCycle Gate = "cycle"
	GateRate  Gate = "rate"
	GateCost  Gate = "cost"
)

// WakeChain travels with a wake through the delivery path (I7-D1). It is a value
// object: each child wake derives a new chain from its parent (see Extend).
type WakeChain struct {
	RootMessageID        string          // the message that started the chain
	Depth                int             // 0 at the root; +1 per hop
	Members              map[string]bool // agentIds already in the chain (for membership/observability)
	TokenBudgetRemaining int             // cost budget; decremented per hop
	ActorKind            ActorKind       // human → bypass gates; agent → gated
}

// NewRootChain builds the chain for a freshly-originated wake.
func NewRootChain(rootMessageID string, actor ActorKind, tokenBudget int) WakeChain {
	return WakeChain{
		RootMessageID:        rootMessageID,
		Depth:                0,
		Members:              map[string]bool{},
		TokenBudgetRemaining: tokenBudget,
		ActorKind:            actor,
	}
}

// Extend derives the child chain for a wake that `to` will now run as a result of
// this delivery: depth+1, `to` added to members, budget decremented by one hop.
// Call this only after a wake is ALLOWED.
func (c WakeChain) Extend(to string) WakeChain {
	members := make(map[string]bool, len(c.Members)+1)
	for k := range c.Members {
		members[k] = true
	}
	members[to] = true
	return WakeChain{
		RootMessageID:        c.RootMessageID,
		Depth:                c.Depth + 1,
		Members:              members,
		TokenBudgetRemaining: c.TokenBudgetRemaining - 1,
		ActorKind:            c.ActorKind,
	}
}

// Config is the (center-settings-backed) gate configuration. Zero value is NOT
// valid — use DefaultConfig and override. All durations/counts are conservative
// by default (I7-D1).
type Config struct {
	MaxDepth    int           // ① max chain depth
	CycleWindow time.Duration // ② rolling window for round-trip detection
	CycleN      int           // ② round-trips within the window that trip the breaker
	RatePerMin  int           // ③ per-agent woken-by-agent tokens per minute
	TokenBudget int           // ④ per-chain cost budget (root)
}

// DefaultConfig returns the conservative defaults (I7-D1): depth 4, 5min/3,
// 10/min, budget 16.
func DefaultConfig() Config {
	return Config{
		MaxDepth:    4,
		CycleWindow: 5 * time.Minute,
		CycleN:      3,
		RatePerMin:  10,
		TokenBudget: 16,
	}
}

// Trace is the per-decision observability record (I7-D1 §5).
type Trace struct {
	From      string
	To        string
	Depth     int
	ActorKind ActorKind
	Allowed   bool
	Gate      Gate // the breaker that denied it (GateNone when allowed)
	Reason    string
}

// Guard evaluates wakes against the four gates. It is safe for concurrent use.
type Guard struct {
	cfgFn func() Config // resolves the live config per evaluation (settings-backed)

	mu         sync.Mutex
	rate       map[string][]time.Time  // to → recent allowed agent-wake times
	cycleEdges map[string][]time.Time  // unordered "A|B" pair → recent hop times
	carried    map[string]carriedChain // agentId → the chain it propagates onward (EvaluateHop)
}

// carriedChain is the chain an agent received when it was last woken, plus when —
// so EvaluateHop can expire a stale carry (older than CycleWindow) back to a root.
type carriedChain struct {
	chain WakeChain
	at    time.Time
}

// NewGuard builds a Guard with a FIXED config (the static path: tests + any
// caller that doesn't tune thresholds at runtime).
func NewGuard(cfg Config) *Guard {
	return NewGuardFunc(func() Config { return cfg })
}

// NewGuardFunc builds a Guard whose config is resolved by cfgFn on EVERY
// evaluation — the I7-D1/D3 seam: wire cfgFn to read the settings store so a
// Settings-UI change takes effect WITHOUT a restart (T224 "参数可配生效"). cfgFn
// must return a valid (all-positive) Config; nil falls back to DefaultConfig.
func NewGuardFunc(cfgFn func() Config) *Guard {
	if cfgFn == nil {
		cfgFn = DefaultConfig
	}
	return &Guard{
		cfgFn:      cfgFn,
		rate:       map[string][]time.Time{},
		cycleEdges: map[string][]time.Time{},
		carried:    map[string]carriedChain{},
	}
}

// RootChain mints a fresh root chain seeded with the Guard's currently-configured
// token budget — used at a wake origin (where no parent chain is carried yet).
func (g *Guard) RootChain(rootMessageID string, actor ActorKind) WakeChain {
	return NewRootChain(rootMessageID, actor, g.cfgFn().TokenBudget)
}

// Evaluate decides whether `from` may wake `to` carrying `chain`, as of `now`.
// A human-actor chain bypasses all gates. On ALLOW it records the rate/cycle
// state (so the next call sees this hop). On DENY it records nothing (the wake
// did not happen). Returns the Trace either way.
func (g *Guard) Evaluate(from, to string, chain WakeChain, now time.Time) Trace {
	tr := Trace{From: from, To: to, Depth: chain.Depth, ActorKind: chain.ActorKind}

	// Human intent always delivers (I7-D1 §3).
	if chain.ActorKind == ActorHuman {
		tr.Allowed, tr.Gate, tr.Reason = true, GateNone, "human actor bypasses wake-chain gates"
		return tr
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	return g.gateLocked(tr, from, to, chain, g.cfgFn(), now)
}

// EvaluateHop is the wired-path entry (I7-D1/T227). It evaluates an agent→agent
// wake using the chain `from` CURRENTLY CARRIES — the chain `from` itself
// received the last time it was woken (within CycleWindow), or a fresh agent root
// (seeded with the configured budget) when it carries none. On ALLOW it records
// the extended chain (depth+1, budget−1, members+to) as `to`'s carried chain, so
// a real multi-hop chain A→B→C… GROWS depth and SPENDS budget across separate
// wake deliveries. This is what makes the depth ① and cost ④ gates fire on the
// live path — without threading an opaque chain token out through the agent
// session and back (the cycle ② / rate ③ gates already accumulate via state).
//
// Callers must NOT pre-bypass human senders here: only agent senders are routed
// to EvaluateHop (human/system senders skip the guard entirely at the call site),
// so every hop is agent-actor and gated.
//
// Staleness is bounded to CycleWindow: a carried chain older than the window is
// treated as absent (a fresh root), so an idle gap resets it and an agent woken
// anew (e.g. by a human turn) is not falsely depth/cost-gated by an old storm.
func (g *Guard) EvaluateHop(from, to, rootMessageID string, now time.Time) Trace {
	g.mu.Lock()
	defer g.mu.Unlock()
	cfg := g.cfgFn()
	chain := g.originChainLocked(from, rootMessageID, cfg, now)
	tr := g.gateLocked(Trace{From: from, To: to, Depth: chain.Depth, ActorKind: chain.ActorKind}, from, to, chain, cfg, now)
	if tr.Allowed {
		g.carried[to] = carriedChain{chain: chain.Extend(to), at: now}
	}
	return tr
}

// originChainLocked returns the chain `from` propagates into a new hop: its
// carried chain when still fresh (≤ cfg.CycleWindow old), else a fresh agent root.
// Caller holds g.mu.
func (g *Guard) originChainLocked(from, rootMessageID string, cfg Config, now time.Time) WakeChain {
	if e, ok := g.carried[from]; ok && now.Sub(e.at) <= cfg.CycleWindow {
		return e.chain
	}
	return NewRootChain(rootMessageID, ActorAgent, cfg.TokenBudget)
}

// gateLocked runs the four gates against `chain` using `cfg` (caller holds g.mu)
// and records the rate/cycle state on ALLOW (a DENY records nothing — the wake
// did not happen).
func (g *Guard) gateLocked(tr Trace, from, to string, chain WakeChain, cfg Config, now time.Time) Trace {
	// ① depth — the NEXT hop would be chain.Depth+1; deny if that exceeds the cap.
	if chain.Depth+1 > cfg.MaxDepth {
		return deny(tr, GateDepth, "depth limit reached")
	}
	// ④ cost — each hop costs one token; deny if the budget is spent.
	if chain.TokenBudgetRemaining <= 0 {
		return deny(tr, GateCost, "chain token budget exhausted")
	}
	// ② cycle — count this pair's recent hops (either direction) in the window.
	key := pairKey(from, to)
	edges := prune(g.cycleEdges[key], now.Add(-cfg.CycleWindow))
	if len(edges) >= cfg.CycleN {
		g.cycleEdges[key] = edges
		return deny(tr, GateCycle, "round-trip cycle detected for this pair")
	}
	// ③ rate — per-target woken-by-agent token bucket (sliding 1-minute window).
	bucket := prune(g.rate[to], now.Add(-time.Minute))
	if len(bucket) >= cfg.RatePerMin {
		g.rate[to] = bucket
		return deny(tr, GateRate, "target agent wake rate exceeded")
	}

	// Allowed — record the hop for cycle + rate state.
	g.cycleEdges[key] = append(edges, now)
	g.rate[to] = append(bucket, now)
	tr.Allowed, tr.Gate = true, GateNone
	return tr
}

func deny(tr Trace, gate Gate, reason string) Trace {
	tr.Allowed, tr.Gate, tr.Reason = false, gate, reason
	return tr
}

// pairKey is the order-independent key for a pair of agents (cycle detection is
// symmetric: A→B and B→A are the same edge).
func pairKey(a, b string) string {
	if a <= b {
		return a + "|" + b
	}
	return b + "|" + a
}

// prune drops timestamps older than cutoff, returning the kept slice.
func prune(ts []time.Time, cutoff time.Time) []time.Time {
	out := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}
