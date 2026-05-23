package identity

import (
	"errors"
	"strings"
	"time"
)

// Identity is the Conversation BC AR. Invariants per conversation/02 § 4:
//   1. id immutable (prefixed kind:id form per ADR-0033)
//   2. kind immutable; one of user / agent / system (ADR-0033)
//   3. display_name non-empty after trimming
//   4. version monotonic on each mutating call
type Identity struct {
	id          IdentityID
	kind        Kind
	displayName string
	createdAt   time.Time
	updatedAt   time.Time
	version     int
}

// NewIdentityInput captures the constructor args.
type NewIdentityInput struct {
	ID          IdentityID
	Kind        Kind
	DisplayName string
	CreatedAt   time.Time
}

// NewIdentity constructs an Identity, validating invariants.
//
// The kind in the input must match the prefix encoded in ID.
func NewIdentity(in NewIdentityInput) (*Identity, error) {
	if err := in.ID.Validate(); err != nil {
		return nil, err
	}
	if !in.Kind.IsValid() {
		return nil, ErrIdentityInvalidKind
	}
	derived, err := KindFromID(in.ID)
	if err != nil {
		return nil, err
	}
	if derived != in.Kind {
		return nil, errors.New("identity: kind does not match id prefix")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return nil, errors.New("identity: display_name required")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("identity: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Identity{
		id:          in.ID,
		kind:        in.Kind,
		displayName: in.DisplayName,
		createdAt:   at,
		updatedAt:   at,
		version:     1,
	}, nil
}

// RehydrateIdentityInput is for repository round-trip.
type RehydrateIdentityInput struct {
	ID          IdentityID
	Kind        Kind
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

// RehydrateIdentity reconstructs without invariant checks except enum.
func RehydrateIdentity(in RehydrateIdentityInput) (*Identity, error) {
	if !in.Kind.IsValid() {
		return nil, ErrIdentityInvalidKind
	}
	if in.Version < 1 {
		return nil, errors.New("identity: version must be >= 1")
	}
	return &Identity{
		id:          in.ID,
		kind:        in.Kind,
		displayName: in.DisplayName,
		createdAt:   in.CreatedAt.UTC(),
		updatedAt:   in.UpdatedAt.UTC(),
		version:     in.Version,
	}, nil
}

// Getters.

// ID returns the identity id.
func (i *Identity) ID() IdentityID { return i.id }

// Kind returns the kind.
func (i *Identity) Kind() Kind { return i.kind }

// DisplayName returns the human-readable name.
func (i *Identity) DisplayName() string { return i.displayName }

// CreatedAt returns the creation time.
func (i *Identity) CreatedAt() time.Time { return i.createdAt }

// UpdatedAt returns the last-modified time.
func (i *Identity) UpdatedAt() time.Time { return i.updatedAt }

// Version returns the CAS version.
func (i *Identity) Version() int { return i.version }

// Rename updates the display_name (CAS bumps version).
func (i *Identity) Rename(newName string, at time.Time) error {
	if strings.TrimSpace(newName) == "" {
		return errors.New("identity: display_name required")
	}
	i.displayName = newName
	i.updatedAt = at.UTC()
	i.version++
	return nil
}
