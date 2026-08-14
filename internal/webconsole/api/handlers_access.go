package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/identity"
)

type accessResourceScopeDTO struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	OrgID     string `json:"org_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Label     string `json:"label,omitempty"`
}

type accessSubjectDTO struct {
	Ref    string `json:"ref"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	Status string `json:"status,omitempty"`
}

type accessPermissionDefinitionDTO struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Description   string   `json:"description"`
	ResourceKinds []string `json:"resource_kinds"`
	Actions       []string `json:"actions"`
	Risk          string   `json:"risk"`
	HighRisk      bool     `json:"high_risk,omitempty"`
	Category      string   `json:"category"`
	LegacySources []string `json:"legacy_sources"`
}

type accessRoleDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ScopeKind   string   `json:"scope_kind"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Editable    bool     `json:"editable"`
	Source      string   `json:"source"`
	HighRisk    bool     `json:"high_risk,omitempty"`
}

type accessDecisionDTO struct {
	Allowed     bool                   `json:"allowed"`
	SubjectRef  string                 `json:"subject_ref"`
	Permission  string                 `json:"permission"`
	Resource    accessResourceScopeDTO `json:"resource"`
	Source      string                 `json:"source"`
	Reason      string                 `json:"reason"`
	EvidenceRef string                 `json:"evidence_ref"`
	Status      string                 `json:"status,omitempty"`
	GrantID     string                 `json:"grant_id,omitempty"`
	Risk        string                 `json:"risk,omitempty"`
}

type accessGrantDTO struct {
	ID          string                 `json:"id"`
	SubjectRef  string                 `json:"subject_ref"`
	SubjectName string                 `json:"subject_name"`
	Permission  string                 `json:"permission"`
	Resource    accessResourceScopeDTO `json:"resource"`
	Source      string                 `json:"source"`
	Status      string                 `json:"status"`
	CreatedBy   string                 `json:"created_by"`
	CreatedAt   string                 `json:"created_at"`
	Risk        string                 `json:"risk"`
}

type accessBatchRequestDTO struct {
	SubjectRefs      []string                 `json:"subject_refs"`
	PermissionKeys   []string                 `json:"permission_keys"`
	Resources        []accessResourceScopeDTO `json:"resources"`
	ExpiresAt        *string                  `json:"expires_at"`
	Reason           string                   `json:"reason"`
	PreviewRequestID string                   `json:"preview_request_id"`
}

type accessBatchItemDTO struct {
	ID          string                 `json:"id"`
	SubjectRef  string                 `json:"subject_ref"`
	SubjectName string                 `json:"subject_name"`
	Permission  string                 `json:"permission"`
	Resource    accessResourceScopeDTO `json:"resource"`
	Status      string                 `json:"status"`
	Risk        string                 `json:"risk"`
	HighRisk    bool                   `json:"high_risk"`
	Reason      string                 `json:"reason"`
	EvidenceRef string                 `json:"evidence_ref,omitempty"`
	GrantID     string                 `json:"grant_id,omitempty"`
}

type accessOverviewState struct {
	generatedAt  time.Time
	subjects     []accessSubjectDTO
	subjectByRef map[string]accessSubjectDTO
	roles        []accessRoleDTO
	catalog      []accessPermissionDefinitionDTO
	catalogByKey map[string]accessPermissionDefinitionDTO
	decisions    []accessDecisionDTO
	grants       []accessGrantDTO
}

func (s *Server) accessEffectiveHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	state, err := s.accessOverviewState(r.Context(), d, orgID, svc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	decisions := accessFilterDecisions(state.decisions, state.subjectByRef, r)
	grants := accessFilterGrants(state.grants, decisions)
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": state.generatedAt.Format(time.RFC3339),
		"subjects":     state.subjects,
		"roles":        state.roles,
		"catalog":      state.catalog,
		"decisions":    decisions,
		"grants":       grants,
		"summary":      accessSummary(decisions),
	})
}

func (s *Server) accessBatchApplyHandler(w http.ResponseWriter, r *http.Request) {
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
	var body accessBatchRequestDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.accessBatchUnifiedHandler(w, r, d, svc, authz.UserSubject(caller.ID()), orgID, body, false)
}

func (s *Server) accessBatchUnifiedHandler(w http.ResponseWriter, r *http.Request, d HandlerDeps, svc *authz.Service, actor authz.SubjectRef, orgID string, body accessBatchRequestDTO, preview bool) {
	if len(body.SubjectRefs) == 0 || len(body.PermissionKeys) == 0 || len(body.Resources) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_access_request", "subject_refs, permission_keys and resources are required")
		return
	}
	state, err := s.accessOverviewState(r.Context(), d, orgID, svc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	items := accessBatchItems(r.Context(), svc, orgID, actor, body, state, preview)
	if preview {
		writeJSON(w, http.StatusOK, map[string]any{
			"request_id": fmt.Sprintf("access-preview-%s", accessHash(fmt.Sprintf("%s|%s|%v", actor, orgID, body))),
			"expires_at": body.ExpiresAt,
			"items":      items,
			"summary":    accessPreviewSummary(items),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"operation_id": fmt.Sprintf("access-apply-%s", accessHash(fmt.Sprintf("%s|%s|%v", actor, orgID, body))),
		"applied_at":   time.Now().UTC().Format(time.RFC3339),
		"items":        items,
		"summary":      accessResultSummary(items),
	})
}

func (s *Server) accessOverviewState(ctx context.Context, d HandlerDeps, orgID string, svc *authz.Service) (accessOverviewState, error) {
	defs, err := svc.ListDefinitions(ctx)
	if err != nil {
		return accessOverviewState{}, err
	}
	catalog := accessCatalogFromDefinitions(defs)
	catalogByKey := make(map[string]accessPermissionDefinitionDTO, len(catalog))
	for _, def := range catalog {
		catalogByKey[def.Key] = def
	}
	subjects, subjectByRef, err := accessSubjects(ctx, d, orgID)
	if err != nil {
		return accessOverviewState{}, err
	}
	resources, err := accessResources(ctx, d, orgID)
	if err != nil {
		return accessOverviewState{}, err
	}
	state := accessOverviewState{
		generatedAt:  time.Now().UTC(),
		subjects:     subjects,
		subjectByRef: subjectByRef,
		roles:        accessRoles(ctx, d, orgID, catalogByKey),
		catalog:      catalog,
		catalogByKey: catalogByKey,
	}
	for _, subj := range subjects {
		for _, res := range resources {
			eff, err := svc.ListEffective(ctx, authz.SubjectRef(subj.Ref), accessAuthzResource(res, orgID))
			if err != nil {
				continue
			}
			resolved := accessResourceFromAuthz(eff.Resource, res)
			for _, p := range eff.Permissions {
				def := catalogByKey[string(p.Key)]
				decision := accessDecisionDTO{
					Allowed:     true,
					SubjectRef:  subj.Ref,
					Permission:  string(p.Key),
					Resource:    resolved,
					Source:      string(p.Source),
					Reason:      "matched " + string(p.Source),
					EvidenceRef: p.EvidenceRef,
					Status:      "allowed",
					GrantID:     accessGrantIDForEffective(subj.Ref, resolved, p),
					Risk:        fallback(def.Risk, "low"),
				}
				state.decisions = append(state.decisions, decision)
				if p.Source == authz.SourceCustomRole && p.AssignmentID != "" {
					state.grants = append(state.grants, accessGrantDTO{
						ID:          p.AssignmentID,
						SubjectRef:  subj.Ref,
						SubjectName: subj.Name,
						Permission:  string(p.Key),
						Resource:    resolved,
						Source:      string(p.Source),
						Status:      "active",
						CreatedBy:   "authorization",
						CreatedAt:   state.generatedAt.Format(time.RFC3339),
						Risk:        fallback(def.Risk, "low"),
					})
				}
			}
		}
	}
	sort.Slice(state.decisions, func(i, j int) bool {
		a, b := state.decisions[i], state.decisions[j]
		return strings.Join([]string{a.SubjectRef, a.Resource.Kind, a.Resource.ID, a.Permission, a.Source, a.EvidenceRef}, "\x00") <
			strings.Join([]string{b.SubjectRef, b.Resource.Kind, b.Resource.ID, b.Permission, b.Source, b.EvidenceRef}, "\x00")
	})
	sort.Slice(state.grants, func(i, j int) bool { return state.grants[i].ID < state.grants[j].ID })
	return state, nil
}

func accessSubjects(ctx context.Context, d HandlerDeps, orgID string) ([]accessSubjectDTO, map[string]accessSubjectDTO, error) {
	byRef := map[string]accessSubjectDTO{}
	if d.MemberRepo != nil {
		members, err := d.MemberRepo.ListByOrganization(ctx, orgID)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range members {
			subj := accessSubjectForMember(ctx, d, m)
			byRef[subj.Ref] = subj
		}
	}
	if d.DB != nil {
		rows, err := d.DB.QueryContext(ctx, `SELECT DISTINCT subject_ref
			FROM authorization_role_assignments
			WHERE org_id = ? AND revoked_at IS NULL`, orgID)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var ref string
			if err := rows.Scan(&ref); err != nil {
				return nil, nil, err
			}
			if _, ok := byRef[ref]; !ok {
				byRef[ref] = accessUnknownSubject(ref)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
	}
	out := make([]accessSubjectDTO, 0, len(byRef))
	for _, subj := range byRef {
		out = append(out, subj)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, byRef, nil
}

func accessSubjectForMember(ctx context.Context, d HandlerDeps, m *identity.Member) accessSubjectDTO {
	kind := "human"
	prefix := "user"
	refID := m.IdentityID()
	name := m.IdentityID()
	if d.IdentityRepo != nil {
		if ident, err := d.IdentityRepo.GetByID(ctx, m.IdentityID()); err == nil && ident != nil {
			name = ident.DisplayName()
			if ident.Kind() == identity.KindAgent {
				kind = "agent"
				prefix = "agent"
				refID = m.ID()
			}
		}
	}
	return accessSubjectDTO{
		Ref:    prefix + ":" + refID,
		Kind:   kind,
		Name:   name,
		Role:   string(m.Role()),
		Status: string(m.Status()),
	}
}

func accessUnknownSubject(ref string) accessSubjectDTO {
	kind := "human"
	if strings.HasPrefix(ref, "agent:") {
		kind = "agent"
	} else if strings.HasPrefix(ref, "worker:") {
		kind = "worker"
	}
	return accessSubjectDTO{Ref: ref, Kind: kind, Name: ref, Status: "unavailable"}
}

func accessResources(ctx context.Context, d HandlerDeps, orgID string) ([]accessResourceScopeDTO, error) {
	byKey := map[string]accessResourceScopeDTO{}
	add := func(res accessResourceScopeDTO) {
		if res.Kind == "" {
			return
		}
		if res.Kind == "org" && res.ID == "" {
			res.ID = orgID
		}
		if res.OrgID == "" {
			res.OrgID = orgID
		}
		if res.Label == "" {
			res.Label = res.ID
		}
		byKey[res.Kind+"\x00"+res.ID] = res
	}
	add(accessResourceScopeDTO{Kind: "org", ID: orgID, OrgID: orgID, Label: orgID})
	if d.PM != nil {
		projects, err := d.PM.ListProjects(ctx, orgID)
		if err != nil {
			return nil, err
		}
		for _, p := range projects {
			add(accessResourceScopeDTO{Kind: "project", ID: string(p.ID()), OrgID: orgID, Label: p.Name()})
		}
	}
	if d.TeamService != nil {
		teams, err := d.TeamService.ListTeams(ctx, orgID)
		if err != nil {
			return nil, err
		}
		for _, tm := range teams {
			add(accessResourceScopeDTO{Kind: "team", ID: tm.ID().String(), OrgID: orgID, Label: tm.Name()})
		}
	}
	if d.DB != nil {
		rows, err := d.DB.QueryContext(ctx, `SELECT DISTINCT resource_kind, resource_id
			FROM authorization_role_assignments
			WHERE org_id = ? AND revoked_at IS NULL`, orgID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var kind, id string
			if err := rows.Scan(&kind, &id); err != nil {
				return nil, err
			}
			add(accessResourceScopeDTO{Kind: kind, ID: id, OrgID: orgID, Label: id})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]accessResourceScopeDTO, 0, len(byKey))
	for _, res := range byKey {
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].ID < out[j].ID
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

func accessRoles(ctx context.Context, d HandlerDeps, orgID string, catalog map[string]accessPermissionDefinitionDTO) []accessRoleDTO {
	if d.DB == nil {
		return nil
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT id, name, COALESCE(description, '')
		FROM authorization_roles
		WHERE org_id = ? AND kind = 'custom' AND revoked_at IS NULL
		ORDER BY name, id`, orgID)
	if err != nil {
		return nil
	}
	var roles []accessRoleDTO
	for rows.Next() {
		var role accessRoleDTO
		if err := rows.Scan(&role.ID, &role.Name, &role.Description); err != nil {
			rows.Close()
			return roles
		}
		role.Editable = true
		role.Source = string(authz.SourceCustomRole)
		roles = append(roles, role)
	}
	if err := rows.Close(); err != nil {
		return roles
	}
	for i := range roles {
		roles[i].Permissions = accessRolePermissions(ctx, d, roles[i].ID)
		roles[i].ScopeKind = accessRoleScopeKind(roles[i].Permissions, catalog)
		for _, permission := range roles[i].Permissions {
			if catalog[permission].Risk == "high" {
				roles[i].HighRisk = true
				break
			}
		}
	}
	return roles
}

