package environment

import "context"

// WorkerRepository persists Worker ARs (D1). The sqlite implementation honors
// persistence.ExecutorFromCtx so writes can compose in one transaction.
type WorkerRepository interface {
	Save(ctx context.Context, w *Worker) error
	Update(ctx context.Context, w *Worker) error
	FindByID(ctx context.Context, id WorkerID) (*Worker, error)
}

// ControlEventRepository persists the per-Worker WorkerControlEvent stream.
//
// Invariants the sqlite layer enforces with UNIQUE constraints (the ControlLog
// service checks them best-effort first): UNIQUE(worker_id, offset) — offsets are
// dense per worker — and UNIQUE(worker_id, idempotency_key) — a logical command
// appears at most once in a worker's stream (the center-side dedup of re-issued
// destructive commands).
type ControlEventRepository interface {
	// Append writes one command. Returns ErrOffsetRegress on a non-monotonic
	// offset and (sqlite) a uniqueness error on a duplicate idempotency key.
	Append(ctx context.Context, e *WorkerControlEvent) error
	// MaxOffset returns the highest offset for a worker, or 0 if none.
	MaxOffset(ctx context.Context, workerID WorkerID) (int64, error)
	// FindByIdempotencyKey returns the existing stream entry for a (worker, key)
	// pair, or ErrWorkerNotFound-style nil,nil when absent.
	FindByIdempotencyKey(ctx context.Context, workerID WorkerID, key string) (*WorkerControlEvent, error)
	// ListAfter returns commands with offset strictly greater than `offset`,
	// ascending — the replay set for a reconnecting worker (offset =
	// worker.LastAckedOffset()).
	ListAfter(ctx context.Context, workerID WorkerID, offset int64) ([]*WorkerControlEvent, error)
}
