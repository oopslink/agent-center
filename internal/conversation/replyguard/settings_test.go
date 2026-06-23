package replyguard

import (
	"testing"
	"time"
)

func TestConfigFromMap_EmptyUsesDefaults(t *testing.T) {
	got := ConfigFromMap(nil)
	if got != DefaultConfig() {
		t.Fatalf("empty map should yield DefaultConfig, got %+v", got)
	}
}

func TestConfigFromMap_ParsesOverrides(t *testing.T) {
	got := ConfigFromMap(map[string]string{
		KeyMaxNudges:        "5",
		KeyIdleGraceSec:     "10",
		KeyObligationTTLSec: "120",
		KeyNudgeCooldownSec: "15",
	})
	want := Config{
		MaxNudges:     5,
		IdleGrace:     10 * time.Second,
		ObligationTTL: 120 * time.Second,
		NudgeCooldown: 15 * time.Second,
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestConfigFromMap_MalformedOrNonPositiveFallsBack(t *testing.T) {
	def := DefaultConfig()
	// Each junk value must fall back to the default for that one field, never
	// weakening/disabling the guardrail.
	cases := []struct {
		name string
		m    map[string]string
	}{
		{"unparseable", map[string]string{KeyMaxNudges: "abc"}},
		{"zero", map[string]string{KeyMaxNudges: "0"}},
		{"negative", map[string]string{KeyIdleGraceSec: "-3"}},
		{"blank", map[string]string{KeyObligationTTLSec: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfigFromMap(tc.m); got != def {
				t.Fatalf("junk %q should yield DefaultConfig, got %+v", tc.m, got)
			}
		})
	}
}

func TestConfig_RoundTripMapConfig(t *testing.T) {
	cfg := Config{
		MaxNudges:     4,
		IdleGrace:     45 * time.Second,
		ObligationTTL: 600 * time.Second,
		NudgeCooldown: 90 * time.Second,
	}
	if got := ConfigFromMap(cfg.ToMap()); got != cfg {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, cfg)
	}
}

func TestConfig_Validate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig should be valid, got %v", err)
	}
	bad := []Config{
		{MaxNudges: 0, IdleGrace: time.Second, ObligationTTL: time.Second, NudgeCooldown: time.Second},
		{MaxNudges: 1, IdleGrace: 0, ObligationTTL: time.Second, NudgeCooldown: time.Second},
		{MaxNudges: 1, IdleGrace: time.Second, ObligationTTL: 0, NudgeCooldown: time.Second},
		{MaxNudges: 1, IdleGrace: time.Second, ObligationTTL: time.Second, NudgeCooldown: 0},
	}
	for i, c := range bad {
		if err := c.Validate(); err == nil {
			t.Fatalf("bad config %d should fail validation: %+v", i, c)
		}
	}
}
