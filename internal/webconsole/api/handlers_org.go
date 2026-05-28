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
		if !o.IsDeleted() {
			arr = append(arr, orgPublicMap(o))
		}
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

func orgPublicMap(o *identity.Organization) map[string]any {
	return map[string]any{
		"id":          o.ID(),
		"slug":        o.Slug(),
		"name":        o.Name(),
		"created_at":  o.CreatedAt().Format("2006-01-02T15:04:05Z"),
	}
}
