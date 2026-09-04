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
	StateIdle      = "idle"      // fresh runtime snapshot: slot is currently admissible + empty
	StateDraining  = "draining"  // fresh runtime snapshot: empty high slot kept only until shrink converges
	StateUnknown   = "unknown"   // stale/legacy view: emptiness cannot be asserted
)

// ExecutorSnapshot is one live executor's point-in-time view.
type ExecutorSnapshot struct {
	ExecutorID      string     `json:"executor_id"`
	SlotIndex       *int       `json:"slot_index,omitempty"`
	TaskID          string     `json:"task_id,omitempty"`
	CLI             string     `json:"cli,omitempty"`
	Model           string     `json:"model,omitempty"`
	State           string     `json:"state"`
	StartedAt       time.Time  `json:"started_at,omitempty"`
	PID             int        `json:"pid,omitempty"`
	LastProgressAt  *time.Time `json:"last_progress_at,omitempty"`
	CurrentActivity string     `json:"current_activity,omitempty"`
}

// SlotSnapshot is one addressable runtime slot in an AgentSnapshot. Occupied slots
// mirror the executor fields the UI needs; empty slots carry only slot_index+state.
type SlotSnapshot struct {
	SlotIndex       int        `json:"slot_index"`
	ExecutorID      string     `json:"executor_id,omitempty"`
	TaskID          string     `json:"task_id,omitempty"`
	CLI             string     `json:"cli,omitempty"`
	Model           string     `json:"model,omitempty"`
	State           string     `json:"state"`
	StartedAt       time.Time  `json:"started_at,omitempty"`
	PID             int        `json:"pid,omitempty"`
	LastProgressAt  *time.Time `json:"last_progress_at,omitempty"`
	CurrentActivity string     `json:"current_activity,omitempty"`
}

// AgentSnapshot is one agent's live executor set at heartbeat time. Active is the
// count of slot-occupying executors. The cap + queued depth are joined center-side
// (they are not the worker's to know), while admission/slot/config fields are the
// runtime's own view and may lag the center profile during mixed-version config
// propagation.
type AgentSnapshot struct {
	AdmissionCap   int                `json:"admission_cap,omitempty"`
	SlotCount      int                `json:"slot_count,omitempty"`
	ConfigVersion  int                `json:"config_version,omitempty"`
	Integrity      string             `json:"integrity,omitempty"`
	IntegrityError string             `json:"integrity_error,omitempty"`
	Active         int                `json:"active"`
	Executors      []ExecutorSnapshot `json:"executors"`
	Slots          []SlotSnapshot     `json:"slots,omitempty"`
}

// Execution state enums are the supervisor-control read model. They deliberately
// separate center task authority from runtime executor authority.
const (
	ExecutionModeNone     = "none"
	ExecutionModeInline   = "inline"
	ExecutionModeExecutor = "executor"

	ExecutorStateNone        = "none"
	ExecutorStateActive      = "active"
	ExecutorStateTerminal    = "terminal"
	ExecutorStateStale       = "stale"
	ExecutorStateNonDelivery = "non_delivery"
	ExecutorStateUnknown     = "unknown"

	DeliveryStateNone    = "none"
	DeliveryStateValid   = "valid"
	DeliveryStateInvalid = "invalid"
	DeliveryStateUnknown = "unknown"

	NextActionForkExecutor      = "fork_executor"
	NextActionHandleInline      = "handle_inline"
	NextActionWaitExecutor      = "wait_executor"
	NextActionJudgeExecutor     = "judge_executor"
	NextActionRepairNonDelivery = "repair_non_delivery"
	NextActionResetStale        = "reset_stale_executor"
)

