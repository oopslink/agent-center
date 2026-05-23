package workforce

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BootstrapTokenStatus is the 4-state enum for BootstrapToken (ADR-0023 § 2).
type BootstrapTokenStatus string

const (
	BootstrapTokenActive  BootstrapTokenStatus = "active"
	BootstrapTokenUsed    BootstrapTokenStatus = "used"
	BootstrapTokenExpired BootstrapTokenStatus = "expired"
	BootstrapTokenRevoked BootstrapTokenStatus = "revoked"
)

// IsValid reports enum membership.
func (s BootstrapTokenStatus) IsValid() bool {
	switch s {
	case BootstrapTokenActive, BootstrapTokenUsed, BootstrapTokenExpired, BootstrapTokenRevoked:
		return true
	}
	return false
}

// IsTerminal reports whether the status is non-active.
func (s BootstrapTokenStatus) IsTerminal() bool {
	return s == BootstrapTokenUsed || s == BootstrapTokenExpired || s == BootstrapTokenRevoked
}

// String returns the underlying value.
func (s BootstrapTokenStatus) String() string { return string(s) }

// BootstrapTokenID is the typed ULID PK.
type BootstrapTokenID string

// String returns the underlying value.
func (id BootstrapTokenID) String() string { return string(id) }

// BootstrapTokenRevokedReason categorises revocation cause (closed enum).
type BootstrapTokenRevokedReason string

const (
	// BootstrapTokenRevokedReasonManual — explicit admin/user `revoke` call.
	BootstrapTokenRevokedReasonManual BootstrapTokenRevokedReason = "manual"
	// BootstrapTokenRevokedReasonReissueSuperseded — the previous active token
	// for the same worker was superseded by reissue.
	BootstrapTokenRevokedReasonReissueSuperseded BootstrapTokenRevokedReason = "reissue_superseded"
)

// IsValid reports enum membership.
func (r BootstrapTokenRevokedReason) IsValid() bool {
	switch r {
	case BootstrapTokenRevokedReasonManual, BootstrapTokenRevokedReasonReissueSuperseded:
		return true
	}
	return false
}

// String returns the underlying value.
func (r BootstrapTokenRevokedReason) String() string { return string(r) }

// BootstrapToken is a Worker enroll token (ADR-0023 § 2).
//
// Plaintext is never stored — only hash (HashTokenValue). Plaintext is
// returned exactly once from BootstrapTokenService.Issue / Reissue.
type BootstrapToken struct {
	id             BootstrapTokenID
	workerID       WorkerID
	valueHash      string
	status         BootstrapTokenStatus
	createdAt      time.Time
	expiresAt      time.Time
	usedAt         *time.Time
	revokedAt      *time.Time
	revokedReason  BootstrapTokenRevokedReason
	revokedMessage string
	createdBy      string
}

// NewBootstrapTokenInput is the constructor input. ValueHash should already
// be the SHA-256 hex of the plaintext (use HashTokenValue).
type NewBootstrapTokenInput struct {
	ID        BootstrapTokenID
	WorkerID  WorkerID
	ValueHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	CreatedBy string
}

// NewBootstrapToken constructs a fresh active token.
func NewBootstrapToken(in NewBootstrapTokenInput) (*BootstrapToken, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("bootstrap token: id required")
	}
	if err := validateWorkerID(in.WorkerID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ValueHash) == "" {
		return nil, errors.New("bootstrap token: value_hash required")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("bootstrap token: created_at required")
	}
	if in.ExpiresAt.IsZero() || !in.ExpiresAt.After(in.CreatedAt) {
		return nil, errors.New("bootstrap token: expires_at must be after created_at")
	}
	if strings.TrimSpace(in.CreatedBy) == "" {
		return nil, errors.New("bootstrap token: created_by required")
	}
	return &BootstrapToken{
		id:        in.ID,
		workerID:  in.WorkerID,
		valueHash: in.ValueHash,
		status:    BootstrapTokenActive,
		createdAt: in.CreatedAt.UTC(),
		expiresAt: in.ExpiresAt.UTC(),
		createdBy: in.CreatedBy,
	}, nil
}

// RehydrateBootstrapTokenInput is used by Repository implementations only.
type RehydrateBootstrapTokenInput struct {
	ID             BootstrapTokenID
	WorkerID       WorkerID
	ValueHash      string
	Status         BootstrapTokenStatus
	CreatedAt      time.Time
	ExpiresAt      time.Time
	UsedAt         *time.Time
	RevokedAt      *time.Time
	RevokedReason  BootstrapTokenRevokedReason
	RevokedMessage string
	CreatedBy      string
}

