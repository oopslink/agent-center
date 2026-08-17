package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/identity"
)

type permissionCheckBody struct {
	SubjectRef  authz.SubjectRef    `json:"subject_ref,omitempty"`
	Permission  authz.PermissionKey `json:"permission"`
	Resource    authz.ResourceScope `json:"resource"`
	RequestID   string              `json:"request_id,omitempty"`
	BearerScope string              `json:"bearer_scope,omitempty"`
}

func (s *Server) permissionsDefinitionsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if !accessGovernanceReadModelEnabled(w, d) {
		return
	}
	if _, _, _, ok := requireOrgMember(w, r, d); !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	defs, err := svc.ListDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authorization_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"definitions": defs})
}

func (s *Server) permissionsCheckHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if !accessGovernanceReadModelEnabled(w, d) {
		return
	}
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	var body permissionCheckBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req, ok := preparePermissionCheck(w, r, svc, caller, member, orgID, body)
	if !ok {
		return
	}
	decision, err := svc.Check(r.Context(), req)
	if err != nil {
		writeAuthorizationError(w, decision, err)
		return
	}
	redactAccessDecision(&decision)
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) permissionsExplainHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if !accessGovernanceReadModelEnabled(w, d) {
		return
	}
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	var body permissionCheckBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req, ok := preparePermissionCheck(w, r, svc, caller, member, orgID, body)
	if !ok {
		return
	}
	explain, err := svc.Explain(r.Context(), req)
	if err != nil && !errors.Is(err, authz.ErrDenied) {
		writeAuthorizationError(w, explain.Decision, err)
		return
	}
	redactExplainResult(&explain)
	writeJSON(w, http.StatusOK, explain)
}

func (s *Server) permissionsEffectiveHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("view") == "access" {
		s.accessEffectiveHandler(w, r)
		return
	}
	d := hd(r)
	if !accessGovernanceReadModelEnabled(w, d) {
		return
	}
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	callerRef := authz.UserSubject(caller.ID())
	subject := authz.SubjectRef(strings.TrimSpace(r.URL.Query().Get("subject_ref")))
	if subject == "" {
		subject = callerRef
	}
	if subject != callerRef && !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner/admin may inspect another subject")
		return
	}
	if !ensurePermissionSubjectVisible(w, r, svc, subject, callerRef, orgID) {
		return
	}
	resource := authz.ResourceScope{
		Kind:  strings.TrimSpace(r.URL.Query().Get("resource_kind")),
		ID:    strings.TrimSpace(r.URL.Query().Get("resource_id")),
		OrgID: orgID,
		URI:   strings.TrimSpace(r.URL.Query().Get("uri")),
	}
	if resource.Kind == "" {
		resource.Kind = "org"
		resource.ID = orgID
	}
	if ok, err := svc.ResourceVisibleInOrg(r.Context(), resource, orgID); err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: subject, Resource: resource}, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	eff, err := svc.ListEffective(r.Context(), subject, resource)
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: subject, Resource: resource}, err)
		return
	}
	redactEffectivePermissions(&eff)
	writeJSON(w, http.StatusOK, eff)
}

func (s *Server) permissionsAccessGraphHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if !accessGovernanceReadModelEnabled(w, d) {
		return
	}
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	callerRef := authz.UserSubject(caller.ID())
	subject := authz.SubjectRef(strings.TrimSpace(r.URL.Query().Get("subject_ref")))
	if subject == "" {
		subject = callerRef
	}
	if subject != callerRef && !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner/admin may inspect another subject")
		return
	}
	if !ensurePermissionSubjectVisible(w, r, svc, subject, callerRef, orgID) {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	shadow := true
	if raw := strings.TrimSpace(r.URL.Query().Get("shadow_parity")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_shadow_parity", "shadow_parity must be a boolean")
			return
		}
		shadow = parsed
	}
	graph, err := svc.ListAccessGraph(r.Context(), authz.AccessGraphRequest{
		SubjectRef:     subject,
		OrgID:          orgID,
		Layer:          r.URL.Query().Get("layer"),
		Cursor:         r.URL.Query().Get("cursor"),
		Limit:          limit,
		RedactEvidence: true,
		ShadowParity:   shadow,
		RequestID:      r.Header.Get("X-Request-ID"),
		ActorRef:       callerRef,
	})
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: subject, Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) permissionsAuditHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	callerRef := authz.UserSubject(caller.ID())
	subject := authz.SubjectRef(strings.TrimSpace(r.URL.Query().Get("subject_ref")))
	if subject == "" {
		subject = callerRef
	}
	if subject != callerRef && !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner/admin may inspect another subject")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	events, err := svc.ListSubjectAudit(r.Context(), subject, orgID, limit)
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: subject, Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) permissionsBatchPreviewHandler(w http.ResponseWriter, r *http.Request) {
	s.permissionsBatchHandler(w, r, true, false)
}

