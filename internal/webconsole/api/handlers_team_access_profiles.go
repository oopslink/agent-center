package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/persistence"
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
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	batch := authz.BatchRequest{ActorRef: authz.UserSubject(caller.ID()), OrgID: orgID, Operations: ops}
	preview, err := svc.PreviewBatch(r.Context(), batch)
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID}}, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": preview.PreviewID,
		"expires_at": preview.ExpiresAt,
		"team": map[string]any{
			"id":               teamID.String(),
			"org_id":           orgID,
			"name":             req.TeamName,
			"roles":            rolePreviewViews(roles, countByRole),
			"access_lint":      team.LintRoleAccessRequirements(roles),
			"template_id":      req.TemplateID,
			"assignments_only": true,
		},
		"candidate_assignments": previewAuthzResults(preview.Operations),
		"operations":            preview.Operations,
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
	ops, err := s.teamAccessAssignmentOps(r.Context(), d, orgID, authz.UserSubject(caller.ID()), teamID, roles, req.Assignments)
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	svc := permissionAuthorizer(d)
	if svc == nil {
		writeError(w, http.StatusNotImplemented, "authorization_not_wired", "authorization service not wired")
		return
	}
	currentBatch := authz.BatchRequest{ActorRef: authz.UserSubject(caller.ID()), OrgID: orgID, Operations: ops}
	storedPreview, err := teamStoredPreview(r.Context(), d, req.PreviewRequestID)
	if err != nil {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID}}, err)
		return
	}
	if !sameTeamPreviewBatch(storedPreview.Batch, currentBatch) {
		writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID}}, authz.ErrPreviewStale)
		return
	}
	idem := strings.TrimSpace(req.IdempotencyKey)
	if idem == "" {
		idem = "team-instantiate-apply-" + accessHash(orgID+"|"+req.PreviewRequestID)
	}
	var t *team.Team
	err = persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		applyReq := storedPreview.Batch
		applyReq.IdempotencyKey = idem
		applyReq.PreviewID = req.PreviewRequestID
		if _, err := svc.ApplyBatch(txCtx, applyReq); err != nil {
			return err
		}
		if err := markTeamPreviewApplied(txCtx, d, req.PreviewRequestID, idem); err != nil {
			return err
		}
		created, err := d.TeamService.CreateTeam(txCtx, teamservice.CreateTeamInput{
			ID: teamID, OrgID: orgID, Name: req.TeamName, Roles: roles,
		})
		if err != nil {
			return err
		}
		t = created
		return nil
	})
	if err != nil {
		if isAuthorizationApplyError(err) {
			writeAuthorizationError(w, authz.AccessDecision{SubjectRef: authz.UserSubject(caller.ID()), Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID}}, err)
			return
		}
		mapTeamWebError(w, err)
		return
	}
	if req.TemplateID != "" {
		s.teamTemplates.addInstance(orgID, req.TemplateID, string(t.ID()))
	}
	writeJSON(w, http.StatusCreated, withMemoryPermissions(instantiatedTeamView(t, countByRole), member.Role(), teamMemoryConfigured(d)))
}

