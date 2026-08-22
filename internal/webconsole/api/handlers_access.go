package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/identity"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/team"
)

type accessResourceScopeDTO struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	OrgID     string `json:"org_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Label     string `json:"label,omitempty"`
}

type accessSubjectDTO struct {
	Ref       string   `json:"ref"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Role      string   `json:"role,omitempty"`
	Status    string   `json:"status,omitempty"`
	TeamNames []string `json:"team_names,omitempty"`
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

type accessRAMRoleDTO struct {
	ID          string   `json:"id"`
	StableKey   string   `json:"stable_key"`
	Name        string   `json:"name"`
	Version     int      `json:"version"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Risk        string   `json:"risk"`
	Scope       string   `json:"scope"`
	RevokedAt   *string  `json:"revoked_at,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	References  int      `json:"references,omitempty"`
}

type accessRAMRoleDetailDTO struct {
	ID          string                      `json:"id"`
	StableKey   string                      `json:"stable_key"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Kind        string                      `json:"kind"`
	Scope       string                      `json:"scope"`
	RevokedAt   *string                     `json:"revoked_at,omitempty"`
	Latest      accessRAMRoleDTO            `json:"latest"`
	Versions    []accessRAMRoleDTO          `json:"versions"`
	References  []accessRAMRoleReferenceDTO `json:"references"`
}

type accessRAMRoleReferenceDTO struct {
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
	TeamRole string `json:"team_role"`
}

type accessRAMRoleWriteDTO struct {
	Name                  string   `json:"name"`
	StableKey             string   `json:"stable_key"`
	Description           string   `json:"description"`
	Permissions           []string `json:"permissions"`
	Scope                 string   `json:"scope"`
	ExpectedLatestVersion *int     `json:"expected_latest_version,omitempty"`
}

type accessRAMRoleRevokeDTO struct {
	ExpectedLatestVersion *int   `json:"expected_latest_version,omitempty"`
	Reason                string `json:"reason,omitempty"`
	ConfirmUnreferenced   bool   `json:"confirm_unreferenced,omitempty"`
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
	ExpiresAt   *string                `json:"expires_at,omitempty"`
	GrantID     string                 `json:"grant_id,omitempty"`
	RoleID      string                 `json:"role_id,omitempty"`
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
	StartsAt    *string                `json:"starts_at,omitempty"`
	ExpiresAt   *string                `json:"expires_at,omitempty"`
	CreatedBy   string                 `json:"created_by"`
	CreatedAt   string                 `json:"created_at"`
	RevokedAt   *string                `json:"revoked_at,omitempty"`
	RoleID      string                 `json:"role_id,omitempty"`
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

type accessRevokeRequestDTO struct {
	GrantIDs       []string `json:"grant_ids"`
	Reason         string   `json:"reason"`
	Message        string   `json:"message,omitempty"`
	PreviewID      string   `json:"preview_id,omitempty"`
	Token          string   `json:"token,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

type accessDerivedState struct {
	generatedAt  time.Time
	subjects     []accessSubjectDTO
	subjectByRef map[string]accessSubjectDTO
	roles        []accessRoleDTO
	catalog      []accessPermissionDefinitionDTO
	catalogByKey map[string]accessPermissionDefinitionDTO
	decisions    []accessDecisionDTO
	grants       []accessGrantDTO
}

var accessCatalog = []accessPermissionDefinitionDTO{
	{Key: "org.read", Label: "Read organization", Description: "Open organization-scoped resources.", ResourceKinds: []string{"org"}, Actions: []string{"read"}, Risk: "low", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "org.settings.manage", Label: "Manage org settings", Description: "Update organization profile settings.", ResourceKinds: []string{"org"}, Actions: []string{"manage"}, Risk: "high", HighRisk: true, Category: "access", LegacySources: []string{"org_role"}},
	{Key: "org.lifecycle.manage", Label: "Manage org lifecycle", Description: "Disable, enable, or delete an organization.", ResourceKinds: []string{"org"}, Actions: []string{"manage"}, Risk: "high", HighRisk: true, Category: "access", LegacySources: []string{"org_role"}},
	{Key: "org.member.list", Label: "List members", Description: "Read the organization member directory.", ResourceKinds: []string{"org"}, Actions: []string{"list"}, Risk: "low", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "org.member.create.human", Label: "Create human member", Description: "Invite or create a human member.", ResourceKinds: []string{"org"}, Actions: []string{"create"}, Risk: "medium", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "org.member.create.agent", Label: "Create agent member", Description: "Provision an agent identity in the organization.", ResourceKinds: []string{"org"}, Actions: []string{"create"}, Risk: "medium", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "org.member.role.manage", Label: "Manage org roles", Description: "Change owner/admin/member assignments.", ResourceKinds: []string{"org"}, Actions: []string{"manage"}, Risk: "high", HighRisk: true, Category: "access", LegacySources: []string{"org_role"}},
	{Key: "org.invitation.manage", Label: "Manage invitations", Description: "Create or cancel organization invitations.", ResourceKinds: []string{"org"}, Actions: []string{"manage"}, Risk: "medium", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "ai_runtime.catalog.manage", Label: "Manage AI runtime catalog", Description: "Create, update, import, or delete runtime catalog entries.", ResourceKinds: []string{"org"}, Actions: []string{"manage"}, Risk: "high", HighRisk: true, Category: "access", LegacySources: []string{"org_role"}},
	{Key: "project.read", Label: "Read project", Description: "Read project work and project metadata.", ResourceKinds: []string{"project"}, Actions: []string{"read"}, Risk: "low", Category: "access", LegacySources: []string{"project_member"}},
	{Key: "project.write", Label: "Write project", Description: "Create and update project work items.", ResourceKinds: []string{"project"}, Actions: []string{"write"}, Risk: "medium", Category: "access", LegacySources: []string{"project_member"}},
	{Key: "project.member.add", Label: "Add project member", Description: "Add a member to a project.", ResourceKinds: []string{"project"}, Actions: []string{"create"}, Risk: "medium", Category: "access", LegacySources: []string{"project_member"}},
	{Key: "project.member.remove", Label: "Remove project member", Description: "Remove a member from a project.", ResourceKinds: []string{"project"}, Actions: []string{"delete"}, Risk: "high", HighRisk: true, Category: "access", LegacySources: []string{"project_member"}},
	{Key: "team.read", Label: "Read team", Description: "Read team roster and settings.", ResourceKinds: []string{"team"}, Actions: []string{"read"}, Risk: "low", Category: "access", LegacySources: []string{"org_role", "team_member"}},
	{Key: "team.write", Label: "Write team", Description: "Update team configuration.", ResourceKinds: []string{"team"}, Actions: []string{"write"}, Risk: "medium", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "team.member.manage", Label: "Manage team members", Description: "Add, move, or remove team members.", ResourceKinds: []string{"team"}, Actions: []string{"manage"}, Risk: "medium", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "team.project.link.manage", Label: "Manage team project links", Description: "Associate or disassociate projects with a team.", ResourceKinds: []string{"team"}, Actions: []string{"manage"}, Risk: "medium", Category: "access", LegacySources: []string{"org_role"}},
	{Key: "team.memory.read", Label: "Read team memory", Description: "Read the team's memory entries.", ResourceKinds: []string{"team"}, Actions: []string{"read"}, Risk: "low", Category: "access", LegacySources: []string{"team_member", "team_memory_policy"}},
	{Key: "team.memory.propose", Label: "Propose team memory", Description: "Submit team memory proposals.", ResourceKinds: []string{"team"}, Actions: []string{"create"}, Risk: "medium", Category: "access", LegacySources: []string{"team_member", "team_memory_policy"}},
	{Key: "team.memory.review", Label: "Review team memory", Description: "Promote or reject team memory proposals.", ResourceKinds: []string{"team"}, Actions: []string{"review"}, Risk: "high", HighRisk: true, Category: "access", LegacySources: []string{"org_role", "team_memory_policy"}},
	{Key: "file.download", Label: "Download files", Description: "Download files reachable through live scope references.", ResourceKinds: []string{"file", "task", "issue", "plan", "conversation"}, Actions: []string{"download"}, Risk: "medium", Category: "access", LegacySources: []string{"file_scope"}},
}

func accessRoles() []accessRoleDTO {
	return []accessRoleDTO{
		{ID: "org:owner", Name: "Org owner", ScopeKind: "org", Description: "Derived from members.role=owner.", Permissions: []string{"org.read", "org.settings.manage", "org.lifecycle.manage", "org.member.list", "org.member.create.human", "org.member.create.agent", "org.member.role.manage", "org.invitation.manage", "ai_runtime.catalog.manage"}, Editable: false, Source: "org_role", HighRisk: true},
		{ID: "org:admin", Name: "Org admin", ScopeKind: "org", Description: "Derived from members.role=admin.", Permissions: []string{"org.read", "org.member.list", "org.member.create.human", "org.member.create.agent", "org.invitation.manage", "ai_runtime.catalog.manage"}, Editable: false, Source: "org_role", HighRisk: true},
		{ID: "org:member", Name: "Org member", ScopeKind: "org", Description: "Derived from members.role=member.", Permissions: []string{"org.read", "org.member.list"}, Editable: false, Source: "org_role"},
		{ID: "project:owner", Name: "Project owner", ScopeKind: "project", Description: "Derived from pm_project_members.role=owner.", Permissions: []string{"project.read", "project.write", "project.member.add", "project.member.remove"}, Editable: false, Source: "project_member", HighRisk: true},
		{ID: "project:member", Name: "Project member", ScopeKind: "project", Description: "Derived from pm_project_members.role=member.", Permissions: []string{"project.read", "project.write", "project.member.add"}, Editable: false, Source: "project_member"},
		{ID: "team:web-member", Name: "Team web member", ScopeKind: "team", Description: "Phase-1 legacy projection from organization membership.", Permissions: []string{"team.read", "team.write", "team.member.manage", "team.project.link.manage"}, Editable: false, Source: "org_role"},
		{ID: "team:memory-curator", Name: "Team memory curator", ScopeKind: "team", Description: "Derived from team memory policy curator refs.", Permissions: []string{"team.memory.read", "team.memory.propose", "team.memory.review"}, Editable: false, Source: "team_memory_policy", HighRisk: true},
	}
}

func accessRolesForOrg(ctx context.Context, d HandlerDeps, orgID string, catalog map[string]accessPermissionDefinitionDTO) []accessRoleDTO {
	roles := accessRoles()
	if d.DB == nil {
		return roles
	}
	rows, err := d.DB.QueryContext(ctx, `
		SELECT id, name, COALESCE(description, '')
		FROM authorization_roles
		WHERE org_id = ? AND kind = 'custom' AND revoked_at IS NULL
		ORDER BY name, id`, orgID)
	if err != nil {
		return roles
	}
	var custom []accessRoleDTO
	for rows.Next() {
		var role accessRoleDTO
		if err := rows.Scan(&role.ID, &role.Name, &role.Description); err != nil {
			rows.Close()
			return roles
		}
		role.Editable = true
		role.Source = string(authz.SourceCustomRole)
		custom = append(custom, role)
	}
	rows.Close()
	for _, role := range custom {
		role.Permissions = accessRolePermissions(ctx, d, role.ID)
		role.ScopeKind = accessRoleScopeKind(role.Permissions, catalog)
		for _, p := range role.Permissions {
			if catalog[p].Risk == "high" {
				role.HighRisk = true
				break
			}
		}
		roles = append(roles, role)
	}
	return roles
}

func accessRolePermissions(ctx context.Context, d HandlerDeps, roleID string) []string {
	rows, err := d.DB.QueryContext(ctx, `
		SELECT permission_key
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

func accessCatalogFromDefinitions(definitions []authz.PermissionDefinition) []accessPermissionDefinitionDTO {
	meta := map[string]accessPermissionDefinitionDTO{}
	for _, entry := range accessCatalog {
		meta[entry.Key] = entry
	}
	out := make([]accessPermissionDefinitionDTO, 0, len(definitions))
	for _, def := range definitions {
		key := string(def.Key)
		entry := meta[key]
		if entry.Key == "" {
			entry = accessPermissionMetadata(key)
		}
		entry.Key = key
		entry.Category = def.Category
		if entry.Category == "" {
			entry.Category = "access"
		}
		entry.ResourceKinds = append([]string(nil), def.ResourceKinds...)
		entry.Actions = append([]string(nil), def.Actions...)
		entry.LegacySources = append([]string(nil), def.LegacySources...)
		if entry.Risk == "" {
			entry.Risk = accessPermissionRisk(def)
		}
		entry.HighRisk = entry.Risk == "high"
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func accessPermissionMetadata(key string) accessPermissionDefinitionDTO {
	label := strings.ReplaceAll(key, ".", " ")
	return accessPermissionDefinitionDTO{
		Key:         key,
		Label:       strings.Title(label),
		Description: "Unified authorization permission " + key + ".",
		Risk:        "low",
		Category:    "access",
	}
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
	state, err := s.accessDerivedState(r.Context(), d, orgID, svc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	decisions := accessFilterDecisions(state.decisions, state.subjectByRef, state.catalogByKey, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": state.generatedAt.Format(time.RFC3339),
		"subjects":     state.subjects,
		"roles":        state.roles,
		"catalog":      state.catalog,
		"decisions":    decisions,
		"grants":       accessFilterGrants(state.grants, decisions),
		"summary":      accessSummary(decisions, state.grants),
	})
}

func (s *Server) accessRAMRolesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if d.DB == nil {
		writeError(w, http.StatusNotImplemented, "ram_roles_not_wired", "RAM role store not wired")
		return
	}
	roles, err := accessListRAMRoles(r.Context(), d.DB, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ram_roles_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (s *Server) accessRAMRoleDetailHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	detail, found, err := accessRAMRoleDetail(r.Context(), d.DB, orgID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ram_role_failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "RAM role not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) accessRAMRoleCreateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner or admin can manage RAM roles")
		return
	}
	var body accessRAMRoleWriteDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	role, err := accessCreateRAMRole(r.Context(), d.DB, orgID, string(authz.UserSubject(caller.ID())), body)
	if err != nil {
		writeAccessRAMRoleWriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) accessRAMRoleNewVersionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner or admin can manage RAM roles")
		return
	}
	var body accessRAMRoleWriteDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	role, err := accessCreateRAMRoleVersion(r.Context(), d.DB, orgID, r.PathValue("id"), string(authz.UserSubject(caller.ID())), body)
	if err != nil {
		writeAccessRAMRoleWriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) accessRAMRoleUpdateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner or admin can manage RAM roles")
		return
	}
	var body accessRAMRoleWriteDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	role, err := accessUpdateRAMRole(r.Context(), d.DB, orgID, r.PathValue("id"), string(authz.UserSubject(caller.ID())), body)
	if err != nil {
		writeAccessRAMRoleWriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (s *Server) accessRAMRoleRevokeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner or admin can manage RAM roles")
		return
	}
	var body accessRAMRoleRevokeDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	body.ConfirmUnreferenced = true
	if err := accessRevokeRAMRole(r.Context(), d.DB, orgID, r.PathValue("id"), string(authz.UserSubject(caller.ID())), body); err != nil {
		writeAccessRAMRoleWriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) accessRAMRoleDeleteHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner or admin can manage RAM roles")
		return
	}
	var body accessRAMRoleRevokeDTO
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
	}
	if err := accessRevokeRAMRole(r.Context(), d.DB, orgID, r.PathValue("id"), string(authz.UserSubject(caller.ID())), body); err != nil {
		writeAccessRAMRoleWriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var (
	errAccessRAMRoleNotFound    = errors.New("RAM role not found")
	errAccessRAMRoleConflict    = errors.New("RAM role version conflict")
	errAccessRAMRoleKeyConflict = errors.New("RAM role stable key conflict")
	errAccessRAMRoleInvalid     = errors.New("invalid RAM role")
	errAccessRAMRoleReferenced  = errors.New("RAM role is still referenced")
)

func accessListRAMRoles(ctx context.Context, db *sql.DB, orgID string) ([]accessRAMRoleDTO, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ar.id, COALESCE(NULLIF(ar.stable_key, ''), ar.id), ar.name, ar.kind, ar.description, COALESCE(NULLIF(ar.scope_kind, ''), 'org'), ar.revoked_at, ar.version, COALESCE(v.permissions_json, '[]'), COALESCE(v.risk, 'low'), ar.created_at, ar.updated_at,
			(SELECT COUNT(*) FROM team_role_ram_role_mappings m WHERE m.ram_role_id = ar.id)
		FROM authorization_roles ar
		LEFT JOIN authorization_role_versions v ON v.role_id = ar.id AND v.version = ar.version
		WHERE ar.org_id IN ('', ?) AND ar.revoked_at IS NULL
		ORDER BY ar.name, ar.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []accessRAMRoleDTO
	var missingPermissions []int
	for rows.Next() {
		role, err := scanAccessRAMRoleVersion(rows)
		if err != nil {
			return nil, err
		}
		if len(role.Permissions) == 0 {
			missingPermissions = append(missingPermissions, len(out))
		}
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, idx := range missingPermissions {
		out[idx].Permissions = accessRAMRoleCurrentPermissions(ctx, db, out[idx].ID)
		out[idx].Risk = accessPermissionsRisk(out[idx].Permissions)
		out[idx].Scope = accessRAMRoleScope(out[idx].Permissions)
	}
	return out, nil
}

func accessRAMRoleDetail(ctx context.Context, db *sql.DB, orgID, roleID string) (accessRAMRoleDetailDTO, bool, error) {
	if db == nil {
		return accessRAMRoleDetailDTO{}, false, errAccessRAMRoleNotFound
	}
	var detail accessRAMRoleDetailDTO
	var revoked sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, COALESCE(NULLIF(stable_key, ''), id), name, kind, description, COALESCE(NULLIF(scope_kind, ''), 'org'), revoked_at
		FROM authorization_roles
		WHERE id = ? AND org_id IN ('', ?)`, roleID, orgID).
		Scan(&detail.ID, &detail.StableKey, &detail.Name, &detail.Kind, &detail.Description, &detail.Scope, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accessRAMRoleDetailDTO{}, false, nil
		}
		return accessRAMRoleDetailDTO{}, false, err
	}
	if revoked.Valid {
		detail.RevokedAt = &revoked.String
	}
	rows, err := db.QueryContext(ctx, `
		SELECT ar.id, v.stable_key, v.name, ar.kind, v.description, v.scope_kind, ar.revoked_at, v.version, v.permissions_json, v.risk, v.created_at, ar.updated_at,
			(SELECT COUNT(*) FROM team_role_ram_role_mappings m WHERE m.ram_role_id = ar.id)
		FROM authorization_roles ar
		JOIN authorization_role_versions v ON v.role_id = ar.id
		WHERE ar.id = ?
		ORDER BY v.version DESC`, roleID)
	if err != nil {
		return accessRAMRoleDetailDTO{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		version, err := scanAccessRAMRoleVersion(rows)
		if err != nil {
			return accessRAMRoleDetailDTO{}, false, err
		}
		detail.Versions = append(detail.Versions, version)
	}
	if err := rows.Err(); err != nil {
		return accessRAMRoleDetailDTO{}, false, err
	}
	if len(detail.Versions) == 0 {
		current := accessRAMRoleDTO{ID: detail.ID, StableKey: detail.StableKey, Name: detail.Name, Kind: detail.Kind, Description: detail.Description, Scope: detail.Scope, Version: 1, Permissions: accessRAMRoleCurrentPermissions(ctx, db, roleID)}
		current.Risk = accessPermissionsRisk(current.Permissions)
		detail.Versions = []accessRAMRoleDTO{current}
	}
	detail.Latest = detail.Versions[0]
	refs, err := accessRAMRoleReferences(ctx, db, roleID)
	if err != nil {
		return accessRAMRoleDetailDTO{}, false, err
	}
	detail.References = refs
	return detail, true, nil
}

type accessRAMRoleScanner interface {
	Scan(dest ...any) error
}

func scanAccessRAMRoleVersion(row accessRAMRoleScanner) (accessRAMRoleDTO, error) {
	var role accessRAMRoleDTO
	var permissionsJSON string
	var revoked sql.NullString
	if err := row.Scan(&role.ID, &role.StableKey, &role.Name, &role.Kind, &role.Description, &role.Scope, &revoked, &role.Version, &permissionsJSON, &role.Risk, &role.CreatedAt, &role.UpdatedAt, &role.References); err != nil {
		return accessRAMRoleDTO{}, err
	}
	if revoked.Valid {
		role.RevokedAt = &revoked.String
	}
	if err := json.Unmarshal([]byte(permissionsJSON), &role.Permissions); err != nil {
		return accessRAMRoleDTO{}, err
	}
	return role, nil
}

func accessCreateRAMRole(ctx context.Context, db *sql.DB, orgID, actor string, body accessRAMRoleWriteDTO) (accessRAMRoleDetailDTO, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return accessRAMRoleDetailDTO{}, errAccessRAMRoleInvalid
	}
	stableKey := normalizeRAMRoleStableKey(body.StableKey)
	if stableKey == "" {
		stableKey = normalizeRAMRoleStableKey(name)
	}
	if stableKey == "" {
		return accessRAMRoleDetailDTO{}, errAccessRAMRoleInvalid
	}
	permissions, risk, err := normalizeAccessRAMRolePermissions(body.Permissions)
	if err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	scope := normalizeRAMRoleScope(body.Scope, permissions)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := "role-" + accessHash(orgID+"|"+stableKey+"|"+now)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO authorization_roles (id, org_id, kind, stable_key, name, description, scope_kind, created_by, created_at, updated_at, version)
		VALUES (?, ?, 'custom', ?, ?, ?, ?, ?, ?, ?, 1)`, id, orgID, stableKey, name, strings.TrimSpace(body.Description), scope, actor, now, now); err != nil {
		if isRAMRoleStableKeyConflict(err) {
			return accessRAMRoleDetailDTO{}, errAccessRAMRoleKeyConflict
		}
		return accessRAMRoleDetailDTO{}, err
	}
	if err := replaceRAMRolePermissionRows(ctx, tx, id, permissions, now); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	if err := insertAccessRAMRoleVersion(ctx, tx, id, 1, stableKey, name, strings.TrimSpace(body.Description), scope, permissions, risk, actor, now); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	if err := insertRAMRoleAudit(ctx, tx, "authorization.ram_role.created", actor, id, "", map[string]any{"name": name, "stable_key": stableKey, "scope": scope, "permissions": permissions}); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	if err := tx.Commit(); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	detail, _, err := accessRAMRoleDetail(ctx, db, orgID, id)
	return detail, err
}

func accessCreateRAMRoleVersion(ctx context.Context, db *sql.DB, orgID, roleID, actor string, body accessRAMRoleWriteDTO) (accessRAMRoleDetailDTO, error) {
	permissions, risk, err := normalizeAccessRAMRolePermissions(body.Permissions)
	if err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	defer tx.Rollback()
	var ownerOrg, kind, currentStableKey, currentName, currentDescription, currentScope string
	var revoked sql.NullString
	var latest int
	if err := tx.QueryRowContext(ctx, `SELECT org_id, kind, revoked_at, version, COALESCE(NULLIF(stable_key, ''), id), name, description, COALESCE(NULLIF(scope_kind, ''), 'org') FROM authorization_roles WHERE id = ? AND org_id IN ('', ?)`, roleID, orgID).Scan(&ownerOrg, &kind, &revoked, &latest, &currentStableKey, &currentName, &currentDescription, &currentScope); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accessRAMRoleDetailDTO{}, errAccessRAMRoleNotFound
		}
		return accessRAMRoleDetailDTO{}, err
	}
	if ownerOrg == "" || kind != "custom" {
		return accessRAMRoleDetailDTO{}, errAccessRAMRoleInvalid
	}
	if revoked.Valid {
		return accessRAMRoleDetailDTO{}, errAccessRAMRoleInvalid
	}
	if body.ExpectedLatestVersion == nil || *body.ExpectedLatestVersion != latest {
		return accessRAMRoleDetailDTO{}, errAccessRAMRoleConflict
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	nextStableKey := normalizeRAMRoleStableKey(body.StableKey)
	if nextStableKey == "" {
		nextStableKey = currentStableKey
	}
	nextName := strings.TrimSpace(body.Name)
	if nextName == "" {
		nextName = currentName
	}
	nextDescription := strings.TrimSpace(body.Description)
	if body.Description == "" {
		nextDescription = currentDescription
	}
	nextScope := normalizeRAMRoleScope(body.Scope, permissions)
	if body.Scope == "" {
		nextScope = currentScope
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_roles SET stable_key = ?, name = ?, description = ?, scope_kind = ?, updated_at = ?, version = version + 1 WHERE id = ? AND version = ? AND revoked_at IS NULL`, nextStableKey, nextName, nextDescription, nextScope, now, roleID, latest); err != nil {
		if isRAMRoleStableKeyConflict(err) {
			return accessRAMRoleDetailDTO{}, errAccessRAMRoleKeyConflict
		}
		return accessRAMRoleDetailDTO{}, err
	}
	if err := replaceRAMRolePermissionRows(ctx, tx, roleID, permissions, now); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	if err := insertAccessRAMRoleVersion(ctx, tx, roleID, latest+1, nextStableKey, nextName, nextDescription, nextScope, permissions, risk, actor, now); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	if err := insertRAMRoleAudit(ctx, tx, "authorization.ram_role.versioned", actor, roleID, "", map[string]any{"version": latest + 1, "name": nextName, "stable_key": nextStableKey, "scope": nextScope, "permissions": permissions}); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	if err := tx.Commit(); err != nil {
		return accessRAMRoleDetailDTO{}, err
	}
	detail, _, err := accessRAMRoleDetail(ctx, db, orgID, roleID)
	return detail, err
}

