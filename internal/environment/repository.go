package environment

import (
	"context"
	"time"
)

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
	// FindByID returns one stream entry by id, or nil,nil when absent.
	FindByID(ctx context.Context, id string) (*WorkerControlEvent, error)
	// LatestNonTerminalByAgentTask returns the newest non-terminal command for a
	// logical agent/task fork attempt, or nil,nil when there is none.
	LatestNonTerminalByAgentTask(ctx context.Context, workerID WorkerID, commandType, agentID, taskID string) (*WorkerControlEvent, error)
	// ListAfter returns commands with offset strictly greater than `offset`,
	// ascending — the replay set for a reconnecting worker (offset =
	// worker.LastAckedOffset()).
	ListAfter(ctx context.Context, workerID WorkerID, offset int64) ([]*WorkerControlEvent, error)
	// ListByAgentTask returns command rows for an agent/task pair ordered newest
	// first. Read models use this to surface pending/terminal fork commands even
	// before an executor.start activity exists.
	ListByAgentTask(ctx context.Context, workerID WorkerID, commandType, agentID, taskID string) ([]*WorkerControlEvent, error)
	UpdateStatus(ctx context.Context, in UpdateCommandStatusInput) (*WorkerControlEvent, error)
}

const (
	CommandStatusPending   = "pending"
	CommandStatusStarted   = "started"
	CommandStatusSucceeded = "succeeded"
	CommandStatusRejected  = "rejected"
	CommandStatusFailed    = "failed"
	CommandStatusExpired   = "expired"
)

func CommandStatusTerminal(status string) bool {
	switch status {
	case CommandStatusSucceeded, CommandStatusRejected, CommandStatusFailed, CommandStatusExpired:
		return true
	default:
		return false
	}
}

type UpdateCommandStatusInput struct {
	WorkerID        WorkerID
	CommandID       string
	AgentID         string
	TaskID          string
	Status          string
	StatusReason    string
	StatusDetail    string
	ExecutionID     string
	StatusUpdatedAt time.Time
}
