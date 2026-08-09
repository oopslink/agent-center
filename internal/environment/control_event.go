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
	id              string
	workerID        WorkerID
	offset          int64
	idempotencyKey  string
	commandType     string
	payload         string
	agentID         string
	taskID          string
	status          string
	statusReason    string
	statusDetail    string
	executionID     string
	statusUpdatedAt time.Time
	createdAt       time.Time
}

// NewWorkerControlEventInput captures constructor args. The offset is assigned by
// the ControlLog service (next per-worker sequence), not by the caller.
type NewWorkerControlEventInput struct {
	ID              string
	WorkerID        WorkerID
	Offset          int64
	IdempotencyKey  string
	CommandType     string
	Payload         string
	AgentID         string
	TaskID          string
	Status          string
	StatusReason    string
	StatusDetail    string
	ExecutionID     string
	StatusUpdatedAt time.Time
	CreatedAt       time.Time
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
		id:              in.ID,
		workerID:        in.WorkerID,
		offset:          in.Offset,
		idempotencyKey:  in.IdempotencyKey,
		commandType:     in.CommandType,
		payload:         in.Payload,
		agentID:         strings.TrimSpace(in.AgentID),
		taskID:          strings.TrimSpace(in.TaskID),
		status:          strings.TrimSpace(in.Status),
		statusReason:    strings.TrimSpace(in.StatusReason),
		statusDetail:    strings.TrimSpace(in.StatusDetail),
		executionID:     strings.TrimSpace(in.ExecutionID),
		statusUpdatedAt: in.StatusUpdatedAt.UTC(),
		createdAt:       in.CreatedAt.UTC(),
	}, nil
}

// Getters.
func (e *WorkerControlEvent) ID() string                 { return e.id }
func (e *WorkerControlEvent) WorkerID() WorkerID         { return e.workerID }
func (e *WorkerControlEvent) Offset() int64              { return e.offset }
func (e *WorkerControlEvent) IdempotencyKey() string     { return e.idempotencyKey }
func (e *WorkerControlEvent) CommandType() string        { return e.commandType }
func (e *WorkerControlEvent) Payload() string            { return e.payload }
func (e *WorkerControlEvent) AgentID() string            { return e.agentID }
func (e *WorkerControlEvent) TaskID() string             { return e.taskID }
func (e *WorkerControlEvent) Status() string             { return e.status }
func (e *WorkerControlEvent) StatusReason() string       { return e.statusReason }
func (e *WorkerControlEvent) StatusDetail() string       { return e.statusDetail }
func (e *WorkerControlEvent) ExecutionID() string        { return e.executionID }
func (e *WorkerControlEvent) StatusUpdatedAt() time.Time { return e.statusUpdatedAt }
func (e *WorkerControlEvent) CreatedAt() time.Time       { return e.createdAt }