func isAuthorizationApplyError(err error) bool {
	return errors.Is(err, authz.ErrDenied) ||
		errors.Is(err, authz.ErrUnauthenticated) ||
		errors.Is(err, authz.ErrNotFound) ||
		errors.Is(err, authz.ErrInvalid) ||
		errors.Is(err, authz.ErrConflict) ||
		errors.Is(err, authz.ErrNotDelegatable) ||
		errors.Is(err, authz.ErrPermissionUndefined) ||
		errors.Is(err, authz.ErrRoleNotFound) ||
		errors.Is(err, authz.ErrAssignmentNotFound) ||
		errors.Is(err, authz.ErrSystemRoleImmutable) ||
		errors.Is(err, authz.ErrIdempotencyRequired) ||
		errors.Is(err, authz.ErrIdempotencyConflict) ||
		errors.Is(err, authz.ErrPreviewRequired) ||
		errors.Is(err, authz.ErrPreviewNotFound) ||
		errors.Is(err, authz.ErrPreviewExpired) ||
		errors.Is(err, authz.ErrPreviewStale) ||
		errors.Is(err, authz.ErrPreviewConsumed)
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

func previewAuthzResults(results []authz.OperationResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, op := range results {
		out = append(out, map[string]any{"id": op.ID, "type": op.Type, "status": op.Status, "role_id": op.RoleID, "assignment_id": op.AssignmentID, "reason": op.Reason})
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
	type resolvedRole struct {
		roleID string
		perms  []authz.RolePermissionInput
		refs   []team.AccessProfileRef
	}
	resolvedByRole := map[string]resolvedRole{}
	for _, rc := range roles {
		if len(rc.AccessProfiles) == 0 {
			continue
		}
		perms, refs, err := resolveRoleAccessProfilePermissions(ctx, d, orgID, rc.AccessProfiles)
		if err != nil {
			return nil, err
		}
		body, _ := json.Marshal(struct {
			TeamID string                      `json:"team_id"`
			Role   string                      `json:"role"`
			Refs   []team.AccessProfileRef     `json:"refs"`
			Perms  []authz.RolePermissionInput `json:"permissions"`
		}{TeamID: teamID.String(), Role: rc.Role, Refs: refs, Perms: perms})
		roleID := "role-team-profile-" + accessHash(string(body))
		resolvedByRole[rc.Role] = resolvedRole{roleID: roleID, perms: perms, refs: refs}
		ops = append(ops,
			authz.BatchOperation{ID: "profile-role-" + accessHash(teamID.String()+"|"+rc.Role), Type: "upsert_role", Role: authz.RoleInput{ID: roleID, Name: "Team " + teamID.String() + " " + rc.Role, Description: "Profile-backed team role " + string(mustJSON(refs))}},
			authz.BatchOperation{ID: "profile-permissions-" + accessHash(teamID.String()+"|"+rc.Role), Type: "set_role_permissions", Role: authz.RoleInput{ID: roleID}, Permissions: perms},
		)
	}
	for _, c := range candidates {
		rc, ok := roleByName[strings.TrimSpace(c.Role)]
		if !ok {
			return nil, fmt.Errorf("%w: assignment candidate role not declared", team.ErrRoleNotDeclared)
		}
		resolved, ok := resolvedByRole[rc.Role]
		if !ok {
			continue
		}
		ops = append(ops, authz.BatchOperation{ID: "assign-" + accessHash(string(c.SubjectRef)+"|"+resolved.roleID+"|"+teamID.String()), Type: "assign_role", Assignment: authz.AssignmentInput{SubjectRef: authz.SubjectRef(c.SubjectRef), RoleID: resolved.roleID, Resource: authz.ResourceScope{Kind: "team", ID: teamID.String(), OrgID: orgID, OwnerRef: pendingTeamInstantiationOwnerRef(orgID, teamID)}}})
	}
	return ops, nil
}

func pendingTeamInstantiationOwnerRef(orgID string, teamID team.TeamID) string {
	return "pending_team_instantiation:" + orgID + ":" + teamID.String()
}

func resolveRoleAccessProfilePermissions(ctx context.Context, d HandlerDeps, orgID string, refs []team.AccessProfileRef) ([]authz.RolePermissionInput, []team.AccessProfileRef, error) {
	permsByKey := map[string]authz.RolePermissionInput{}
	var normalizedRefs []team.AccessProfileRef
	overrideSeen := false
	for _, ref := range refs {
		ref.ProfileID = strings.TrimSpace(ref.ProfileID)
		if ref.Mode == "" {
			ref.Mode = team.AccessProfileDefault
		}
		if ref.ProfileID == "" || ref.Version <= 0 {
			return nil, nil, team.ErrInvalidAccessProfileRef
		}
		switch ref.Mode {
		case team.AccessProfileDefault, team.AccessProfileAdditional:
		case team.AccessProfileOverride:
			if overrideSeen {
				return nil, nil, team.ErrInvalidAccessProfileRef
			}
			overrideSeen = true
			permsByKey = map[string]authz.RolePermissionInput{}
		default:
			return nil, nil, team.ErrInvalidAccessProfileRef
		}
		perms, err := accessProfilePermissions(ctx, d, orgID, ref)
		if err != nil {
			return nil, nil, err
		}
		for _, perm := range perms {
			permsByKey[string(perm.PermissionKey)+"\x00"+perm.ResourceKind] = perm
		}
		normalizedRefs = append(normalizedRefs, ref)
	}
	perms := make([]authz.RolePermissionInput, 0, len(permsByKey))
	for _, perm := range permsByKey {
		perms = append(perms, perm)
	}
	sort.Slice(perms, func(i, j int) bool {
		left := string(perms[i].PermissionKey) + "\x00" + perms[i].ResourceKind
		right := string(perms[j].PermissionKey) + "\x00" + perms[j].ResourceKind
		return left < right
	})
	return perms, normalizedRefs, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

type teamPreviewRecord struct {
	Batch authz.BatchRequest
}

func teamStoredPreview(ctx context.Context, d HandlerDeps, previewID string) (teamPreviewRecord, error) {
	if strings.TrimSpace(previewID) == "" {
		return teamPreviewRecord{}, authz.ErrPreviewRequired
	}
	var raw, status, expires string
	err := d.DB.QueryRowContext(ctx, `SELECT normalized_request_json, status, expires_at FROM authorization_preview_records WHERE preview_id = ?`, strings.TrimSpace(previewID)).Scan(&raw, &status, &expires)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return teamPreviewRecord{}, authz.ErrPreviewNotFound
		}
		return teamPreviewRecord{}, err
	}
	if status != "pending" {
		return teamPreviewRecord{}, authz.ErrPreviewConsumed
	}
	if expires != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, expires)
		if err == nil && time.Now().UTC().After(expiresAt) {
			return teamPreviewRecord{}, authz.ErrPreviewExpired
		}
	}
	var req authz.BatchRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return teamPreviewRecord{}, err
	}
	return teamPreviewRecord{Batch: req}, nil
}

