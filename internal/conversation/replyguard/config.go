// Package replyguard is the Conversation runtime reply-obligation guardrail
// (T341). It hardens the soft system-prompt contract "you MUST reply to every
// directed message" into a runtime guarantee: when an agent has PERCEIVED a
// human-directed message (a DM, or an @mention by a human) and then goes TRULY
// IDLE (no in-flight work, no queued work) WITHOUT having replied, the worker
// re-injects a bounded reply nudge forcing the agent itself to discharge the
// obligation (方案 A). After the nudge budget is exhausted, the system posts a
// single fallback annotation into the conversation so the human is never left
// silently hanging (方案 B — fallback only; it never fabricates an agent reply).
//
// This package owns the policy CONFIG (thresholds, settings-backed, mirroring
// cognition/wakeguard). The obligation DERIVATION (what counts as perceived /
// discharged) lives in conversation/service; the turn-end + TrueIdle enforcement
// hook lives in the worker daemon's AgentController.
//
// Design: docs/design/architecture/tactical/conversation/03-reply-guardrail.md.
package replyguard

import "time"

// Config is the (center-settings-backed) reply-guardrail configuration. The zero
// value is NOT valid — use DefaultConfig and override. Defaults are conservative
// (§3.5).
type Config struct {
	MaxNudges     int           // ① max bounded re-inject attempts before fallback Backfill
	IdleGrace     time.Duration // ② debounce after a turn ends / agent goes idle before the first nudge
	ObligationTTL time.Duration // ③ how long a perceived-unanswered obligation stays live
	NudgeCooldown time.Duration // ④ min gap between two nudges for the same obligation
}

// DefaultConfig returns the conservative defaults (§3.5): 3 nudges, 30s grace,
// 1h obligation TTL, 60s cooldown.
func DefaultConfig() Config {
	return Config{
		MaxNudges:     3,
		IdleGrace:     30 * time.Second,
		ObligationTTL: time.Hour,
		NudgeCooldown: time.Minute,
	}
}
