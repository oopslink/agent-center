// Package environment is the Environment bounded context (v2.7, ADR-0050): the
// runtime that hosts machine-deployed Workers, the worker-initiated control
// channel, and (later phases) the AgentController + FileTransfer.
//
// D1 (task #102) ships the protocol CORE: the Worker aggregate + the ordered,
// replayable WorkerControlEvent command stream with offset-cumulative ack and
// per-command idempotency keys, so a reconnecting Worker replays from its last
// acked offset WITHOUT re-issuing destructive commands. Process control
// (AgentController) and the agent.lifecycle→command projector are D2; FileTransfer
// is D3. This BC stands ALONGSIDE the legacy workforce.Worker (Fleet + the old
// dispatch path keep running unchanged until the D2 cutover + #107 retirement).
package environment

import "errors"

// Typed identifiers.
type WorkerID string

func (id WorkerID) String() string { return string(id) }

// WorkerStatus is the Worker's CONNECTION state — deliberately separate from
// Agent.lifecycle (ADR-0050 §3). Agent.availability derives from both, with the
// Worker dimension highest priority (a Worker offline → all its Agents
// unavailable, plan §10 OQ2).
type WorkerStatus string

const (
	// WorkerOffline is the initial state and the state after a disconnect or a
	// heartbeat timeout — no live control stream.
	WorkerOffline WorkerStatus = "offline"
	// WorkerOnline means the Worker holds a live control stream to the center.
	WorkerOnline WorkerStatus = "online"
)

// IsValid reports enum membership.
func (s WorkerStatus) IsValid() bool {
	return s == WorkerOffline || s == WorkerOnline
}

// Sentinel errors.
var (
	ErrWorkerNotFound      = errors.New("environment: worker not found")
	ErrWorkerExists        = errors.New("environment: worker already exists")
	ErrInvalidWorkerStatus = errors.New("environment: invalid worker status")
	// ErrOffsetRegress is returned when an ack or an appended offset would move
	// the per-worker monotonic offset backwards.
	ErrOffsetRegress       = errors.New("environment: offset must not move backwards")
	ErrEmptyCommandType    = errors.New("environment: command type required")
	ErrEmptyIdempotencyKey = errors.New("environment: idempotency key required")
)
