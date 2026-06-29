package agent

import "testing"

// TestEffectiveMaxConcurrentTasks covers the F3 default-3 helper (design §5):
// unset/zero/negative all resolve to DefaultMaxConcurrentTasks; a positive value
// is returned as-is.
func TestEffectiveMaxConcurrentTasks(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero defaults to 3", 0, DefaultMaxConcurrentTasks},
		{"negative defaults to 3", -1, DefaultMaxConcurrentTasks},
		{"positive kept", 5, 5},
		{"one kept", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Profile{MaxConcurrentTasks: c.in}
			if got := p.EffectiveMaxConcurrentTasks(); got != c.want {
				t.Fatalf("EffectiveMaxConcurrentTasks(%d) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestConcurrencyEnabledAndCap covers the opt-in predicate + run-slot cap shared by
// the daemon executor gate and the center start cap: enabled requires BOTH
// MaxConcurrentTasks>0 AND ≥1 allowed EXECUTOR (v2.18.1 BE-1: the authoritative list
// is AllowedExecutors [{cli,model}], not the legacy model-only AllowedModels); the
// effective cap is EffectiveMaxConcurrentTasks when enabled, else 1 (single-active —
// no regression for a default agent even though the persisted column defaults to 3).
func TestConcurrencyEnabledAndCap(t *testing.T) {
	ex := []ExecutorProfile{{CLI: "claude-code", Model: "m"}}
	ex2 := []ExecutorProfile{{CLI: "claude-code", Model: "a"}, {CLI: "codex", Model: "b"}}
	cases := []struct {
		name        string
		maxConc     int
		allowed     []ExecutorProfile
		wantEnabled bool
		wantCap     int
	}{
		{"default agent (no executors) → disabled, cap 1", 3, nil, false, 1},
		{"executors but maxConc 0 → disabled, cap 1", 0, ex, false, 1},
		{"maxConc>0 but empty executors → disabled, cap 1", 5, []ExecutorProfile{}, false, 1},
		{"enabled: maxConc 3 + an executor → cap 3", 3, ex, true, 3},
		{"enabled: maxConc 1 + an executor → cap 1", 1, ex, true, 1},
		{"enabled: maxConc unset(0)... stays disabled", 0, ex, false, 1},
		{"enabled: maxConc 7 + executors → cap 7", 7, ex2, true, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Profile{MaxConcurrentTasks: c.maxConc, AllowedExecutors: c.allowed}
			if got := p.ConcurrencyEnabled(); got != c.wantEnabled {
				t.Fatalf("ConcurrencyEnabled() = %v, want %v", got, c.wantEnabled)
			}
			if got := p.EffectiveConcurrencyCap(); got != c.wantCap {
				t.Fatalf("EffectiveConcurrencyCap() = %d, want %d", got, c.wantCap)
			}
		})
	}
}
