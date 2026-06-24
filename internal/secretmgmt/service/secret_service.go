// Package service hosts the SecretManagement BC domain services
// (UserSecretService + SecretResolutionService).
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// UserSecretService manages user secret CRUD (ADR-0026 § 2).
//
// Plaintext flows ONLY through method args (Create / Rotate) — never stored
// on the service or written to logs / events.
type UserSecretService struct {
	db        *sql.DB
	repo      secretmgmt.UserSecretRepository
	gen       idgen.Generator
	sink      *observability.EventSink
	clock     clock.Clock
	masterKey *secretmgmt.MasterKey
}

// NewUserSecretService wires the service. masterKey is required for
// Create / Rotate; nil masterKey causes both to return ErrMasterKeyNotLoaded.
func NewUserSecretService(db *sql.DB, repo secretmgmt.UserSecretRepository, gen idgen.Generator, sink *observability.EventSink, clk clock.Clock, masterKey *secretmgmt.MasterKey) *UserSecretService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &UserSecretService{db: db, repo: repo, gen: gen, sink: sink, clock: clk, masterKey: masterKey}
}

// CreateSecretCommand inputs.
type CreateSecretCommand struct {
	Name           string
	Kind           secretmgmt.UserSecretKind
	Plaintext      []byte // wiped from caller after Create returns
	OrganizationID string // v2.6: scopes the secret to an org (multi-tenant isolation)
	ActorIdentity  observability.Actor
}

// CreateSecretResult — only metadata; plaintext NOT echoed back.
type CreateSecretResult struct {
	ID      secretmgmt.UserSecretID
	Name    string
	EventID observability.EventID
}

// Create encrypts plaintext + inserts a new active UserSecret + emits
// user_secret.created (without plaintext).
func (s *UserSecretService) Create(ctx context.Context, cmd CreateSecretCommand) (CreateSecretResult, error) {
	if s.masterKey == nil {
		return CreateSecretResult{}, secretmgmt.ErrMasterKeyNotLoaded
	}
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return CreateSecretResult{}, fmt.Errorf("secret create: %w", err)
	}
	if len(cmd.Plaintext) == 0 {
		return CreateSecretResult{}, errors.New("secret create: plaintext required")
	}
	ciphertext, nonce, err := s.masterKey.Encrypt(cmd.Plaintext)
	if err != nil {
		return CreateSecretResult{}, err
	}
	now := s.clock.Now()
	id := secretmgmt.UserSecretID(s.gen.NewULID())
	sec, err := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID:             id,
		Name:           cmd.Name,
		Kind:           cmd.Kind,
		Ciphertext:     ciphertext,
		Nonce:          nonce,
		OrganizationID: cmd.OrganizationID,
		CreatedAt:      now,
		CreatedBy:      cmd.ActorIdentity.String(),
	})
	if err != nil {
		return CreateSecretResult{}, err
	}
	var resp CreateSecretResult
	resp.ID = id
	resp.Name = cmd.Name
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, sec); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "secretmgmt.user_secret.created",
			Refs:      observability.EventRefs{},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"id":   string(id),
				"name": cmd.Name,
				"kind": string(cmd.Kind),
				"by":   cmd.ActorIdentity.String(),
				// plaintext NOT included
			},
		})
		if err != nil {
			return err
		}
		resp.EventID = evID
		return nil
	})
	return resp, err
}

// RotateSecretCommand inputs.
type RotateSecretCommand struct {
	ID            secretmgmt.UserSecretID
	NewPlaintext  []byte
	Version       int
	ActorIdentity observability.Actor
}

// Rotate replaces ciphertext/nonce in place; state stays active; bumps version
// + rotated_at. Emits user_secret.rotated.
func (s *UserSecretService) Rotate(ctx context.Context, cmd RotateSecretCommand) (observability.EventID, error) {
	if s.masterKey == nil {
		return "", secretmgmt.ErrMasterKeyNotLoaded
	}
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return "", fmt.Errorf("secret rotate: %w", err)
	}
	if len(cmd.NewPlaintext) == 0 {
		return "", errors.New("secret rotate: new plaintext required")
	}
	ciphertext, nonce, err := s.masterKey.Encrypt(cmd.NewPlaintext)
	if err != nil {
		return "", err
	}
	var evID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		now := s.clock.Now()
		if err := s.repo.UpdateValue(txCtx, cmd.ID, ciphertext, nonce, now, cmd.Version); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "secretmgmt.user_secret.rotated",
			Refs:      observability.EventRefs{},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"id": string(cmd.ID),
				"by": cmd.ActorIdentity.String(),
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	return evID, err
}

// RevokeSecretCommand inputs.
type RevokeSecretCommand struct {
	ID            secretmgmt.UserSecretID
	Reason        secretmgmt.UserSecretRevokedReason
	Message       string
	Version       int
	ActorIdentity observability.Actor
}

