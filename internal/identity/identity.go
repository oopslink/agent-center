package identity

import (
	"errors"
	"strings"
	"time"
)

// Identity is the BC9 Identity AR (v2.6, supersedes ADR-0033).
//
// Invariants:
//
//	I1: kind=user ⇒ passcodeHash non-empty + passcodeSetAt non-nil
//	I2: kind=agent ⇒ passcodeHash empty
//	I3: id prefix must match kind ("user-" ↔ user / "agent-" ↔ agent)
//	I4: displayName 1-40 chars
//	I5: accountStatus transitions are controlled; disabled is not a terminal state
type Identity struct {
	id              string
	kind            IdentityKind
	displayName     string
	description     string
	accountStatus   AccountStatus
	passcodeHash    string
	passcodeSetAt   *time.Time
	createdAt       time.Time
	updatedAt       time.Time
}

// IdentityFactory creates new Identity instances with invariants enforced.
type IdentityFactory struct{}

// NewUser creates a user Identity with the provided passcode hash.
// passcodePlainHash must already be an argon2 hash (callers use HashPasscode).
func (IdentityFactory) NewUser(displayName, passcodeHash string) (*Identity, error) {
	if err := validateDisplayName(displayName); err != nil {
		return nil, err
	}
	if passcodeHash == "" {
		return nil, errors.New("identity: passcode_hash required for user")
	}
	now := time.Now().UTC()
	id := NewIdentityID(KindUser)
	return &Identity{
		id:            id,
		kind:          KindUser,
		displayName:   strings.TrimSpace(displayName),
		accountStatus: AccountActive,
		passcodeHash:  passcodeHash,
		passcodeSetAt: &now,
		createdAt:     now,
		updatedAt:     now,
	}, nil
}

// NewAgent creates an agent Identity (no passcode).
func (IdentityFactory) NewAgent(displayName, description string) (*Identity, error) {
	if err := validateDisplayName(displayName); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := NewIdentityID(KindAgent)
	return &Identity{
		id:            id,
		kind:          KindAgent,
		displayName:   strings.TrimSpace(displayName),
		description:   description,
		accountStatus: AccountActive,
		createdAt:     now,
		updatedAt:     now,
	}, nil
}

// RehydrateIdentity reconstructs an Identity from persisted data (no
// invariant re-validation except basic nil-checks). Used by repositories.
func RehydrateIdentity(
	id string,
	kind IdentityKind,
	displayName string,
	description string,
	accountStatus AccountStatus,
	passcodeHash string,
	passcodeSetAt *time.Time,
	createdAt time.Time,
	updatedAt time.Time,
) *Identity {
	return &Identity{
		id:            id,
		kind:          kind,
		displayName:   displayName,
		description:   description,
		accountStatus: accountStatus,
		passcodeHash:  passcodeHash,
		passcodeSetAt: passcodeSetAt,
		createdAt:     createdAt.UTC(),
		updatedAt:     updatedAt.UTC(),
	}
}

// Getters.

func (i *Identity) ID() string                { return i.id }
func (i *Identity) Kind() IdentityKind        { return i.kind }
func (i *Identity) DisplayName() string       { return i.displayName }
func (i *Identity) Description() string       { return i.description }
func (i *Identity) AccountStatus() AccountStatus { return i.accountStatus }
func (i *Identity) PasscodeHash() string      { return i.passcodeHash }
func (i *Identity) PasscodeSetAt() *time.Time { return i.passcodeSetAt }
func (i *Identity) CreatedAt() time.Time      { return i.createdAt }
func (i *Identity) UpdatedAt() time.Time      { return i.updatedAt }

// Rename updates the displayName.
func (i *Identity) Rename(newName string) error {
	if err := validateDisplayName(newName); err != nil {
		return err
	}
	i.displayName = strings.TrimSpace(newName)
	i.updatedAt = time.Now().UTC()
	return nil
}

// UpdateDescription sets the description.
func (i *Identity) UpdateDescription(desc string) {
	i.description = desc
	i.updatedAt = time.Now().UTC()
}

// Disable sets accountStatus to disabled (I5: not a terminal state; can re-enable).
func (i *Identity) Disable() {
	i.accountStatus = AccountDisabled
	i.updatedAt = time.Now().UTC()
}

// ReEnable sets accountStatus back to active.
func (i *Identity) ReEnable() {
	i.accountStatus = AccountActive
	i.updatedAt = time.Now().UTC()
}

// SetPasscode replaces the passcode hash (user kind only).
func (i *Identity) SetPasscode(hash string) error {
	if i.kind != KindUser {
		return errors.New("identity: passcode only supported for user kind")
	}
	if hash == "" {
		return errors.New("identity: passcode_hash required")
	}
	now := time.Now().UTC()
	i.passcodeHash = hash
	i.passcodeSetAt = &now
	i.updatedAt = now
	return nil
}

func validateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("identity: display_name required")
	}
	if len(trimmed) > 40 {
		return errors.New("identity: display_name must be 1-40 chars")
	}
	return nil
}
