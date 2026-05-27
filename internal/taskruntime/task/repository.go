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
	// FindAll returns every task, with optional status / limit from
	// Filter applied. Added in v2.5.15 to back the Web Console "All
	// projects" filter — FindByStatus alone could not service the
	// "All status × All projects" cell.
	FindAll(ctx context.Context, filter Filter) ([]*Task, error)
	Save(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
}
