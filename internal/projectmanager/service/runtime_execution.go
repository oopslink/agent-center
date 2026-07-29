package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ensureRuntimeExecution is the single production consumer of the AI Runtime
// resolve/freeze contract. A project task is the logical execution identity in the
// current PM model, so all continuations deliberately use the same task id.
func (s *Service) ensureRuntimeExecution(ctx context.Context, task *pm.Task) error {
	if s.runtimeExecutions == nil {
		return nil
	}
	project, err := s.projects.FindByID(ctx, task.ProjectID())
	if err != nil {
		return err
	}
	return s.runtimeExecutions.EnsureExecution(ctx, project.OrganizationID(), string(task.ID()))
}