func (s *Server) permissionsBatchApplyHandler(w http.ResponseWriter, r *http.Request) {
	s.permissionsBatchHandler(w, r, false, false)
}

func (s *Server) permissionsBatchRevokeHandler(w http.ResponseWriter, r *http.Request) {
	s.permissionsBatchHandler(w, r, false, true)
}

func (s *Server) permissionsBatchHandler(w http.ResponseWriter, r *http.Request, preview, revoke bool) {
	d := hd(r)
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	var env struct {
		IdempotencyKey string                 `json:"idempotency_key,omitempty"`
		ActorRef       authz.SubjectRef       `json:"actor_ref"`
		OrgID          string                 `json:"org_id"`
		Operations     []authz.BatchOperation `json:"operations"`

		SubjectRefs      []string                 `json:"subject_refs"`
		PermissionKeys   []string                 `json:"permission_keys"`
		Resources        []accessResourceScopeDTO `json:"resources"`
		ExpiresAt        *string                  `json:"expires_at"`
		Reason           string                   `json:"reason"`
		PreviewRequestID string                   `json:"preview_request_id"`
		GrantIDs         []string                 `json:"grant_ids"`
	}
	if err := decodeJSON(r, &env); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(env.SubjectRefs) > 0 || len(env.PermissionKeys) > 0 || len(env.Resources) > 0 || len(env.GrantIDs) > 0 {
		body := accessBatchRequestDTO{
			SubjectRefs:      env.SubjectRefs,
			PermissionKeys:   env.PermissionKeys,
			Resources:        env.Resources,
			ExpiresAt:        env.ExpiresAt,
			Reason:           env.Reason,
			PreviewRequestID: env.PreviewRequestID,
		}
		if revoke || len(env.GrantIDs) > 0 {
			s.accessBulkRevokeUnifiedHandler(w, r, d, svc, authz.UserSubject(caller.ID()), orgID, env.GrantIDs, env.Reason, env.PreviewRequestID, env.IdempotencyKey)
			return
		}
		s.accessBatchUnifiedHandler(w, r, d, svc, authz.UserSubject(caller.ID()), orgID, body, preview)
		return
	}
	req := authz.BatchRequest{
		IdempotencyKey: env.IdempotencyKey,
		ActorRef:       env.ActorRef,
		OrgID:          env.OrgID,
		Operations:     env.Operations,
	}
	req.ActorRef = authz.UserSubject(caller.ID())
	req.OrgID = orgID
	var (
		res authz.BatchResult
		err error
	)
	switch {
	case preview:
		res, err = svc.PreviewBatch(r.Context(), req)
	case revoke:
		res, err = svc.RevokeBatch(r.Context(), req)
	default:
		res, err = svc.ApplyBatch(r.Context(), req)
	}
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: req.ActorRef, Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func preparePermissionCheck(w http.ResponseWriter, r *http.Request, svc *authz.Service, caller *identity.Identity, member *identity.Member, orgID string, body permissionCheckBody) (authz.CheckRequest, bool) {
	callerRef := authz.UserSubject(caller.ID())
	subject := body.SubjectRef
	if subject == "" {
		subject = callerRef
	}
	if subject != callerRef && !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner/admin may check another subject")
		return authz.CheckRequest{}, false
	}
	if !ensurePermissionSubjectVisible(w, r, svc, subject, callerRef, orgID) {
		return authz.CheckRequest{}, false
	}
	resource := body.Resource
	if resource.Kind == "" {
		resource.Kind = "org"
		resource.ID = orgID
	}
	if resource.Kind == "org" && resource.ID == "" {
		resource.ID = orgID
	}
	if resource.OrgID == "" {
		resource.OrgID = orgID
	}
	if ok, err := svc.ResourceVisibleInOrg(r.Context(), resource, orgID); err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: subject, Resource: resource}, err)
		return authz.CheckRequest{}, false
	} else if !ok {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return authz.CheckRequest{}, false
	}
	return authz.CheckRequest{
		SubjectRef:  subject,
		Transport:   authz.TransportWeb,
		BearerScope: body.BearerScope,
		Permission:  body.Permission,
		Resource:    resource,
		RequestID:   body.RequestID,
	}, true
}

