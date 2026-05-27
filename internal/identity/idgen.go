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
