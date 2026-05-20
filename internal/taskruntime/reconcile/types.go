// Package reconcile hosts the ReconcileService domain service + VOs
// (ReconcileRequest / ReconcileResponse).
package reconcile

import (
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// LocalActiveExecution describes one execution the worker thinks is active
// locally (still has running shim + agent).
type LocalActiveExecution struct {
	ExecutionID taskruntime.TaskExecutionID `json:"execution_id"`
	Status      string                      `json:"status"` // worker-side status snapshot
}

// Request is Worker → Center reconcile payload (00-overview § 3.2).
type Request struct {
	WorkerID         string                 `json:"worker_id"`
	LocalActives     []LocalActiveExecution `json:"local_active_executions"`
}

// Group is one of the three reconcile-response classifications.
type Group string

const (
	GroupActive  Group = "active"
	GroupStale   Group = "stale"
	GroupUnknown Group = "unknown"
)

// Response is Center → Worker reconcile decision split into 3 groups.
type Response struct {
	Active  []taskruntime.TaskExecutionID `json:"active"`
	Stale   []taskruntime.TaskExecutionID `json:"stale"`
	Unknown []taskruntime.TaskExecutionID `json:"unknown"`
}
