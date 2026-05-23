package secretmgmt

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// UserSecret is the AR for one centrally-managed secret (ADR-0026 § 2).
//
// Plaintext NEVER lives on the AR — only ciphertext + nonce. Decryption
// happens in the dedicated SecretResolutionService inside a tx.
//
// Invariants:
//  1. name globally unique
//  2. kind / created_by / created_at / id immutable post-create
//  3. revoked is terminal — irreversible
//  4. rotate updates ciphertext + nonce + bumps rotated_at + version (state
//     stays active)
type UserSecret struct {
	id             UserSecretID
	name           string
	kind           UserSecretKind
	ciphertext     []byte
	nonce          []byte
	state          UserSecretState
	createdAt      time.Time
	createdBy      string
	lastUsedAt     *time.Time
	rotatedAt      *time.Time
	revokedAt      *time.Time
	revokedBy      string
	revokedReason  UserSecretRevokedReason
	revokedMessage string
	version        int
}

// NewUserSecretInput is the constructor input. Ciphertext + Nonce are produced
// by the service layer via aes.Encrypt.
type NewUserSecretInput struct {
	ID         UserSecretID
	Name       string
	Kind       UserSecretKind
	Ciphertext []byte
	Nonce      []byte
	CreatedAt  time.Time
	CreatedBy  string
}

// NewUserSecret constructs an active secret.
func NewUserSecret(in NewUserSecretInput) (*UserSecret, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("user secret: id required")
	}
	if err := validateSecretName(in.Name); err != nil {
		return nil, err
	}
	if !in.Kind.IsValid() {
		return nil, ErrUserSecretInvalidKind
	}
	if len(in.Ciphertext) == 0 {
		return nil, errors.New("user secret: ciphertext required")
	}
	if len(in.Nonce) == 0 {
		return nil, errors.New("user secret: nonce required")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("user secret: created_at required")
	}
	if strings.TrimSpace(in.CreatedBy) == "" {
		return nil, errors.New("user secret: created_by required")
	}
	return &UserSecret{
		id:         in.ID,
		name:       in.Name,
		kind:       in.Kind,
		ciphertext: append([]byte(nil), in.Ciphertext...),
		nonce:      append([]byte(nil), in.Nonce...),
		state:      UserSecretActive,
		createdAt:  in.CreatedAt.UTC(),
		createdBy:  in.CreatedBy,
		version:    1,
	}, nil
}

// RehydrateUserSecretInput is for Repository implementations only.
type RehydrateUserSecretInput struct {
	ID             UserSecretID
	Name           string
	Kind           UserSecretKind
	Ciphertext     []byte
	Nonce          []byte
	State          UserSecretState
	CreatedAt      time.Time
	CreatedBy      string
	LastUsedAt     *time.Time
	RotatedAt      *time.Time
	RevokedAt      *time.Time
	RevokedBy      string
	RevokedReason  UserSecretRevokedReason
	RevokedMessage string
	Version        int
}

// RehydrateUserSecret reconstructs from persisted state.
func RehydrateUserSecret(in RehydrateUserSecretInput) (*UserSecret, error) {
	if !in.State.IsValid() {
		return nil, ErrUserSecretInvalidState
	}
	if in.Version < 1 {
		return nil, errors.New("user secret: version must be >= 1")
	}
	return &UserSecret{
		id:             in.ID,
		name:           in.Name,
		kind:           in.Kind,
		ciphertext:     append([]byte(nil), in.Ciphertext...),
		nonce:          append([]byte(nil), in.Nonce...),
		state:          in.State,
		createdAt:      in.CreatedAt.UTC(),
		createdBy:      in.CreatedBy,
		lastUsedAt:     copyTimePtr(in.LastUsedAt),
		rotatedAt:      copyTimePtr(in.RotatedAt),
		revokedAt:      copyTimePtr(in.RevokedAt),
		revokedBy:      in.RevokedBy,
		revokedReason:  in.RevokedReason,
		revokedMessage: in.RevokedMessage,
		version:        in.Version,
	}, nil
}

// Getters.

func (u *UserSecret) ID() UserSecretID                   { return u.id }
func (u *UserSecret) Name() string                       { return u.name }
func (u *UserSecret) Kind() UserSecretKind               { return u.kind }
func (u *UserSecret) Ciphertext() []byte                 { return append([]byte(nil), u.ciphertext...) }
func (u *UserSecret) Nonce() []byte                      { return append([]byte(nil), u.nonce...) }
func (u *UserSecret) State() UserSecretState             { return u.state }
func (u *UserSecret) CreatedAt() time.Time               { return u.createdAt }
func (u *UserSecret) CreatedBy() string                  { return u.createdBy }
func (u *UserSecret) LastUsedAt() *time.Time             { return copyTimePtr(u.lastUsedAt) }
func (u *UserSecret) RotatedAt() *time.Time              { return copyTimePtr(u.rotatedAt) }
func (u *UserSecret) RevokedAt() *time.Time              { return copyTimePtr(u.revokedAt) }
func (u *UserSecret) RevokedBy() string                  { return u.revokedBy }
func (u *UserSecret) RevokedReason() UserSecretRevokedReason { return u.revokedReason }
func (u *UserSecret) RevokedMessage() string             { return u.revokedMessage }
func (u *UserSecret) Version() int                       { return u.version }

// Rotate replaces ciphertext + nonce. Bumps rotated_at + version. Rejects
// revoked secrets.
func (u *UserSecret) Rotate(at time.Time, newCiphertext, newNonce []byte) error {
	if u.state == UserSecretRevoked {
		return ErrUserSecretRevoked
	}
	if len(newCiphertext) == 0 || len(newNonce) == 0 {
		return errors.New("user secret: rotate requires non-empty ciphertext + nonce")
	}
	at = at.UTC()
	u.ciphertext = append([]byte(nil), newCiphertext...)
	u.nonce = append([]byte(nil), newNonce...)
	u.rotatedAt = &at
	u.version++
	return nil
}

// Revoke transitions active → revoked + records audit metadata.
func (u *UserSecret) Revoke(at time.Time, by string, reason UserSecretRevokedReason, message string) error {
	if u.state == UserSecretRevoked {
		return ErrUserSecretRevoked
	}
	if !reason.IsValid() {
		return fmt.Errorf("user secret: invalid revoked reason %q", reason)
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("user secret: revoked message required (conventions § 16)")
	}
	if strings.TrimSpace(by) == "" {
		return errors.New("user secret: revoked_by required")
	}
	at = at.UTC()
	u.state = UserSecretRevoked
	u.revokedAt = &at
	u.revokedBy = by
	u.revokedReason = reason
	u.revokedMessage = message
	u.version++
	return nil
}

// MarkUsed bumps last_used_at (called from SecretResolutionService).
// Does not bump version (read-frequency is high; not a CAS field).
func (u *UserSecret) MarkUsed(at time.Time) {
	at = at.UTC()
	u.lastUsedAt = &at
}

func validateSecretName(name string) error {
	s := strings.TrimSpace(name)
	if s == "" {
		return errors.New("user secret: name required")
	}
	if len(s) > 128 {
		return errors.New("user secret: name too long (max 128)")
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.':
		default:
			return fmt.Errorf("user secret: name %q contains invalid character %q", s, c)
		}
	}
	return nil
}

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
