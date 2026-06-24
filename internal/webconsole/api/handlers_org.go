package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/identity"
)

// listOrgsHandler handles GET /api/orgs.
// Returns all active organizations the current identity is a member of.
func (s *Server) listOrgsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.OrgRepo == nil || d.AuthSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "org endpoints not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return
	}
	orgs, err := d.OrgRepo.ListForIdentity(r.Context(), id.ID())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	arr := make([]map[string]any, 0, len(orgs))
	for _, o := range orgs {
		if o.IsDeleted() {
			continue
		}
		// I41 (T470): a DISABLED org is visible only to its owner (so they can enter
		// + re-enable). Non-owner members can't use it (requireOrgMember blocks them),
		// so it is hidden from their org list rather than dangled as a dead entry.
		if o.IsDisabled() {
			member, merr := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), o.ID(), id.ID())
			if merr != nil || member == nil || member.Role() != identity.RoleOwner {
				continue
			}
		}
		arr = append(arr, orgPublicMap(o))
	}
	writeJSON(w, http.StatusOK, arr)
}

// createOrgHandler handles POST /api/orgs.
func (s *Server) createOrgHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.OrgCreateSvc == nil || d.AuthSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "org create not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return
	}
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "name_required", "name is required")
		return
	}
	org, _, err := d.OrgCreateSvc.Create(r.Context(), body.Slug, body.Name, id.ID())
	if err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, orgPublicMap(org))
}

// updateOrgHandler handles PATCH /api/orgs/{id}.
// Requires the caller to be an owner of the target organization.
// Currently supports updating name; slug/description are schema follow-ups.
func (s *Server) updateOrgHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.OrgUpdateSvc == nil || d.AuthSvc == nil || d.MemberRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "org update not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return
	}
	orgID := r.PathValue("id")
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "id_required", "org id required")
		return
	}
	member, err := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), orgID, id.ID())
	if err != nil || member == nil || string(member.Role()) != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "only org owners can update the organization")
		return
	}
	var body struct {
		Name        *string `json:"name,omitempty"`
		Slug        *string `json:"slug,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if body.Name != nil {
		if err := d.OrgUpdateSvc.UpdateName(r.Context(), orgID, *body.Name, id.ID()); err != nil {
			writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
			return
		}
	}
	if body.Slug != nil {
		if err := d.OrgUpdateSvc.UpdateSlug(r.Context(), orgID, *body.Slug, id.ID()); err != nil {
			writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
			return
		}
	}
	if body.Description != nil {
		if err := d.OrgUpdateSvc.UpdateDescription(r.Context(), orgID, *body.Description, id.ID()); err != nil {
			writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteOrgHandler handles DELETE /api/orgs/{id}.
// Requires the caller to be an owner member of the organization.
func (s *Server) deleteOrgHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.OrgLifecycleSvc == nil || d.AuthSvc == nil || d.MemberRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "org delete not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return
	}
	orgID := r.PathValue("id")
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "id_required", "org id required")
		return
	}
	// RBAC: caller must be an owner member of the org.
	member, err := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), orgID, id.ID())
	if err != nil || member == nil || string(member.Role()) != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "only org owners can delete the organization")
		return
	}
	if err := d.OrgLifecycleSvc.Delete(r.Context(), orgID); err != nil {
		if errors.Is(err, identity.ErrOrganizationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// disableOrgHandler handles POST /api/orgs/{id}/disable.
// Owner-only. Marks the org disabled (I41/T470): non-owner members are blocked
// from entering it (requireOrgMember gate) while the owner keeps full access.
func (s *Server) disableOrgHandler(w http.ResponseWriter, r *http.Request) {
	s.orgEnableDisable(w, r, true)
}

// enableOrgHandler handles POST /api/orgs/{id}/enable. Owner-only; reverses disable.
func (s *Server) enableOrgHandler(w http.ResponseWriter, r *http.Request) {
	s.orgEnableDisable(w, r, false)
}

// orgEnableDisable is the shared owner-gated disable/enable path (I41/T470).
func (s *Server) orgEnableDisable(w http.ResponseWriter, r *http.Request, disable bool) {
	d := hd(r)
	if d.OrgLifecycleSvc == nil || d.AuthSvc == nil || d.MemberRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "org disable not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return
	}
	orgID := r.PathValue("id")
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "id_required", "org id required")
		return
	}
	// RBAC: only an owner may toggle the org's disabled state.
	member, err := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), orgID, id.ID())
	if err != nil || member == nil || member.Role() != identity.RoleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "only org owners can disable or enable the organization")
		return
	}
	if disable {
		err = d.OrgLifecycleSvc.Disable(r.Context(), orgID, id.ID())
	} else {
		err = d.OrgLifecycleSvc.Enable(r.Context(), orgID, id.ID())
	}
	if err != nil {
		if errors.Is(err, identity.ErrOrganizationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		if errors.Is(err, identity.ErrOrganizationDeleted) {
			writeError(w, http.StatusConflict, "org_deleted", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "disable_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func orgPublicMap(o *identity.Organization) map[string]any {
	return map[string]any{
		"id":          o.ID(),
		"slug":        o.Slug(),
		"name":        o.Name(),
		"description": o.Description(),
		"created_at":  o.CreatedAt().Format("2006-01-02T15:04:05Z"),
		// I41 (T470): the owner's Org Settings reads this to render the Danger Zone
		// toggle (Disable ↔ Enable). false for every enabled org.
		"disabled": o.IsDisabled(),
	}
}
