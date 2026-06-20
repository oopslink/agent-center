package identity

import (
	"crypto/rand"
	"fmt"
)

// newHexID generates a random 8-byte hex string.
func newHexID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("identity: crypto/rand failed: %v", err))
	}
	return fmt.Sprintf("%08x", b)
}

// NewIdentityID generates a new identity ID in the format "user-<8hex>" or
// "agent-<8hex>".
func NewIdentityID(kind IdentityKind) string {
	return string(kind) + "-" + newHexID()
}

// NewOrganizationID generates a new organization ID.
func NewOrganizationID() string {
	return "organization-" + newHexID()
}

// NewOrganizationSlug generates a backend-assigned organization slug in the form
// "org-<8hex>" (T237). The user no longer supplies a slug at signup; this is
// generated server-side and is globally unique (the random 8-hex space plus the
// signup uniqueness check + DB unique index). It satisfies ValidateSlug (O1): a
// 12-char string of [a-z0-9-] starting with 'o' and ending in a hex digit.
func NewOrganizationSlug() string {
	return "org-" + newHexID()
}

// NewMemberID generates a new member ID.
func NewMemberID() string {
	return "mem-" + newHexID()
}

// NewInvitationID generates a new invitation ID.
func NewInvitationID() string {
	return "inv-" + newHexID()
}

// NewInvitationToken generates a 32-byte hex token.
func NewInvitationToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("identity: crypto/rand failed: %v", err))
	}
	return fmt.Sprintf("%x", b)
}
