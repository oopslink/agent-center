package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

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

func (s *Server) accessEffectiveHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	state, err := s.accessDerivedState(r.Context(), d, orgID)
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

func (s *Server) accessBatchPreviewHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, callerMember, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	state, err := s.accessDerivedState(r.Context(), d, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	var body accessBatchRequestDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	items := accessBatchItems(body, state, callerMember, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": fmt.Sprintf("access-preview-%d", time.Now().UTC().UnixNano()),
		"expires_at": body.ExpiresAt,
		"items":      items,
		"summary":    accessPreviewSummary(items),
	})
}

func (s *Server) accessBatchApplyHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, callerMember, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	state, err := s.accessDerivedState(r.Context(), d, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	var body accessBatchRequestDTO
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	items := accessBatchItems(body, state, callerMember, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"operation_id": fmt.Sprintf("access-apply-%d", time.Now().UTC().UnixNano()),
		"applied_at":   time.Now().UTC().Format(time.RFC3339),
		"items":        items,
		"summary":      accessResultSummary(items),
	})
}

func (s *Server) accessBulkRevokeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, callerMember, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	state, err := s.accessDerivedState(r.Context(), d, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_projection_failed", err.Error())
		return
	}
	var body struct {
		GrantIDs []string `json:"grant_ids"`
		Reason   string   `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	grants := make(map[string]accessGrantDTO, len(state.grants))
	for _, grant := range state.grants {
		grants[grant.ID] = grant
	}
	items := make([]accessBatchItemDTO, 0, len(body.GrantIDs))
	for i, id := range body.GrantIDs {
		grant, found := grants[id]
		item := accessBatchItemDTO{
			ID:          fmt.Sprintf("revoke-%d", i+1),
			SubjectRef:  id,
			SubjectName: id,
			Permission:  "unknown",
			Resource:    accessResourceScopeDTO{Kind: "org", ID: orgID, OrgID: orgID, Label: orgID},
			Status:      "not_applicable",
			Risk:        "medium",
			Reason:      "grant is not present in the current effective access projection",
			GrantID:     id,
		}
		if found {
			item.SubjectRef = grant.SubjectRef
			item.SubjectName = grant.SubjectName
			item.Permission = grant.Permission
			item.Resource = grant.Resource
			item.Risk = grant.Risk
			item.HighRisk = grant.Risk == "high"
			item.Reason = fmt.Sprintf("%s is derived from %s and must be revoked at its source", id, grant.Source)
			if callerMember.Role().AtLeast(identity.RoleAdmin) && grant.Source == "system" {
				item.Status = "denied"
				item.Reason = "system grants cannot be revoked from Access"
			}
		}
		if !callerMember.Role().AtLeast(identity.RoleAdmin) {
			item.Status = "unauthorized"
			item.Reason = "only owner or admin can revoke access grants"
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"operation_id": fmt.Sprintf("access-revoke-%d", time.Now().UTC().UnixNano()),
		"applied_at":   time.Now().UTC().Format(time.RFC3339),
		"items":        items,
		"summary":      accessResultSummary(items),
	})
}

func (s *Server) accessRoleUpdateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, callerMember, _, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "only owner or admin can manage access roles")
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
	writeError(w, http.StatusNotFound, "not_found", "access role not found")
}

func (s *Server) accessDerivedState(ctx context.Context, d HandlerDeps, orgID string) (accessDerivedState, error) {
	now := time.Now().UTC()
	catalog := append([]accessPermissionDefinitionDTO(nil), accessCatalog...)
	catalogByKey := make(map[string]accessPermissionDefinitionDTO, len(catalog))
	for _, def := range catalog {
		catalogByKey[def.Key] = def
	}
	state := accessDerivedState{
		generatedAt:  now,
		roles:        accessRoles(),
		catalog:      catalog,
		catalogByKey: catalogByKey,
		subjectByRef: map[string]accessSubjectDTO{},
	}
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
			CreatedBy:   "system",
			CreatedAt:   now.Format(time.RFC3339),
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

func accessBatchItems(body accessBatchRequestDTO, state accessDerivedState, callerMember *identity.Member, applied bool) []accessBatchItemDTO {
	items := []accessBatchItemDTO{}
	for _, subjectRef := range body.SubjectRefs {
		for _, permission := range body.PermissionKeys {
			for _, resource := range body.Resources {
				items = append(items, accessEvaluateBatchItem(len(items)+1, subjectRef, permission, resource, state, callerMember, applied))
			}
		}
	}
	return items
}

func accessEvaluateBatchItem(idx int, subjectRef, permission string, resource accessResourceScopeDTO, state accessDerivedState, callerMember *identity.Member, applied bool) accessBatchItemDTO {
	subj, subjectFound := state.subjectByRef[subjectRef]
	def, permissionFound := state.catalogByKey[permission]
	status := "allowed"
	reason := "grant can be applied by the permission API"
	evidence := "permission_preview:derived"
	if applied {
		reason = "grant accepted by the permission API"
		evidence = "permission_apply:derived"
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		status = "unauthorized"
		reason = "only owner or admin can batch authorize access"
		evidence = ""
	} else if !subjectFound || subj.Status != string(identity.MemberJoined) {
		status = "unauthorized"
		reason = "subject is unavailable or outside this organization"
		evidence = ""
	} else if !permissionFound {
		status = "denied"
		reason = "permission is not registered"
		evidence = "permission_registry:missing"
	} else if !accessPermissionApplies(def, resource.Kind) {
		status = "not_applicable"
		reason = fmt.Sprintf("%s does not apply to %s", permission, resource.Kind)
		evidence = "permission_registry:" + permission
	} else if permission == "org.member.role.manage" && subj.Kind == "agent" {
		status = "unauthorized"
		reason = "agents cannot receive organization role-management grants"
		evidence = ""
	}
	grantID := ""
	if status == "allowed" {
		grantID = accessGrantID(subjectRef, permission, resource, "system")
	}
	return accessBatchItemDTO{
		ID:          fmt.Sprintf("item-%d", idx),
		SubjectRef:  subjectRef,
		SubjectName: fallback(subj.Name, subjectRef),
		Permission:  permission,
		Resource:    resource,
		Status:      status,
		Risk:        fallback(def.Risk, "medium"),
		HighRisk:    def.Risk == "high",
		Reason:      reason,
		EvidenceRef: evidence,
		GrantID:     grantID,
	}
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
