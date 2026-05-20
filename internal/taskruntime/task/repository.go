package task

import (
	"context"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// Filter narrows TaskRepository.Find queries.
type Filter struct {
	ProjectID string
	Status    *Status
	Limit     int
}

// DefaultLimit caps Find when Limit <= 0.
const DefaultLimit = 100

// Repository per 00-overview § 5.1.
type Repository interface {
	FindByID(ctx context.Context, id taskruntime.TaskID) (*Task, error)
	FindByProject(ctx context.Context, projectID string, filter Filter) ([]*Task, error)
	FindByStatus(ctx context.Context, status Status, filter Filter) ([]*Task, error)
	FindBlockedBy(ctx context.Context, blockerTaskID taskruntime.TaskID) ([]*Task, error)
	Save(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
}
