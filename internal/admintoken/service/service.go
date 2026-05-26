// Package service hosts the AdminTokenService application service —
// the only entry to AdminToken state per conventions § 0.4.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// Service wraps the AdminToken repository with creation + revocation
// + bookkeeping.
type Service struct {
	repo  admintoken.Repository
	idgen idgen.Generator
	clock clock.Clock
	// masterKey is the AES-256 master key used to encrypt enroll-token
	// plaintext at rest for the v2.5-B2 show-install-command flow. nil
	// when secret management isn't configured: CreateEnrollToken still
	// works (no plaintext stored) and ShowInstallCommand 401s. Long-
	// term tokens never touch this path.
	masterKey *secretmgmt.MasterKey

	// markUsedCh serializes last_used_at writes onto a single goroutine
	// so concurrent MarkUsedAsync calls don't pile up SQLite write-lock
	// contention. We coalesce duplicates per-id within the channel — the
	// last value wins, which is fine for a best-effort timestamp.
	markUsedCh chan admintoken.TokenID

	// lastMarkUsed throttles per-token last_used_at writes to
	// markUsedThrottle. Without throttling, a busy worker daemon
	// (polling the queue ~5/sec) generates 5 writes/sec that contend
	// with foreground request transactions on the same SQLite db
	// (manifests as "database is locked (517)" on macOS).
	muLastMark   sync.Mutex
	lastMarkUsed map[admintoken.TokenID]time.Time
}

// markUsedThrottle is the minimum interval between LastUsedAt writes
// for the same token. 30s is more than coarse enough for the audit use
// case (operator viewing list) and absorbs every realistic poll burst.
const markUsedThrottle = 30 * time.Second

// New constructs the service. Starts a background pump for
// MarkUsedAsync writes so the per-request bookkeeping never contends
// with the admin endpoint's foreground tx writes (v2.3-3a observation:
// SQLite WAL can return SQLITE_LOCKED (517) under heavy concurrent
// writes on macOS even with busy_timeout=5s).
func New(repo admintoken.Repository, gen idgen.Generator, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	s := &Service{
		repo:  repo,
		idgen: gen,
		clock: clk,
		// Buffer is large enough to absorb bursts; full channel = drop
		// (we'd rather drop a bookkeeping write than block a request).
		markUsedCh:   make(chan admintoken.TokenID, 256),
		lastMarkUsed: map[admintoken.TokenID]time.Time{},
	}
	go s.markUsedPump()
	return s
}

// WithMasterKey enables AES-GCM encryption of enroll-token plaintext
// so the Web Console's show-install-command endpoint (v2.5-B2) can
// re-display the bearer after the Add Worker Modal closes. Without a
// master key, CreateEnrollToken stops persisting plaintext and the
// show endpoint always 401s. Returns the service for fluent chaining.
func (s *Service) WithMasterKey(mk *secretmgmt.MasterKey) *Service {
	if s != nil {
		s.masterKey = mk
	}
	return s
}

// markUsedPump drains markUsedCh on a single goroutine. Each tick
// writes one row; failures are swallowed (best-effort bookkeeping).
// The loop exits when markUsedCh is closed by Close().
func (s *Service) markUsedPump() {
	for id := range s.markUsedCh {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.repo.UpdateLastUsedAt(ctx, id, s.clock.Now().UTC().Format(time.RFC3339Nano))
		cancel()
	}
}

// Close shuts down the background pump. Idempotent. Tests call this
// during cleanup to ensure no goroutine outlives the test's DB handle
// (writing through a closed sql.DB causes panics on some sqlite
// drivers).
func (s *Service) Close() {
	if s == nil || s.markUsedCh == nil {
		return
	}
	defer func() {
		// Closing a closed channel panics; the guard makes Close
		// idempotent.
		_ = recover()
	}()
	close(s.markUsedCh)
	s.markUsedCh = nil
}

// CreateCommand captures token creation parameters.
type CreateCommand struct {
	Owner     admintoken.Owner
	Scopes    []admintoken.Scope
	CreatedBy string
}

// CreateResult returns the new id AND the plaintext bearer. Plaintext
// MUST be shown to the operator immediately; we never persist it.
type CreateResult struct {
	ID        admintoken.TokenID
	Plaintext string // includes the `acat_` prefix
}

// Create generates a fresh plaintext, persists the AR, and returns the
// plaintext exactly once. Callers responsible for surfacing the plaintext
// to the operator (CLI prints; file write for bootstrap).
func (s *Service) Create(ctx context.Context, cmd CreateCommand) (CreateResult, error) {
	if strings.TrimSpace(string(cmd.Owner)) == "" {
		return CreateResult{}, admintoken.ErrTokenOwnerRequired
	}
	if len(cmd.Scopes) == 0 {
		return CreateResult{}, admintoken.ErrTokenScopesRequired
	}
	plaintext, err := admintoken.GeneratePlaintext()
	if err != nil {
		return CreateResult{}, fmt.Errorf("admin token: generate plaintext: %w", err)
	}
	hash := admintoken.HashPlaintext(plaintext)
	id := admintoken.TokenID(s.idgen.NewULID())
	t, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID:        id,
		Owner:     cmd.Owner,
		Scopes:    cmd.Scopes,
		ValueHash: hash,
		CreatedAt: s.clock.Now(),
		CreatedBy: cmd.CreatedBy,
	})
	if err != nil {
		return CreateResult{}, err
	}
	if err := s.repo.Save(ctx, t); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{ID: id, Plaintext: plaintext}, nil
}

