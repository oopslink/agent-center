package usage

import (
	"context"
	"time"
)

// ModelPriceRepository persists the historical unit-price table and loads it for
// cost computation.
type ModelPriceRepository interface {
	// Upsert inserts or replaces price rows keyed by (model, effective_from).
	// Re-asserting an existing (model, effective_from) with corrected numbers is
	// how a mispriced row is fixed (idempotent on the key).
	Upsert(ctx context.Context, prices ...ModelPrice) error
	// List returns every price row, ordered (model, effective_from).
	List(ctx context.Context) ([]ModelPrice, error)
	// LoadPriceBook returns List() wrapped in a *PriceBook ready for point-in-time
	// cost lookup. Convenience for the write path / recompute.
	LoadPriceBook(ctx context.Context) (*PriceBook, error)
}

// UsageEventRepository persists raw usage events and serves the dashboard's
// per-agent and per-task reads.
type UsageEventRepository interface {
	// Append persists events (cost already materialized). Each must Validate().
	Append(ctx context.Context, events ...UsageEvent) error
	// ListByAgent returns an agent's events with from <= ts < to, ordered by ts —
	// the heatmap / trend / overview-card source. A zero `to` means open-ended.
	ListByAgent(ctx context.Context, agentRef string, from, to time.Time) ([]UsageEvent, error)
	// ListByTask returns a task's events ordered by ts — the Top-Cost-Tasks
	// drill-down source.
	ListByTask(ctx context.Context, taskID string) ([]UsageEvent, error)
}
