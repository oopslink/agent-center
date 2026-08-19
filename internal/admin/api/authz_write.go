package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/agent"
	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/files"
)

func (s *Server) requireAgentAuthorization(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, permission authz.PermissionKey, resource authz.ResourceScope) bool {
	if d.Authorizer == nil {
		return true
	}
	decision, err := d.Authorizer.Check(r.Context(), authz.CheckRequest{
		SubjectRef: authz.SubjectRef(agentActor(a)),
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
	return s.requireAgentAuthorization(w, r, d, a, "project.write", authz.ResourceScope{
		Kind:  "project",
		ID:    projectID,
		OrgID: a.OrganizationID(),
	})
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
	return s.requireAgentAuthorization(w, r, d, a, permission, authz.ResourceScope{
		Kind:  "file",
		ID:    fileURI,
		URI:   fileURI,
		OrgID: a.OrganizationID(),
		Refs:  refs,
	})
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