func accessRolePermissions(ctx context.Context, d HandlerDeps, roleID string) []string {
	if d.DB == nil {
		return nil
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT permission_key
		FROM authorization_role_permissions
		WHERE role_id = ?
		ORDER BY permission_key`, roleID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var permissions []string
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return permissions
		}
		permissions = append(permissions, permission)
	}
	return permissions
}

func accessRoleScopeKind(permissions []string, catalog map[string]accessPermissionDefinitionDTO) string {
	for _, permission := range permissions {
		def := catalog[permission]
		if len(def.ResourceKinds) > 0 {
			return def.ResourceKinds[0]
		}
	}
	return "org"
}

func accessBatchItems(ctx context.Context, svc *authz.Service, orgID string, actor authz.SubjectRef, body accessBatchRequestDTO, state accessOverviewState, preview bool) []accessBatchItemDTO {
	items := []accessBatchItemDTO{}
	for _, subjectRef := range body.SubjectRefs {
		for _, permission := range body.PermissionKeys {
			for _, resource := range body.Resources {
				idx := len(items) + 1
				items = append(items, accessBatchItem(ctx, svc, orgID, actor, body, state, idx, strings.TrimSpace(subjectRef), strings.TrimSpace(permission), resource, preview))
			}
		}
	}
	return items
}

func accessBatchItem(ctx context.Context, svc *authz.Service, orgID string, actor authz.SubjectRef, body accessBatchRequestDTO, state accessOverviewState, idx int, subjectRef, permission string, resource accessResourceScopeDTO, preview bool) accessBatchItemDTO {
	subj, subjectFound := state.subjectByRef[subjectRef]
	def, permissionFound := state.catalogByKey[permission]
	item := accessBatchItemDTO{
		ID:          fmt.Sprintf("item-%d", idx),
		SubjectRef:  subjectRef,
		SubjectName: fallback(subj.Name, subjectRef),
		Permission:  permission,
		Resource:    normalizeAccessResource(resource, orgID),
		Status:      "denied",
		Risk:        fallback(def.Risk, "medium"),
		HighRisk:    def.Risk == "high",
		Reason:      "permission denied by unified authorization service",
	}
	switch {
	case len(body.SubjectRefs) == 0 || len(body.PermissionKeys) == 0 || len(body.Resources) == 0:
		item.Reason = "subject_refs, permission_keys and resources are required"
		return item
	case !subjectFound || subj.Status != string(identity.MemberJoined):
		item.Status = "unauthorized"
		item.Reason = "subject is unavailable or outside this organization"
		return item
	case !permissionFound:
		item.Reason = "permission is not registered"
		item.EvidenceRef = "permission_registry:missing"
		return item
	case !accessPermissionApplies(def, item.Resource.Kind):
		item.Status = "not_applicable"
		item.Reason = fmt.Sprintf("%s does not apply to %s", permission, item.Resource.Kind)
		item.EvidenceRef = "permission_registry:" + permission
		return item
	}
	req := accessBatchAuthorizationRequest(orgID, actor, body, item)
	if preview {
		item.Status = "allowed"
		item.Reason = "grant can be applied by unified authorization API"
		item.EvidenceRef = "authorization_preview:" + item.ID
		item.GrantID = req.Operations[len(req.Operations)-1].Assignment.ID
		return item
	}
	res, err := svc.ApplyBatch(ctx, req)
	if err != nil {
		item.Status = accessBatchStatusForError(err)
		item.Reason = err.Error()
		return item
	}
	for _, op := range res.Operations {
		if op.Reason != "" || op.Status == "denied" {
			item.Status = accessBatchStatusForReason(op.Reason)
			item.Reason = fallback(op.Reason, op.Status)
			return item
		}
		if op.AssignmentID != "" {
			item.GrantID = op.AssignmentID
		}
	}
	item.Status = "allowed"
	item.Reason = "grant applied by unified authorization API"
	item.EvidenceRef = "authorization_batch:" + req.IdempotencyKey
	return item
}

func accessBatchAuthorizationRequest(orgID string, actor authz.SubjectRef, body accessBatchRequestDTO, item accessBatchItemDTO) authz.BatchRequest {
	roleID := accessRoleIDForPermission(orgID, item.Permission, item.Resource.Kind)
	resource := accessAuthzResource(item.Resource, orgID)
	assignmentID := "asgn-" + accessHash(strings.Join([]string{orgID, item.SubjectRef, roleID, item.Resource.Kind, item.Resource.ID}, "|"))
	return authz.BatchRequest{
		IdempotencyKey: accessBatchIdempotencyKey("apply", orgID, actor, body.PreviewRequestID, item),
		ActorRef:       actor,
		OrgID:          orgID,
		Operations: []authz.BatchOperation{
			{
				ID:   item.ID + "-role",
				Type: "upsert_role",
				Role: authz.RoleInput{
					ID:          roleID,
					Name:        "Access grant " + item.Permission + " on " + item.Resource.Kind,
					Description: "Managed by the Access batch authorization flow.",
				},
			},
			{
				ID:   item.ID + "-permissions",
				Type: "set_role_permissions",
				Role: authz.RoleInput{ID: roleID},
				Permissions: []authz.RolePermissionInput{{
					PermissionKey: authz.PermissionKey(item.Permission),
					ResourceKind:  item.Resource.Kind,
					Delegatable:   false,
				}},
			},
			{
				ID:   item.ID,
				Type: "assign_role",
				Assignment: authz.AssignmentInput{
					ID:         assignmentID,
					SubjectRef: authz.SubjectRef(item.SubjectRef),
					RoleID:     roleID,
					Resource:   resource,
				},
			},
		},
	}
}

func accessCatalogFromDefinitions(definitions []authz.PermissionDefinition) []accessPermissionDefinitionDTO {
	out := make([]accessPermissionDefinitionDTO, 0, len(definitions))
	for _, def := range definitions {
		key := string(def.Key)
		risk := accessPermissionRisk(def)
		out = append(out, accessPermissionDefinitionDTO{
			Key:           key,
			Label:         accessLabel(key),
			Description:   "Unified authorization permission " + key + ".",
			ResourceKinds: append([]string(nil), def.ResourceKinds...),
			Actions:       append([]string(nil), def.Actions...),
			Risk:          risk,
			HighRisk:      risk == "high",
			Category:      fallback(def.Category, "access"),
			LegacySources: append([]string(nil), def.LegacySources...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func accessPermissionRisk(def authz.PermissionDefinition) string {
	key := string(def.Key)
	for _, token := range []string{"lifecycle", "role.manage", "member.disable", "admin_token", "secret", "delete", "remove", "manage"} {
		if strings.Contains(key, token) {
			return "high"
		}
	}
	for _, action := range def.Actions {
		switch action {
		case "create", "update", "write", "upload", "attach", "review", "export", "put", "complete", "block", "report", "pull":
			return "medium"
		}
	}
	return "low"
}

func accessPermissionApplies(def accessPermissionDefinitionDTO, kind string) bool {
	for _, k := range def.ResourceKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func accessAuthzResource(resource accessResourceScopeDTO, orgID string) authz.ResourceScope {
	resource = normalizeAccessResource(resource, orgID)
	out := authz.ResourceScope{
		Kind:      resource.Kind,
		ID:        resource.ID,
		OrgID:     resource.OrgID,
		ProjectID: resource.ProjectID,
	}
	if resource.Kind == "file" {
		out.URI = resource.ID
	}
	return out
}

func normalizeAccessResource(resource accessResourceScopeDTO, orgID string) accessResourceScopeDTO {
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.OrgID = strings.TrimSpace(resource.OrgID)
	resource.ProjectID = strings.TrimSpace(resource.ProjectID)
	if resource.Kind == "org" && resource.ID == "" {
		resource.ID = orgID
	}
	if resource.OrgID == "" {
		resource.OrgID = orgID
	}
	if resource.Label == "" {
		resource.Label = resource.ID
	}
	return resource
}

func accessResourceFromAuthz(resource authz.ResourceScope, fallbackResource accessResourceScopeDTO) accessResourceScopeDTO {
	out := fallbackResource
	if resource.Kind != "" {
		out.Kind = resource.Kind
	}
	if resource.ID != "" {
		out.ID = resource.ID
	}
	if resource.OrgID != "" {
		out.OrgID = resource.OrgID
	}
	if resource.ProjectID != "" {
		out.ProjectID = resource.ProjectID
	}
	if out.Label == "" {
		out.Label = out.ID
	}
	return out
}

func accessGrantIDForEffective(subjectRef string, resource accessResourceScopeDTO, p authz.EffectivePermission) string {
	if p.Source == authz.SourceCustomRole && p.AssignmentID != "" {
		return p.AssignmentID
	}
	return strings.Join([]string{"grant", string(p.Source), subjectRef, string(p.Key), resource.Kind, resource.ID}, ":")
}

func accessFilterDecisions(decisions []accessDecisionDTO, subjects map[string]accessSubjectDTO, r *http.Request) []accessDecisionDTO {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	resourceKind := strings.TrimSpace(r.URL.Query().Get("resource_kind"))
	subjectKind := strings.TrimSpace(r.URL.Query().Get("subject_kind"))
	out := make([]accessDecisionDTO, 0, len(decisions))
	for _, d := range decisions {
		subj := subjects[d.SubjectRef]
		if q != "" {
			haystack := strings.ToLower(strings.Join([]string{subj.Name, d.SubjectRef, d.Permission, d.Resource.Label, d.Reason, d.Source, d.EvidenceRef}, " "))
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		if status != "" && status != "all" && d.Status != status {
			continue
		}
		if resourceKind != "" && resourceKind != "all" && d.Resource.Kind != resourceKind {
			continue
		}
		if subjectKind != "" && subjectKind != "all" && subj.Kind != subjectKind {
			continue
		}
		out = append(out, d)
	}
	return out
}

func accessFilterGrants(grants []accessGrantDTO, decisions []accessDecisionDTO) []accessGrantDTO {
	allowed := map[string]struct{}{}
	for _, d := range decisions {
		if d.GrantID != "" {
			allowed[d.GrantID] = struct{}{}
		}
	}
	out := make([]accessGrantDTO, 0, len(grants))
	for _, grant := range grants {
		if _, ok := allowed[grant.ID]; ok {
			out = append(out, grant)
		}
	}
	return out
}

func accessSummary(decisions []accessDecisionDTO) map[string]int {
	summary := map[string]int{"allowed": 0, "high_risk": 0, "expiring": 0, "denied": 0, "not_applicable": 0}
	for _, d := range decisions {
		switch d.Status {
		case "allowed":
			summary["allowed"]++
		case "not_applicable":
			summary["not_applicable"]++
		case "denied", "unauthorized":
			summary["denied"]++
		}
		if d.Risk == "high" {
			summary["high_risk"]++
		}
	}
	return summary
}

func accessPreviewSummary(items []accessBatchItemDTO) map[string]int {
	out := map[string]int{"total": len(items), "grantable": 0, "high_risk": 0, "unauthorized": 0, "not_applicable": 0}
	for _, item := range items {
		if item.Status == "allowed" {
			out["grantable"]++
		}
		if item.HighRisk {
			out["high_risk"]++
		}
		if item.Status == "unauthorized" {
			out["unauthorized"]++
		}
		if item.Status == "not_applicable" {
			out["not_applicable"]++
		}
	}
	return out
}

func accessResultSummary(items []accessBatchItemDTO) map[string]any {
	failed, succeeded, unauthorized, notApplicable := 0, 0, 0, 0
	for _, item := range items {
		if item.Status == "allowed" {
			succeeded++
		} else {
			failed++
		}
		if item.Status == "unauthorized" {
			unauthorized++
		}
		if item.Status == "not_applicable" {
			notApplicable++
		}
	}
	return map[string]any{
		"total":           len(items),
		"succeeded":       succeeded,
		"failed":          failed,
		"unauthorized":    unauthorized,
		"not_applicable":  notApplicable,
		"partial_failure": failed > 0,
	}
}

func accessBatchStatusForError(err error) string {
	switch {
	case strings.Contains(err.Error(), "not defined"):
		return "not_applicable"
	case strings.Contains(err.Error(), "not delegatable"), strings.Contains(err.Error(), "permission denied"), strings.Contains(err.Error(), "not found"):
		return "unauthorized"
	default:
		return "denied"
	}
}

func accessBatchStatusForReason(reason string) string {
	reason = strings.ToLower(reason)
	switch {
	case strings.Contains(reason, "not defined"), strings.Contains(reason, "does not apply"):
		return "not_applicable"
	case strings.Contains(reason, "not delegatable"), strings.Contains(reason, "permission denied"), strings.Contains(reason, "not found"):
		return "unauthorized"
	default:
		return "denied"
	}
}

func accessRoleIDForPermission(orgID, permission, resourceKind string) string {
	return "role-access-" + accessHash(strings.Join([]string{orgID, permission, resourceKind}, "|"))
}

func accessBatchIdempotencyKey(prefix, orgID string, actor authz.SubjectRef, previewID string, item accessBatchItemDTO) string {
	seed := strings.Join([]string{prefix, orgID, string(actor), previewID, item.SubjectRef, item.Permission, item.Resource.Kind, item.Resource.ID}, "|")
	return "access-" + prefix + "-" + accessHash(seed)
}

func accessHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])[:16]
}

func accessLabel(key string) string {
	return strings.ReplaceAll(key, ".", " ")
}

func fallback(value, fb string) string {
	if value != "" {
		return value
	}
	return fb
}