func markTeamPreviewApplied(ctx context.Context, d HandlerDeps, previewID, idempotencyKey string) error {
	exec, err := persistence.ExecutorFromCtx(ctx, d.DB)
	if err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx, `UPDATE authorization_preview_records
		SET status = 'applied', applied_at = ?, apply_idempotency_key = ?
		WHERE preview_id = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339Nano), strings.TrimSpace(idempotencyKey), strings.TrimSpace(previewID))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return authz.ErrPreviewConsumed
	}
	return nil
}

func sameTeamPreviewBatch(stored, current authz.BatchRequest) bool {
	stored.IdempotencyKey = ""
	stored.PreviewID = ""
	current.IdempotencyKey = ""
	current.PreviewID = ""
	stored.ActorRef = authz.SubjectRef(strings.TrimSpace(string(stored.ActorRef)))
	current.ActorRef = authz.SubjectRef(strings.TrimSpace(string(current.ActorRef)))
	stored.OrgID = strings.TrimSpace(stored.OrgID)
	current.OrgID = strings.TrimSpace(current.OrgID)
	return reflect.DeepEqual(stored, current)
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
	previewID := "team-revoke-preview-" + accessHash(orgID+"|"+teamID.String()+"|"+subjectRef)
	if r.URL.Query().Get("preview") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{"request_id": previewID, "operations": previewRevokeResults(ops), "destructive_change_blocked": true})
		return true, true
	}
	if len(ops) == 0 {
		return false, true
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
	exec, err := persistence.ExecutorFromCtx(ctx, d.DB)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, query, args...)
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
