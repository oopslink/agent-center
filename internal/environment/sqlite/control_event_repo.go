package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

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
	if isUnique(err) {
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
