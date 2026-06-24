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
//
// v2.4-D-A3 (task #37) added the enroll-token fields: isEnroll +
// expiresAt + usedAt. Long-term tokens (v2.3-3a) leave all three zero.
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
	// v2.4-D-A3 enroll-token fields.
	isEnroll  bool
	expiresAt *time.Time // nil for long-term tokens
	usedAt    *time.Time // nil until first verify burns the enroll token
	// v2.5-B2 enroll-token install-command re-display fields.
	// workerID binds the enroll token to a Worker AR row pre-created
	// at mint time (#49). Empty for long-term tokens and legacy
	// enroll tokens minted before v2.5.
	workerID string
	// plaintextCiphertext + plaintextNonce hold the AES-GCM-encrypted
	// `acat_…` bearer so the show-install-command endpoint can
	// reconstruct the install command after the Modal closes. Both
	// nil for long-term tokens and for enroll tokens that have
	// been Consume()d.
	plaintextCiphertext []byte
	plaintextNonce      []byte
}

// NewAdminTokenInput captures the fields available at creation.
type NewAdminTokenInput struct {
	ID        TokenID
	Owner     Owner
	Scopes    []Scope
	ValueHash []byte
	CreatedAt time.Time
	CreatedBy string
	// v2.4-D-A3: set IsEnroll=true + ExpiresAt non-nil to mint an
	// enroll-token (short TTL, first-use-burns). Long-term tokens
	// leave both at zero values.
	IsEnroll  bool
	ExpiresAt *time.Time
	// v2.5-B2: WorkerID + PlaintextCiphertext + PlaintextNonce are
	// optional companions for enroll tokens. WorkerID binds the
	// token to a Worker AR; ciphertext/nonce hold the AES-GCM
	// encrypted bearer so /api/workers/{id}/install-command can
	// reconstruct the install line. Legacy callers that don't pass
	// them remain valid (Show endpoint will 401 on no-plaintext).
	WorkerID            string
	PlaintextCiphertext []byte
	PlaintextNonce      []byte
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
	t := &AdminToken{
		id:        in.ID,
		owner:     in.Owner,
		scopes:    scopes,
		valueHash: hashCopy,
		createdAt: in.CreatedAt.UTC(),
		createdBy: strings.TrimSpace(in.CreatedBy),
		version:   1,
		isEnroll:  in.IsEnroll,
	}
	if in.ExpiresAt != nil {
		ea := in.ExpiresAt.UTC()
		t.expiresAt = &ea
	}
	if in.IsEnroll && t.expiresAt == nil {
		return nil, errors.New("admintoken: enroll token requires ExpiresAt")
	}
	t.workerID = strings.TrimSpace(in.WorkerID)
	if len(in.PlaintextCiphertext) > 0 {
		t.plaintextCiphertext = append([]byte(nil), in.PlaintextCiphertext...)
	}
	if len(in.PlaintextNonce) > 0 {
		t.plaintextNonce = append([]byte(nil), in.PlaintextNonce...)
	}
	if (len(t.plaintextCiphertext) == 0) != (len(t.plaintextNonce) == 0) {
		return nil, errors.New("admintoken: plaintext ciphertext + nonce must both be present or both absent")
	}
	return t, nil
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
	// v2.4-D-A3 enroll-token fields.
	IsEnroll  bool
	ExpiresAt *time.Time
	UsedAt    *time.Time
	// v2.5-B2 install-command re-display fields.
	WorkerID            string
	PlaintextCiphertext []byte
	PlaintextNonce      []byte
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
		isEnroll:      in.IsEnroll,
		workerID:      strings.TrimSpace(in.WorkerID),
	}
	if in.RevokedAt != nil {
		ra := in.RevokedAt.UTC()
		t.revokedAt = &ra
	}
	if in.LastUsedAt != nil {
		lu := in.LastUsedAt.UTC()
		t.lastUsedAt = &lu
	}
	if in.ExpiresAt != nil {
		ea := in.ExpiresAt.UTC()
		t.expiresAt = &ea
	}
	if in.UsedAt != nil {
		ua := in.UsedAt.UTC()
		t.usedAt = &ua
	}
	if len(in.PlaintextCiphertext) > 0 {
		t.plaintextCiphertext = append([]byte(nil), in.PlaintextCiphertext...)
	}
	if len(in.PlaintextNonce) > 0 {
		t.plaintextNonce = append([]byte(nil), in.PlaintextNonce...)
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

// IsEnroll reports whether this is an enroll-token (v2.4-D-A3): short
// TTL + one-time-use. Long-term v2.3-3a tokens return false.
func (t *AdminToken) IsEnroll() bool { return t.isEnroll }

// ExpiresAt returns the expiry time for enroll tokens (nil for
// long-term tokens which never expire by built-in mechanism).
func (t *AdminToken) ExpiresAt() *time.Time { return t.expiresAt }

// UsedAt returns the time the enroll token was first consumed. nil
// until burnt by middleware (Consume).
func (t *AdminToken) UsedAt() *time.Time { return t.usedAt }

// WorkerID returns the worker_id this enroll token was minted for
// (v2.5-B2). Empty for long-term tokens and legacy enroll tokens.
func (t *AdminToken) WorkerID() string { return t.workerID }

// PlaintextCiphertext + PlaintextNonce return the AES-GCM encrypted
// bearer + paired nonce so the service layer can decrypt with the
// master key for /api/workers/{id}/install-command (v2.5-B2). Both
// nil after Consume() or for tokens minted without plaintext-storage
// opt-in (legacy enroll tokens, long-term bearers).
func (t *AdminToken) PlaintextCiphertext() []byte {
	return append([]byte(nil), t.plaintextCiphertext...)
}
func (t *AdminToken) PlaintextNonce() []byte {
	return append([]byte(nil), t.plaintextNonce...)
}

// HasShowablePlaintext reports whether the AR currently carries an
// encrypted bearer the show-install-command endpoint can decrypt.
func (t *AdminToken) HasShowablePlaintext() bool {
	return len(t.plaintextCiphertext) > 0 && len(t.plaintextNonce) > 0
}

// IsExpired reports whether an enroll token is past its expires_at.
// Long-term tokens never expire (returns false).
func (t *AdminToken) IsExpired(now time.Time) bool {
	if t.expiresAt == nil {
		return false
	}
	return now.UTC().After(*t.expiresAt)
}

// IsConsumed reports whether an enroll token has been burnt. Long-term
// tokens are never "consumed" (they have many uses by design).
func (t *AdminToken) IsConsumed() bool {
	return t.isEnroll && t.usedAt != nil
}

// Consume burns an enroll token: marks used_at = now AND clears the
// stored encrypted plaintext (v2.5-B2 defense in depth, so a burned
// token can never be re-shown by /api/workers/{id}/install-command).
// Idempotent: a second call returns ErrTokenConsumed so middleware
// can reject reuse. No-op + nil for long-term tokens (MarkUsed is
// the right call for those).
func (t *AdminToken) Consume(at time.Time) error {
	if !t.isEnroll {
		return nil
	}
	if t.usedAt != nil {
		return ErrTokenConsumed
	}
	u := at.UTC()
	t.usedAt = &u
	t.plaintextCiphertext = nil
	t.plaintextNonce = nil
	t.version++
	return nil
}

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
