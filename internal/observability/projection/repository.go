package projection

import (
	"context"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// Repository is the Observability BC projection repository (observability/00
// § 5.2). Owned by Observability — TaskRuntime must NOT import / write
// this table directly (conventions § 9.z + plan-4 § 1.4).
type Repository interface {
	// FindByID returns the projection row for the given task execution.
	// Returns ErrProjectionNotFound if absent.
	FindByID(ctx context.Context, id taskruntime.TaskExecutionID) (*TaskExecutionProjection, error)

	// FindByIDs returns projections for the given task executions in any
	// order. IDs without a row are simply absent from the result map.
	FindByIDs(ctx context.Context, ids []taskruntime.TaskExecutionID) (map[taskruntime.TaskExecutionID]*TaskExecutionProjection, error)

	// UpsertIfFresh tries to INSERT-or-UPDATE a row.
	// staleness rule: if a row already exists with a stored
	// last_push_at >= update.LastPushAt, the row is NOT modified and the
	// method returns (existingLastPushAt, false, ErrProjectionStale).
	// On a fresh write, returns (update.LastPushAt, true, nil).
	UpsertIfFresh(ctx context.Context, id taskruntime.TaskExecutionID, update ProjectionUpdate) (existing TaskExecutionProjection, fresh bool, err error)
}