func accessUpdateRAMRole(ctx context.Context, db *sql.DB, orgID, roleID, actor string, body accessRAMRoleWriteDTO) (accessRAMRoleDetailDTO, error) {
	return accessCreateRAMRoleVersion(ctx, db, orgID, roleID, actor, body)
}

func insertAccessRAMRoleVersion(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, roleID string, version int, stableKey, name, description, scope string, permissions []string, risk, actor, now string) error {
	raw, err := json.Marshal(permissions)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO authorization_role_versions (role_id, version, stable_key, name, description, scope_kind, permissions_json, risk, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, roleID, version, stableKey, name, description, scope, string(raw), risk, actor, now)
	return err
}

func accessRevokeRAMRole(ctx context.Context, db *sql.DB, orgID, roleID, actor string, body accessRAMRoleRevokeDTO) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM authorization_roles WHERE id=? AND org_id=? AND kind='custom' AND revoked_at IS NULL`, roleID, orgID).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errAccessRAMRoleNotFound
		}
		return err
	}
	if body.ExpectedLatestVersion != nil && *body.ExpectedLatestVersion != version {
		return errAccessRAMRoleConflict
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_role_ram_role_mappings WHERE ram_role_id=?`, roleID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: team_role_mappings=%d", errAccessRAMRoleReferenced, n)
	}
	if !body.ConfirmUnreferenced {
		return fmt.Errorf("%w: confirm_unreferenced required", errAccessRAMRoleConflict)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_role_assignments WHERE role_id=? AND org_id=? AND revoked_at IS NULL`, roleID, orgID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// no direct bindings
	} else {
		return fmt.Errorf("%w: direct_bindings=%d", errAccessRAMRoleReferenced, n)
	}
	res, err := tx.ExecContext(ctx, `UPDATE authorization_roles SET revoked_at=?, updated_at=?, version=version+1 WHERE id=? AND org_id=? AND kind='custom' AND revoked_at IS NULL`, now, now, roleID, orgID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return errAccessRAMRoleNotFound
	}
	if err := insertRAMRoleAudit(ctx, tx, "authorization.ram_role.revoked", actor, roleID, "", map[string]any{"reason": strings.TrimSpace(body.Reason), "previous_version": version, "next_version": version + 1}); err != nil {
		return err
	}
	return tx.Commit()
}

func accessRAMRoleReferences(ctx context.Context, db *sql.DB, roleID string) ([]accessRAMRoleReferenceDTO, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.team_id, COALESCE(t.name, m.team_id), m.team_role
		FROM team_role_ram_role_mappings m
		LEFT JOIN teams t ON t.id = m.team_id
		WHERE m.ram_role_id = ?
		ORDER BY t.name, m.team_id, m.team_role`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []accessRAMRoleReferenceDTO
	for rows.Next() {
		var ref accessRAMRoleReferenceDTO
		if err := rows.Scan(&ref.TeamID, &ref.TeamName, &ref.TeamRole); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func normalizeAccessRAMRolePermissions(in []string) ([]string, string, error) {
	known := map[string]accessPermissionDefinitionDTO{}
	for _, def := range accessCatalog {
		known[def.Key] = def
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	risk := "low"
	for _, permission := range in {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		def, ok := known[permission]
		if !ok {
			return nil, "", fmt.Errorf("%w: permission is not registered: %s", errAccessRAMRoleInvalid, permission)
		}
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		out = append(out, permission)
		if def.Risk == "high" {
			risk = "high"
		} else if def.Risk == "medium" && risk == "low" {
			risk = "medium"
		}
	}
	if len(out) == 0 {
		return nil, "", errAccessRAMRoleInvalid
	}
	sort.Strings(out)
	return out, risk, nil
}

func writeAccessRAMRoleWriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAccessRAMRoleNotFound):
		writeError(w, http.StatusNotFound, "not_found", "RAM role not found")
	case errors.Is(err, errAccessRAMRoleConflict):
		writeError(w, http.StatusConflict, "version_conflict", "RAM role latest version changed")
	case errors.Is(err, errAccessRAMRoleKeyConflict):
		writeError(w, http.StatusConflict, "stable_key_conflict", "RAM role stable key already exists")
	case errors.Is(err, errAccessRAMRoleReferenced):
		writeError(w, http.StatusConflict, "ram_role_referenced", err.Error())
	case errors.Is(err, errAccessRAMRoleInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_ram_role", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "ram_role_failed", err.Error())
	}
}

func isRAMRoleStableKeyConflict(err error) bool {
	message := err.Error()
	return strings.Contains(message, "idx_authorization_roles_system_stable_key") ||
		strings.Contains(message, "idx_authorization_roles_custom_org_stable_key")
}

func replaceRAMRolePermissionRows(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, roleID string, permissions []string, now string) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM authorization_role_permissions WHERE role_id=?`, roleID); err != nil {
		return err
	}
	for _, permission := range permissions {
		resourceKind := "org"
		for _, def := range accessCatalog {
			if def.Key == permission && len(def.ResourceKinds) > 0 {
				resourceKind = def.ResourceKinds[0]
				break
			}
		}
		if _, err := exec.ExecContext(ctx, `INSERT INTO authorization_role_permissions (role_id,permission_key,resource_kind,delegatable,created_at) VALUES (?,?,?,?,?)`, roleID, permission, resourceKind, 0, now); err != nil {
			return err
		}
	}
	return nil
}

func accessRAMRoleCurrentPermissions(ctx context.Context, db *sql.DB, roleID string) []string {
	rows, err := db.QueryContext(ctx, `SELECT permission_key FROM authorization_role_permissions WHERE role_id=? ORDER BY permission_key`, roleID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return out
		}
		out = append(out, permission)
	}
	return out
}

func accessPermissionsRisk(permissions []string) string {
	_, risk, err := normalizeAccessRAMRolePermissions(permissions)
	if err != nil {
		return "low"
	}
	return risk
}

func accessRAMRoleScope(permissions []string) string {
	return normalizeRAMRoleScope("", permissions)
}

func normalizeRAMRoleScope(scope string, permissions []string) string {
	scope = strings.TrimSpace(scope)
	if scope != "" {
		return scope
	}
	kinds := map[string]struct{}{}
	for _, permission := range permissions {
		for _, def := range accessCatalog {
			if def.Key != permission {
				continue
			}
			for _, kind := range def.ResourceKinds {
				kinds[kind] = struct{}{}
			}
		}
	}
	if len(kinds) == 1 {
		for kind := range kinds {
			return kind
		}
	}
	if len(kinds) > 1 {
		return "mixed"
	}
	return "org"
}

func normalizeRAMRoleStableKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func insertRAMRoleAudit(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, eventType, actor, roleID, requestID string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO authorization_audit_events (id,event_type,actor_ref,role_id,request_id,payload_json,created_at) VALUES (?,?,?,?,?,?,?)`, "audit-"+accessHash(eventType+"|"+actor+"|"+roleID+"|"+time.Now().UTC().Format(time.RFC3339Nano)), eventType, actor, roleID, requestID, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Server) accessBatchPreviewHandler(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	s.accessBatchUnifiedHandler(w, r, d, svc, authz.UserSubject(caller.ID()), orgID, body, true)
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
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	s.accessBatchUnifiedHandler(w, r, d, svc, authz.UserSubject(caller.ID()), orgID, body, false)
}

