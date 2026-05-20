package inputrequest

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// Repository per 00-overview § 5.3.
type Repository interface {
	FindByID(ctx context.Context, id taskruntime.InputRequestID) (*InputRequest, error)
	FindByTaskExecutionID(ctx context.Context, executionID taskruntime.TaskExecutionID) (*InputRequest, error)
	FindPending(ctx context.Context, olderThan time.Time) ([]*InputRequest, error)
	Save(ctx context.Context, r *InputRequest) error
	Update(ctx context.Context, r *InputRequest) error
}
