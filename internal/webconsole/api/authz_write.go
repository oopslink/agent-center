package api

import (
	"net/http"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/identity"
)

func requireWebAuthorization(w http.ResponseWriter, r *http.Request, d HandlerDeps, caller *identity.Identity, permission authz.PermissionKey, resource authz.ResourceScope) bool {
	if d.Authorizer == nil {
		return true
	}
	decision, err := d.Authorizer.Check(r.Context(), authz.CheckRequest{
		SubjectRef: authz.UserSubject(caller.ID()),
		Transport:  authz.TransportWeb,
		Permission: permission,
		Resource:   resource,
	})
	if err != nil || !decision.Allowed {
		writeAuthorizationError(w, decision, err)
		return false
	}
	return true
}

func requireWebSubjectAuthorization(w http.ResponseWriter, r *http.Request, d HandlerDeps, subject authz.SubjectRef, permission authz.PermissionKey, resource authz.ResourceScope) bool {
	if d.Authorizer == nil {
		return true
	}
	decision, err := d.Authorizer.Check(r.Context(), authz.CheckRequest{
		SubjectRef: subject,
		Transport:  authz.TransportWeb,
		Permission: permission,
		Resource:   resource,
	})
	if err != nil || !decision.Allowed {
		writeAuthorizationError(w, decision, err)
		return false
	}
	return true
}