func (s *Server) accessBulkRevokeHandler(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusBadRequest, "revoke_preview_required", "revoke requires /access/grants/revoke/preview followed by /confirm")
}

func (s *Server) accessBulkRevokePreviewHandler(w http.ResponseWriter, r *http.Request) {
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
	var body accessRevokeRequestDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	s.accessBulkRevokeUnifiedHandler(w, r, d, svc, authz.UserSubject(caller.ID()), orgID, body.GrantIDs, body.Reason, body.Message, true, "", "", "")
}

func (s *Server) accessBulkRevokeConfirmHandler(w http.ResponseWriter, r *http.Request) {
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
	var body accessRevokeRequestDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	s.accessBulkRevokeUnifiedHandler(w, r, d, svc, authz.UserSubject(caller.ID()), orgID, body.GrantIDs, body.Reason, body.Message, false, body.PreviewID, body.Token, body.IdempotencyKey)
}

func (s *Server) accessRoleUpdateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, callerMember, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner or admin can manage access roles")
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	var body struct {
		Permissions []string `json:"permissions"`
		Reason      string   `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	roleID := r.PathValue("id")
	for _, role := range accessRoles() {
		if role.ID == roleID {
			writeError(w, http.StatusConflict, "role_derived", "phase-1 access roles are derived from production membership and policy rows")
			return
		}
	}
	role, found, err := accessCustomRole(r.Context(), d, orgID, roleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "role_lookup_failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "access role not found")
		return
	}
	definitions, err := svc.ListDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authorization_failed", err.Error())
		return
	}
	catalog := accessCatalogFromDefinitions(definitions)
	catalogByKey := make(map[string]accessPermissionDefinitionDTO, len(catalog))
	for _, def := range catalog {
		catalogByKey[def.Key] = def
	}
	perms := make([]authz.RolePermissionInput, 0, len(body.Permissions))
	for _, permission := range body.Permissions {
		def := catalogByKey[permission]
		if def.Key == "" || len(def.ResourceKinds) == 0 {
			writeError(w, http.StatusUnprocessableEntity, "invalid_permission_request", "permission is not registered: "+permission)
			return
		}
		perms = append(perms, authz.RolePermissionInput{PermissionKey: authz.PermissionKey(permission), ResourceKind: def.ResourceKinds[0]})
	}
	_, err = svc.ApplyBatch(r.Context(), authz.BatchRequest{
		IdempotencyKey: "access-role-" + accessHash(orgID+"|"+authz.UserSubject(caller.ID()).BareID()+"|"+roleID+"|"+strings.Join(body.Permissions, ",")),
		ActorRef:       authz.UserSubject(caller.ID()),
		OrgID:          orgID,
		Operations: []authz.BatchOperation{{
			ID:          "set-role-permissions",
			Type:        "set_role_permissions",
			Role:        authz.RoleInput{ID: roleID},
			Permissions: perms,
		}},
	})
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
		return
	}
	role.Permissions = append([]string(nil), body.Permissions...)
	sort.Strings(role.Permissions)
	role.ScopeKind = accessRoleScopeKind(role.Permissions, catalogByKey)
	role.HighRisk = false
	for _, permission := range role.Permissions {
		if catalogByKey[permission].Risk == "high" {
			role.HighRisk = true
			break
		}
	}
	writeJSON(w, http.StatusOK, role)
}

func accessCustomRole(ctx context.Context, d HandlerDeps, orgID, roleID string) (accessRoleDTO, bool, error) {
	if d.DB == nil {
		return accessRoleDTO{}, false, nil
	}
	var role accessRoleDTO
	err := d.DB.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(description, '')
		FROM authorization_roles
		WHERE id = ? AND org_id = ? AND kind = 'custom' AND revoked_at IS NULL`, roleID, orgID).
		Scan(&role.ID, &role.Name, &role.Description)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accessRoleDTO{}, false, nil
		}
		return accessRoleDTO{}, false, err
	}
	role.Editable = true
	role.Source = string(authz.SourceCustomRole)
	role.Permissions = accessRolePermissions(ctx, d, role.ID)
	role.ScopeKind = "org"
	return role, true, nil
}

