package environment

import (
	"errors"
	"strings"
	"time"
)

// Worker is the Environment-BC aggregate for a machine-deployed daemon
// (ADR-0050 §2/§3). One machine may run several Workers; one Worker controls
// many Agents (1:N). The Worker carries the connection status + the cumulative
// ack cursor (lastAckedOffset) the center uses to decide what to replay on
// reconnect. The runtime binding from Agent→Worker is immutable and lives on
// the Agent AR (ADR-0049); the Worker does not own Agents here.
type Worker struct {
	id              WorkerID
	name            string
	status          WorkerStatus
	lastAckedOffset int64 // highest contiguous command offset the Worker has acked
	lastHeartbeatAt time.Time
	createdAt       time.Time
	updatedAt       time.Time
	version         int
}

// NewWorkerInput captures constructor args. v2.7 #140 step-3: org is no longer
// stored on the Worker — it is derived from the canonical workforce.Worker at the
// handler layer; the control-channel AR carries only cursor/online state.
type NewWorkerInput struct {
	ID        WorkerID
	Name      string
	CreatedAt time.Time
}

// NewWorker registers a fresh Worker in the offline state (it has not yet opened
// a control stream). lastAckedOffset starts at 0 (no command acked).
func NewWorker(in NewWorkerInput) (*Worker, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("environment: worker id required")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("environment: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Worker{
		id:              in.ID,
		name:            in.Name,
		status:          WorkerOffline,
		lastAckedOffset: 0,
		createdAt:       at,
		updatedAt:       at,
		version:         1,
	}, nil
}

// RehydrateWorkerInput is for repository round-trip.
type RehydrateWorkerInput struct {
	ID              WorkerID
	Name            string
	Status          WorkerStatus
	LastAckedOffset int64
	LastHeartbeatAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

// RehydrateWorker reconstructs without invariant checks (repo use).
func RehydrateWorker(in RehydrateWorkerInput) (*Worker, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidWorkerStatus
	}
	if in.Version < 1 {
		return nil, errors.New("environment: version must be >= 1")
	}
	return &Worker{
		id:              in.ID,
		name:            in.Name,
		status:          in.Status,
		lastAckedOffset: in.LastAckedOffset,
		lastHeartbeatAt: in.LastHeartbeatAt.UTC(),
		createdAt:       in.CreatedAt.UTC(),
		updatedAt:       in.UpdatedAt.UTC(),
		version:         in.Version,
	}, nil
}

// Getters.
func (w *Worker) ID() WorkerID               { return w.id }
func (w *Worker) Name() string               { return w.name }
func (w *Worker) Status() WorkerStatus       { return w.status }
func (w *Worker) LastAckedOffset() int64     { return w.lastAckedOffset }
func (w *Worker) LastHeartbeatAt() time.Time { return w.lastHeartbeatAt }
func (w *Worker) CreatedAt() time.Time       { return w.createdAt }
func (w *Worker) UpdatedAt() time.Time       { return w.updatedAt }
func (w *Worker) Version() int               { return w.version }

// Connect marks the Worker online (a control stream opened) and records the
// heartbeat. Idempotent: connecting an already-online Worker just refreshes the
// heartbeat.
func (w *Worker) Connect(at time.Time) {
	w.status = WorkerOnline
	w.lastHeartbeatAt = at.UTC()
	w.touch(at)
}

// Disconnect marks the Worker offline (stream closed / heartbeat timeout). The
// Agent availability derivation treats every Agent on an offline Worker as
// unavailable (OQ2).
func (w *Worker) Disconnect(at time.Time) {
	w.status = WorkerOffline
	w.touch(at)
}

// Heartbeat refreshes liveness while online. A heartbeat from an offline Worker
// also brings it online (the stream is evidently live).
func (w *Worker) Heartbeat(at time.Time) {
	w.status = WorkerOnline
	w.lastHeartbeatAt = at.UTC()
	w.touch(at)
}

// AckOffset advances the cumulative ack cursor to `offset` (the Worker has
// processed every command up to and including it). Cumulative + monotonic: a
// stale/duplicate ack (offset <= current) is a tolerated no-op (so replayed acks
// after a reconnect don't error); a forward ack advances the cursor. This cursor
// is what CommandsAfter replays from, so it is the heart of "reconnect doesn't
// re-deliver already-processed commands".
func (w *Worker) AckOffset(offset int64, at time.Time) {
	if offset <= w.lastAckedOffset {
		return // idempotent: tolerate stale/replayed acks
	}
	w.lastAckedOffset = offset
	w.touch(at)
}

func (w *Worker) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	w.updatedAt = at.UTC()
	w.version++
}
