// Package settings is the center-wide (system) key/value settings store. It is a
// thin override layer: a key absent from the store means "use the reading code's
// default", so a missing setting never changes behavior by itself.
//
// The first consumer is the wake-guardrail config (I7-D1 / design
// 04-wake-guardrail.md §3.5): the WakeGuard reads its five `wake.*` thresholds
// through this store, and the I7-D3 Settings UI writes them. Keys are an open
// namespace owned by their reading code; this package stays value-agnostic
// (everything is a string — callers parse/validate per key).
package settings

import "context"

// Store is the center settings persistence port.
type Store interface {
	// Get returns the value for key and whether it was present.
	Get(ctx context.Context, key string) (value string, found bool, err error)
	// GetByPrefix returns every key/value whose key starts with prefix (one query —
	// the WakeGuard provider reads all `wake.*` keys at once).
	GetByPrefix(ctx context.Context, prefix string) (map[string]string, error)
	// Set upserts key→value (last write wins).
	Set(ctx context.Context, key, value string) error
}
