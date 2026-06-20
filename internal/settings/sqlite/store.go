// Package sqlite implements the center settings Store backed by SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/settings"
)

// Store is the SQLite-backed center settings KV store (table center_settings).
type Store struct {
	db  *sql.DB
	clk clock.Clock
}

// NewStore constructs the store. A nil clock falls back to the system clock.
func NewStore(db *sql.DB, clk clock.Clock) *Store {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Store{db: db, clk: clk}
}

// Get returns the value for key and whether the row exists.
func (s *Store) Get(ctx context.Context, key string) (string, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	var v string
	err := exec.QueryRowContext(ctx, `SELECT value FROM center_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// GetByPrefix returns every key/value whose key starts with prefix.
func (s *Store) GetByPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	rows, err := exec.QueryContext(ctx, `SELECT key, value FROM center_settings WHERE key LIKE ? || '%'`, prefix)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Set upserts key→value (last write wins), stamping updated_at.
func (s *Store) Set(ctx context.Context, key, value string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO center_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, s.clk.Now().UTC().Format(time.RFC3339Nano))
	return err
}

var _ settings.Store = (*Store)(nil)
