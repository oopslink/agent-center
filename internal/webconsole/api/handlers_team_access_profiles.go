package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

type teamRoleAssignmentCandidateReq struct {
	SubjectRef string `json:"subject_ref"`
	Role       string `json:"role"`
}

type instantiateTeamAccessReq struct {
	TemplateID       string                           `json:"template_id"`
	TeamName         string                           `json:"team_name"`
	Roles            []roleInputReq                   `json:"roles"`
	Assignments      []teamRoleAssignmentCandidateReq `json:"assignments"`
	PreviewRequestID string                           `json:"preview_request_id"`
	IdempotencyKey   string                           `json:"idempotency_key"`
}

func (s *Server) instantiateTeamPreviewHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req instantiateTeamAccessReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	roles, countByRole, ok := s.instantiateRolesFromRequest(w, r, d, orgID, req.TemplateID, req.Roles)
	if !ok {
		return
	}
	teamID := pendingTeamID(orgID, req.TemplateID, req.TeamName, roles)
	ops, err := s.teamAccessAssignmentOps(r.Context(), d, orgID, authz.UserSubject(caller.ID()), teamID, roles, req.Assignments)
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "org", ID: orgID}}, err)
		return
	}
	for i := range ops {
		ops[i].Type = strings.TrimSpace(ops[i].Type)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": "team-instantiate-preview-" + accessHash(orgID+"|"+req.TemplateID+"|"+req.TeamName+"|"+teamID.String()),
		"team": map[string]any{
			"id":               teamID.String(),
			"org_id":           orgID,
			"name":             req.TeamName,
			"roles":            rolePreviewViews(roles, countByRole),
			"access_lint":      team.LintRoleAccessRequirements(roles),
			"template_id":      req.TemplateID,
			"assignments_only": true,
		},
		"candidate_assignments": previewOperationResults(ops),
		"operations":            ops,
	})
}

func (s *Server) instantiateTeamApplyHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req instantiateTeamAccessReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.PreviewRequestID) == "" {
		writeError(w, http.StatusConflict, "preview_required", "apply requires a team instantiation preview_request_id")
		return
	}
	roles, countByRole, ok := s.instantiateRolesFromRequest(w, r, d, orgID, req.TemplateID, req.Roles)
	if !ok {
		return
	}
	teamID := pendingTeamID(orgID, req.TemplateID, req.TeamName, roles)
	expected := "team-instantiate-preview-" + accessHash(orgID+"|"+req.TemplateID+"|"+req.TeamName+"|"+teamID.String())
	if req.PreviewRequestID != expected {
		writeError(w, http.StatusConflict, "preview_stale", "preview_request_id does not match the current team instantiation request")
		return
	}
	t, err := d.TeamService.CreateTeam(r.Context(), teamservice.CreateTeamInput{
		ID: teamID, OrgID: orgID, Name: req.TeamName, Roles: roles,
	})
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	ops, err := s.teamAccessAssignmentOps(r.Context(), d, orgID, authz.UserSubject(caller.ID()), teamID, roles, req.Assignments)
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	if len(ops) > 0 {
		svc := permissionAuthorizer(d)
		if svc == nil {
			writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
			return
		}
		idem := strings.TrimSpace(req.IdempotencyKey)
		if idem == "" {
			idem = "team-instantiate-apply-" + accessHash(orgID+"|"+req.PreviewRequestID)
		}
		if _, err := svc.ApplyBatch(r.Context(), authz.BatchRequest{IdempotencyKey: idem, ActorRef: authz.UserSubject(caller.ID()), OrgID: orgID, Operations: ops}); err != nil {
			writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID}}, err)
			return
		}
	}
	if req.TemplateID != "" {
		s.teamTemplates.addInstance(orgID, req.TemplateID, string(t.ID()))
	}
	writeJSON(w, http.StatusCreated, withMemoryPermissions(instantiatedTeamView(t, countByRole), member.Role(), teamMemoryConfigured(d)))
}

