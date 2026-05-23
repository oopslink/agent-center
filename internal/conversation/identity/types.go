// Package identity hosts the Conversation BC Identity AR (v2 per
// ADR-0033).
//
// v2 identity model:
//   - 3 kinds: user / agent / system
//   - id format: 'kind:id' (e.g. 'user:hayang', 'agent:s-X', 'system')
//   - ChannelBinding removed (Bridge BC撤回 per ADR-0031)
package identity

import (
	"errors"
	"strings"
)

// IdentityID is the formal kind-prefixed id string. Examples:
//   - "user:hayang"
//   - "agent:s-1"
//   - "system"
type IdentityID string

// String returns the underlying string.
func (id IdentityID) String() string { return string(id) }

// Validate enforces the v2 ID vocabulary (ADR-0033).
func (id IdentityID) Validate() error {
	s := string(id)
	if s == "" {
		return errors.New("identity_id: required")
	}
	if s == "system" {
		return nil
	}
	for _, p := range []string{"user:", "agent:"} {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return errors.New("identity_id: must be 'system' or 'user:<id>' / 'agent:<id>' (ADR-0033)")
}

// Kind is the 3-value enum (ADR-0033).
type Kind string

// Kind enum values.
const (
	KindUser   Kind = "user"
	KindAgent  Kind = "agent"
	KindSystem Kind = "system"
)

// IsValid checks enum membership.
func (k Kind) IsValid() bool {
	switch k {
	case KindUser, KindAgent, KindSystem:
		return true
	}
	return false
}

// String returns the enum value.
func (k Kind) String() string { return string(k) }

// KindFromID derives the kind from a formal id string (ADR-0033).
func KindFromID(id IdentityID) (Kind, error) {
	s := string(id)
	switch {
	case s == "system":
		return KindSystem, nil
	case strings.HasPrefix(s, "user:") && len(s) > len("user:"):
		return KindUser, nil
	case strings.HasPrefix(s, "agent:") && len(s) > len("agent:"):
		return KindAgent, nil
	}
	return "", errors.New("identity_id: cannot derive kind (expect 'system' / 'user:<id>' / 'agent:<id>')")
}

// Sentinel errors.
var (
	ErrIdentityNotFound        = errors.New("identity: identity not found")
	ErrIdentityAlreadyExists   = errors.New("identity: id already exists")
	ErrIdentityVersionConflict = errors.New("identity: version conflict (optimistic lock)")
	ErrIdentityInvalidKind     = errors.New("identity: invalid kind")
	ErrIdentityKindImmutable   = errors.New("identity: kind is immutable")
	ErrIdentityIDImmutable     = errors.New("identity: id is immutable")
)
