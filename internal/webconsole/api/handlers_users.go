// handlers_users.go — v2.7.1 #214: user profile read surface (Humans list
// enrichment + the UserDetail page). The user-detail route keys on the member-id
// (user-<8hex>), stable across display-name renames.
package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/identity"
)

// addUserProfileFields enriches a row with the v2.7.1 #214 user fields. Nullable
// email / last_session_at are emitted as explicit JSON null (NOT "" or omitted) so
// the UI's null-handling is unambiguous (Tester seam contract). created_at is the
// account creation time (always present).
func addUserProfileFields(row map[string]any, ident *identity.Identity) {
	if e := ident.Email(); e != nil {
		row["email"] = *e
	} else {
		row["email"] = nil
	}
	row["created_at"] = ident.CreatedAt().Format(time.RFC3339Nano)
	if ls := ident.LastSessionAt(); ls != nil {
		row["last_session_at"] = ls.Format(time.RFC3339Nano)
	} else {
		row["last_session_at"] = nil
	}
}

// userDetailHandler handles GET /api/users/{user_id} (v2.7.1 #214). The path is the
// member-id (user-<8hex>). Returns the user profile + the orgs they belong to (with
// role per org). No activity stream (deferred to v2.8). Caller is authenticated by
// authMiddleware (all /api/* except /api/auth/*).
func (s *Server) userDetailHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IdentityRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "identity not configured")
		return
	}
	userID := r.PathValue("user_id")
	ident, err := d.IdentityRepo.GetByID(r.Context(), userID)
	if err != nil || ident == nil || ident.Kind() != identity.KindUser {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	out := map[string]any{
		"user_id":      ident.ID(),
		"display_name": ident.DisplayName(),
	}
	addUserProfileFields(out, ident)
	// Org memberships with role. T478 #1: emit org_name + org_slug here so the
	// UserDetail page shows a stable "name + id" for every membership. The earlier
	// design (#192) had the frontend resolve names via useOrgs, but that only knows
	// the VIEWER's own orgs — viewing another user (or any org the viewer doesn't
	// share) fell back to the raw "organization-<hex>" id. The name lives on the org
	// row we already load here, so the server is the authoritative source.
	orgs := make([]map[string]any, 0)
	if d.OrgRepo != nil {
		if list, lerr := d.OrgRepo.ListForIdentity(r.Context(), ident.ID()); lerr == nil {
			for _, org := range list {
				entry := map[string]any{
					"org_id":   org.ID(),
					"org_name": org.Name(),
					"org_slug": org.Slug(),
				}
				if d.MemberRepo != nil {
					if m, merr := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), org.ID(), ident.ID()); merr == nil && m != nil {
						entry["role"] = string(m.Role())
					}
				}
				orgs = append(orgs, entry)
			}
		}
	}
	out["orgs"] = orgs
	writeJSON(w, http.StatusOK, out)
}