func (s *Server) instantiateRolesFromRequest(w http.ResponseWriter, r *http.Request, d HandlerDeps, orgID, templateID string, input []roleInputReq) ([]team.RoleConfig, map[string]int, bool) {
	roles := input
	if len(roles) == 0 && templateID != "" {
		if st, found := s.teamTemplates.get(orgID, templateID); found {
			for _, sl := range st.tmpl.Roles {
				roles = append(roles, roleInputReq{Role: sl.Config.Role, CLI: sl.Config.CLI, Model: sl.Config.Model, MaxConcurrency: sl.Config.MaxConcurrency, Count: sl.Count, Tags: strings.Join(sl.Config.CapabilityTags, ","), AccessRequirements: sl.Config.AccessRequirements, AccessProfiles: sl.Config.AccessProfiles})
			}
		}
	}
	configs := make([]team.RoleConfig, 0, len(roles))
	countByRole := make(map[string]int, len(roles))
	for _, ri := range roles {
		configs = append(configs, team.RoleConfig{Role: ri.Role, CLI: ri.CLI, Model: ri.Model, CapabilityTags: splitTags(ri.Tags), MaxConcurrency: ri.MaxConcurrency, AccessRequirements: ri.AccessRequirements, AccessProfiles: ri.AccessProfiles})
		count := ri.Count
		if count <= 0 {
			count = 1
		}
		countByRole[ri.Role] = count
	}
	var valid bool
	configs, valid = s.validateTeamRuntimeRoles(w, r, d, orgID, configs)
	return configs, countByRole, valid
}

func pendingTeamID(orgID, templateID, name string, roles []team.RoleConfig) team.TeamID {
	body, _ := json.Marshal(roles)
	return team.TeamID("team-" + accessHash(orgID + "|" + templateID + "|" + strings.TrimSpace(name) + "|" + string(body))[:8])
}

func rolePreviewViews(roles []team.RoleConfig, countByRole map[string]int) []map[string]any {
	out := make([]map[string]any, 0, len(roles))
	for _, rc := range roles {
		out = append(out, roleViewMap(rc, countByRole[rc.Role]))
	}
	return out
}

func previewOperationResults(ops []authz.BatchOperation) []map[string]any {
	out := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		status := "would_" + strings.TrimPrefix(op.Type, "upsert_")
		if op.Type == "assign_role" {
			status = "would_create"
		}
		out = append(out, map[string]any{"id": op.ID, "type": op.Type, "status": status, "role_id": op.Assignment.RoleID, "subject_ref": op.Assignment.SubjectRef})
	}
	return out
}

func (s *Server) teamAccessAssignmentOps(ctx context.Context, d HandlerDeps, orgID string, actor authz.SubjectRef, teamID team.TeamID, roles []team.RoleConfig, candidates []teamRoleAssignmentCandidateReq) ([]authz.BatchOperation, error) {
	if d.DB == nil {
		return nil, nil
	}
	roleByName := map[string]team.RoleConfig{}
	for _, rc := range roles {
		roleByName[rc.Role] = rc
	}
	var ops []authz.BatchOperation
	for _, c := range candidates {
		rc, ok := roleByName[strings.TrimSpace(c.Role)]
		if !ok {
			return nil, fmt.Errorf("%w: assignment candidate role not declared", team.ErrRoleNotDeclared)
		}
		for _, ref := range rc.AccessProfiles {
			perms, err := accessProfilePermissions(ctx, d, orgID, ref)
			if err != nil {
				return nil, err
			}
			roleID := "role-profile-" + accessHash(orgID+"|"+ref.ProfileID+"|"+fmt.Sprint(ref.Version)+"|"+string(ref.Mode))
			ops = append(ops,
				authz.BatchOperation{ID: "profile-role-" + roleID, Type: "upsert_role", Role: authz.RoleInput{ID: roleID, Name: "Profile " + ref.ProfileID, Description: "Versioned access profile " + ref.ProfileID}},
				authz.BatchOperation{ID: "profile-permissions-" + roleID, Type: "set_role_permissions", Role: authz.RoleInput{ID: roleID}, Permissions: perms},
				authz.BatchOperation{ID: "assign-" + accessHash(string(c.SubjectRef)+"|"+roleID+"|"+teamID.String()), Type: "assign_role", Assignment: authz.AssignmentInput{SubjectRef: authz.SubjectRef(c.SubjectRef), RoleID: roleID, Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID}}},
			)
		}
	}
	return ops, nil
}

