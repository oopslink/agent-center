package service

import (
	"context"

	authz "github.com/oopslink/agent-center/internal/authorization"
)

func requireBackgroundAuthorization(ctx context.Context, authorizer *authz.Service, capability string) error {
	if authorizer == nil {
		return nil
	}
	req := authz.CheckRequest{
		SubjectRef: authz.BackgroundSubject,
		Transport:  authz.TransportSystem,
		Permission: backgroundCapabilityPermission(capability),
		Resource:   authz.BackgroundResource(capability),
	}
	explain, err := authorizer.ResolveEffective(ctx, req)
	if err != nil {
		return err
	}
	if !explain.Decision.Allowed {
		return authz.ErrDenied
	}
	return nil
}

func backgroundCapabilityPermission(capability string) authz.PermissionKey {
	switch capability {
	case "auto_assign":
		return "background.auto_assign"
	case "lease_check":
		return "background.lease_check"
	case "overdue_block_reminder":
		return "background.overdue_block_reminder"
	case "plan_reconcile":
		return "background.plan_reconcile"
	case "resolved_issue_close":
		return "background.resolved_issue_close"
	default:
		return authz.PermissionKey("background." + capability)
	}
}
