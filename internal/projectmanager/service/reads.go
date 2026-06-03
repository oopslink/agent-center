package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Read-through methods for the webconsole (B3). Reads need no transaction;
// they delegate to the repos. Org/project scoping is enforced by the HTTP
// layer (project-in-org check) + requireProjectMember on writes.

func (s *Service) ListProjects(ctx context.Context, orgID string) ([]*pm.Project, error) {
	return s.projects.ListByOrg(ctx, orgID)
}

func (s *Service) GetProject(ctx context.Context, id pm.ProjectID) (*pm.Project, error) {
	return s.projects.FindByID(ctx, id)
}

func (s *Service) ListMembers(ctx context.Context, projectID pm.ProjectID) ([]*pm.ProjectMember, error) {
	return s.members.ListByProject(ctx, projectID)
}

func (s *Service) ListIssues(ctx context.Context, projectID pm.ProjectID) ([]*pm.Issue, error) {
	return s.issues.ListByProject(ctx, projectID)
}

func (s *Service) GetIssue(ctx context.Context, id pm.IssueID) (*pm.Issue, error) {
	return s.issues.FindByID(ctx, id)
}

func (s *Service) ListTasks(ctx context.Context, projectID pm.ProjectID) ([]*pm.Task, error) {
	return s.tasks.ListByProject(ctx, projectID)
}

func (s *Service) GetTask(ctx context.Context, id pm.TaskID) (*pm.Task, error) {
	return s.tasks.FindByID(ctx, id)
}

func (s *Service) ListCodeRepos(ctx context.Context, projectID pm.ProjectID) ([]*pm.CodeRepoRef, error) {
	return s.codeRepoRefs.ListByProject(ctx, projectID)
}

func (s *Service) ListTaskSubscribers(ctx context.Context, taskID pm.TaskID) ([]*pm.TaskSubscriber, error) {
	return s.taskSubs.ListByTask(ctx, taskID)
}
