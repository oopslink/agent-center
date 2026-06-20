package wakeguard

import (
	"testing"
	"time"
)

func TestConfigFromMap_EmptyIsDefault(t *testing.T) {
	if got := ConfigFromMap(nil); got != DefaultConfig() {
		t.Errorf("ConfigFromMap(nil) = %+v, want DefaultConfig %+v", got, DefaultConfig())
	}
	if got := ConfigFromMap(map[string]string{}); got != DefaultConfig() {
		t.Errorf("ConfigFromMap({}) = %+v, want DefaultConfig", got)
	}
}

func TestConfigFromMap_JunkOrNonPositiveDefaults(t *testing.T) {
	def := DefaultConfig()
	// Every key set to a value that must be rejected (blank/zero/negative/junk):
	// the guard must NEVER be weakened by a malformed stored setting.
	m := map[string]string{
		KeyMaxDepth:         "0",
		KeyCycleWindowSec:   "-5",
		KeyCycleThreshold:   "notanint",
		KeyRatePerMin:       "",
		KeyChainTokenBudget: "0",
	}
	if got := ConfigFromMap(m); got != def {
		t.Errorf("ConfigFromMap(junk) = %+v, want all defaults %+v", got, def)
	}
}

func TestConfigFromMap_ValidParse(t *testing.T) {
	m := map[string]string{
		KeyMaxDepth:         "6",
		KeyCycleWindowSec:   "120",
		KeyCycleThreshold:   "5",
		KeyRatePerMin:       "20",
		KeyChainTokenBudget: "32",
	}
	got := ConfigFromMap(m)
	want := Config{MaxDepth: 6, CycleWindow: 120 * time.Second, CycleN: 5, RatePerMin: 20, TokenBudget: 32}
	if got != want {
		t.Errorf("ConfigFromMap = %+v, want %+v", got, want)
	}
}

func TestConfig_ToMap_RoundTrip(t *testing.T) {
	c := Config{MaxDepth: 4, CycleWindow: 300 * time.Second, CycleN: 3, RatePerMin: 10, TokenBudget: 16}
	if got := ConfigFromMap(c.ToMap()); got != c {
		t.Errorf("round-trip: ConfigFromMap(c.ToMap()) = %+v, want %+v", got, c)
	}
}

func TestConfig_Validate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("DefaultConfig must be valid, got %v", err)
	}
	base := DefaultConfig()
	for name, mut := range map[string]func(*Config){
		"max_depth":    func(c *Config) { c.MaxDepth = 0 },
		"cycle_window": func(c *Config) { c.CycleWindow = 0 },
		"cycle_n":      func(c *Config) { c.CycleN = -1 },
		"rate_per_min": func(c *Config) { c.RatePerMin = 0 },
		"token_budget": func(c *Config) { c.TokenBudget = -3 },
	} {
		c := base
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("Validate(%s non-positive) = nil, want error", name)
		}
	}
}

// TestGuard_LiveConfig_NoRestart is the T224/D3 acceptance at the unit level: a
// Guard built with NewGuardFunc reads its config on EVERY evaluation, so raising
// a threshold (as the Settings PUT does) takes effect WITHOUT rebuilding the
// guard. Here the rate gate denies a 2nd hop at R=1, then allows again once the
// live config raises R — same Guard instance throughout.
func TestGuard_LiveConfig_NoRestart(t *testing.T) {
	// Only RatePerMin matters; other gates set wide so they never interfere.
	cfg := Config{MaxDepth: 10, CycleWindow: time.Hour, CycleN: 100, RatePerMin: 1, TokenBudget: 100}
	g := NewGuardFunc(func() Config { return cfg })
	now := time.Now()

	if tr := g.EvaluateHop("A", "B", "root", now); !tr.Allowed {
		t.Fatalf("hop1 should allow: %+v", tr)
	}
	if tr := g.EvaluateHop("A", "B", "root", now); tr.Allowed || tr.Gate != GateRate {
		t.Fatalf("hop2 should deny by rate at R=1: %+v", tr)
	}
	// Raise the limit LIVE on the same Guard (mirrors a Settings PUT).
	cfg.RatePerMin = 5
	if tr := g.EvaluateHop("A", "B", "root", now); !tr.Allowed {
		t.Fatalf("hop3 should allow after raising R to 5 live (no restart): %+v", tr)
	}
}