func (s *Server) accessDerivedState(ctx context.Context, d HandlerDeps, orgID string, svc *authz.Service) (accessDerivedState, error) {
	now := time.Now().UTC()
	definitions, err := svc.ListDefinitions(ctx)
	if err != nil {
		return accessDerivedState{}, err
	}
	catalog := accessCatalogFromDefinitions(definitions)
	catalogByKey := make(map[string]accessPermissionDefinitionDTO, len(catalog))
	for _, def := range catalog {
		catalogByKey[def.Key] = def
	}
	state := accessDerivedState{
		generatedAt:  now,
		catalog:      catalog,
		catalogByKey: catalogByKey,
		subjectByRef: map[string]accessSubjectDTO{},
	}
	state.roles = accessRolesForOrg(ctx, d, orgID, catalogByKey)
	members, err := d.MemberRepo.ListByOrganization(ctx, orgID)
	if err != nil {
		return state, err
	}
	teamNamesByRef := map[string][]string{}
	var teams []*team.Team
	if d.TeamService != nil {
		teams, err = d.TeamService.ListTeams(ctx, orgID)
		if err != nil {
			return state, err
		}
		for _, tm := range teams {
			rows, merr := d.TeamService.ListMembers(ctx, tm.ID())
			if merr != nil {
				return state, merr
			}
			for _, row := range rows {
				ref := row.Ref.String()
				teamNamesByRef[ref] = append(teamNamesByRef[ref], tm.Name())
			}
		}
	}
	for _, m := range members {
		ident := accessIdentity(ctx, d, m.IdentityID())
		subj := accessSubjectForMember(m, ident)
		subj.TeamNames = sortedStrings(teamNamesByRef[subj.Ref])
		state.subjects = append(state.subjects, subj)
		state.subjectByRef[subj.Ref] = subj
	}
	sort.Slice(state.subjects, func(i, j int) bool {
		return state.subjects[i].Name < state.subjects[j].Name
	})
	orgRes := accessResourceScopeDTO{Kind: "org", ID: orgID, OrgID: orgID, Label: orgID}
	for _, m := range members {
		subj := accessSubjectForMember(m, accessIdentity(ctx, d, m.IdentityID()))
		state.addOrgRoleDecisions(subj, m, orgRes)
	}
	if d.PM != nil {
		projects, perr := d.PM.ListProjects(ctx, orgID)
		if perr != nil {
			return state, perr
		}
		for _, p := range projects {
			state.addProjectDecisions(ctx, d, p)
		}
	}
	for _, tm := range teams {
		state.addTeamDecisions(ctx, d, members, tm)
	}
	state.decisions = state.authorizedDecisions(ctx, svc)
	state.decisions = append(state.decisions, accessNotApplicableRows(state)...)
	state.grants = accessGrantsFromDecisions(state.generatedAt, state.decisions, state.subjectByRef)
	return state, nil
}

