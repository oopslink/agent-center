package admintoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// PlaintextPrefix is the human-recognisable token marker. Greppable by
// design: anything matching `acat_[A-Za-z0-9_-]+` in logs or commits
// is almost certainly a leaked admin-token plaintext.
const PlaintextPrefix = "acat_"

// AdminToken is the AR. value_hash is the only secret-derived field —
// plaintext is never persisted and only flows back to the operator at
// creation time via NewAdminTokenWithPlaintext.
type AdminToken struct {
	id            TokenID
	owner         Owner
	scopes        []Scope
	valueHash     []byte // sha256(plaintext)
	createdAt     time.Time
	createdBy     string
	revokedAt     *time.Time
	revokedBy     string
	revokedReason string
	lastUsedAt    *time.Time
	version       int
}

// NewAdminTokenInput captures the fields available at creation.
type NewAdminTokenInput struct {
	ID         TokenID
	Owner      Owner
	Scopes     []Scope
	ValueHash  []byte
	CreatedAt  time.Time
	CreatedBy  string
}

// New constructs an AdminToken AR with the given pre-computed value hash.
// Callers typically use GeneratePlaintext + HashPlaintext to obtain the
// hash; the AR doesn't generate randomness itself so the AR is
// deterministically testable.
func New(in NewAdminTokenInput) (*AdminToken, error) {
	if strings.TrimSpace(string(in.Owner)) == "" {
		return nil, ErrTokenOwnerRequired
	}
	if len(in.Scopes) == 0 {
		return nil, ErrTokenScopesRequired
	}
	if len(in.ValueHash) != sha256.Size {
		return nil, errors.New("admintoken: value_hash must be 32 bytes (sha256)")
	}
	scopes := make([]Scope, 0, len(in.Scopes))
	seen := map[Scope]struct{}{}
	for _, s := range in.Scopes {
		s = Scope(strings.TrimSpace(string(s)))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		scopes = append(scopes, s)
	}
	if len(scopes) == 0 {
		return nil, ErrTokenScopesRequired
	}
	hashCopy := make([]byte, len(in.ValueHash))
	copy(hashCopy, in.ValueHash)
	return &AdminToken{
		id:        in.ID,
		owner:     in.Owner,
		scopes:    scopes,
		valueHash: hashCopy,
		createdAt: in.CreatedAt.UTC(),
		createdBy: strings.TrimSpace(in.CreatedBy),
		version:   1,
	}, nil
}

// RehydrateInput rebuilds an AR from persistence. Mirrors UserSecret's
// pattern: explicit pointer fields for nullable columns.
type RehydrateInput struct {
	ID            TokenID
	Owner         Owner
	Scopes        []Scope
	ValueHash     []byte
	CreatedAt     time.Time
	CreatedBy     string
	RevokedAt     *time.Time
	RevokedBy     string
	RevokedReason string
	LastUsedAt    *time.Time
	Version       int
}

// Rehydrate rebuilds an AR from a row read.
func Rehydrate(in RehydrateInput) *AdminToken {
	hashCopy := make([]byte, len(in.ValueHash))
	copy(hashCopy, in.ValueHash)
	t := &AdminToken{
		id:            in.ID,
		owner:         in.Owner,
		scopes:        append([]Scope(nil), in.Scopes...),
		valueHash:     hashCopy,
		createdAt:     in.CreatedAt.UTC(),
		createdBy:     in.CreatedBy,
		revokedBy:     in.RevokedBy,
		revokedReason: in.RevokedReason,
		version:       in.Version,
	}
	if in.RevokedAt != nil {
		ra := in.RevokedAt.UTC()
		t.revokedAt = &ra
	}
	if in.LastUsedAt != nil {
		lu := in.LastUsedAt.UTC()
		t.lastUsedAt = &lu
	}
	return t
}

// Accessors
func (t *AdminToken) ID() TokenID            { return t.id }
func (t *AdminToken) Owner() Owner           { return t.owner }
func (t *AdminToken) Scopes() []Scope        { return append([]Scope(nil), t.scopes...) }
func (t *AdminToken) ValueHash() []byte      { return append([]byte(nil), t.valueHash...) }
func (t *AdminToken) CreatedAt() time.Time   { return t.createdAt }
func (t *AdminToken) CreatedBy() string      { return t.createdBy }
func (t *AdminToken) RevokedAt() *time.Time  { return t.revokedAt }
func (t *AdminToken) RevokedBy() string      { return t.revokedBy }
func (t *AdminToken) RevokedReason() string  { return t.revokedReason }
func (t *AdminToken) LastUsedAt() *time.Time { return t.lastUsedAt }
func (t *AdminToken) Version() int           { return t.version }

// IsRevoked is a convenience predicate.
func (t *AdminToken) IsRevoked() bool { return t.revokedAt != nil }

// HasScope returns true when the token carries the named scope or the
// superuser `*` scope.
func (t *AdminToken) HasScope(s Scope) bool {
	for _, have := range t.scopes {
		if have == "*" || have == s {
			return true
		}
	}
	return false
}

// Revoke transitions the AR to revoked. Idempotent: a second call
// returns ErrTokenRevoked so callers can detect double-revoke; persistence
// layer turns that into a 200 if needed.
func (t *AdminToken) Revoke(at time.Time, by, reason string) error {
	if t.revokedAt != nil {
		return ErrTokenRevoked
	}
	r := at.UTC()
	t.revokedAt = &r
	t.revokedBy = strings.TrimSpace(by)
	t.revokedReason = strings.TrimSpace(reason)
	t.version++
	return nil
}

// MarkUsed bumps last_used_at; concurrency-safe at the AR level (single-
// goroutine ownership), repo handles the optimistic update.
func (t *AdminToken) MarkUsed(at time.Time) {
	u := at.UTC()
	t.lastUsedAt = &u
}

// =============================================================================
// Plaintext helpers (no state — pure functions)
// =============================================================================

// GeneratePlaintext returns a fresh `acat_<32 base64url chars>` token.
// 32 bytes of entropy → ~43 base64url chars (no padding).
func GeneratePlaintext() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return PlaintextPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashPlaintext returns the sha256 of the plaintext. The caller is
// responsible for matching prefixes — we hash whatever was provided so
// the hash includes the `acat_` prefix and operators can spot leaked
// prefixes in DB dumps too.
func HashPlaintext(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}

// ParseBearer pulls the token plaintext out of an `Authorization` header
// value. Accepts both `Bearer <value>` and bare `<value>` forms; the
// former is RFC 6750, the latter is convenient for environments where
// the leading `Bearer` is dropped.
func ParseBearer(headerValue string) (string, error) {
	v := strings.TrimSpace(headerValue)
	if v == "" {
		return "", ErrTokenMissingBearer
	}
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		v = strings.TrimSpace(v[len("Bearer "):])
	}
	if v == "" {
		return "", ErrTokenMissingBearer
	}
	if !strings.HasPrefix(v, PlaintextPrefix) {
		return "", ErrTokenInvalidFormat
	}
	return v, nil
}
