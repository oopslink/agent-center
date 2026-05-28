package secretmgmt

import (
	"context"
	"time"
)

// UserSecretFilter narrows UserSecretRepository.FindAll.
type UserSecretFilter struct {
	Kind  *UserSecretKind
	State *UserSecretState
	// OrganizationID scopes to a specific organization (v2.6).
	OrganizationID string
}

// UserSecretRepository defines persistence for UserSecret AR (ADR-0026 § 2).
type UserSecretRepository interface {
	FindByID(ctx context.Context, id UserSecretID) (*UserSecret, error)
	FindByName(ctx context.Context, name string) (*UserSecret, error)
	FindAll(ctx context.Context, filter UserSecretFilter) ([]*UserSecret, error)
	Save(ctx context.Context, s *UserSecret) error
	// UpdateValue replaces ciphertext + nonce + rotated_at + version (rotate path).
	UpdateValue(ctx context.Context, id UserSecretID, ciphertext, nonce []byte, rotatedAt time.Time, version int) error
	// UpdateState transitions active → revoked with audit metadata (CAS on version).
	UpdateState(ctx context.Context, id UserSecretID, from, to UserSecretState, at time.Time, by string, reason UserSecretRevokedReason, message string, version int) error
	// UpdateLastUsedAt is a non-CAS hot path (resolve frequency is high).
	UpdateLastUsedAt(ctx context.Context, id UserSecretID, at time.Time) error
}
