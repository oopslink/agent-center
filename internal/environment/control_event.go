package environment

import (
	"strings"
	"time"
)

// WorkerControlEvent is one command in a Worker's ordered, replayable control
// stream (ADR-0050 §4). Per-Worker `offset` is a strictly increasing sequence
// the center assigns; the Worker acks cumulatively up to an offset (Worker AR
// lastAckedOffset) and the center replays everything after that on reconnect.
// `idempotencyKey` lets the Worker (D2) skip re-executing a destructive command
// (stop/reset) seen again after a reconnect, and lets the center dedup a
// re-issued logical command into the same stream entry.
type WorkerControlEvent struct {
	id             string
	workerID       WorkerID
	offset         int64
	idempotencyKey string
	commandType    string
	payload        string
	createdAt      time.Time
}

// NewWorkerControlEventInput captures constructor args. The offset is assigned by
// the ControlLog service (next per-worker sequence), not by the caller.
type NewWorkerControlEventInput struct {
	ID             string
	WorkerID       WorkerID
	Offset         int64
	IdempotencyKey string
	CommandType    string
	Payload        string
	CreatedAt      time.Time
}

// NewWorkerControlEvent constructs a command stream entry.
func NewWorkerControlEvent(in NewWorkerControlEventInput) (*WorkerControlEvent, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, ErrWorkerNotFound // defensive; id is service-generated
	}
	if strings.TrimSpace(string(in.WorkerID)) == "" {
		return nil, ErrWorkerNotFound
	}
	if strings.TrimSpace(in.CommandType) == "" {
		return nil, ErrEmptyCommandType
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, ErrEmptyIdempotencyKey
	}
	if in.Offset < 1 {
		return nil, ErrOffsetRegress
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now()
	}
	return &WorkerControlEvent{
		id:             in.ID,
		workerID:       in.WorkerID,
		offset:         in.Offset,
		idempotencyKey: in.IdempotencyKey,
		commandType:    in.CommandType,
		payload:        in.Payload,
		createdAt:      in.CreatedAt.UTC(),
	}, nil
}

// Getters.
func (e *WorkerControlEvent) ID() string             { return e.id }
func (e *WorkerControlEvent) WorkerID() WorkerID     { return e.workerID }
func (e *WorkerControlEvent) Offset() int64          { return e.offset }
func (e *WorkerControlEvent) IdempotencyKey() string { return e.idempotencyKey }
func (e *WorkerControlEvent) CommandType() string    { return e.commandType }
func (e *WorkerControlEvent) Payload() string        { return e.payload }
func (e *WorkerControlEvent) CreatedAt() time.Time   { return e.createdAt }
