package identity

import (
	"time"
)

// Member is the BC9 Member AR — the Identity↔Organization relationship (ADR-0042).
//
// Invariants:
//
//	M1: (organization_id, identity_id) unique among joined members (DB enforced)
//	M2: each Organization has at least 1 owner with status=joined (Domain Service enforced)
//	M3: role demotion of last owner rejected (Domain Service)
//	M4: disabling last owner rejected (Domain Service)
//	M5: disabled_at non-nil ⟺ status=disabled
//	M6: Identity.account_status=disabled ⇒ member treated as disabled (runtime check)
type Member struct {
	id                  string
	organizationID      string
	identityID          string
	role                MemberRole
	status              MemberStatus
	joinedAt            time.Time
	invitedByIdentityID *string
	invitedAt           *time.Time
	disabledAt          *time.Time
	disabledReason      string
}

// MemberFactory creates Member instances.
type MemberFactory struct{}

// New creates a new Member. invitedBy may be nil (signup-bootstrap case).
func (MemberFactory) New(organizationID, identityID string, role MemberRole, invitedBy *string) (*Member, error) {
	if organizationID == "" {
		return nil, ErrOrganizationNotFound
	}
	if identityID == "" {
		return nil, ErrIdentityNotFound
	}
	if !role.IsValid() {
		return nil, ErrForbidden
	}
	now := time.Now().UTC()
	m := &Member{
		id:             NewMemberID(),
		organizationID: organizationID,
		identityID:     identityID,
		role:           role,
		status:         MemberJoined,
		joinedAt:       now,
	}
	if invitedBy != nil {
		s := *invitedBy
		m.invitedByIdentityID = &s
		m.invitedAt = &now
	}
	return m, nil
}

// RehydrateMember reconstructs from DB. Used by repositories.
func RehydrateMember(
	id, organizationID, identityID string,
	role MemberRole,
	status MemberStatus,
	joinedAt time.Time,
	invitedByIdentityID *string,
	invitedAt *time.Time,
	disabledAt *time.Time,
	disabledReason string,
) *Member {
	return &Member{
		id:                  id,
		organizationID:      organizationID,
		identityID:          identityID,
		role:                role,
		status:              status,
		joinedAt:            joinedAt.UTC(),
		invitedByIdentityID: invitedByIdentityID,
		invitedAt:           invitedAt,
		disabledAt:          disabledAt,
		disabledReason:      disabledReason,
	}
}

// Getters.

func (m *Member) ID() string                   { return m.id }
func (m *Member) OrganizationID() string       { return m.organizationID }
func (m *Member) IdentityID() string           { return m.identityID }
func (m *Member) Role() MemberRole             { return m.role }
func (m *Member) Status() MemberStatus         { return m.status }
func (m *Member) JoinedAt() time.Time          { return m.joinedAt }
func (m *Member) InvitedByIdentityID() *string { return m.invitedByIdentityID }
func (m *Member) InvitedAt() *time.Time        { return m.invitedAt }
func (m *Member) DisabledAt() *time.Time       { return m.disabledAt }
func (m *Member) DisabledReason() string       { return m.disabledReason }
func (m *Member) IsJoined() bool               { return m.status == MemberJoined }

// ChangeRole updates the member's role. Cross-AR last-owner invariant is
// enforced by MemberRoleChangeService before calling this.
func (m *Member) ChangeRole(newRole MemberRole) {
	m.role = newRole
}

// Disable sets status to disabled. Cross-AR last-owner invariant is enforced
// by MemberDisableService before calling this.
func (m *Member) Disable(reason string) {
	now := time.Now().UTC()
	m.status = MemberDisabled
	m.disabledAt = &now
	m.disabledReason = reason
}

// ReEnable restores the member to joined status.
func (m *Member) ReEnable() {
	m.status = MemberJoined
	m.disabledAt = nil
	m.disabledReason = ""
}