func (s *accessDerivedState) addOrgRoleDecisions(subj accessSubjectDTO, m *identity.Member, org accessResourceScopeDTO) {
	joined := m.Status() == identity.MemberJoined
	evidence := "members:" + m.ID()
	if joined {
		s.addDecision(subj.Ref, "org.read", org, "org_role", "organization membership derives org.read", evidence, "allowed")
		s.addDecision(subj.Ref, "org.member.list", org, "org_role", "organization membership derives org.member.list", evidence, "allowed")
	} else {
		s.addDecision(subj.Ref, "org.read", org, "org_role", "member status is disabled", evidence, "denied")
	}
	if joined && m.Role().AtLeast(identity.RoleAdmin) {
		s.addDecision(subj.Ref, "org.member.create.human", org, "org_role", "owner/admin role derives human-member creation", evidence, "allowed")
		s.addDecision(subj.Ref, "org.member.create.agent", org, "org_role", "owner/admin role derives agent-member creation", evidence, "allowed")
		s.addDecision(subj.Ref, "org.invitation.manage", org, "org_role", "owner/admin role derives invitation management", evidence, "allowed")
		s.addDecision(subj.Ref, "ai_runtime.catalog.manage", org, "org_role", "owner/admin role derives runtime catalog management", evidence, "allowed")
	} else {
		s.addDecision(subj.Ref, "org.member.create.agent", org, "org_role", "requires owner or admin role", evidence, "unauthorized")
	}
	if joined && m.Role() == identity.RoleOwner {
		s.addDecision(subj.Ref, "org.settings.manage", org, "org_role", "owner role derives organization settings management", evidence, "allowed")
		s.addDecision(subj.Ref, "org.lifecycle.manage", org, "org_role", "owner role derives organization lifecycle management", evidence, "allowed")
		s.addDecision(subj.Ref, "org.member.role.manage", org, "org_role", "owner role derives organization role management", evidence, "allowed")
	} else {
		s.addDecision(subj.Ref, "org.member.role.manage", org, "org_role", "requires owner role", evidence, "unauthorized")
	}
}

func (s *accessDerivedState) addProjectDecisions(ctx context.Context, d HandlerDeps, p *pm.Project) {
	res := accessResourceScopeDTO{Kind: "project", ID: string(p.ID()), OrgID: p.OrganizationID(), Label: p.Name()}
	rows, err := d.PM.ListMembers(ctx, p.ID())
	if err != nil {
		return
	}
	memberRefs := map[string]*pm.ProjectMember{}
	for _, row := range rows {
		ref := string(row.IdentityID())
		memberRefs[ref] = row
		if _, ok := s.subjectByRef[ref]; !ok {
			s.subjectByRef[ref] = accessSubjectDTO{Ref: ref, Kind: accessKindFromRef(ref), Name: ref, Status: "unavailable"}
			s.subjects = append(s.subjects, s.subjectByRef[ref])
		}
		evidence := "pm_project_members:" + string(row.ID())
		s.addDecision(ref, "project.read", res, "project_member", "project membership derives project.read", evidence, "allowed")
		s.addDecision(ref, "project.write", res, "project_member", "project membership derives project.write", evidence, "allowed")
		s.addDecision(ref, "project.member.add", res, "project_member", "project membership derives project.member.add in the current service", evidence, "allowed")
		if row.Role() == pm.RoleOwner {
			s.addDecision(ref, "project.member.remove", res, "project_member", "project owner role derives project.member.remove", evidence, "allowed")
		} else {
			s.addDecision(ref, "project.member.remove", res, "project_member", "requires project owner role", evidence, "unauthorized")
		}
	}
	for _, subj := range s.subjects {
		if _, ok := memberRefs[subj.Ref]; ok {
			continue
		}
		s.addDecision(subj.Ref, "project.write", res, "project_member", "subject is not a project member", "pm_project_members:missing", "unauthorized")
	}
}