// VerifyPlaintext is the middleware fast-path: hash + lookup + revoked
// check. Returns the AR for actor + scope use, or a sentinel error.
//
// v2.4-D-A3 (task #37): enroll tokens additionally checked for expiry
// + consumption. Long-term tokens (IsEnroll=false) skip those checks
// and behave identically to v2.3-3a.
func (s *Service) VerifyPlaintext(ctx context.Context, plaintext string) (*admintoken.AdminToken, error) {
	if plaintext == "" {
		return nil, admintoken.ErrTokenMissingBearer
	}
	hash := admintoken.HashPlaintext(plaintext)
	t, err := s.repo.FindByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if t.IsRevoked() {
		return nil, admintoken.ErrTokenRevoked
	}
	if t.IsEnroll() {
		if t.IsExpired(s.clock.Now()) {
			return nil, admintoken.ErrTokenExpired
		}
		if t.IsConsumed() {
			return nil, admintoken.ErrTokenConsumed
		}
	}
	return t, nil
}

// CreateEnrollCommand mints a one-time-use bootstrap-enroll token.
// v2.4-D-A3 (task #37). v2.5-B2 adds WorkerID — when non-empty AND
// the service was wired with a master key, the plaintext bearer is
// AES-GCM encrypted and persisted so the Web Console can re-display
// the install command later (#50).
type CreateEnrollCommand struct {
	Owner     admintoken.Owner
	Scopes    []admintoken.Scope
	CreatedBy string
	TTL       time.Duration // window of validity from now; e.g. 30 * time.Minute
	// WorkerID binds the enroll token to a Worker AR row (v2.5-B1).
	// Optional; legacy callers leave empty and lose show-install-command
	// support for that token.
	WorkerID string
}

// CreateEnrollToken mints a fresh enroll token. The plaintext is
// returned ONCE; the AR records expires_at = now + ttl. After first
// successful VerifyPlaintext, middleware should call ConsumeEnrollToken
// to mark used_at and prevent reuse.
//
// v2.5-B2: when WorkerID is non-empty AND the service has a master
// key, the plaintext is encrypted and persisted alongside the row so
// `/api/workers/{id}/install-command` can re-display the bearer after
// the Add Worker Modal closes.
func (s *Service) CreateEnrollToken(ctx context.Context, cmd CreateEnrollCommand) (CreateResult, error) {
	if strings.TrimSpace(string(cmd.Owner)) == "" {
		return CreateResult{}, admintoken.ErrTokenOwnerRequired
	}
	if len(cmd.Scopes) == 0 {
		return CreateResult{}, admintoken.ErrTokenScopesRequired
	}
	if cmd.TTL <= 0 {
		cmd.TTL = 30 * time.Minute
	}
	plaintext, err := admintoken.GeneratePlaintext()
	if err != nil {
		return CreateResult{}, fmt.Errorf("admin token: generate plaintext: %w", err)
	}
	hash := admintoken.HashPlaintext(plaintext)
	id := admintoken.TokenID(s.idgen.NewULID())
	expiresAt := s.clock.Now().Add(cmd.TTL)

	in := admintoken.NewAdminTokenInput{
		ID:        id,
		Owner:     cmd.Owner,
		Scopes:    cmd.Scopes,
		ValueHash: hash,
		CreatedAt: s.clock.Now(),
		CreatedBy: cmd.CreatedBy,
		IsEnroll:  true,
		ExpiresAt: &expiresAt,
		WorkerID:  cmd.WorkerID,
	}
	if strings.TrimSpace(cmd.WorkerID) != "" && s.masterKey != nil {
		ct, nonce, encErr := s.masterKey.Encrypt([]byte(plaintext))
		if encErr != nil {
			return CreateResult{}, fmt.Errorf("admin token: encrypt plaintext: %w", encErr)
		}
		in.PlaintextCiphertext = ct
		in.PlaintextNonce = nonce
	}
	t, err := admintoken.New(in)
	if err != nil {
		return CreateResult{}, err
	}
	if err := s.repo.Save(ctx, t); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{ID: id, Plaintext: plaintext}, nil
}

// ShowInstallTokenResult is the lookup payload for an active enroll
// token. Plaintext is the decrypted bearer the install command needs.
type ShowInstallTokenResult struct {
	ID        admintoken.TokenID
	Plaintext string
	ExpiresAt time.Time
}