// RehydrateBootstrapToken reconstructs from persisted state.
func RehydrateBootstrapToken(in RehydrateBootstrapTokenInput) (*BootstrapToken, error) {
	if !in.Status.IsValid() {
		return nil, fmt.Errorf("bootstrap token: invalid status %q", in.Status)
	}
	return &BootstrapToken{
		id:             in.ID,
		workerID:       in.WorkerID,
		valueHash:      in.ValueHash,
		status:         in.Status,
		createdAt:      in.CreatedAt.UTC(),
		expiresAt:      in.ExpiresAt.UTC(),
		usedAt:         copyTimePtr(in.UsedAt),
		revokedAt:      copyTimePtr(in.RevokedAt),
		revokedReason:  in.RevokedReason,
		revokedMessage: in.RevokedMessage,
		createdBy:      in.CreatedBy,
	}, nil
}

// Getters.

func (t *BootstrapToken) ID() BootstrapTokenID                       { return t.id }
func (t *BootstrapToken) WorkerID() WorkerID                         { return t.workerID }
func (t *BootstrapToken) ValueHash() string                          { return t.valueHash }
func (t *BootstrapToken) Status() BootstrapTokenStatus               { return t.status }
func (t *BootstrapToken) CreatedAt() time.Time                       { return t.createdAt }
func (t *BootstrapToken) ExpiresAt() time.Time                       { return t.expiresAt }
func (t *BootstrapToken) UsedAt() *time.Time                         { return copyTimePtr(t.usedAt) }
func (t *BootstrapToken) RevokedAt() *time.Time                      { return copyTimePtr(t.revokedAt) }
func (t *BootstrapToken) RevokedReason() BootstrapTokenRevokedReason { return t.revokedReason }
func (t *BootstrapToken) RevokedMessage() string                     { return t.revokedMessage }
func (t *BootstrapToken) CreatedBy() string                          { return t.createdBy }

// IsExpiredAt reports whether the token is past its TTL at the given time.
// Does not mutate state; the scanner (BootstrapTokenService.ScanExpired) is
// responsible for state transitions.
func (t *BootstrapToken) IsExpiredAt(now time.Time) bool {
	return !t.expiresAt.IsZero() && !now.Before(t.expiresAt)
}

// MarkUsed transitions active → used. Returns ErrBootstrapTokenNotActive if
// the token is not currently active.
func (t *BootstrapToken) MarkUsed(at time.Time) error {
	if t.status != BootstrapTokenActive {
		return ErrBootstrapTokenNotActive
	}
	at = at.UTC()
	t.status = BootstrapTokenUsed
	t.usedAt = &at
	return nil
}

// MarkExpired transitions active → expired (scanner path). Returns
// ErrBootstrapTokenNotActive if not active.
func (t *BootstrapToken) MarkExpired() error {
	if t.status != BootstrapTokenActive {
		return ErrBootstrapTokenNotActive
	}
	t.status = BootstrapTokenExpired
	return nil
}

// MarkRevoked transitions active → revoked with reason + message (conv § 16).
func (t *BootstrapToken) MarkRevoked(at time.Time, reason BootstrapTokenRevokedReason, message string) error {
	if t.status != BootstrapTokenActive {
		return ErrBootstrapTokenNotActive
	}
	if !reason.IsValid() {
		return fmt.Errorf("bootstrap token: invalid revoked reason %q", reason)
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("bootstrap token: revoked message required (conventions § 16)")
	}
	at = at.UTC()
	t.status = BootstrapTokenRevoked
	t.revokedAt = &at
	t.revokedReason = reason
	t.revokedMessage = message
	return nil
}

// HashTokenValue returns the SHA-256 hex digest of the plaintext.
func HashTokenValue(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// BootstrapToken sentinel errors.
var (
	ErrBootstrapTokenNotFound         = errors.New("workforce: bootstrap token not found")
	ErrBootstrapTokenAlreadyExists    = errors.New("workforce: bootstrap token id already exists")
	ErrBootstrapTokenValueHashConflict = errors.New("workforce: bootstrap token value_hash conflict")
	ErrBootstrapTokenNotActive        = errors.New("workforce: bootstrap token not in active state")
	ErrBootstrapTokenAlreadyUsed      = errors.New("workforce: bootstrap token already used (terminal; not reissuable per ADR-0023)")
	ErrBootstrapTokenStatusConflict   = errors.New("workforce: bootstrap token status conflict (concurrent transition)")
	ErrBootstrapTokenActiveExists     = errors.New("workforce: worker already has an active bootstrap token")
)
