package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/persistence"
)

const defaultInvitationTTL = 14 * 24 * time.Hour

// listInvitationsHandler handles GET /api/orgs/{slug}/invitations.
func (s *Server) listInvitationsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvitationRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "invitations not configured")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	invs, err := d.InvitationRepo.ListByOrganization(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	result := make([]map[string]any, 0, len(invs))
	for _, inv := range invs {
		result = append(result, invitationPublicMap(r, d, inv))
	}
	writeJSON(w, http.StatusOK, result)
}

// createInvitationHandler handles POST /api/orgs/{slug}/invitations.
// The invitee must be an existing, self-registered user identity. Accepting the
// invitation is a separate authenticated action by that exact user.
func (s *Server) createInvitationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvitationRepo == nil || d.IdentityRepo == nil || d.MemberRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "invitations not configured")
		return
	}
	caller, callerMember, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can invite members")
		return
	}
	var body struct {
		InviteeUserID string `json:"invitee_user_id"`
		Role          string `json:"role"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	body.InviteeUserID = strings.TrimSpace(body.InviteeUserID)
	if body.InviteeUserID == "" {
		writeError(w, http.StatusBadRequest, "invitee_user_id_required", "invitee_user_id is required")
		return
	}
	if body.Role == "" {
		body.Role = string(identity.RoleMember)
	}
	role := identity.MemberRole(body.Role)
	if !role.IsValid() {
		writeError(w, http.StatusBadRequest, "invalid_role", "role must be owner, admin, or member")
		return
	}
	if callerMember.Role() == identity.RoleAdmin && role != identity.RoleMember {
		writeError(w, http.StatusForbidden, "forbidden", "admin can only invite member-role users")
		return
	}
	invitee, err := d.IdentityRepo.GetByID(r.Context(), body.InviteeUserID)
	if err != nil || invitee == nil || invitee.Kind() != identity.KindUser {
		writeError(w, http.StatusNotFound, "invitee_not_found", "invitee user identity not found")
		return
	}
	if existing, _ := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), orgID, invitee.ID()); existing != nil {
		writeError(w, http.StatusConflict, "already_member", "invitee is already a member")
		return
	}
	inv, err := identity.InvitationFactory{}.New(orgID, invitee.ID(), role, caller.ID(), defaultInvitationTTL)
	if err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	if err := d.InvitationRepo.Save(r.Context(), inv); err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, invitationPublicMap(r, d, inv))
}

// cancelInvitationHandler handles POST /api/orgs/{slug}/invitations/{id}/cancel.
func (s *Server) cancelInvitationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvitationRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "invitations not configured")
		return
	}
	_, callerMember, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can cancel invitations")
		return
	}
	inv, err := d.InvitationRepo.GetByID(r.Context(), r.PathValue("id"))
	if err != nil || inv == nil || inv.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "invitation not found")
		return
	}
	if err := inv.Cancel(); err != nil {
		writeError(w, http.StatusConflict, "not_pending", "only pending invitations can be cancelled")
		return
	}
	if err := d.InvitationRepo.Save(r.Context(), inv); err != nil {
		writeError(w, http.StatusInternalServerError, "cancel_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, invitationPublicMap(r, d, inv))
}

// deleteInvitationHandler handles DELETE /api/orgs/{slug}/invitations/{id}.
// Unlike cancel, delete hard-removes the invitation row regardless of lifecycle
// status. Owner/admin only; scoped to the current organization.
func (s *Server) deleteInvitationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvitationRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "invitations not configured")
		return
	}
	_, callerMember, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can delete invitations")
		return
	}
	inv, err := d.InvitationRepo.GetByID(r.Context(), r.PathValue("id"))
	if err != nil || inv == nil || inv.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "invitation not found")
		return
	}
	if err := d.InvitationRepo.Delete(r.Context(), inv.ID()); err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// acceptInvitationHandler handles POST /api/orgs/{slug}/invitations/{token}/accept.
// It intentionally authenticates without requireOrgMember: the invitee is not a
// member until this call succeeds.
func (s *Server) acceptInvitationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvitationRepo == nil || d.AuthSvc == nil || d.OrgRepo == nil || d.MemberRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "invitation accept not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	caller, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return
	}
	org, err := d.OrgRepo.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil || org == nil || org.IsDeleted() {
		writeError(w, http.StatusNotFound, "org_not_found", "organization not found")
		return
	}
	inv, err := d.InvitationRepo.GetByToken(r.Context(), r.PathValue("token"))
	if err != nil || inv == nil || inv.OrganizationID() != org.ID() {
		writeError(w, http.StatusNotFound, "not_found", "invitation not found")
		return
	}
	if inv.Status() == identity.InvitationRevoked {
		writeError(w, http.StatusGone, "cancelled", "invitation has been cancelled")
		return
	}
	if inv.Status() == identity.InvitationAccepted {
		writeJSON(w, http.StatusOK, invitationPublicMap(r, d, inv))
		return
	}
	if inv.InviteeHandle() != caller.ID() {
		writeError(w, http.StatusForbidden, "wrong_invitee", "this invitation is for a different user")
		return
	}
	now := time.Now().UTC()
	if err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		fresh, ferr := d.InvitationRepo.GetByID(txCtx, inv.ID())
		if ferr != nil {
			return ferr
		}
		if fresh.Status() == identity.InvitationRevoked {
			return errInvitationCancelled
		}
		if fresh.Status() == identity.InvitationAccepted {
			inv = fresh
			return nil
		}
		if err := fresh.Accept(caller.ID(), now); err != nil {
			if errors.Is(err, identity.ErrInvitationExpired) {
				_ = d.InvitationRepo.Save(txCtx, fresh)
			}
			return err
		}
		if existing, _ := d.MemberRepo.GetByOrganizationAndIdentity(txCtx, fresh.OrganizationID(), caller.ID()); existing == nil {
			invitedBy := fresh.InvitedByIdentityID()
			member, merr := identity.MemberFactory{}.New(fresh.OrganizationID(), caller.ID(), fresh.RoleToGrant(), &invitedBy)
			if merr != nil {
				return merr
			}
			if merr := d.MemberRepo.Save(txCtx, member); merr != nil {
				return merr
			}
		}
		if ferr := d.InvitationRepo.Save(txCtx, fresh); ferr != nil {
			return ferr
		}
		inv = fresh
		return nil
	}); err != nil {
		switch {
		case errors.Is(err, errInvitationCancelled):
			writeError(w, http.StatusGone, "cancelled", "invitation has been cancelled")
		case errors.Is(err, identity.ErrInvitationExpired):
			writeError(w, http.StatusGone, "expired", "invitation has expired")
		case errors.Is(err, identity.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "invitation cannot be accepted")
		default:
			writeError(w, http.StatusInternalServerError, "accept_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, invitationPublicMap(r, d, inv))
}

var errInvitationCancelled = errors.New("invitation cancelled")

func invitationPublicMap(r *http.Request, d HandlerDeps, inv *identity.Invitation) map[string]any {
	status := string(inv.Status())
	if inv.Status() == identity.InvitationRevoked {
		status = "cancelled"
	}
	row := map[string]any{
		"id":                     inv.ID(),
		"organization_id":        inv.OrganizationID(),
		"invitee_user_id":        inv.InviteeHandle(),
		"role":                   string(inv.RoleToGrant()),
		"invited_by_identity_id": inv.InvitedByIdentityID(),
		"status":                 status,
		"token":                  inv.Token(),
		"created_at":             inv.CreatedAt().Format(time.RFC3339Nano),
		"expires_at":             inv.ExpiresAt().Format(time.RFC3339Nano),
	}
	if inv.AcceptedByIdentityID() != nil {
		row["accepted_by_identity_id"] = *inv.AcceptedByIdentityID()
	}
	if inv.AcceptedAt() != nil {
		row["accepted_at"] = inv.AcceptedAt().Format(time.RFC3339Nano)
	}
	if d.IdentityRepo != nil {
		if invitee, err := d.IdentityRepo.GetByID(r.Context(), inv.InviteeHandle()); err == nil && invitee != nil {
			row["invitee_display_name"] = invitee.DisplayName()
		}
		if by, err := d.IdentityRepo.GetByID(r.Context(), inv.InvitedByIdentityID()); err == nil && by != nil {
			row["invited_by_display_name"] = by.DisplayName()
		}
	}
	return row
}
