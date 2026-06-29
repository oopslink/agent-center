// Package concurrency holds the shared, dependency-free types for the real-time
// per-agent executor concurrency view (v2.19.0, #并发讨论2): the worker daemon
// builds a per-agent Snapshot from its live executor pool + orphans and ships it on
// the heartbeat; the center stores the latest snapshot per agent (LiveStateStore)
// and serves it on GET .../agents/{id}/concurrency.
//
// It imports nothing from the rest of the tree (only time) so both the worker
// (workerdaemon) and the center (admin/api heartbeat handler + webconsole endpoint)
// can share ONE wire/contract type with no import cycle.
package concurrency

import (
	"sync"
	"time"
)

// Executor states reported in a snapshot.
const (
	StateStarting  = "starting"  // spawned, no running status yet
	StateRunning   = "running"   // status=running
	StateFinishing = "finishing" // terminal status (done/failed), slot not yet freed
	StateOrphan    = "orphan"    // adopted across a daemon restart (no reapable handle)
)

// ExecutorSnapshot is one live executor's point-in-time view.
type ExecutorSnapshot struct {
	ExecutorID     string     `json:"executor_id"`
	TaskID         string     `json:"task_id,omitempty"`
	CLI            string     `json:"cli,omitempty"`
	Model          string     `json:"model,omitempty"`
	State          string     `json:"state"`
	StartedAt      time.Time  `json:"started_at,omitempty"`
	PID            int        `json:"pid,omitempty"`
	LastProgressAt *time.Time `json:"last_progress_at,omitempty"`
}

// AgentSnapshot is one agent's live executor set at heartbeat time. Active is the
// count of slot-occupying executors (== len(Executors)); the cap + queued depth are
// joined center-side (they are not the worker's to know).
type AgentSnapshot struct {
	Active    int                `json:"active"`
	Executors []ExecutorSnapshot `json:"executors"`
}

// LiveStateStore keeps the latest per-agent snapshot the center received on a
// heartbeat. The interface is small + behind a port so a future backend (Redis /
// shared cache for a multi-process center) can replace the in-memory default.
type LiveStateStore interface {
	// Put records agent's latest snapshot, stamped received-at=now.
	Put(agentID string, snap AgentSnapshot, now time.Time)
	// Get returns the last-known snapshot for agent, its age (now - received_at),
	// and ok=false when none was ever recorded.
	Get(agentID string, now time.Time) (snap AgentSnapshot, age time.Duration, ok bool)
}

// storedSnapshot is the in-memory record: the snapshot + when it arrived.
type storedSnapshot struct {
	snap       AgentSnapshot
	receivedAt time.Time
}

// InMemoryStore is the single-process default LiveStateStore (a mutex-guarded map).
// Staleness (age > TTL) is decided by the READER against its own TTL, so the store
// stays a dumb latest-value cache and always returns the last-known value.
type InMemoryStore struct {
	mu sync.Mutex
	m  map[string]storedSnapshot
}

// NewInMemoryStore builds an empty store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{m: make(map[string]storedSnapshot)}
}

// Put replaces agent's snapshot.
func (s *InMemoryStore) Put(agentID string, snap AgentSnapshot, now time.Time) {
	if agentID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[agentID] = storedSnapshot{snap: snap, receivedAt: now}
}

// Get returns the last-known snapshot + its age (ok=false when never recorded).
func (s *InMemoryStore) Get(agentID string, now time.Time) (AgentSnapshot, time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[agentID]
	if !ok {
		return AgentSnapshot{}, 0, false
	}
	return st.snap, now.Sub(st.receivedAt), true
}

// compile-time: InMemoryStore is a LiveStateStore.
var _ LiveStateStore = (*InMemoryStore)(nil)
