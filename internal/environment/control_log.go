package environment

import (
	"context"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
)

// ControlLog is the command-stream service (ADR-0050 §4 core). It assigns the
// per-Worker monotonic offset, enforces center-side idempotency (a re-issued
// logical command — same idempotency_key — does NOT create a second stream
// entry, so a destructive stop/reset is never duplicated), and serves the replay
// set for a reconnecting Worker (everything after its last acked offset).
//
// This is the D1 #102 acceptance core. Process control (executing the commands)
// and the agent.lifecycle→command projector are D2.
type ControlLog struct {
	events ControlEventRepository
	idgen  idgen.Generator
	clock  clock.Clock
}

// NewControlLog constructs the service.
func NewControlLog(events ControlEventRepository, gen idgen.Generator, clk clock.Clock) *ControlLog {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ControlLog{events: events, idgen: gen, clock: clk}
}

// AppendCommandInput captures an enqueue request.
type AppendCommandInput struct {
	WorkerID       WorkerID
	CommandType    string
	Payload        string
	IdempotencyKey string
}

// AppendCommand enqueues a command for a Worker. IDEMPOTENT: if a command with
// the same (worker, idempotency_key) already exists, it returns that existing
// entry unchanged (no new offset, no duplicate destructive command). Otherwise
// it assigns the next offset (max+1) and appends.
func (l *ControlLog) AppendCommand(ctx context.Context, in AppendCommandInput) (*WorkerControlEvent, error) {
	if in.CommandType == "" {
		return nil, ErrEmptyCommandType
	}
	if in.IdempotencyKey == "" {
		return nil, ErrEmptyIdempotencyKey
	}
	// Center-side idempotency: re-issuing the same logical command is a no-op
	// that returns the already-enqueued entry (best-effort here; the sqlite
	// UNIQUE(worker_id, idempotency_key) constraint is the race backstop).
	if existing, err := l.events.FindByIdempotencyKey(ctx, in.WorkerID, in.IdempotencyKey); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	maxOff, err := l.events.MaxOffset(ctx, in.WorkerID)
	if err != nil {
		return nil, err
	}
	evt, err := NewWorkerControlEvent(NewWorkerControlEventInput{
		ID:             l.idgen.NewULID(),
		WorkerID:       in.WorkerID,
		Offset:         maxOff + 1,
		IdempotencyKey: in.IdempotencyKey,
		CommandType:    in.CommandType,
		Payload:        in.Payload,
		CreatedAt:      l.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	if err := l.events.Append(ctx, evt); err != nil {
		return nil, err
	}
	return evt, nil
}

// CommandsAfter returns the replay set: commands with offset strictly greater
// than `offset`, ascending. Pass worker.LastAckedOffset() to get exactly what a
// reconnecting Worker still needs.
func (l *ControlLog) CommandsAfter(ctx context.Context, workerID WorkerID, offset int64) ([]*WorkerControlEvent, error) {
	return l.events.ListAfter(ctx, workerID, offset)
}

// Replay is the convenience wrapper for a reconnecting Worker: the commands
// after its cumulative ack cursor.
func (l *ControlLog) Replay(ctx context.Context, w *Worker) ([]*WorkerControlEvent, error) {
	return l.events.ListAfter(ctx, w.ID(), w.LastAckedOffset())
}
