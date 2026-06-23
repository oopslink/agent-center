package usage

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// perMTok is the token denominator: prices are quoted per MILLION tokens, so a
// raw token count is multiplied by the per-Mtoken price and divided by this.
const perMTok = 1_000_000

// ErrNoPrice is returned when no model_prices row is in force for a (model, ts) —
// i.e. the model is unknown or ts predates the earliest effective_from for it.
var ErrNoPrice = errors.New("usage: no price in force for model at time")

// TokenCounts is the raw per-event token tally cost is computed from. cache_read
// and cache_write are split because Anthropic prices them differently.
type TokenCounts struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// ModelPrice is one historical unit-price row: the price for model that takes
// effect at EffectiveFrom (RFC3339 instant) and stays in force until a later row
// supersedes it. All prices are micros (1e-6 USD) per million tokens.
type ModelPrice struct {
	Model                   string
	EffectiveFrom           time.Time
	InputPerMTokMicros      int64
	OutputPerMTokMicros     int64
	CacheReadPerMTokMicros  int64
	CacheWritePerMTokMicros int64
}

// CostMicros returns the materialized cost of c at this price, in micros. Prices
// are per million tokens, so cost = Σ(tokens × per_mtok_micros) / 1e6. The sum is
// rounded once (round half up), not per-component, to match the Build Spec's
// Σ(...)/1e6 formula exactly. Token counts and prices are non-negative, so the
// numerator never overflows int64 for any realistic usage.
func (p ModelPrice) CostMicros(c TokenCounts) int64 {
	num := c.Input*p.InputPerMTokMicros +
		c.Output*p.OutputPerMTokMicros +
		c.CacheRead*p.CacheReadPerMTokMicros +
		c.CacheWrite*p.CacheWritePerMTokMicros
	return (num + perMTok/2) / perMTok
}

// PriceBook resolves "the price in force at a point in time" for cost
// materialization. It is the in-memory projection of the model_prices table: the
// repo loads all rows into it, and CostMicrosAt looks up (model, ts) and applies
// the effective price. Construct via NewPriceBook; it is read-only afterwards.
type PriceBook struct {
	// byModel maps model → its price rows sorted ascending by EffectiveFrom.
	byModel map[string][]ModelPrice
}

// NewPriceBook indexes prices by model, sorted by EffectiveFrom ascending, ready
// for point-in-time lookup. The input slice is not retained.
func NewPriceBook(prices []ModelPrice) *PriceBook {
	byModel := make(map[string][]ModelPrice)
	for _, p := range prices {
		byModel[p.Model] = append(byModel[p.Model], p)
	}
	for _, rows := range byModel {
		sort.Slice(rows, func(i, j int) bool { return rows[i].EffectiveFrom.Before(rows[j].EffectiveFrom) })
	}
	return &PriceBook{byModel: byModel}
}

// PriceAt returns the price in force for model at ts — the row with the greatest
// EffectiveFrom that is <= ts. Returns ErrNoPrice if model is unknown or every row
// for it is in the future relative to ts.
func (b *PriceBook) PriceAt(model string, ts time.Time) (ModelPrice, error) {
	rows := b.byModel[model]
	// Largest index i with rows[i].EffectiveFrom <= ts. sort.Search finds the
	// first row strictly after ts; the one before it is the row in force.
	i := sort.Search(len(rows), func(i int) bool { return rows[i].EffectiveFrom.After(ts) })
	if i == 0 {
		return ModelPrice{}, fmt.Errorf("%w: model=%q ts=%s", ErrNoPrice, model, ts.Format(time.RFC3339))
	}
	return rows[i-1], nil
}

// CostMicrosAt materializes the cost of c for (model, ts) using the effective
// price. This is the single entry point the write path (F2) and any recompute
// (correction / backfill) call. Returns ErrNoPrice when no row covers ts.
func (b *PriceBook) CostMicrosAt(model string, ts time.Time, c TokenCounts) (int64, error) {
	p, err := b.PriceAt(model, ts)
	if err != nil {
		return 0, err
	}
	return p.CostMicros(c), nil
}
