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

// TestConcurrencyEnabledAndCap covers the v2.18.0 W4c opt-in predicate + run-slot
// cap shared by the daemon executor gate and the center start cap: enabled requires
// BOTH MaxConcurrentTasks>0 AND ≥1 allowed model; the effective cap is the
// EffectiveMaxConcurrentTasks when enabled, else 1 (single-active — no regression
// for a default agent even though the persisted column defaults to 3).
func TestConcurrencyEnabledAndCap(t *testing.T) {
	cases := []struct {
		name        string
		maxConc     int
		allowed     []string
		wantEnabled bool
		wantCap     int
	}{
		{"default agent (no models) → disabled, cap 1", 3, nil, false, 1},
		{"models but maxConc 0 → disabled, cap 1", 0, []string{"m"}, false, 1},
		{"maxConc>0 but empty models → disabled, cap 1", 5, []string{}, false, 1},
		{"enabled: maxConc 3 + a model → cap 3", 3, []string{"m"}, true, 3},
		{"enabled: maxConc 1 + a model → cap 1", 1, []string{"m"}, true, 1},
		{"enabled: maxConc unset(0)... stays disabled", 0, []string{"m"}, false, 1},
		{"enabled: maxConc 7 + models → cap 7", 7, []string{"a", "b"}, true, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Profile{MaxConcurrentTasks: c.maxConc, AllowedModels: c.allowed}
			if got := p.ConcurrencyEnabled(); got != c.wantEnabled {
				t.Fatalf("ConcurrencyEnabled() = %v, want %v", got, c.wantEnabled)
			}
			if got := p.EffectiveConcurrencyCap(); got != c.wantCap {
				t.Fatalf("EffectiveConcurrencyCap() = %d, want %d", got, c.wantCap)
			}
		})
	}
}