func accessProfilePermissions(ctx context.Context, d HandlerDeps, orgID string, ref team.AccessProfileRef) ([]authz.RolePermissionInput, error) {
	var raw string
	err := d.DB.QueryRowContext(ctx, `SELECT permissions_json FROM access_profile_versions WHERE org_id = ? AND profile_id = ? AND version = ?`, orgID, ref.ProfileID, ref.Version).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var perms []authz.RolePermissionInput
	if err := json.Unmarshal([]byte(raw), &perms); err != nil {
		return nil, err
	}
	for _, perm := range perms {
		if membershipDerivedPermission(perm.PermissionKey) {
			return nil, fmt.Errorf("%w: %s is membership-derived and cannot be copied into a custom assignment", authz.ErrInvalid, perm.PermissionKey)
		}
	}
	return perms, nil
}

func membershipDerivedPermission(key authz.PermissionKey) bool {
	switch key {
	case "org.read", "org.member.list", "org.work_items.read",
		"project.read", "project.write", "project.member.add", "project.member.remove",
		"team.read", "team.write", "team.member.manage", "team.project.link.manage",
		"team.memory.read", "team.memory.propose", "team.git.read", "team.git.write":
		return true
	default:
		return false
	}
}

func (s *Server) handleTeamRevokeBeforeDestructiveChange(w http.ResponseWriter, r *http.Request, d HandlerDeps, orgID string, teamID team.TeamID, subjectRef string) (handled bool, ok bool) {
	ops, err := activeTeamRevokeOps(r.Context(), d, orgID, teamID, subjectRef)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "revoke_preview_failed", err.Error())
		return true, false
	}
	if len(ops) == 0 {
		return false, true
	}
	previewID := "team-revoke-preview-" + accessHash(orgID+"|"+teamID.String()+"|"+subjectRef)
	if r.URL.Query().Get("preview") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{"request_id": previewID, "operations": previewRevokeResults(ops), "destructive_change_blocked": true})
		return true, true
	}
	if r.URL.Query().Get("preview_request_id") != previewID {
		writeError(w, http.StatusConflict, "revoke_preview_required", "custom team-scoped assignments require revoke preview before this destructive change")
		return true, false
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return true, false
	}
	caller, _, _, authOK := requireOrgMember(w, r, d)
	if !authOK {
		return true, false
	}
	idem := r.URL.Query().Get("idempotency_key")
	if idem == "" {
		idem = "team-revoke-apply-" + accessHash(previewID)
	}
	if _, err := svc.ApplyBatch(r.Context(), authz.BatchRequest{IdempotencyKey: idem, ActorRef: authz.UserSubject(caller.ID()), OrgID: orgID, Operations: ops}); err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID}}, err)
		return true, false
	}
	return false, true
}

func activeTeamRevokeOps(ctx context.Context, d HandlerDeps, orgID string, teamID team.TeamID, subjectRef string) ([]authz.BatchOperation, error) {
	if d.DB == nil {
		return nil, nil
	}
	query := `SELECT id FROM authorization_role_assignments WHERE org_id = ? AND resource_kind = 'team' AND resource_id = ? AND revoked_at IS NULL`
	args := []any{orgID, teamID.String()}
	if subjectRef != "" {
		query += ` AND subject_ref = ?`
		args = append(args, subjectRef)
	}
	query += ` ORDER BY created_at, id`
	rows, err := d.DB.QueryContext(ctx, query, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var ops []authz.BatchOperation
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ops = append(ops, authz.BatchOperation{ID: "revoke-" + id, Type: "revoke_assignment", Revoke: authz.RevokeInput{AssignmentID: id, Reason: "team destructive change confirmed after revoke preview"}})
	}
	return ops, rows.Err()
}

func previewRevokeResults(ops []authz.BatchOperation) []map[string]any {
	out := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		out = append(out, map[string]any{"id": op.ID, "type": op.Type, "status": "would_revoke", "assignment_id": op.Revoke.AssignmentID, "reason": op.Revoke.Reason})
	}
	return out
}
