package execution

import (
	"context"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// Repository per 00-overview § 5.2.
type Repository interface {
	FindByID(ctx context.Context, id taskruntime.TaskExecutionID) (*TaskExecution, error)
	FindByTaskID(ctx context.Context, taskID taskruntime.TaskID) ([]*TaskExecution, error)
	FindByWorkerID(ctx context.Context, workerID string, statuses ...Status) ([]*TaskExecution, error)
	FindActive(ctx context.Context) ([]*TaskExecution, error)
	FindPendingAckOlderThan(ctx context.Context, cutoff string) ([]*TaskExecution, error)
	FindSubmittedOlderThan(ctx context.Context, cutoff string) ([]*TaskExecution, error)
	Save(ctx context.Context, e *TaskExecution) error
	Update(ctx context.Context, e *TaskExecution) error
}
