package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/identity"
)

// resolveCallerAndOrg authenticates the request, resolves the target org
// (from ?org_id= query param or the caller's first org), and returns the
// caller's member record. Returns nil, nil, "" on success with empty orgID
// when the user has no orgs.
func resolveCallerAndOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps) (callerIdentity *identity.Identity, callerMember *identity.Member, orgID string) {
	if d.AuthSvc == nil || d.OrgRepo == nil || d.MemberRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member endpoints not configured")
		return nil, nil, ""
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return nil, nil, ""
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return nil, nil, ""
	}
	// Resolve org: prefer explicit ?org_id= param, otherwise first org.
	orgID = r.URL.Query().Get("org_id")
	if orgID == "" {
		orgs, err := d.OrgRepo.ListForIdentity(r.Context(), id.ID())
		if err != nil || len(orgs) == 0 {
			writeJSON(w, http.StatusOK, []any{})
			return nil, nil, ""
		}
		orgID = orgs[0].ID()
	}
	// Verify caller is a member of the org.
	member, err := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), orgID, id.ID())
	if err != nil || member == nil {
		writeError(w, http.StatusForbidden, "forbidden", "not a member of this organization")
		return nil, nil, ""
	}
	return id, member, orgID
}

// listMembersHandler handles GET /api/members[?org_id=].
func (s *Server) listMembersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID := resolveCallerAndOrg(w, r, d)
	if orgID == "" {
		return // error already written
	}
	members, err := d.MemberRepo.ListByOrganization(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	arr := make([]map[string]any, 0, len(members))
	for _, m := range members {
		arr = append(arr, memberPublicMap(m))
	}
	writeJSON(w, http.StatusOK, arr)
}

// addMemberHandler handles POST /api/members[?org_id=].
// Requires owner or admin role.
func (s *Server) addMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberAddSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member add not configured")
		return
	}
	callerID, callerMember, orgID := resolveCallerAndOrg(w, r, d)
	if orgID == "" {
		return
	}
	// RBAC: owner or admin only.
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can add members")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
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
	// Admin cannot add owners.
	if string(callerMember.Role()) == "admin" && body.Role == "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "admin cannot add owner-role members")
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
}

// changeMemberRoleHandler handles PATCH /api/members/{id}/role[?org_id=].
// Requires owner role (only owners can change roles to prevent privilege escalation).
func (s *Server) changeMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberRoleChangeSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "role change not configured")
		return
	}
	callerID, callerMember, orgID := resolveCallerAndOrg(w, r, d)
	if orgID == "" {
		return
	}
	_ = orgID
	if string(callerMember.Role()) != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "only owners can change member roles")
		return
	}
	memberID := r.PathValue("id")
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
	_, callerMember, orgID := resolveCallerAndOrg(w, r, d)
	if orgID == "" {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can disable members")
		return
	}
	memberID := r.PathValue("id")
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
	_, callerMember, orgID := resolveCallerAndOrg(w, r, d)
	if orgID == "" {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can re-enable members")
		return
	}
	memberID := r.PathValue("id")
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