func (s *accessDerivedState) addTeamDecisions(ctx context.Context, d HandlerDeps, orgMembers []*identity.Member, tm *team.Team) {
	res := accessResourceScopeDTO{Kind: "team", ID: tm.ID().String(), OrgID: tm.OrgID(), Label: tm.Name()}
	teamMembers, err := d.TeamService.ListMembers(ctx, tm.ID())
	if err != nil {
		return
	}
	teamMemberRefs := map[string]*team.TeamMember{}
	for _, row := range teamMembers {
		teamMemberRefs[row.Ref.String()] = row
	}
	for _, m := range orgMembers {
		subj := accessSubjectForMember(m, accessIdentity(ctx, d, m.IdentityID()))
		evidence := "members:" + m.ID()
		if m.Status() != identity.MemberJoined {
			s.addDecision(subj.Ref, "team.read", res, "org_role", "disabled organization member does not derive team.read", evidence, "denied")
			continue
		}
		s.addDecision(subj.Ref, "team.read", res, "org_role", "phase-1 Web compatibility derives team.read from organization membership", evidence, "allowed")
		s.addDecision(subj.Ref, "team.write", res, "org_role", "phase-1 Web compatibility derives team.write from organization membership", evidence, "allowed")
		s.addDecision(subj.Ref, "team.member.manage", res, "org_role", "phase-1 Web compatibility derives team.member.manage from organization membership", evidence, "allowed")
		s.addDecision(subj.Ref, "team.project.link.manage", res, "org_role", "phase-1 Web compatibility derives team.project.link.manage from organization membership", evidence, "allowed")
		if m.Role().AtLeast(identity.RoleAdmin) {
			s.addDecision(subj.Ref, "team.memory.review", res, "org_role", "owner/admin role derives team.memory.review", evidence, "allowed")
		}
		if row := teamMemberRefs[subj.Ref]; row != nil {
			mev := fmt.Sprintf("team_members:%s/%s/%s", tm.ID().String(), row.Ref.String(), row.Role)
			s.addDecision(subj.Ref, "team.memory.read", res, "team_member", "current team membership derives team.memory.read", mev, "allowed")
			if row.Kind == team.MemberKindAgent {
				s.addDecision(subj.Ref, "team.memory.propose", res, "team_member", "current team agent membership derives team.memory.propose", mev, "allowed")
			}
			if tm.MemoryPolicy().IsCurator(team.MemberRef(subj.Ref)) {
				s.addDecision(subj.Ref, "team.memory.review", res, "team_memory_policy", "team memory curator policy derives team.memory.review", "team_memory_policy:"+tm.ID().String(), "allowed")
			} else if row.Kind == team.MemberKindAgent && !s.hasAllowedDecision(subj.Ref, "team.memory.review", res) {
				s.addDecision(subj.Ref, "team.memory.review", res, "team_memory_policy", "agent is not a curator for this team policy", "team_memory_policy:"+tm.ID().String(), "denied")
			}
		}
	}
}

func (s *accessDerivedState) addDecision(subjectRef, permission string, resource accessResourceScopeDTO, source, reason, evidenceRef, status string) {
	def := s.catalogByKey[permission]
	allowed := status == "allowed"
	grantID := ""
	if allowed {
		grantID = accessGrantID(subjectRef, permission, resource, source)
	}
	s.decisions = append(s.decisions, accessDecisionDTO{
		Allowed:     allowed,
		SubjectRef:  subjectRef,
		Permission:  permission,
		Resource:    resource,
		Source:      source,
		Reason:      reason,
		EvidenceRef: evidenceRef,
		Status:      status,
		GrantID:     grantID,
		Risk:        def.Risk,
	})
}

func (s accessDerivedState) authorizedDecisions(ctx context.Context, svc *authz.Service) []accessDecisionDTO {
	out := make([]accessDecisionDTO, 0, len(s.decisions))
	seen := map[string]struct{}{}
	resources := s.decisionResources()
	for _, decision := range s.decisions {
		key := strings.Join([]string{decision.SubjectRef, decision.Permission, resourceKey(decision.Resource)}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		def := s.catalogByKey[decision.Permission]
		if def.Key == "" {
			def = accessPermissionDefinitionDTO{Key: decision.Permission, Risk: "medium"}
		}
		decision.Risk = fallback(decision.Risk, def.Risk)
		if !accessPermissionApplies(def, decision.Resource.Kind) {
			decision.Allowed = false
			decision.Status = "not_applicable"
			decision.Source = fallback(decision.Source, strings.Join(def.LegacySources, ","))
			decision.Reason = fmt.Sprintf("%s does not apply to %s resources", decision.Permission, decision.Resource.Kind)
			decision.EvidenceRef = "permission_registry:" + decision.Permission
			decision.GrantID = ""
			out = append(out, decision)
			continue
		}
		explain, err := svc.Explain(ctx, authz.CheckRequest{
			SubjectRef: authz.SubjectRef(decision.SubjectRef),
			Transport:  authz.TransportWeb,
			Permission: authz.PermissionKey(decision.Permission),
			Resource:   accessAuthzResource(decision.Resource),
		})
		decision.Allowed = explain.Decision.Allowed
		if explain.Decision.Resource.Kind != "" {
			decision.Resource = accessResourceFromAuthz(explain.Decision.Resource, decision.Resource)
		}
		if explain.Decision.Source != "" {
			decision.Source = string(explain.Decision.Source)
		}
		decision.EvidenceRef = explain.Decision.EvidenceRef
		if eff, ok := accessDecisionEffective(explain.Effective, decision.Permission, decision.EvidenceRef); ok {
			decision.RoleID = eff.RoleID
			if eff.ExpiresAt != nil {
				value := eff.ExpiresAt.UTC().Format(time.RFC3339)
				decision.ExpiresAt = &value
			}
		}
		if decision.Allowed {
			decision.Status = "allowed"
			decision.Reason = fallback(explain.Decision.Reason, "matched unified authorization service")
			decision.GrantID = accessGrantIDForDecision(decision)
		} else {
			decision.Status = accessStatusForAuthorizationError(err, explain)
			decision.Reason = accessReasonForAuthorizationError(err, explain)
			decision.GrantID = ""
		}
		out = append(out, decision)
	}
	out = s.appendAdditionalEffectiveDecisions(ctx, svc, out, seen, resources)
	return out
}

func (s accessDerivedState) decisionResources() []accessResourceScopeDTO {
	seen := map[string]struct{}{}
	var resources []accessResourceScopeDTO
	for _, decision := range s.decisions {
		key := resourceKey(decision.Resource)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		resources = append(resources, decision.Resource)
	}
	return resources
}

func (s accessDerivedState) appendAdditionalEffectiveDecisions(ctx context.Context, svc *authz.Service, decisions []accessDecisionDTO, seen map[string]struct{}, resources []accessResourceScopeDTO) []accessDecisionDTO {
	seenEffective := map[string]struct{}{}
	for _, decision := range decisions {
		seenEffective[accessEffectiveDecisionKey(decision)] = struct{}{}
	}
	for _, subj := range s.subjects {
		for _, resource := range resources {
			effective, err := svc.ListEffective(ctx, authz.SubjectRef(subj.Ref), accessAuthzResource(resource))
			if err != nil {
				continue
			}
			resolved := accessResourceFromAuthz(effective.Resource, resource)
			for _, permission := range effective.Permissions {
				if permission.Source != authz.SourceCustomRole && permission.Source != authz.SourceTeamRoleRAM {
					continue
				}
				key := strings.Join([]string{subj.Ref, string(permission.Key), resourceKey(resolved)}, "|")
				if _, ok := seen[key]; ok && permission.Source != authz.SourceCustomRole && permission.Source != authz.SourceTeamRoleRAM {
					continue
				}
				def := s.catalogByKey[string(permission.Key)]
				decision := accessDecisionDTO{
					Allowed:     true,
					SubjectRef:  subj.Ref,
					Permission:  string(permission.Key),
					Resource:    resolved,
					Source:      string(permission.Source),
					Reason:      "matched unified authorization service",
					EvidenceRef: permission.EvidenceRef,
					Status:      "allowed",
					RoleID:      permission.RoleID,
					Risk:        fallback(def.Risk, "medium"),
				}
				if permission.ExpiresAt != nil {
					value := permission.ExpiresAt.UTC().Format(time.RFC3339)
					decision.ExpiresAt = &value
				}
				decision.GrantID = accessGrantIDForDecision(decision)
				if _, ok := seenEffective[accessEffectiveDecisionKey(decision)]; ok {
					continue
				}
				seen[key] = struct{}{}
				seenEffective[accessEffectiveDecisionKey(decision)] = struct{}{}
				decisions = append(decisions, decision)
			}
		}
	}
	return decisions
}

func accessEffectiveDecisionKey(decision accessDecisionDTO) string {
	return strings.Join([]string{decision.SubjectRef, decision.Permission, resourceKey(decision.Resource), decision.Source, decision.EvidenceRef}, "|")
}

func accessDecisionEffective(effective []authz.EffectivePermission, permission, evidenceRef string) (authz.EffectivePermission, bool) {
	for _, eff := range effective {
		if string(eff.Key) == permission && eff.EvidenceRef == evidenceRef {
			return eff, true
		}
	}
	return authz.EffectivePermission{}, false
}

func accessGrantIDForDecision(decision accessDecisionDTO) string {
	if decision.Source == string(authz.SourceCustomRole) && strings.HasPrefix(decision.EvidenceRef, "authorization_role_assignments:") {
		return strings.TrimPrefix(decision.EvidenceRef, "authorization_role_assignments:")
	}
	return accessGrantID(decision.SubjectRef, decision.Permission, decision.Resource, decision.Source)
}

func accessStatusForAuthorizationError(err error, explain authz.ExplainResult) string {
	if errors.Is(err, authz.ErrPermissionUndefined) {
		return "not_applicable"
	}
	reason := accessReasonForAuthorizationError(err, explain)
	switch {
	case strings.Contains(reason, "disabled"):
		return "denied"
	case errors.Is(err, authz.ErrDenied), errors.Is(err, authz.ErrNotDelegatable), errors.Is(err, authz.ErrNotFound):
		return "unauthorized"
	default:
		return "denied"
	}
}

func accessReasonForAuthorizationError(err error, explain authz.ExplainResult) string {
	if explain.Decision.Reason != "" && explain.Decision.Reason != "permission_denied" {
		return explain.Decision.Reason
	}
	if len(explain.DeniedBy) > 0 {
		return strings.Join(explain.DeniedBy, "; ")
	}
	if err != nil {
		return err.Error()
	}
	return "permission denied by unified authorization service"
}

func accessAuthzResource(resource accessResourceScopeDTO) authz.ResourceScope {
	return authz.ResourceScope{
		Kind:      resource.Kind,
		ID:        resource.ID,
		OrgID:     resource.OrgID,
		ProjectID: resource.ProjectID,
		URI:       resource.ID,
	}
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
	return out
}

func (s accessDerivedState) hasAllowedDecision(subjectRef, permission string, resource accessResourceScopeDTO) bool {
	key := resourceKey(resource)
	for _, d := range s.decisions {
		if d.SubjectRef == subjectRef && d.Permission == permission && d.Allowed && resourceKey(d.Resource) == key {
			return true
		}
	}
	return false
}

func accessIdentity(ctx context.Context, d HandlerDeps, id string) *identity.Identity {
	if d.IdentityRepo == nil {
		return nil
	}
	ident, err := d.IdentityRepo.GetByID(ctx, id)
	if err != nil {
		return nil
	}
	return ident
}

func accessSubjectForMember(m *identity.Member, ident *identity.Identity) accessSubjectDTO {
	kind := "human"
	prefix := "user"
	name := m.IdentityID()
	if ident != nil {
		name = ident.DisplayName()
		if ident.Kind() == identity.KindAgent {
			kind = "agent"
			prefix = "agent"
		}
	}
	return accessSubjectDTO{
		Ref:    prefix + ":" + m.IdentityID(),
		Kind:   kind,
		Name:   name,
		Role:   string(m.Role()),
		Status: string(m.Status()),
	}
}

func accessKindFromRef(ref string) string {
	if strings.HasPrefix(ref, "agent:") {
		return "agent"
	}
	if strings.HasPrefix(ref, "worker:") {
		return "worker"
	}
	return "human"
}

func accessNotApplicableRows(state accessDerivedState) []accessDecisionDTO {
	rows := []accessDecisionDTO{}
	if len(state.subjects) == 0 {
		return rows
	}
	orgID := ""
	for _, d := range state.decisions {
		if d.Resource.Kind == "org" {
			orgID = d.Resource.ID
			break
		}
	}
	if orgID == "" {
		return rows
	}
	subj := state.subjects[0]
	def := state.catalogByKey["file.download"]
	rows = append(rows, accessDecisionDTO{
		Allowed:     false,
		SubjectRef:  subj.Ref,
		Permission:  "file.download",
		Resource:    accessResourceScopeDTO{Kind: "org", ID: orgID, OrgID: orgID, Label: orgID},
		Source:      "file_scope",
		Reason:      "file.download does not apply to org resources",
		EvidenceRef: "permission_registry:file.download",
		Status:      "not_applicable",
		Risk:        def.Risk,
	})
	return rows
}

func accessGrantsFromDecisions(now time.Time, decisions []accessDecisionDTO, subjects map[string]accessSubjectDTO) []accessGrantDTO {
	grants := []accessGrantDTO{}
	seen := map[string]struct{}{}
	for _, d := range decisions {
		if !d.Allowed || d.GrantID == "" || d.Permission == "org.read" || d.Permission == "org.member.list" {
			continue
		}
		if _, ok := seen[d.GrantID]; ok {
			continue
		}
		seen[d.GrantID] = struct{}{}
		subj := subjects[d.SubjectRef]
		grants = append(grants, accessGrantDTO{
			ID:          d.GrantID,
			SubjectRef:  d.SubjectRef,
			SubjectName: subj.Name,
			Permission:  d.Permission,
			Resource:    d.Resource,
			Source:      d.Source,
			Status:      "active",
			ExpiresAt:   d.ExpiresAt,
			CreatedBy:   "system",
			CreatedAt:   now.Format(time.RFC3339),
			RoleID:      d.RoleID,
			Risk:        d.Risk,
		})
	}
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].Risk != grants[j].Risk {
			return grants[i].Risk == "high"
		}
		return grants[i].Permission < grants[j].Permission
	})
	return grants
}

