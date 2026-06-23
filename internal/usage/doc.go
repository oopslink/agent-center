// Package usage is the I28 (issue-a7ff560e) per-agent analytics collection
// domain — the v2.15.0 F1 slice. It owns the raw token/cost telemetry that the
// dashboard reads: the historical model-price table and the per-turn usage
// events, plus the cost-calculation rules that tie them together.
//
// This is a new bounded context, deliberately separate from `observability`
// (which models system lifecycle events, not billing): usage/cost is its own
// subdomain. Tables are unprefixed (`model_prices`, `usage_events`) to match the
// observability-domain naming convention rather than the pm_-prefixed PM tables.
//
// F1 scope (Build Spec): model_prices + usage_events + cost calculation + repos +
// migration + unit tests. It does NOT build the daily rollup (agent_activity_daily
// — F3) or the report_usage MCP / worker hook that writes events (F2). It is a
// standalone, fully-tested unit with no wiring into the API surface yet (F4).
//
// Cost model (decisions 2 & 4): cost is a list-price estimate. `model_prices` is a
// historical table keyed by (model, effective_from); the price in force for an
// event is the row with the greatest effective_from <= the event's ts, so a price
// change never rewrites past events (non-retroactive). `usage_events.cost_micros`
// is materialized at write time from that price (hot path / rollup read it
// directly), but effective_from is retained so a mispriced batch can be recomputed.
package usage
