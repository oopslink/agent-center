// Package identity hosts the Conversation BC Identity AR + ChannelBinding
// sub-VO. Per conversation/02-identity.md.
//
// Identity is the unified actor identity (cross-vendor stable): the formal
// id string carries the kind prefix (user:hayang / supervisor:inv-N /
// agent:s-X / bot). ChannelBinding maps an Identity to one vendor user id
// per channel.
//
// Phase 5 first vendor consumer is Bridge; per plan-5 § 1.1 the AR + repos
// physically land in this package though strategic ownership belongs to
// Conversation BC.
package identity

import (
	"errors"
	"strings"
)

// IdentityID is the formal-prefix id string. Examples:
//   - "user:hayang"
//   - "supervisor:inv-1"
//   - "agent:s-1"
//   - "bot"
type IdentityID string

// String returns the underlying string.
func (id IdentityID) String() string { return string(id) }

// Validate enforces the formal id vocabulary (mirrors conversation.IdentityRef).
func (id IdentityID) Validate() error {
	s := string(id)
	if s == "" {
		return errors.New("identity_id: required")
	}
	if s == "bot" {
		return nil
	}
	for _, p := range []string{"user:", "supervisor:", "agent:"} {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return errors.New("identity_id: must be 'bot' or one of user:/supervisor:/agent: with non-empty suffix")
}

// Kind is the 4-value enum (conversation/02 § 1).
type Kind string

// Kind enum values.
const (
	KindUser       Kind = "user"
	KindSupervisor Kind = "supervisor"
	KindAgent      Kind = "agent"
	KindBot        Kind = "bot"
)

// IsValid checks enum membership.
func (k Kind) IsValid() bool {
	switch k {
	case KindUser, KindSupervisor, KindAgent, KindBot:
		return true
	}
	return false
}

// String returns the enum value.
func (k Kind) String() string { return string(k) }

// KindFromID derives the kind from a formal id string.
func KindFromID(id IdentityID) (Kind, error) {
	s := string(id)
	switch {
	case s == "bot":
		return KindBot, nil
	case strings.HasPrefix(s, "user:") && len(s) > len("user:"):
		return KindUser, nil
	case strings.HasPrefix(s, "supervisor:") && len(s) > len("supervisor:"):
		return KindSupervisor, nil
	case strings.HasPrefix(s, "agent:") && len(s) > len("agent:"):
		return KindAgent, nil
	}
	return "", errors.New("identity_id: cannot derive kind")
}

// Channel is the vendor channel identifier ("feishu" / "dingtalk" / ...).
type Channel string

// String returns the channel value.
func (c Channel) String() string { return string(c) }

// Validate ensures non-empty lower-case channel name.
func (c Channel) Validate() error {
	s := string(c)
	if s == "" {
		return errors.New("channel: required")
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return errors.New("channel: must be lowercase alphanumeric / '_' / '-'")
		}
	}
	return nil
}

// Sentinel errors.
var (
	// Identity
	ErrIdentityNotFound      = errors.New("identity: identity not found")
	ErrIdentityAlreadyExists = errors.New("identity: id already exists")
	ErrIdentityVersionConflict = errors.New("identity: version conflict (optimistic lock)")
	ErrIdentityInvalidKind   = errors.New("identity: invalid kind")
	ErrIdentityKindImmutable = errors.New("identity: kind is immutable")
	ErrIdentityIDImmutable   = errors.New("identity: id is immutable")

	// ChannelBinding
	ErrChannelBindingNotFound      = errors.New("identity: channel binding not found")
	ErrChannelBindingAlreadyExists = errors.New("identity: (channel, vendor_user_id) already bound")
	ErrChannelBindingPreferredConflict = errors.New("identity: another binding is already preferred for this (identity, channel)")
)
