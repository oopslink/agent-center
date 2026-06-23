package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	env "github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ControlEventRepo implements env.ControlEventRepository. `offset` is quoted in
// every query because it is a reserved-ish word in some engines.
type ControlEventRepo struct{ db *sql.DB }

// NewControlEventRepo constructs the repo.
func NewControlEventRepo(db *sql.DB) *ControlEventRepo { return &ControlEventRepo{db: db} }

// Append writes one command. A UNIQUE violation on (worker_id, idempotency_key)
// maps to env.ErrDuplicateIdempotencyKey (the lost-race backstop); any other
// UNIQUE violation — i.e. a duplicate (worker_id, offset) — surfaces as-is so
// the caller sees the offset clash.
func (r *ControlEventRepo) Append(ctx context.Context, e *env.WorkerControlEvent) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO worker_control_events
		 (id, worker_id, "offset", idempotency_key, command_type, payload, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		e.ID(), string(e.WorkerID()), e.Offset(), e.IdempotencyKey(),
		e.CommandType(), nullString(e.Payload()), ts(e.CreatedAt()))
	if persistence.IsUniqueViolation(err) {
		// Distinguish the idempotency-key clash (race backstop) from an
		// offset clash. The constraint name appears in the modernc.org/sqlite
		// error text (e.g. "UNIQUE constraint failed: worker_control_events.worker_id, worker_control_events.idempotency_key").
		if strings.Contains(strings.ToLower(err.Error()), "idempotency_key") {
			return env.ErrDuplicateIdempotencyKey
		}
		return err
	}
	return err
}

func (r *ControlEventRepo) MaxOffset(ctx context.Context, workerID env.WorkerID) (int64, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var maxOff sql.NullInt64
	err := exec.QueryRowContext(ctx,
		`SELECT MAX("offset") FROM worker_control_events WHERE worker_id = ?`, string(workerID)).
		Scan(&maxOff)
	if err != nil {
		return 0, err
	}
	if !maxOff.Valid {
		return 0, nil
	}
	return maxOff.Int64, nil
}

func (r *ControlEventRepo) FindByIdempotencyKey(ctx context.Context, workerID env.WorkerID, key string) (*env.WorkerControlEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, eventSelect+` WHERE worker_id = ? AND idempotency_key = ?`,
		string(workerID), key)
	e, err := scanEvent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

func (r *ControlEventRepo) ListAfter(ctx context.Context, workerID env.WorkerID, offset int64) ([]*env.WorkerControlEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		eventSelect+` WHERE worker_id = ? AND "offset" > ? ORDER BY "offset"`,
		string(workerID), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*env.WorkerControlEvent
	for rows.Next() {
		e, err := scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteAckedBefore prunes up to `limit` command-stream rows that are SAFE to GC
// (T340, issue-b71ee81f): rows created strictly before `cutoff` whose offset the
// OWNING worker has already acked (e."offset" <= env_workers.last_acked_offset), plus
// orphan rows whose worker no longer exists (COALESCE to MaxInt64 → matched by time
// alone). It NEVER deletes an un-acked row (offset > last_acked_offset) — that is the
// safety guard guaranteeing a worker offline past the retention window still replays
// every undelivered command (CommandsAfter = offset > last_acked) on reconnect; the
// desired lifecycle/work state is re-derived on reconnect (ResumeState boot-reconcile
// + the server work_available sweep) so already-acked rows are dead weight.
//
// Batched (id IN (SELECT ... LIMIT ?)) so a large backlog never locks the table in one
// big transaction — the caller loops until it returns < limit. Times are stored as
// RFC3339Nano UTC strings (clock.Now is always UTC), so the lexicographic '<' is a
// correct time comparison — the same convention the files-GC ListCollectable relies on.
// Returns the number of rows deleted.
func (r *ControlEventRepo) DeleteAckedBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `DELETE FROM worker_control_events
		WHERE id IN (
			SELECT e.id FROM worker_control_events e
			LEFT JOIN env_workers w ON w.id = e.worker_id
			WHERE e.created_at < ?
			  AND e."offset" <= COALESCE(w.last_acked_offset, 9223372036854775807)
			ORDER BY e.created_at
			LIMIT ?
		)`
	res, err := exec.ExecContext(ctx, stmt, ts(cutoff.UTC()), limit)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const eventSelect = `SELECT id, worker_id, "offset", idempotency_key, command_type, payload, created_at FROM worker_control_events`

func scanEvent(scan func(...any) error) (*env.WorkerControlEvent, error) {
	var (
		id, workerID, idempotencyKey, commandType, createdAt string
		offset                                               int64
		payload                                              sql.NullString
	)
	if err := scan(&id, &workerID, &offset, &idempotencyKey, &commandType, &payload, &createdAt); err != nil {
		return nil, err
	}
	return env.NewWorkerControlEvent(env.NewWorkerControlEventInput{
		ID: id, WorkerID: env.WorkerID(workerID), Offset: offset,
		IdempotencyKey: idempotencyKey, CommandType: commandType,
		Payload: payload.String, CreatedAt: parseTime(createdAt),
	})
}

var _ env.ControlEventRepository = (*ControlEventRepo)(nil)
