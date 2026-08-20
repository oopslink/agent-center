package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/agent"
	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/files"
)

func (s *Server) requireAgentAuthorization(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, permission authz.PermissionKey, resource authz.ResourceScope) bool {
	subject := authz.SubjectRef(agentActor(a))
	if d.Authorizer == nil {
		writeAuthorizationError(w, authz.AccessDecision{
			SubjectRef: subject,
			Permission: permission,
			Resource:   resource,
			Reason:     "authorization_not_wired",
		}, authz.ErrDenied)
		return false
	}
	decision, err := checkAdminAuthorization(r.Context(), d, authz.CheckRequest{
		SubjectRef: subject,
		Transport:  authz.TransportMCP,
		Permission: permission,
		Resource:   resource,
	})
	if err != nil || !decision.Allowed {
		writeAuthorizationError(w, decision, err)
		return false
	}
	return true
}

func (s *Server) requireAgentProjectWrite(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, projectID string) bool {
	resource := authz.ResourceScope{
		Kind:  "project",
		ID:    projectID,
		OrgID: a.OrganizationID(),
	}
	if d.Authorizer == nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.SubjectRef(agentActor(a)), Permission: "project.write", Resource: resource, Reason: "authorization_not_wired"}, authz.ErrDenied)
		return false
	}
	decision, err := checkAdminAuthorization(r.Context(), d, authz.CheckRequest{
		SubjectRef: authz.SubjectRef(agentActor(a)),
		Transport:  authz.TransportMCP,
		Permission: "project.write",
		Resource:   resource,
	})
	if err != nil || !decision.Allowed {
		switch {
		case errors.Is(err, authz.ErrNotFound):
			memberID := a.IdentityMemberID()
			if memberID == "" {
				memberID = string(a.ID())
			}
			writeError(w, http.StatusNotFound, "project_not_found",
				"project "+projectID+" not found"+availableProjectsHint(r.Context(), d, a.OrganizationID(), memberID))
		case errors.Is(err, authz.ErrDenied):
			if agentProjectMember(r.Context(), d, projectID, agentActor(a)) {
				writeAuthorizationError(w, decision, err)
			} else {
				writeError(w, http.StatusForbidden, "not_a_project_member",
					"not a member of project "+projectID+", ask owner to add this agent")
			}
		default:
			writeAuthorizationError(w, decision, err)
		}
		return false
	}
	return true
}

func agentProjectMember(ctx context.Context, d HandlerDeps, projectID, actor string) bool {
	if d.DB == nil {
		return false
	}
	var n int
	if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pm_project_members WHERE project_id = ? AND identity_id = ?`, projectID, actor).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func (s *Server) requireAgentTeamCreate(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent) bool {
	return s.requireAgentAuthorization(w, r, d, a, "team.create", authz.ResourceScope{
		Kind:  "org",
		ID:    a.OrganizationID(),
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentOrgTemplateWrite(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent) bool {
	return s.requireAgentAuthorization(w, r, d, a, "template.write", authz.ResourceScope{
		Kind:  "org",
		ID:    a.OrganizationID(),
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentOrgTemplateRead(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent) bool {
	return s.requireAgentAuthorization(w, r, d, a, "template.read", authz.ResourceScope{
		Kind:  "org",
		ID:    a.OrganizationID(),
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentConversationRead(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, convID string) bool {
	return s.requireAgentAuthorization(w, r, d, a, "conversation.read", authz.ResourceScope{
		Kind:  "conversation",
		ID:    convID,
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentTeamPermission(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, teamID string, permission authz.PermissionKey) bool {
	return s.requireAgentAuthorization(w, r, d, a, permission, authz.ResourceScope{
		Kind:  "team",
		ID:    teamID,
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentTaskWrite(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, taskID string) bool {
	return s.requireAgentAuthorization(w, r, d, a, "task.write", authz.ResourceScope{
		Kind:  "task",
		ID:    taskID,
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentIssueWrite(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, issueID string) bool {
	return s.requireAgentAuthorization(w, r, d, a, "issue.write", authz.ResourceScope{
		Kind:  "issue",
		ID:    issueID,
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentPlanWrite(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, planID string) bool {
	return s.requireAgentAuthorization(w, r, d, a, "plan.write", authz.ResourceScope{
		Kind:  "plan",
		ID:    planID,
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentTaskSelf(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, taskID string, permission authz.PermissionKey) bool {
	return s.requireAgentAuthorization(w, r, d, a, permission, authz.ResourceScope{
		Kind:  "task",
		ID:    taskID,
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentConversationPost(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, convID string) bool {
	return s.requireAgentAuthorization(w, r, d, a, "conversation.post", authz.ResourceScope{
		Kind:  "conversation",
		ID:    convID,
		OrgID: a.OrganizationID(),
	})
}

func (s *Server) requireAgentFilePermission(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, permission authz.PermissionKey, fileURI string, refs []authz.FileRef) bool {
	resource := authz.ResourceScope{
		Kind:             "file",
		ID:               fileURI,
		URI:              fileURI,
		OrgID:            a.OrganizationID(),
		OwnerRef:         string(a.ID()),
		IdentityMemberID: a.IdentityMemberID(),
		Refs:             refs,
	}
	if d.Authorizer == nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.SubjectRef(agentActor(a)), Permission: permission, Resource: resource, Reason: "authorization_not_wired"}, authz.ErrDenied)
		return false
	}
	decision, err := checkAdminAuthorization(r.Context(), d, authz.CheckRequest{
		SubjectRef: authz.SubjectRef(agentActor(a)),
		Transport:  authz.TransportMCP,
		Permission: permission,
		Resource:   resource,
	})
	if err != nil || !decision.Allowed {
		if permission == "file.upload" || permission == "file.attach" {
			writeError(w, http.StatusForbidden, "scope_not_in_agent_domain", "requested scope is not in the agent's own domain")
		} else {
			writeAuthorizationError(w, decision, err)
		}
		return false
	}
	return true
}

func checkAdminAuthorization(ctx context.Context, d HandlerDeps, req authz.CheckRequest) (authz.AccessDecision, error) {
	var resolver authz.EffectiveResolver = d.Authorizer
	exp, err := resolver.ResolveEffective(ctx, req)
	if err == nil && !exp.Decision.Allowed {
		err = authz.ErrDenied
	}
	return exp.Decision, err
}

func agentAuthzFileRef(scope files.FileScope, scopeID string) authz.FileRef {
	return authz.FileRef{Scope: string(scope), ScopeID: scopeID}
}

func writeAuthorizationError(w http.ResponseWriter, decision authz.AccessDecision, err error) {
	status := http.StatusForbidden
	code := "permission_denied"
	message := decision.Reason
	if message == "" && err != nil {
		message = err.Error()
	}
	switch {
	case errors.Is(err, authz.ErrUnauthenticated):
		status = http.StatusUnauthorized
		code = "unauthenticated"
	case errors.Is(err, authz.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
	case errors.Is(err, authz.ErrInvalid), errors.Is(err, authz.ErrPermissionUndefined):
		status = http.StatusBadRequest
		code = "invalid_authorization_request"
	}
	if message == "" {
		message = code
	}
	writeError(w, status, code, message)
}