// ExecutionStateSnapshot is the runtime-local control-plane view a supervisor should
// read before deciding work. Center-sourced fields answer "what tasks are assigned
// and runnable"; runtime-sourced fields answer "what this agent is actually doing".
type ExecutionStateSnapshot struct {
	AgentID             string                `json:"agent_id"`
	AvailableTasks      []TaskAuthorityRow    `json:"available_tasks"`
	ActiveTasks         []ExecutionTaskRow    `json:"active_tasks"`
	TaskExecutorMapping []TaskExecutorBinding `json:"task_executor_mapping"`
	Executors           []ExecutorStateRow    `json:"executors"`
	Integrity           string                `json:"integrity,omitempty"`
	IntegrityError      string                `json:"integrity_error,omitempty"`
	UpdatedAt           time.Time             `json:"updated_at"`
}

// TaskAuthorityRow is a center-authority task projection. Runnable means the center
// currently considers it eligible to start; running is only a task-layer occupancy
// projection, not executor liveness.
type TaskAuthorityRow struct {
	TaskID             string `json:"task_id"`
	Title              string `json:"title,omitempty"`
	Status             string `json:"status,omitempty"`
	Runnable           bool   `json:"runnable"`
	RequiredNextAction string `json:"required_next_action,omitempty"`
	BlockedReason      string `json:"blocked_reason,omitempty"`
	BlockedReasonType  string `json:"blocked_reason_type,omitempty"`
	BlockedComment     string `json:"blocked_comment,omitempty"`
	LeaseExpiresAt     string `json:"lease_expires_at,omitempty"`
}

// ExecutionTaskRow is a task row annotated with runtime-owned execution truth and
// the one primary action the supervisor should take next.
type ExecutionTaskRow struct {
	TaskID             string              `json:"task_id"`
	Title              string              `json:"title,omitempty"`
	TaskStatus         string              `json:"task_status,omitempty"`
	Runnable           bool                `json:"runnable"`
	ExecutionMode      string              `json:"execution_mode"`
	ExecutorID         string              `json:"executor_id,omitempty"`
	ExecutorState      string              `json:"executor_state"`
	DeliveryState      string              `json:"delivery_state"`
	Branch             string              `json:"branch,omitempty"`
	HeadSHA            string              `json:"head_sha,omitempty"`
	Worktree           string              `json:"worktree,omitempty"`
	RequiredNextAction string              `json:"required_next_action"`
	Evidence           []ExecutionEvidence `json:"evidence,omitempty"`
}

// TaskExecutorBinding is the canonical runtime mapping projected for the
// supervisor. An empty ExecutorID with mode=inline means the supervisor owns that
// task in-process.
type TaskExecutorBinding struct {
	TaskID        string    `json:"task_id"`
	Mode          string    `json:"mode"`
	ExecutorID    string    `json:"executor_id,omitempty"`
	ExecutorState string    `json:"executor_state,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// ExecutorStateRow is runtime executor authority, enriched from the local file
// protocol when available.
type ExecutorStateRow struct {
	ExecutorID      string              `json:"executor_id"`
	TaskID          string              `json:"task_id,omitempty"`
	SlotIndex       *int                `json:"slot_index,omitempty"`
	State           string              `json:"state"`
	PID             int                 `json:"pid,omitempty"`
	StartedAt       time.Time           `json:"started_at,omitempty"`
	LastProgressAt  *time.Time          `json:"last_progress_at,omitempty"`
	CurrentActivity string              `json:"current_activity,omitempty"`
	CLI             string              `json:"cli,omitempty"`
	Model           string              `json:"model,omitempty"`
	Worktree        string              `json:"worktree,omitempty"`
	DeliveryState   string              `json:"delivery_state,omitempty"`
	Branch          string              `json:"branch,omitempty"`
	HeadSHA         string              `json:"head_sha,omitempty"`
	ReasonCodes     []string            `json:"reason_codes,omitempty"`
	RequiredAction  string              `json:"required_action,omitempty"`
	Evidence        []ExecutionEvidence `json:"evidence,omitempty"`
}

// ExecutionEvidence carries concise machine-readable reasons behind a row's state.
type ExecutionEvidence struct {
	Source  string `json:"source"`
	Kind    string `json:"kind"`
	Message string `json:"message,omitempty"`
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