func accessFilterDecisions(decisions []accessDecisionDTO, subjects map[string]accessSubjectDTO, catalog map[string]accessPermissionDefinitionDTO, r *http.Request) []accessDecisionDTO {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	risk := r.URL.Query().Get("risk")
	status := r.URL.Query().Get("status")
	resourceKind := r.URL.Query().Get("resource_kind")
	subjectKind := r.URL.Query().Get("subject_kind")
	out := make([]accessDecisionDTO, 0, len(decisions))
	for _, d := range decisions {
		subj := subjects[d.SubjectRef]
		def := catalog[d.Permission]
		if q != "" {
			haystack := strings.ToLower(strings.Join([]string{subj.Name, d.SubjectRef, d.Permission, d.Resource.Label, d.Reason, d.Source}, " "))
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		if risk != "" && risk != "all" && d.Risk != risk {
			continue
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
		if d.Risk == "" {
			d.Risk = def.Risk
		}
		out = append(out, d)
	}
	return out
}

func accessFilterGrants(grants []accessGrantDTO, decisions []accessDecisionDTO) []accessGrantDTO {
	if len(decisions) == 0 {
		return []accessGrantDTO{}
	}
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

func accessSummary(decisions []accessDecisionDTO, grants []accessGrantDTO) map[string]int {
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
	for _, grant := range grants {
		if grant.Status == "expires_soon" {
			summary["expiring"]++
		}
	}
	return summary
}

func (s *Server) accessBatchUnifiedHandler(w http.ResponseWriter, r *http.Request, d HandlerDeps, svc *authz.Service, actor authz.SubjectRef, orgID string, body accessBatchRequestDTO, preview bool) {
	state, err := s.accessDerivedState(r.Context(), d, orgID, svc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	if preview {
		items := accessBatchItems(r.Context(), svc, orgID, actor, body, state, false)
		writeJSON(w, http.StatusOK, map[string]any{
			"request_id": fmt.Sprintf("access-preview-%s", accessHash(fmt.Sprintf("%s|%s|%v", actor, orgID, body))),
			"expires_at": body.ExpiresAt,
			"items":      items,
			"summary":    accessPreviewSummary(items),
		})
		return
	}
	for _, item := range accessBatchItems(r.Context(), svc, orgID, actor, body, state, false) {
		if strings.Contains(strings.ToLower(item.Reason), "duplicate direct grant") {
			writeError(w, http.StatusConflict, "duplicate_direct_grant", item.Reason)
			return
		}
	}
	items := accessBatchItems(r.Context(), svc, orgID, actor, body, state, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"operation_id": fmt.Sprintf("access-apply-%s", accessHash(fmt.Sprintf("%s|%s|%v", actor, orgID, body))),
		"applied_at":   time.Now().UTC().Format(time.RFC3339),
		"items":        items,
		"summary":      accessResultSummary(items),
	})
}

func accessBatchItems(ctx context.Context, svc *authz.Service, orgID string, actor authz.SubjectRef, body accessBatchRequestDTO, state accessDerivedState, applied bool) []accessBatchItemDTO {
	items := []accessBatchItemDTO{}
	expiresAt, expiresErr := parseAccessExpiry(body.ExpiresAt)
	for _, subjectRef := range body.SubjectRefs {
		for _, permission := range body.PermissionKeys {
			for _, resource := range body.Resources {
				idx := len(items) + 1
				item := accessEvaluateBatchItem(ctx, svc, orgID, actor, body, state, idx, subjectRef, permission, resource, expiresAt, expiresErr, applied)
				items = append(items, item)
			}
		}
	}
	return items
}

func accessEvaluateBatchItem(ctx context.Context, svc *authz.Service, orgID string, actor authz.SubjectRef, body accessBatchRequestDTO, state accessDerivedState, idx int, subjectRef, permission string, resource accessResourceScopeDTO, expiresAt *time.Time, expiresErr error, applied bool) accessBatchItemDTO {
	subj, subjectFound := state.subjectByRef[subjectRef]
	def, permissionFound := state.catalogByKey[permission]
	item := accessBatchItemDTO{
		ID:          fmt.Sprintf("item-%d", idx),
		SubjectRef:  subjectRef,
		SubjectName: fallback(subj.Name, subjectRef),
		Permission:  permission,
		Resource:    resource,
		Status:      "denied",
		Risk:        fallback(def.Risk, "medium"),
		HighRisk:    def.Risk == "high",
		Reason:      "permission denied by unified authorization service",
	}
	switch {
	case expiresErr != nil:
		item.Status = "denied"
		item.Reason = "invalid expiry: " + expiresErr.Error()
		return item
	case !subjectFound || subj.Status != string(identity.MemberJoined):
		item.Status = "unauthorized"
		item.Reason = "subject is unavailable or outside this organization"
		return item
	case !permissionFound:
		item.Status = "denied"
		item.Reason = "permission is not registered"
		item.EvidenceRef = "permission_registry:missing"
		return item
	case !accessPermissionApplies(def, resource.Kind):
		item.Status = "not_applicable"
		item.Reason = fmt.Sprintf("%s does not apply to %s", permission, resource.Kind)
		item.EvidenceRef = "permission_registry:" + permission
		return item
	}
	req := accessBatchAuthorizationRequest(orgID, actor, body, item, expiresAt)
	var (
		res authz.BatchResult
		err error
	)
	if applied {
		res, err = svc.ApplyBatch(ctx, req)
	} else {
		res, err = svc.PreviewBatch(ctx, req)
	}
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
	if applied {
		item.Reason = "grant applied by unified authorization API"
		item.EvidenceRef = "authorization_batch:" + req.IdempotencyKey
	} else {
		item.Reason = "grant can be applied by unified authorization API"
		item.EvidenceRef = "authorization_preview:" + item.ID
	}
	return item
}

func accessBatchAuthorizationRequest(orgID string, actor authz.SubjectRef, body accessBatchRequestDTO, item accessBatchItemDTO, expiresAt *time.Time) authz.BatchRequest {
	roleID := accessRoleIDForPermission(item.Permission, item.Resource.Kind)
	resource := accessAuthzResource(item.Resource)
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
					SubjectRef: authz.SubjectRef(item.SubjectRef),
					RoleID:     roleID,
					Resource:   resource,
					ExpiresAt:  expiresAt,
				},
			},
		},
	}
}

