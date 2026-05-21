// Package projection implements Observability BC projection read models.
//
// Per observability/00-overview § 1.4 + plan-4 § 3.1 / § 3.2 / § 1.3:
//   - TaskExecutionProjection — independent table task_execution_projections
//     (PK 1:1 with task_executions; BC-owned per conventions § 9.z).
//   - TaskStatusProjection helper — same-tx double write into caller-BC's
//     own task / task_execution rows (conventions § 9.z: this helper does
//     NOT write the projections table).
package projection

import (
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// TaskExecutionProjection is the read-model VO mirroring a single
// task_execution_projections row.
//
// Per 02-persistence-schema § 8.2.1: PK = task_execution_id, no version
// column (high-frequency UPSERT, staleness guarded by last_push_at).
type TaskExecutionProjection struct {
	TaskExecutionID           taskruntime.TaskExecutionID
	CurrentActivity           string
	CurrentActivityAt         time.Time
	TotalToolCalls            int64
	TotalTokensInput          int64
	TotalTokensOutput         int64
	WorkingSecondsAccumulated int64
	LastPushAt                time.Time
}

// ProjectionUpdate is the VO carrying a single worker daemon push payload.
//
// Per plan-4 § 1.3 + observability/00-overview § 5.2: UpdateProjection
// accepts this; the service merges into the existing row (UPSERT) and
// applies staleness protection on LastPushAt.
type ProjectionUpdate struct {
	CurrentActivity           string
	CurrentActivityAt         time.Time
	TotalToolCalls            int64
	TotalTokensInput          int64
	TotalTokensOutput         int64
	WorkingSecondsAccumulated int64
	LastPushAt                time.Time
}

// Validate checks that LastPushAt is set; other fields are allowed to be
// zero (e.g. an early heartbeat may not yet have tool calls).
func (u ProjectionUpdate) Validate() error {
	if u.LastPushAt.IsZero() {
		return errors.New("projection update: last_push_at required")
	}
	if u.TotalToolCalls < 0 ||
		u.TotalTokensInput < 0 ||
		u.TotalTokensOutput < 0 ||
		u.WorkingSecondsAccumulated < 0 {
		return errors.New("projection update: counters cannot be negative")
	}
	return nil
}

// Sentinel errors for the projection repository / service.
var (
	// ErrProjectionNotFound — FindByID with no matching row.
	ErrProjectionNotFound = errors.New("observability/projection: not found")
	// ErrProjectionStale — UPSERT request's LastPushAt is older than the
	// stored row's LastPushAt; service drops the write and emits an event
	// instead.
	ErrProjectionStale = errors.New("observability/projection: stale push (out of order)")
)
