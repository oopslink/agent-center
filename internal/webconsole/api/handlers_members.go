package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/workforce"
)

// resolveCallerAndOrg is the strict org-scope resolver for member endpoints.
// It delegates to requireOrgMember (v2.6 X1 §1): NO first-org fallback. Missing
// or unknown org scope → 400; non-member → 403; unauthenticated → 401. On
// failure the error response is already written and ok=false is returned.
func (s *Server) resolveCallerAndOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps) (callerIdentity *identity.Identity, callerMember *identity.Member, orgID string, ok bool) {
	return requireOrgMember(w, r, d)
}

// listMembersHandler handles GET /api/members?org_slug=|org_id=.
func (s *Server) listMembersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	members, err := d.MemberRepo.ListByOrganization(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	// v2.6 X1 §4: enrich agent members with "running on {worker}". Build an
	// identity_id → worker_id map from the org's AgentInstances (an agent
	// member may not yet have a bound AgentInstance — then worker is empty).
	agentWorker := map[string]string{}
	if d.AgentInstanceRepo != nil {
		instances, aerr := d.AgentInstanceRepo.FindAll(r.Context(), workforce.AgentInstanceFilter{OrganizationID: orgID})
		if aerr == nil {
			for _, ai := range instances {
				if ai.IdentityID() != "" && ai.WorkerID() != nil {
					agentWorker[ai.IdentityID()] = string(*ai.WorkerID())
				}
			}
		}
	}
	arr := make([]map[string]any, 0, len(members))
	for _, m := range members {
		row := memberPublicMap(m)
		if wid, ok := agentWorker[m.IdentityID()]; ok {
			row["worker_id"] = wid
		}
		arr = append(arr, row)
	}
	writeJSON(w, http.StatusOK, arr)
}

// addMemberHandler handles POST /api/members[?org_id=].
// Creates a NEW user identity (with temp passcode) + member row, OR adds an
// existing identity by display_name when ?reuse=true. Default behavior is
// "create new user" per v2.6 acceptance plan §3.
// Requires owner or admin role.
func (s *Server) addMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberCreateUserSvc == nil && d.MemberAddSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member add not configured")
		return
	}
	callerID, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can add members")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Reuse       bool   `json:"reuse"` // when true, add existing identity instead of creating new
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if body.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name_required", "display_name is required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if string(callerMember.Role()) == "admin" && body.Role == "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "admin cannot add owner-role members")
		return
	}

	// Path A: reuse existing identity (backwards-compat).
	if body.Reuse {
		if d.MemberAddSvc == nil {
			writeError(w, http.StatusNotImplemented, "not_configured", "reuse path not configured")
			return
		}
		member, err := d.MemberAddSvc.Add(r.Context(), orgID, body.DisplayName, body.Role, callerID.ID())
		if err != nil {
			if errors.Is(err, identity.ErrIdentityNotFound) {
				writeError(w, http.StatusNotFound, "identity_not_found", "identity not found")
				return
			}
			if errors.Is(err, identity.ErrMemberAlreadyExists) {
				writeError(w, http.StatusConflict, "already_member", "identity is already a member")
				return
			}
			writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, memberPublicMap(member))
		return
	}

	// Path B (default v2.6 §3): create new user identity + member with temp passcode.
	res, err := d.MemberCreateUserSvc.Create(r.Context(), orgID, body.DisplayName, body.Role, callerID.ID())
	if err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	result := memberPublicMap(res.Member)
	result["temp_passcode"] = res.TempPasscode // returned ONCE; UI must show then never echo again
	result["display_name"] = res.Identity.DisplayName()
	writeJSON(w, http.StatusCreated, result)
}

// addAgentMemberHandler handles POST /api/members/agent[?org_id=].
// Creates a new agent identity + member.
func (s *Server) addAgentMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentProvisionSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "agent provision not configured")
		return
	}
	callerID, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can add agents")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Role        string `json:"role"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if body.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name_required", "display_name is required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	// v2.6 ship-block fix (X1 §3): admin cannot create owner-role agent.
	if string(callerMember.Role()) == "admin" && body.Role == "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "admin cannot add owner-role agents")
		return
	}
	res, err := d.AgentProvisionSvc.Provision(r.Context(),
		identity.AgentProvisionForm{
			DisplayName: body.DisplayName,
			Description: body.Description,
			Role:        identity.MemberRole(body.Role),
		},
		orgID, callerID.ID())
	if err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	result := memberPublicMap(res.Member)
	result["display_name"] = res.Identity.DisplayName()
	writeJSON(w, http.StatusCreated, result)
}

// requireTargetMemberInOrg validates that memberID exists and belongs to orgID.
// Returns the target Member on success; on failure writes the error response.
// Prevents cross-org member mutations (PM X1 §2 ship-block).
func (s *Server) requireTargetMemberInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, memberID, orgID string) (*identity.Member, bool) {
	target, err := d.MemberRepo.GetByID(r.Context(), memberID)
	if err != nil || target == nil {
		writeError(w, http.StatusNotFound, "member_not_found", "target member not found")
		return nil, false
	}
	if target.OrganizationID() != orgID {
		writeError(w, http.StatusForbidden, "forbidden", "target member is not in this organization")
		return nil, false
	}
	return target, true
}

// changeMemberRoleHandler handles PATCH /api/members/{id}/role[?org_id=].
// Requires owner role (only owners can change roles to prevent privilege escalation).
func (s *Server) changeMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberRoleChangeSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "role change not configured")
		return
	}
	callerID, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "only owners can change member roles")
		return
	}
	memberID := r.PathValue("id")
	// v2.6 ship-block fix (X1 §2): verify target member is in the same org as the caller's resolved scope.
	if _, ok := s.requireTargetMemberInOrg(w, r, d, memberID, orgID); !ok {
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := d.MemberRoleChangeSvc.Change(r.Context(), memberID, identity.MemberRole(body.Role), callerID.ID()); err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// disableMemberHandler handles POST /api/members/{id}/disable[?org_id=].
// Requires owner or admin role.
func (s *Server) disableMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberDisableSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member disable not configured")
		return
	}
	_, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can disable members")
		return
	}
	memberID := r.PathValue("id")
	if _, ok := s.requireTargetMemberInOrg(w, r, d, memberID, orgID); !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &body)
	if err := d.MemberDisableSvc.Disable(r.Context(), memberID, body.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, "disable_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reEnableMemberHandler handles POST /api/members/{id}/reenable[?org_id=].
// Requires owner or admin role.
func (s *Server) reEnableMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberDisableSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member reenable not configured")
		return
	}
	_, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can re-enable members")
		return
	}
	memberID := r.PathValue("id")
	if _, ok := s.requireTargetMemberInOrg(w, r, d, memberID, orgID); !ok {
		return
	}
	if err := d.MemberDisableSvc.ReEnable(r.Context(), memberID); err != nil {
		writeError(w, http.StatusInternalServerError, "reenable_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func memberPublicMap(m *identity.Member) map[string]any {
	// Infer kind from identity_id prefix ("user-" or "agent-").
	kind := "user"
	if len(m.IdentityID()) >= 6 && m.IdentityID()[:6] == "agent-" {
		kind = "agent"
	}
	return map[string]any{
		"id":              m.ID(),
		"organization_id": m.OrganizationID(),
		"identity_id":     m.IdentityID(),
		"kind":            kind,
		"role":            string(m.Role()),
		"status":          string(m.Status()),
		"joined_at":       m.JoinedAt().Format("2006-01-02T15:04:05Z"),
	}
}
