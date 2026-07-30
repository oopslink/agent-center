package service

import (
	"context"
	"strings"

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
	agentID := strings.TrimPrefix(string(task.Assignee()), "agent:")
	return s.runtimeExecutions.EnsureExecution(ctx, project.OrganizationID(), string(task.ID()), agentID)
}

type inlineRuntimeCompatibility interface {
	EnsureInlineCompatible(context.Context, string, string, string) error
}

func (s *Service) ensureInlineRuntimeCompatible(ctx context.Context, task *pm.Task) error {
	if task.DispatchMode() != pm.DispatchSupervisorInline || s.runtimeExecutions == nil {
		return nil
	}
	checker, ok := s.runtimeExecutions.(inlineRuntimeCompatibility)
	if !ok {
		return nil
	}
	project, err := s.projects.FindByID(ctx, task.ProjectID())
	if err != nil {
		return err
	}
	agentID := strings.TrimPrefix(string(task.Assignee()), "agent:")
	return checker.EnsureInlineCompatible(ctx, project.OrganizationID(), string(task.ID()), agentID)
}

func (s *Service) RuntimeExecution(ctx context.Context, task *pm.Task) (any, bool, error) {
	if s.runtimeExecutions == nil {
		return nil, false, nil
	}
	project, err := s.projects.FindByID(ctx, task.ProjectID())
	if err != nil {
		return nil, false, err
	}
	return s.runtimeExecutions.GetExecution(ctx, project.OrganizationID(), string(task.ID()))
}