// ErrShowInstallNoMasterKey signals that the service was constructed
// without WithMasterKey, so no enroll token has stored ciphertext —
// callers should surface a "server not configured for show-install"
// hint rather than a generic 401.
var ErrShowInstallNoMasterKey = errors.New("admin token: show-install-command requires a master key (set secret_management.master_key_file)")

// ShowInstallToken returns the active enroll token's decrypted
// plaintext for workerID, suitable for re-displaying the install
// command in the Web Console. Returns:
//   - ErrTokenNotFound: no active enroll token for this worker (or
//     it was burned/expired/revoked, or plaintext was never stored)
//   - ErrTokenExpired: row found but the TTL has elapsed
//   - ErrShowInstallNoMasterKey: service has no master key
//
// v2.5-B2 (#50).
func (s *Service) ShowInstallToken(ctx context.Context, workerID string) (ShowInstallTokenResult, error) {
	if s.masterKey == nil {
		return ShowInstallTokenResult{}, ErrShowInstallNoMasterKey
	}
	t, err := s.repo.FindActiveEnrollByWorkerID(ctx, workerID)
	if err != nil {
		return ShowInstallTokenResult{}, err
	}
	if t.IsExpired(s.clock.Now()) {
		return ShowInstallTokenResult{}, admintoken.ErrTokenExpired
	}
	if !t.HasShowablePlaintext() {
		return ShowInstallTokenResult{}, admintoken.ErrTokenNotFound
	}
	plain, err := s.masterKey.Decrypt(t.PlaintextCiphertext(), t.PlaintextNonce())
	if err != nil {
		return ShowInstallTokenResult{}, fmt.Errorf("admin token: decrypt plaintext: %w", err)
	}
	exp := time.Time{}
	if e := t.ExpiresAt(); e != nil {
		exp = *e
	}
	return ShowInstallTokenResult{
		ID:        t.ID(),
		Plaintext: string(plain),
		ExpiresAt: exp,
	}, nil
}

// ConsumeEnrollToken burns an enroll token (idempotent at the repo
// CAS level). Returns ErrTokenConsumed if already used, ErrTokenNotFound
// if id doesn't exist or isn't an enroll token. Middleware calls this
// after VerifyPlaintext succeeds for an enroll token.
func (s *Service) ConsumeEnrollToken(ctx context.Context, id admintoken.TokenID) error {
	return s.repo.ConsumeEnrollToken(ctx, id, s.clock.Now().UTC().Format(time.RFC3339Nano))
}

// FindByID is a thin pass-through for the list/revoke CLI surface.
func (s *Service) FindByID(ctx context.Context, id admintoken.TokenID) (*admintoken.AdminToken, error) {
	return s.repo.FindByID(ctx, id)
}

// FindAll returns every token (revoked included) for the `list` CLI.
func (s *Service) FindAll(ctx context.Context) ([]*admintoken.AdminToken, error) {
	return s.repo.FindAll(ctx)
}

// RevokeCommand captures the revoke arguments.
type RevokeCommand struct {
	ID     admintoken.TokenID
	By     string
	Reason string
}

// Revoke marks the token revoked. Loads first to get the version for
// optimistic concurrency.
func (s *Service) Revoke(ctx context.Context, cmd RevokeCommand) error {
	t, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}
	if t.IsRevoked() {
		return admintoken.ErrTokenRevoked
	}
	return s.repo.Revoke(ctx, cmd.ID, cmd.By, cmd.Reason, t.Version())
}

// MarkUsedAsync schedules a non-blocking last_used_at bump. Failure is
// swallowed — this is bookkeeping, never a request blocker. The write
// is offloaded to a serialized pump (one goroutine) so concurrent calls
// don't trigger SQLite write-lock contention with the request's own
// foreground tx.
func (s *Service) MarkUsedAsync(id admintoken.TokenID) {
	if s == nil {
		return
	}
	ch := s.markUsedCh
	if ch == nil {
		return // Close() was called
	}
	// Per-id throttle: drop if we wrote this id less than
	// markUsedThrottle ago. The audit use case (operator viewing list)
	// has no need for sub-minute precision, but the writes are hot
	// enough under worker-daemon polling (5/sec) to contend with
	// foreground request transactions on SQLite.
	s.muLastMark.Lock()
	now := s.clock.Now()
	if last, ok := s.lastMarkUsed[id]; ok && now.Sub(last) < markUsedThrottle {
		s.muLastMark.Unlock()
		return
	}
	s.lastMarkUsed[id] = now
	s.muLastMark.Unlock()
	defer func() {
		// If Close() runs concurrently and closes the channel between
		// our nil-check and the send, the send panics. Swallow it.
		_ = recover()
	}()
	select {
	case ch <- id:
	default:
		// Channel full: drop the update. Bookkeeping is best-effort;
		// blocking the request thread to write a timestamp is the
		// wrong tradeoff.
	}
}

// Static guard. Used by tests via errors.Is.
var ErrServiceMisconfigured = errors.New("admin token service: nil dep")
