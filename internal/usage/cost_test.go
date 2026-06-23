package usage

import (
	"errors"
	"testing"
	"time"
)

func tm(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// opusPrice mirrors the seeded Opus 4.8 list price (micros per Mtoken).
var opusPrice = ModelPrice{
	Model:                   "claude-opus-4-8",
	EffectiveFrom:           tm("2026-01-01T00:00:00Z"),
	InputPerMTokMicros:      5_000_000,
	OutputPerMTokMicros:     25_000_000,
	CacheReadPerMTokMicros:  500_000,
	CacheWritePerMTokMicros: 6_250_000,
}

func TestCostMicros(t *testing.T) {
	cases := []struct {
		name string
		c    TokenCounts
		want int64
	}{
		{"zero", TokenCounts{}, 0},
		// 1M input @ $5/Mtok = $5 = 5,000,000 micros.
		{"one million input", TokenCounts{Input: 1_000_000}, 5_000_000},
		// 1M output @ $25/Mtok = 25,000,000 micros.
		{"one million output", TokenCounts{Output: 1_000_000}, 25_000_000},
		// cache split: 1M read @0.1x=500k, 1M write @1.25x=6.25M.
		{"cache read+write", TokenCounts{CacheRead: 1_000_000, CacheWrite: 1_000_000}, 500_000 + 6_250_000},
		// mixed realistic turn: 1000 in, 500 out, 2000 cache_read, 100 cache_write.
		//   1000*5e6/1e6=5000 ; 500*25e6/1e6=12500 ; 2000*5e5/1e6=1000 ; 100*6.25e6/1e6=625
		{"mixed turn", TokenCounts{Input: 1000, Output: 500, CacheRead: 2000, CacheWrite: 100}, 5000 + 12500 + 1000 + 625},
		// rounding: 1 input token @ $5/Mtok = 5 micros exactly; 3 tokens = 15.
		{"tiny rounds", TokenCounts{Input: 3}, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := opusPrice.CostMicros(tc.c); got != tc.want {
				t.Fatalf("CostMicros(%+v) = %d, want %d", tc.c, got, tc.want)
			}
		})
	}
}

func TestCostMicros_RoundHalfUp(t *testing.T) {
	// A price that produces a fractional micro: 1 token @ 1.5 micros/Mtok would be
	// 0.0000015 micros — far below rounding. Construct a deliberate half-case:
	// 500,000 tokens @ 1 micro/Mtok = 0.5 micro → rounds to 1.
	p := ModelPrice{InputPerMTokMicros: 1}
	if got := p.CostMicros(TokenCounts{Input: 500_000}); got != 1 {
		t.Fatalf("round-half-up: got %d, want 1", got)
	}
	// 499,999 tokens @ 1 micro/Mtok = 0.499999 → rounds to 0.
	if got := p.CostMicros(TokenCounts{Input: 499_999}); got != 0 {
		t.Fatalf("round-down: got %d, want 0", got)
	}
}

func TestPriceBook_EffectiveFrom(t *testing.T) {
	// Two prices for the same model: original, then a hike effective 2026-06-01.
	v1 := opusPrice // effective 2026-01-01, input 5_000_000
	v2 := opusPrice
	v2.EffectiveFrom = tm("2026-06-01T00:00:00Z")
	v2.InputPerMTokMicros = 6_000_000 // hike
	// Insert out of order to prove NewPriceBook sorts.
	book := NewPriceBook([]ModelPrice{v2, v1})

	cases := []struct {
		name      string
		ts        string
		wantInput int64
	}{
		{"exactly at v1", "2026-01-01T00:00:00Z", 5_000_000},
		{"between v1 and v2", "2026-03-15T12:00:00Z", 5_000_000},
		{"exactly at v2", "2026-06-01T00:00:00Z", 6_000_000},
		{"after v2 (no retroactive)", "2026-09-01T00:00:00Z", 6_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := book.PriceAt("claude-opus-4-8", tm(tc.ts))
			if err != nil {
				t.Fatalf("PriceAt err: %v", err)
			}
			if p.InputPerMTokMicros != tc.wantInput {
				t.Fatalf("PriceAt(%s) input = %d, want %d", tc.ts, p.InputPerMTokMicros, tc.wantInput)
			}
		})
	}

	// ts before the earliest effective_from → ErrNoPrice.
	if _, err := book.PriceAt("claude-opus-4-8", tm("2025-12-31T23:59:59Z")); !errors.Is(err, ErrNoPrice) {
		t.Fatalf("pre-earliest err = %v, want ErrNoPrice", err)
	}
	// unknown model → ErrNoPrice.
	if _, err := book.PriceAt("nope", tm("2026-06-01T00:00:00Z")); !errors.Is(err, ErrNoPrice) {
		t.Fatalf("unknown model err = %v, want ErrNoPrice", err)
	}
}

func TestPriceBook_CostMicrosAt(t *testing.T) {
	book := NewPriceBook([]ModelPrice{opusPrice})
	got, err := book.CostMicrosAt("claude-opus-4-8", tm("2026-06-01T00:00:00Z"), TokenCounts{Input: 1_000_000})
	if err != nil || got != 5_000_000 {
		t.Fatalf("CostMicrosAt = %d, %v; want 5000000, nil", got, err)
	}
	if _, err := book.CostMicrosAt("nope", tm("2026-06-01T00:00:00Z"), TokenCounts{Input: 1}); !errors.Is(err, ErrNoPrice) {
		t.Fatalf("unknown model err = %v, want ErrNoPrice", err)
	}
}

func TestUsageEvent_Validate(t *testing.T) {
	base := UsageEvent{
		ID: "u1", AgentRef: "agent:a", ProjectID: "p1", Model: "claude-opus-4-8",
		TS: tm("2026-06-01T00:00:00Z"), Source: SourceReport,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	// task_id may be empty (not task-scoped).
	if base.TaskID != "" {
		t.Fatal("precondition")
	}

	bad := []struct {
		name   string
		mutate func(*UsageEvent)
	}{
		{"empty id", func(e *UsageEvent) { e.ID = "" }},
		{"empty agent", func(e *UsageEvent) { e.AgentRef = "  " }},
		{"empty project", func(e *UsageEvent) { e.ProjectID = "" }},
		{"empty model", func(e *UsageEvent) { e.Model = "" }},
		{"zero ts", func(e *UsageEvent) { e.TS = time.Time{} }},
		{"bad source", func(e *UsageEvent) { e.Source = "guess" }},
		{"negative tokens", func(e *UsageEvent) { e.Tokens.Input = -1 }},
		{"negative cost", func(e *UsageEvent) { e.CostMicros = -1 }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.mutate(&e)
			if err := e.Validate(); err == nil {
				t.Fatalf("%s: Validate accepted invalid event", tc.name)
			}
		})
	}
}
