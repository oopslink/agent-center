package wakeguard

import (
	"errors"
	"strconv"
	"time"
)

// Center-settings keys for the wake-guardrail thresholds (design
// 04-wake-guardrail.md §3.5). The WakeGuard reads these via the settings store;
// the I7-D3 Settings UI writes them. An absent/invalid key falls back to the
// corresponding DefaultConfig value — the guard is NEVER disabled by a missing
// or malformed setting (§3 invariant: "阈值缺省即生效").
const (
	KeyMaxDepth         = "wake.max_depth"
	KeyCycleWindowSec   = "wake.cycle_window_sec"
	KeyCycleThreshold   = "wake.cycle_threshold"
	KeyRatePerMin       = "wake.rate_per_min"
	KeyChainTokenBudget = "wake.chain_token_budget"
)

// ConfigFromMap builds a Config from a settings key→value map, defaulting any
// missing OR malformed OR non-positive entry to DefaultConfig. It never returns
// an invalid Config, so it is safe to feed straight into NewGuard / a provider.
func ConfigFromMap(m map[string]string) Config {
	def := DefaultConfig()
	return Config{
		MaxDepth:    posIntOr(m[KeyMaxDepth], def.MaxDepth),
		CycleWindow: time.Duration(posIntOr(m[KeyCycleWindowSec], int(def.CycleWindow/time.Second))) * time.Second,
		CycleN:      posIntOr(m[KeyCycleThreshold], def.CycleN),
		RatePerMin:  posIntOr(m[KeyRatePerMin], def.RatePerMin),
		TokenBudget: posIntOr(m[KeyChainTokenBudget], def.TokenBudget),
	}
}

// ToMap renders a Config as the canonical settings key→value strings (the shape
// PUT persists and GET returns).
func (c Config) ToMap() map[string]string {
	return map[string]string{
		KeyMaxDepth:         strconv.Itoa(c.MaxDepth),
		KeyCycleWindowSec:   strconv.Itoa(int(c.CycleWindow / time.Second)),
		KeyCycleThreshold:   strconv.Itoa(c.CycleN),
		KeyRatePerMin:       strconv.Itoa(c.RatePerMin),
		KeyChainTokenBudget: strconv.Itoa(c.TokenBudget),
	}
}

// Validate rejects a Config with any non-positive field (the §3.5 contract: all
// thresholds > 0). Used to reject a bad PUT before persisting.
func (c Config) Validate() error {
	switch {
	case c.MaxDepth <= 0:
		return errors.New("wake.max_depth must be > 0")
	case c.CycleWindow <= 0:
		return errors.New("wake.cycle_window_sec must be > 0")
	case c.CycleN <= 0:
		return errors.New("wake.cycle_threshold must be > 0")
	case c.RatePerMin <= 0:
		return errors.New("wake.rate_per_min must be > 0")
	case c.TokenBudget <= 0:
		return errors.New("wake.chain_token_budget must be > 0")
	}
	return nil
}

// posIntOr parses s as a positive int, returning def when s is empty, unparseable,
// or ≤ 0 (a stored junk/zero value must not weaken the guard).
func posIntOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
