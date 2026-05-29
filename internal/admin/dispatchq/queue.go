// Package dispatchq is the v2.2 in-process dispatch/kill queue —
// production transport between center DispatchService / KillCoordinator
// and the worker daemon over the admin unix-socket endpoint. Replaces
// the dispatch.NoopSender / kill.NoopKillSender stubs that v2.0 GA
// shipped with (per the conventions § 0.4 mock-as-default cleanup).
//
// Model: per-worker queues, FIFO. Senders (DispatchService /
// KillCoordinator) append; workers drain via admin endpoint. The
// admin endpoint serializes drain to one consumer per worker — if a
// second poller hits the same worker_id, both see the same envelopes
// (idempotency belongs to the protocol, not the queue, per
// dispatch/protocol.md § 3 — DispatchAck/Nack carries the dedup).
//
// v2.2 single-host scope: no persistence. If the server crashes the
// queue is lost; this matches v2.0 GA behavior (NoopSender lost
// everything). v2.3+ may persist or move to event-sourced replay; out
// of scope here.
package dispatchq

import (
	"context"
	"sync"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// KillRequest is the v2.2 kill payload sent to a worker. Mirrors
// kill.KillSender.SendKill args; defined here so consumers don't
// have to import the kill package.
type KillRequest struct {
	WorkerID    string                      `json:"worker_id"`
	ExecutionID taskruntime.TaskExecutionID `json:"execution_id"`
	Reason      execution.KilledReason      `json:"reason"`
	Message     string                      `json:"message"`
}

// Queue is the per-worker in-process dispatch + kill backlog.
type Queue struct {
	mu         sync.Mutex
	dispatches map[string][]dispatch.DispatchEnvelope // worker_id → FIFO
	kills      map[string][]KillRequest               // worker_id → FIFO
}

// New constructs an empty queue.
func New() *Queue {
	return &Queue{
		dispatches: make(map[string][]dispatch.DispatchEnvelope),
		kills:      make(map[string][]KillRequest),
	}
}

// PushDispatch appends an envelope to the worker's queue. Routed by
// env.WorkerID.
func (q *Queue) PushDispatch(env dispatch.DispatchEnvelope) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dispatches[env.WorkerID] = append(q.dispatches[env.WorkerID], env)
}

// PushKill appends a kill request to the worker's queue.
func (q *Queue) PushKill(req KillRequest) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.kills[req.WorkerID] = append(q.kills[req.WorkerID], req)
}

// DrainDispatches returns all pending envelopes for the worker and
// clears its dispatch queue. Returns an empty slice (not nil) when
// nothing is pending so JSON marshaling is deterministic.
func (q *Queue) DrainDispatches(workerID string) []dispatch.DispatchEnvelope {
	q.mu.Lock()
	defer q.mu.Unlock()
	pending := q.dispatches[workerID]
	delete(q.dispatches, workerID)
	if pending == nil {
		return []dispatch.DispatchEnvelope{}
	}
	return pending
}

// DrainKills returns all pending kill requests for the worker.
func (q *Queue) DrainKills(workerID string) []KillRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	pending := q.kills[workerID]
	delete(q.kills, workerID)
	if pending == nil {
		return []KillRequest{}
	}
	return pending
}

// Senders returns the dispatch.EnvelopeSender + kill.KillSender
// adapters that route into this queue. Used in cli.App wiring to
// replace NoopSender / NoopKillSender.

// DispatchSender implements dispatch.EnvelopeSender by pushing onto
// the queue.
type DispatchSender struct{ Q *Queue }

// Send pushes env onto the worker's dispatch queue. Never blocks.
func (s DispatchSender) Send(_ context.Context, env dispatch.DispatchEnvelope) error {
	s.Q.PushDispatch(env)
	return nil
}

// KillSender implements kill.KillSender by pushing onto the queue.
type KillSender struct{ Q *Queue }

// SendKill pushes a kill request onto the worker's kill queue. The
// worker_id is resolved by looking up the execution's task assignment
// — but at the KillCoordinator boundary we don't have it yet, so the
// queue accepts the kill addressed to the worker the coordinator
// already resolved. Until v2.3 adds execution→worker_id lookup at the
// admin transport boundary, this sender stores kills keyed by
// execution_id and worker daemons match by claiming.
//
// v2.2 simplification: workers poll for ALL kills they're owed (i.e.
// kills whose execution they currently hold). The polling endpoint
// filters by the worker's claimed executions. This is the smallest
// correct semantic; v2.3 can optimize.
func (s KillSender) SendKill(_ context.Context, execID taskruntime.TaskExecutionID, reason execution.KilledReason, message string) error {
	// v2.2: store under "" worker_id key — admin endpoint
	// /admin/kill/pending?worker_id=X filters by checking which
	// executions worker X currently owns. Phase C worker daemon owns
	// the claim/own state, so this filter lives on the daemon side.
	s.Q.PushKill(KillRequest{
		WorkerID:    "", // resolved at drain time
		ExecutionID: execID,
		Reason:      reason,
		Message:     message,
	})
	return nil
}

// Pending returns counts (for observability / debug endpoints).
func (q *Queue) Pending(workerID string) (dispatchCount, killCount int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.dispatches[workerID]), len(q.kills[workerID])
}

// AllKills drains the "" worker_id slot (kills not yet routed).
// Phase C worker daemon will call this and filter by owned executions.
func (q *Queue) AllKills() []KillRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	pending := q.kills[""]
	delete(q.kills, "")
	if pending == nil {
		return []KillRequest{}
	}
	return pending
}
