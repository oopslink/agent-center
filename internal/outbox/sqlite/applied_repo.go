package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// AppliedRepo implements outbox.AppliedStore: the per-projector dedup ledger
// that makes the relay idempotent by event_id (plan §10 OQ1).
type AppliedRepo struct {
	db *sql.DB
}

// NewAppliedRepo constructs the repo.
func NewAppliedRepo(db *sql.DB) *AppliedRepo {
	return &AppliedRepo{db: db}
}

// IsApplied reports whether projector already applied eventID.
func (r *AppliedRepo) IsApplied(ctx context.Context, projector, eventID string) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT 1 FROM outbox_applied WHERE projector_name = ? AND event_id = ?`,
		projector, eventID)
	var one int
	err := row.Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// MarkApplied records (projector, eventID) as applied. The INSERT OR IGNORE
// makes a redelivered mark a harmless no-op (the PK is the dedup key).
func (r *AppliedRepo) MarkApplied(ctx context.Context, projector, eventID string, t time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO outbox_applied (projector_name, event_id, applied_at) VALUES (?,?,?)`,
		projector, eventID, t.Format(time.RFC3339Nano))
	return err
}

var _ outbox.AppliedStore = (*AppliedRepo)(nil)
