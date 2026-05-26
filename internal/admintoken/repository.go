package admintoken

import "context"

// Repository is the persistence port for AdminToken.
//
// Lookup is hash-keyed: the middleware hashes the incoming bearer and
// asks FindByHash. No method takes plaintext.
type Repository interface {
	Save(ctx context.Context, t *AdminToken) error
	FindByID(ctx context.Context, id TokenID) (*AdminToken, error)
	FindByHash(ctx context.Context, valueHash []byte) (*AdminToken, error)
	FindAll(ctx context.Context) ([]*AdminToken, error)
	FindByOwner(ctx context.Context, owner Owner) ([]*AdminToken, error)
	// Revoke writes the revoked fields. Returns ErrTokenNotFound if id
	// is unknown, ErrTokenRevoked if already revoked.
	Revoke(ctx context.Context, id TokenID, by, reason string, expectedVersion int) error
	// UpdateLastUsedAt is best-effort, never blocks middleware. Implementations
	// may swallow constraint errors.
	UpdateLastUsedAt(ctx context.Context, id TokenID, atRFC3339Nano string) error
	// ConsumeEnrollToken atomically burns an enroll token (v2.4-D-A3,
	// task #37). Returns ErrTokenConsumed if already burnt,
	// ErrTokenNotFound if id isn't an enroll token / doesn't exist.
	// v2.5-B2 additionally clears plaintext_ciphertext + nonce so a
	// burned token can never be re-shown.
	ConsumeEnrollToken(ctx context.Context, id TokenID, atRFC3339Nano string) error
	// FindActiveEnrollByWorkerID returns the most recent enroll token
	// minted for workerID that is still showable (not revoked, not
	// consumed, plaintext_ciphertext present). Returns ErrTokenNotFound
	// if nothing matches. v2.5-B2 (#50).
	FindActiveEnrollByWorkerID(ctx context.Context, workerID string) (*AdminToken, error)
}