func parseAccessExpiry(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func accessRoleIDForPermission(permission, resourceKind string) string {
	return "role-access-" + accessHash(permission+"|"+resourceKind)
}

func accessBatchIdempotencyKey(prefix, orgID string, actor authz.SubjectRef, previewID string, item accessBatchItemDTO) string {
	seed := strings.Join([]string{prefix, orgID, string(actor), previewID, item.SubjectRef, item.Permission, resourceKey(item.Resource)}, "|")
	return "access-" + prefix + "-" + accessHash(seed)
}

func accessHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])[:16]
}

func accessBatchStatusForError(err error) string {
	switch {
	case errors.Is(err, authz.ErrPermissionUndefined):
		return "not_applicable"
	case errors.Is(err, authz.ErrDenied), errors.Is(err, authz.ErrNotDelegatable), errors.Is(err, authz.ErrNotFound), errors.Is(err, authz.ErrUnauthenticated):
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
	case strings.Contains(reason, "not delegatable"), strings.Contains(reason, "permission denied"), strings.Contains(reason, "not a joined org member"), strings.Contains(reason, "agents cannot"):
		return "unauthorized"
	default:
		return "denied"
	}
}

func (s *Server) accessBulkRevokeUnifiedHandler(w http.ResponseWriter, r *http.Request, d HandlerDeps, svc *authz.Service, actor authz.SubjectRef, orgID string, grantIDs []string, reason string, message string, preview bool, previewID string, token string, idempotencyKey string) {
	state, err := s.accessDerivedState(r.Context(), d, orgID, svc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	grants := make(map[string]accessGrantDTO, len(state.grants))
	for _, grant := range state.grants {
		grants[grant.ID] = grant
	}
	items := make([]accessBatchItemDTO, 0, len(grantIDs))
	operations := make([]authz.BatchOperation, 0, len(grantIDs))
	if strings.TrimSpace(message) == "" {
		message = reason
	}
	for i, id := range grantIDs {
		item, op := accessRevokeItem(r.Context(), svc, orgID, actor, grants, i+1, id, reason, message, false)
		if !preview && op.Type == "" && strings.TrimSpace(previewID) != "" && strings.TrimSpace(token) != "" {
			op = authz.BatchOperation{
				ID:     item.ID,
				Type:   "revoke_assignment",
				Revoke: authz.RevokeInput{AssignmentID: id, Reason: reason, Message: message},
			}
		}
		items = append(items, item)
		if op.Type != "" {
			operations = append(operations, op)
		}
	}
	if preview {
		if len(operations) > 0 {
			rp, err := svc.PreviewRevoke(r.Context(), authz.RevokePreviewRequest{
				ActorRef: actor, OrgID: orgID, Operations: operations, TTL: 5 * time.Minute,
			})
			if err != nil {
				writeAuthorizationError(w, authz.AccessDecision{SubjectRef: actor, Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
				return
			}
			allowed := map[string]authz.OperationResult{}
			for _, op := range rp.Operations {
				allowed[op.AssignmentID] = op
			}
			for i := range items {
				if op, ok := allowed[items[i].GrantID]; ok {
					items[i].Status = "allowed"
					items[i].Reason = "grant can be revoked by unified authorization API"
					items[i].EvidenceRef = "authorization_revoke_preview:" + rp.PreviewID
					items[i].GrantID = op.AssignmentID
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"preview_id": rp.PreviewID,
				"token":      rp.Token,
				"expires_at": rp.ExpiresAt.UTC().Format(time.RFC3339),
				"items":      items,
				"summary":    accessPreviewSummary(items),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"preview_id": fmt.Sprintf("access-revoke-preview-%s", accessHash(fmt.Sprintf("%s|%s|%v", actor, orgID, grantIDs))),
			"expires_at": time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339),
			"items":      items,
			"summary":    accessPreviewSummary(items),
		})
		return
	}
	if strings.TrimSpace(previewID) == "" || strings.TrimSpace(token) == "" {
		writeError(w, http.StatusBadRequest, "revoke_preview_required", "preview_id and token are required")
		return
	}
	res, err := svc.ConfirmRevoke(r.Context(), authz.RevokeConfirmRequest{
		PreviewID: previewID, Token: token, IdempotencyKey: idempotencyKey, ActorRef: actor, OrgID: orgID, Operations: operations,
	})
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: actor, Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
		return
	}
	confirmed := map[string]authz.OperationResult{}
	for _, op := range res.Operations {
		confirmed[op.AssignmentID] = op
	}
	for i := range items {
		if op, ok := confirmed[items[i].GrantID]; ok {
			items[i].Status = "allowed"
			items[i].Reason = "grant revoked by unified authorization API"
			items[i].EvidenceRef = "authorization_revoke_confirm:" + previewID
			items[i].GrantID = op.AssignmentID
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"operation_id": fmt.Sprintf("access-revoke-%s", accessHash(fmt.Sprintf("%s|%s|%v", actor, orgID, grantIDs))),
		"applied_at":   time.Now().UTC().Format(time.RFC3339),
		"items":        items,
		"summary":      accessResultSummary(items),
	})
}

func accessRevokeItem(ctx context.Context, svc *authz.Service, orgID string, actor authz.SubjectRef, grants map[string]accessGrantDTO, idx int, id, reason string, message string, apply bool) (accessBatchItemDTO, authz.BatchOperation) {
	grant, found := grants[id]
	item := accessBatchItemDTO{
		ID:          fmt.Sprintf("revoke-%d", idx),
		SubjectRef:  id,
		SubjectName: id,
		Permission:  "unknown",
		Resource:    accessResourceScopeDTO{Kind: "org", ID: orgID, OrgID: orgID, Label: orgID},
		Status:      "not_applicable",
		Risk:        "medium",
		Reason:      "grant is not present in the current effective access projection",
		GrantID:     id,
	}
	if !found {
		return item, authz.BatchOperation{}
	}
	item.SubjectRef = grant.SubjectRef
	item.SubjectName = grant.SubjectName
	item.Permission = grant.Permission
	item.Resource = grant.Resource
	item.Risk = grant.Risk
	item.HighRisk = grant.Risk == "high"
	item.Reason = fmt.Sprintf("%s is a derived permission and must be revoked at its source", id)
	if grant.Source != string(authz.SourceCustomRole) {
		return item, authz.BatchOperation{}
	}
	op := authz.BatchOperation{
		ID:     item.ID,
		Type:   "revoke_assignment",
		Revoke: authz.RevokeInput{AssignmentID: id, Reason: reason, Message: message},
	}
	if !apply {
		return item, op
	}
	req := authz.BatchRequest{
		IdempotencyKey: accessBatchIdempotencyKey("revoke", orgID, actor, "", item),
		ActorRef:       actor,
		OrgID:          orgID,
		Operations:     []authz.BatchOperation{op},
	}
	res, err := svc.RevokeBatch(ctx, req)
	if err != nil {
		item.Status = accessBatchStatusForError(err)
		item.Reason = err.Error()
		return item, op
	}
	for _, result := range res.Operations {
		if result.Reason != "" || result.Status == "denied" {
			item.Status = accessBatchStatusForReason(result.Reason)
			item.Reason = fallback(result.Reason, result.Status)
			return item, op
		}
	}
	item.Status = "allowed"
	item.Reason = "grant revoked by unified authorization API"
	item.EvidenceRef = "authorization_batch:" + req.IdempotencyKey
	return item, op
}

func accessPermissionApplies(def accessPermissionDefinitionDTO, kind string) bool {
	for _, k := range def.ResourceKinds {
		if k == kind {
			return true
		}
	}
	return false
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

func accessGrantID(subjectRef, permission string, resource accessResourceScopeDTO, source string) string {
	return strings.Join([]string{"grant", source, subjectRef, permission, resource.Kind, resource.ID}, ":")
}

func resourceKey(resource accessResourceScopeDTO) string {
	return resource.Kind + ":" + resource.ID
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func fallback(value, fb string) string {
	if value != "" {
		return value
	}
	return fb
}
