// Package sqlite implements the outbox Repository + AppliedStore (v2.7 A0,
// plan §10 OQ1).
package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// OutboxRepo implements outbox.Repository.
type OutboxRepo struct {
	db *sql.DB
}

// NewOutboxRepo constructs the repo.
func NewOutboxRepo(db *sql.DB) *OutboxRepo {
	return &OutboxRepo{db: db}
}

// Append writes one event. Called inside the producer's own tx so the BC
// state write and the event commit atomically.
func (r *OutboxRepo) Append(ctx context.Context, e outbox.Event) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	refs := e.Refs
	if refs == "" {
		refs = "{}"
	}
	payload := e.Payload
	if payload == "" {
		payload = "{}"
	}
	const stmt = `INSERT INTO outbox_events (id, event_type, refs, payload, created_at, processed_at)
		VALUES (?,?,?,?,?,NULL)`
	_, err := exec.ExecContext(ctx, stmt,
		e.ID, e.EventType, refs, payload, e.CreatedAt.Format(time.RFC3339Nano))
	return err
}

// FetchUnprocessed returns up to limit unprocessed events, oldest first.
func (r *OutboxRepo) FetchUnprocessed(ctx context.Context, limit int) ([]outbox.Event, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT id, event_type, refs, payload, created_at, processed_at
		 FROM outbox_events WHERE processed_at IS NULL ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []outbox.Event
	for rows.Next() {
		var (
			e           outbox.Event
			createdAt   string
			processedAt sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.EventType, &e.Refs, &e.Payload, &createdAt, &processedAt); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			e.CreatedAt = t
		}
		if processedAt.Valid && processedAt.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, processedAt.String); err == nil {
				e.ProcessedAt = &t
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkProcessed sets processed_at (idempotent: only unprocessed rows).
func (r *OutboxRepo) MarkProcessed(ctx context.Context, id string, t time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE outbox_events SET processed_at = ? WHERE id = ? AND processed_at IS NULL`,
		t.Format(time.RFC3339Nano), id)
	return err
}

var _ outbox.Repository = (*OutboxRepo)(nil)
