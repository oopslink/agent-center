package identity

import (
	"time"
)

// Invitation is the BC9 Invitation AR — v2.6 schema placeholder only.
// Application Service entry points are not exposed until v2.7.
type Invitation struct {
	id                    string
	organizationID        string
	inviteeHandle         string
	roleToGrant           MemberRole
	invitedByIdentityID   string
	status                InvitationStatus
	token                 string
	createdAt             time.Time
	expiresAt             time.Time
	acceptedByIdentityID  *string
	acceptedAt            *time.Time
}

// InvitationFactory creates Invitation instances (v2.7).
type InvitationFactory struct{}

// New creates a pending Invitation (v2.7; included here for schema completeness).
func (InvitationFactory) New(
	organizationID, inviteeHandle string,
	roleToGrant MemberRole,
	invitedByIdentityID string,
	expiresIn time.Duration,
) (*Invitation, error) {
	if organizationID == "" || inviteeHandle == "" || invitedByIdentityID == "" {
		return nil, ErrInvitationNotFound
	}
	now := time.Now().UTC()
	return &Invitation{
		id:                  NewInvitationID(),
		organizationID:      organizationID,
		inviteeHandle:       inviteeHandle,
		roleToGrant:         roleToGrant,
		invitedByIdentityID: invitedByIdentityID,
		status:              InvitationPending,
		token:               NewInvitationToken(),
		createdAt:           now,
		expiresAt:           now.Add(expiresIn),
	}, nil
}

// RehydrateInvitation reconstructs from DB.
func RehydrateInvitation(
	id, organizationID, inviteeHandle string,
	roleToGrant MemberRole,
	invitedByIdentityID string,
	status InvitationStatus,
	token string,
	createdAt, expiresAt time.Time,
	acceptedByIdentityID *string,
	acceptedAt *time.Time,
) *Invitation {
	return &Invitation{
		id:                   id,
		organizationID:       organizationID,
		inviteeHandle:        inviteeHandle,
		roleToGrant:          roleToGrant,
		invitedByIdentityID:  invitedByIdentityID,
		status:               status,
		token:                token,
		createdAt:            createdAt.UTC(),
		expiresAt:            expiresAt.UTC(),
		acceptedByIdentityID: acceptedByIdentityID,
		acceptedAt:           acceptedAt,
	}
}

// Getters.

func (inv *Invitation) ID() string                       { return inv.id }
func (inv *Invitation) OrganizationID() string           { return inv.organizationID }
func (inv *Invitation) InviteeHandle() string            { return inv.inviteeHandle }
func (inv *Invitation) RoleToGrant() MemberRole          { return inv.roleToGrant }
func (inv *Invitation) InvitedByIdentityID() string      { return inv.invitedByIdentityID }
func (inv *Invitation) Status() InvitationStatus         { return inv.status }
func (inv *Invitation) Token() string                    { return inv.token }
func (inv *Invitation) CreatedAt() time.Time             { return inv.createdAt }
func (inv *Invitation) ExpiresAt() time.Time             { return inv.expiresAt }
func (inv *Invitation) AcceptedByIdentityID() *string    { return inv.acceptedByIdentityID }
func (inv *Invitation) AcceptedAt() *time.Time           { return inv.acceptedAt }