// Revoke transitions active → revoked + emits user_secret.revoked.
func (s *UserSecretService) Revoke(ctx context.Context, cmd RevokeSecretCommand) (observability.EventID, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return "", fmt.Errorf("secret revoke: %w", err)
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		now := s.clock.Now()
		if err := s.repo.UpdateState(txCtx, cmd.ID,
			secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
			now, cmd.ActorIdentity.String(), cmd.Reason, cmd.Message, cmd.Version); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "secretmgmt.user_secret.revoked",
			Refs:      observability.EventRefs{},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"id":              string(cmd.ID),
				"revoked_by":      cmd.ActorIdentity.String(),
				"revoked_reason":  string(cmd.Reason),
				"revoked_message": cmd.Message,
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	return evID, err
}

// =============================================================================
// SecretResolutionService — worker daemon caller
// =============================================================================

// SecretResolutionService decrypts secrets just-in-time for worker daemons
// spawning agents (ADR-0026 § 7).
type SecretResolutionService struct {
	db        *sql.DB
	repo      secretmgmt.UserSecretRepository
	sink      *observability.EventSink
	clock     clock.Clock
	masterKey *secretmgmt.MasterKey
}

// NewSecretResolutionService wires the service.
func NewSecretResolutionService(db *sql.DB, repo secretmgmt.UserSecretRepository, sink *observability.EventSink, clk clock.Clock, masterKey *secretmgmt.MasterKey) *SecretResolutionService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &SecretResolutionService{db: db, repo: repo, sink: sink, clock: clk, masterKey: masterKey}
}

// ResolveRequest inputs.
type ResolveRequest struct {
	SecretName  string
	CallerActor observability.Actor // worker:<worker_id> for worker daemon
}

// ResolveResponse — plaintext returned ONCE, caller must wipe after use.
type ResolveResponse struct {
	ID        secretmgmt.UserSecretID
	Name      string
	Plaintext []byte
}

// Resolve looks up secret by name + verifies state=active + decrypts.
// Bumps last_used_at + emits user_secret.accessed (without plaintext).
// On denial (revoked / not active), emits user_secret.access_denied in a
// separate tx so the audit trail persists even though Resolve returns an error.
func (s *SecretResolutionService) Resolve(ctx context.Context, req ResolveRequest) (ResolveResponse, error) {
	if s.masterKey == nil {
		return ResolveResponse{}, secretmgmt.ErrMasterKeyNotLoaded
	}
	if err := req.CallerActor.Validate(); err != nil {
		return ResolveResponse{}, fmt.Errorf("secret resolve: %w", err)
	}
	if req.SecretName == "" {
		return ResolveResponse{}, errors.New("secret resolve: name required")
	}
	// First lookup outside tx — fast path for state check + decryption.
	sec, err := s.repo.FindByName(ctx, req.SecretName)
	if err != nil {
		return ResolveResponse{}, err
	}
	if sec.State() != secretmgmt.UserSecretActive {
		// Persist audit event in its own tx so it survives the denial.
		if emitErr := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			_, e := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "secretmgmt.user_secret.access_denied",
				Refs:      observability.EventRefs{},
				Actor:     req.CallerActor,
				Payload: map[string]any{
					"id":      string(sec.ID()),
					"name":    sec.Name(),
					"reason":  "not_active",
					"message": "secret state is " + string(sec.State()) + "; resolve rejected",
					"state":   string(sec.State()),
				},
			})
			return e
		}); emitErr != nil {
			return ResolveResponse{}, fmt.Errorf("secret resolve: emit access_denied: %w", emitErr)
		}
		return ResolveResponse{}, secretmgmt.ErrUserSecretRevoked
	}
	plain, err := s.masterKey.Decrypt(sec.Ciphertext(), sec.Nonce())
	if err != nil {
		return ResolveResponse{}, err
	}
	// Update last_used_at + emit accessed event in one tx.
	var resp ResolveResponse
	resp.ID = sec.ID()
	resp.Name = sec.Name()
	resp.Plaintext = plain
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		now := s.clock.Now()
		if err := s.repo.UpdateLastUsedAt(txCtx, sec.ID(), now); err != nil {
			return err
		}
		_, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "secretmgmt.user_secret.accessed",
			Refs:      observability.EventRefs{},
			Actor:     req.CallerActor,
			Payload: map[string]any{
				"id":   string(sec.ID()),
				"name": sec.Name(),
				"kind": string(sec.Kind()),
				// plaintext NOT included
			},
		})
		return err
	})
	if err != nil {
		// Zero plaintext on error before returning.
		for i := range resp.Plaintext {
			resp.Plaintext[i] = 0
		}
		return ResolveResponse{}, err
	}
	return resp, nil
}