func accessGovernanceReadModelEnabled(w http.ResponseWriter, d HandlerDeps) bool {
	if d.AccessGovernanceReadModelDisabled {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return false
	}
	return true
}

func ensurePermissionSubjectVisible(w http.ResponseWriter, r *http.Request, svc *authz.Service, subject, callerRef authz.SubjectRef, orgID string) bool {
	if subject == callerRef {
		return true
	}
	visible, err := svc.SubjectVisibleInOrg(r.Context(), subject, orgID)
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: subject, Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
		return false
	}
	if !visible {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return false
	}
	return true
}

func redactExplainResult(exp *authz.ExplainResult) {
	if exp == nil {
		return
	}
	redactAccessDecision(&exp.Decision)
	for i := range exp.Effective {
		redactEffectivePermission(&exp.Effective[i])
	}
}

func redactEffectivePermissions(eff *authz.EffectivePermissions) {
	if eff == nil {
		return
	}
	for i := range eff.Permissions {
		redactEffectivePermission(&eff.Permissions[i])
	}
}

func redactEffectivePermission(p *authz.EffectivePermission) {
	if p == nil || p.EvidenceRef == "" {
		return
	}
	p.EvidenceRef = authz.RedactEvidenceRef(p.EvidenceRef)
}

func redactAccessDecision(decision *authz.AccessDecision) {
	if decision == nil || decision.EvidenceRef == "" {
		return
	}
	decision.EvidenceRef = authz.RedactEvidenceRef(decision.EvidenceRef)
}

func permissionAuthorizer(d HandlerDeps) *authz.Service {
	if d.Authorizer != nil {
		return d.Authorizer
	}
	if d.DB == nil {
		return nil
	}
	return authz.New(authz.Deps{DB: d.DB, EventSink: d.EventSink})
}

func writeAuthorizationError(w http.ResponseWriter, decision authz.AccessDecision, err error) {
	redactAccessDecision(&decision)
	status := http.StatusForbidden
	code := "permission_denied"
	switch {
	case errors.Is(err, authz.ErrUnauthenticated):
		status = http.StatusUnauthorized
		code = "unauthenticated"
	case errors.Is(err, authz.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
	case errors.Is(err, authz.ErrInvalid), errors.Is(err, authz.ErrPermissionUndefined):
		status = http.StatusUnprocessableEntity
		code = "invalid_permission_request"
	case errors.Is(err, authz.ErrConflict), errors.Is(err, authz.ErrIdempotencyConflict):
		status = http.StatusConflict
		code = "authorization_conflict"
	case errors.Is(err, authz.ErrPreviewStale), errors.Is(err, authz.ErrPreviewExpired), errors.Is(err, authz.ErrPreviewConsumed):
		status = http.StatusConflict
		code = "preview_stale"
	case errors.Is(err, authz.ErrPreviewNotFound):
		status = http.StatusNotFound
		code = "preview_not_found"
	case errors.Is(err, authz.ErrIdempotencyRequired):
		status = http.StatusBadRequest
		code = "idempotency_required"
	}
	msg := "permission denied"
	if decision.Reason != "" {
		msg = decision.Reason
	}
	if err != nil && msg == "permission denied" {
		msg = err.Error()
	}
	writeJSON(w, status, map[string]any{
		"error":    code,
		"message":  msg,
		"decision": decision,
	})
}
