package sqlite

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/usage"
)

// ModelPriceRepo is the SQLite-backed usage.ModelPriceRepository over the
// model_prices table (migration 0077).
type ModelPriceRepo struct {
	db *sql.DB
}

// NewModelPriceRepo constructs the repo.
func NewModelPriceRepo(db *sql.DB) *ModelPriceRepo { return &ModelPriceRepo{db: db} }

// Upsert inserts or replaces each price keyed by (model, effective_from).
func (r *ModelPriceRepo) Upsert(ctx context.Context, prices ...usage.ModelPrice) error {
	if len(prices) == 0 {
		return nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	for _, p := range prices {
		if _, err := exec.ExecContext(ctx,
			`INSERT INTO model_prices
			   (model, effective_from, input_per_mtok_micros, output_per_mtok_micros,
			    cache_read_per_mtok_micros, cache_write_per_mtok_micros)
			 VALUES (?,?,?,?,?,?)
			 ON CONFLICT(model, effective_from) DO UPDATE SET
			   input_per_mtok_micros       = excluded.input_per_mtok_micros,
			   output_per_mtok_micros      = excluded.output_per_mtok_micros,
			   cache_read_per_mtok_micros  = excluded.cache_read_per_mtok_micros,
			   cache_write_per_mtok_micros = excluded.cache_write_per_mtok_micros`,
			p.Model, ts(p.EffectiveFrom), p.InputPerMTokMicros, p.OutputPerMTokMicros,
			p.CacheReadPerMTokMicros, p.CacheWritePerMTokMicros); err != nil {
			return err
		}
	}
	return nil
}

// List returns every price row ordered (model, effective_from).
func (r *ModelPriceRepo) List(ctx context.Context) ([]usage.ModelPrice, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT model, effective_from, input_per_mtok_micros, output_per_mtok_micros,
		        cache_read_per_mtok_micros, cache_write_per_mtok_micros
		   FROM model_prices ORDER BY model, effective_from`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.ModelPrice
	for rows.Next() {
		var p usage.ModelPrice
		var eff string
		if err := rows.Scan(&p.Model, &eff, &p.InputPerMTokMicros, &p.OutputPerMTokMicros,
			&p.CacheReadPerMTokMicros, &p.CacheWritePerMTokMicros); err != nil {
			return nil, err
		}
		p.EffectiveFrom = parseTime(eff)
		out = append(out, p)
	}
	return out, rows.Err()
}

// LoadPriceBook returns List() wrapped in a *usage.PriceBook.
func (r *ModelPriceRepo) LoadPriceBook(ctx context.Context) (*usage.PriceBook, error) {
	prices, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	return usage.NewPriceBook(prices), nil
}

var _ usage.ModelPriceRepository = (*ModelPriceRepo)(nil)
