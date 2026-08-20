package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/agent"
	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/files"
)

func (s *Server) requireAgentAuthorization(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, permission authz.PermissionKey, resource authz.ResourceScope) bool {
	decision, err := authz.Authorize(r.Context(), authz.NewResolver(d.Authorizer), authz.CheckRequest{
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
	resource := authz.ResourceScope{
		Kind:  "project",
		ID:    projectID,
		OrgID: a.OrganizationID(),
	}
	decision, err := authz.Authorize(r.Context(), authz.NewResolver(d.Authorizer), authz.CheckRequest{
		SubjectRef: authz.SubjectRef(agentActor(a)),
		Transport:  authz.TransportMCP,
		Permission: "project.write",
		Resource:   resource,
	})
	if err == nil && decision.Allowed {
		return true
	}
	if errors.Is(err, authz.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project_not_found",
			"project "+projectID+" not found"+availableProjectsHint(r.Context(), d, a.OrganizationID(), a.IdentityMemberID()))
		return false
	}
	if decision.Reason == "authorization_not_wired" {
		writeAuthorizationError(w, decision, err)
		return false
	}
	if d.Authorizer != nil && d.Authorizer.EnforcementMode() == authz.EnforcementEnforce {
		writeAuthorizationError(w, decision, err)
		return false
	}
	writeError(w, http.StatusForbidden, "not_a_project_member",
		"not a member of project "+projectID+", please ask an owner to add you")
	return false
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
	resource := authz.ResourceScope{
		Kind:  "conversation",
		ID:    convID,
		OrgID: a.OrganizationID(),
	}
	decision, err := authz.Authorize(r.Context(), authz.NewResolver(d.Authorizer), authz.CheckRequest{
		SubjectRef: authz.SubjectRef(agentActor(a)),
		Transport:  authz.TransportMCP,
		Permission: "conversation.read",
		Resource:   resource,
	})
	if err == nil && decision.Allowed {
		return true
	}
	if err != nil && !errors.Is(err, authz.ErrNotFound) && decision.Reason != "authorization_not_wired" {
		writeError(w, http.StatusForbidden, "not_a_channel_member", "agent is not a member of this channel")
		return false
	}
	writeAuthorizationError(w, decision, err)
	return false
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
	if fileURI == "" && permission == "file.upload" {
		fileURI = "upload://pending"
	}
	resource := authz.ResourceScope{
		Kind:             "file",
		ID:               fileURI,
		URI:              fileURI,
		OrgID:            a.OrganizationID(),
		OwnerRef:         string(a.ID()),
		IdentityMemberID: a.IdentityMemberID(),
		Refs:             refs,
	}
	decision, err := authz.Authorize(r.Context(), authz.NewResolver(d.Authorizer), authz.CheckRequest{
		SubjectRef: authz.SubjectRef(agentActor(a)),
		Transport:  authz.TransportMCP,
		Permission: permission,
		Resource:   resource,
	})
	if err == nil && decision.Allowed {
		return true
	}
	if len(refs) > 0 && (permission == "file.upload" || permission == "file.attach") && decision.Reason != "authorization_not_wired" {
		writeError(w, http.StatusForbidden, "scope_not_in_agent_domain", "scope is not in the agent domain")
		return false
	}
	writeAuthorizationError(w, decision, err)
	return false
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
