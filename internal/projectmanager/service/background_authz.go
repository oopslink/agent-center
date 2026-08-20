package service

import (
	"context"

	authz "github.com/oopslink/agent-center/internal/authorization"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

const backgroundSubject authz.SubjectRef = "worker:background-reconciler"

func (s *Service) requireBackgroundAuthorization(ctx context.Context, permission authz.PermissionKey, resource authz.ResourceScope) error {
	decision, err := authz.Authorize(ctx, s.authorizer, authz.CheckRequest{
		SubjectRef: backgroundSubject,
		Transport:  authz.TransportSystem,
		Permission: permission,
		Resource:   resource,
	})
	if err != nil || !decision.Allowed {
		if err != nil {
			return err
		}
		return authz.ErrDenied
	}
	return nil
}

func projectWriteResource(projectID pm.ProjectID) authz.ResourceScope {
	return authz.ResourceScope{Kind: "project", ID: string(projectID)}
}

func taskWriteResource(taskID pm.TaskID, projectID pm.ProjectID) authz.ResourceScope {
	return authz.ResourceScope{Kind: "task", ID: string(taskID), ProjectID: string(projectID)}
}

func issueWriteResource(issueID pm.IssueID, projectID pm.ProjectID) authz.ResourceScope {
	return authz.ResourceScope{Kind: "issue", ID: string(issueID), ProjectID: string(projectID)}
}

func planWriteResource(planID pm.PlanID, projectID pm.ProjectID) authz.ResourceScope {
	return authz.ResourceScope{Kind: "plan", ID: string(planID), ProjectID: string(projectID)}
}
