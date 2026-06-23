package replyguard

import (
	"errors"
	"strconv"
	"time"
)

// Center-settings keys for the reply-guardrail thresholds (design
// 03-reply-guardrail.md §3.5). The enforcement path reads these via the settings
// store; the Settings UI writes them. An absent/invalid key falls back to the
// corresponding DefaultConfig value — the guardrail is NEVER disabled by a
// missing or malformed setting (§3 invariant: "阈值缺省即生效").
const (
	KeyMaxNudges        = "reply.max_nudges"
	KeyIdleGraceSec     = "reply.idle_grace_sec"
	KeyObligationTTLSec = "reply.obligation_ttl_sec"
	KeyNudgeCooldownSec = "reply.nudge_cooldown_sec"
)

// ConfigFromMap builds a Config from a settings key→value map, defaulting any
// missing OR malformed OR non-positive entry to DefaultConfig. It never returns
// an invalid Config, so it is safe to feed straight into a provider.
func ConfigFromMap(m map[string]string) Config {
	def := DefaultConfig()
	return Config{
		MaxNudges:     posIntOr(m[KeyMaxNudges], def.MaxNudges),
		IdleGrace:     time.Duration(posIntOr(m[KeyIdleGraceSec], int(def.IdleGrace/time.Second))) * time.Second,
		ObligationTTL: time.Duration(posIntOr(m[KeyObligationTTLSec], int(def.ObligationTTL/time.Second))) * time.Second,
		NudgeCooldown: time.Duration(posIntOr(m[KeyNudgeCooldownSec], int(def.NudgeCooldown/time.Second))) * time.Second,
	}
}

// ToMap renders a Config as the canonical settings key→value strings (the shape
// PUT persists and GET returns).
func (c Config) ToMap() map[string]string {
	return map[string]string{
		KeyMaxNudges:        strconv.Itoa(c.MaxNudges),
		KeyIdleGraceSec:     strconv.Itoa(int(c.IdleGrace / time.Second)),
		KeyObligationTTLSec: strconv.Itoa(int(c.ObligationTTL / time.Second)),
		KeyNudgeCooldownSec: strconv.Itoa(int(c.NudgeCooldown / time.Second)),
	}
}

// Validate rejects a Config with any non-positive field (the §3.5 contract: all
// thresholds > 0). Used to reject a bad PUT before persisting.
func (c Config) Validate() error {
	switch {
	case c.MaxNudges <= 0:
		return errors.New("reply.max_nudges must be > 0")
	case c.IdleGrace <= 0:
		return errors.New("reply.idle_grace_sec must be > 0")
	case c.ObligationTTL <= 0:
		return errors.New("reply.obligation_ttl_sec must be > 0")
	case c.NudgeCooldown <= 0:
		return errors.New("reply.nudge_cooldown_sec must be > 0")
	}
	return nil
}

// posIntOr parses s as a positive int, returning def when s is empty, unparseable,
// or ≤ 0 (a stored junk/zero value must not weaken the guardrail).
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
