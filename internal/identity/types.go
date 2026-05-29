// Package identity implements the Identity BC (BC9) introduced in v2.6.
//
// Aggregates: Identity, Organization, Member, Invitation.
// ID formats:
//
//	Identity     → "user-<8hex>" | "agent-<8hex>"
//	Organization → "organization-<8hex>"
//	Member       → "mem-<8hex>"
//	Invitation   → "inv-<8hex>"
package identity

import (
	"errors"
	"regexp"
)

// IdentityKind enumerates the two valid identity kinds (v2.6 removes system).
type IdentityKind string

const (
	KindUser  IdentityKind = "user"
	KindAgent IdentityKind = "agent"
)

// IsValid returns true for known kinds.
func (k IdentityKind) IsValid() bool {
	return k == KindUser || k == KindAgent
}

func (k IdentityKind) String() string { return string(k) }

// AccountStatus represents the global on/off toggle of an Identity.
type AccountStatus string

const (
	AccountActive   AccountStatus = "active"
	AccountDisabled AccountStatus = "disabled"
)

func (s AccountStatus) IsValid() bool {
	return s == AccountActive || s == AccountDisabled
}

// MemberRole represents the permission level within an Organization.
type MemberRole string

const (
	RoleOwner  MemberRole = "owner"
	RoleAdmin  MemberRole = "admin"
	RoleMember MemberRole = "member"
)

// IsValid checks enum membership.
func (r MemberRole) IsValid() bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleMember
}

// AtLeast returns true if this role is at least as privileged as minimum.
func (r MemberRole) AtLeast(minimum MemberRole) bool {
	rank := map[MemberRole]int{RoleMember: 0, RoleAdmin: 1, RoleOwner: 2}
	return rank[r] >= rank[minimum]
}

// MemberStatus represents the active/inactive state within an Organization.
type MemberStatus string

const (
	MemberJoined   MemberStatus = "joined"
	MemberDisabled MemberStatus = "disabled"
)

func (s MemberStatus) IsValid() bool {
	return s == MemberJoined || s == MemberDisabled
}

// InvitationStatus represents the lifecycle state of an invitation.
type InvitationStatus string

const (
	InvitationPending  InvitationStatus = "pending"
	InvitationAccepted InvitationStatus = "accepted"
	InvitationExpired  InvitationStatus = "expired"
	InvitationRevoked  InvitationStatus = "revoked"
)

func (s InvitationStatus) IsValid() bool {
	switch s {
	case InvitationPending, InvitationAccepted, InvitationExpired, InvitationRevoked:
		return true
	}
	return false
}

// slugRegex validates organization slugs per v2.6-design § 4.2.2 O1.
var slugRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])?$`)

// ValidateSlug returns nil if slug is a valid organization slug.
func ValidateSlug(slug string) error {
	if len(slug) < 3 || len(slug) > 40 {
		return ErrOrganizationSlugInvalid
	}
	if !slugRegex.MatchString(slug) {
		return ErrOrganizationSlugInvalid
	}
	return nil
}

// Sentinel domain errors.
var (
	ErrIdentityNotFound          = errors.New("identity: not found")
	ErrIdentityDisplayNameTaken  = errors.New("identity: display_name already taken")
	ErrIdentityInvalidKind       = errors.New("identity: invalid kind")
	ErrIdentityAlreadyExists     = errors.New("identity: already exists")

	ErrOrganizationNotFound    = errors.New("organization: not found")
	ErrOrganizationSlugTaken   = errors.New("organization: slug already taken")
	ErrOrganizationSlugInvalid = errors.New("organization: slug format invalid")
	ErrOrganizationDeleted     = errors.New("organization: already deleted")

	ErrMemberNotFound       = errors.New("member: not found")
	ErrMemberAlreadyExists  = errors.New("member: already exists in organization")
	ErrLastOwnerCannotLeave = errors.New("member: cannot remove last owner of organization")

	ErrInvitationNotFound = errors.New("invitation: not found")
	ErrInvitationExpired  = errors.New("invitation: expired")

	ErrPasscodeInvalid       = errors.New("auth: passcode incorrect")
	ErrForbidden             = errors.New("auth: forbidden")
	ErrUnauthenticated       = errors.New("auth: unauthenticated")
)
