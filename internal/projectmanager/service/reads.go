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

// --- Plan reads (v2.9 #285) -------------------------------------------------

// ListPlans returns a project's Plans (parallel plans, §2), stable-ordered.
func (s *Service) ListPlans(ctx context.Context, projectID pm.ProjectID) ([]*pm.Plan, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	return s.plans.ListByProject(ctx, projectID)
}

// GetPlan returns one Plan AR.
func (s *Service) GetPlan(ctx context.Context, id pm.PlanID) (*pm.Plan, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	return s.plans.FindByID(ctx, id)
}

// PlanDetail bundles a Plan with its selected tasks and the DERIVED view (§9.2):
// per-node status, ready-set, has_failed, progress. The HTTP layer renders the
// Plan DTO (nodes + ready-set + has_failed + progress) from this.
type PlanDetail struct {
	Plan  *pm.Plan
	Tasks []*pm.Task
	View  pm.PlanView
}

// GetPlanDetail loads a Plan + its tasks + edges + dispatch records and derives
// the whole-Plan read model (§9.2). Node status is DERIVED here, never stored.
func (s *Service) GetPlanDetail(ctx context.Context, id pm.PlanID) (*PlanDetail, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	p, err := s.plans.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	tasks, err := s.tasks.ListByPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	edges, err := s.plans.ListDependencies(ctx, id)
	if err != nil {
		return nil, err
	}
	records, err := s.plans.ListDispatchRecords(ctx, id)
	if err != nil {
		return nil, err
	}
	return &PlanDetail{Plan: p, Tasks: tasks, View: pm.ComputePlanView(tasks, edges, records)}, nil
}
