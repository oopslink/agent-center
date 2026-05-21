// Package dispatcher hosts the Bridge BC FeishuOutboundDispatcher
// (plan-5 § 3.5).
//
// It is the first real subscriber of the events table; the cursor table
// (`bridge_subscription_cursors`) keeps the per-subscriber "last_event_id
// processed" pointer so a dispatcher restart resumes from where it left
// off — no events table schema change required.
package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/persistence"
)

// CursorStore persists per-subscriber cursors keyed by `subscriber` name.
type CursorStore interface {
	// Load returns the saved cursor (last processed event id) or "" when no
	// row exists yet.
	Load(ctx context.Context, subscriber string) (string, error)
	// Save UPSERTs the new cursor + updated_at.
	Save(ctx context.Context, subscriber, lastEventID string) error
}

// SQLiteCursorStore implements CursorStore on SQLite using
// `bridge_subscription_cursors`.
type SQLiteCursorStore struct {
	db    *sql.DB
	clock clock.Clock
}

// NewSQLiteCursorStore constructs the cursor store.
func NewSQLiteCursorStore(db *sql.DB, clk clock.Clock) *SQLiteCursorStore {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &SQLiteCursorStore{db: db, clock: clk}
}

// Load returns the saved cursor or "" if no row yet.
func (s *SQLiteCursorStore) Load(ctx context.Context, subscriber string) (string, error) {
	if subscriber == "" {
		return "", errors.New("cursor store: subscriber required")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, s.db)
	if err != nil {
		return "", err
	}
	row := exec.QueryRowContext(ctx,
		`SELECT last_event_id FROM bridge_subscription_cursors WHERE subscriber = ?`, subscriber)
	var v string
	err = row.Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cursor load: %w", err)
	}
	return v, nil
}

// Save UPSERTs the cursor.
func (s *SQLiteCursorStore) Save(ctx context.Context, subscriber, lastEventID string) error {
	if subscriber == "" {
		return errors.New("cursor store: subscriber required")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, s.db)
	if err != nil {
		return err
	}
	now := s.clock.Now().UTC().Format(time.RFC3339Nano)
	const stmt = `INSERT INTO bridge_subscription_cursors (subscriber, last_event_id, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(subscriber) DO UPDATE
		SET last_event_id = excluded.last_event_id, updated_at = excluded.updated_at`
	_, err = exec.ExecContext(ctx, stmt, subscriber, lastEventID, now)
	if err != nil {
		return fmt.Errorf("cursor save: %w", err)
	}
	return nil
}
