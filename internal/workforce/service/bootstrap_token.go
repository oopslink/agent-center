package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// DefaultBootstrapTokenTTL is the v2 enroll-token TTL (ADR-0023 § 2).
const DefaultBootstrapTokenTTL = 30 * time.Minute

// BootstrapTokenService manages BootstrapToken lifecycle (ADR-0023 § 2).
//
// Plaintext returned ONLY in IssueResult / ReissueResult; never persisted.
type BootstrapTokenService struct {
	db    *sql.DB
	repo  workforce.BootstrapTokenRepository
	gen   idgen.Generator
	sink  *observability.EventSink
	clock clock.Clock
	ttl   time.Duration
}

// NewBootstrapTokenService wires the service. ttl=0 → DefaultBootstrapTokenTTL.
func NewBootstrapTokenService(db *sql.DB, repo workforce.BootstrapTokenRepository, gen idgen.Generator, sink *observability.EventSink, clk clock.Clock, ttl time.Duration) *BootstrapTokenService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if ttl <= 0 {
		ttl = DefaultBootstrapTokenTTL
	}
	return &BootstrapTokenService{db: db, repo: repo, gen: gen, sink: sink, clock: clk, ttl: ttl}
}

// IssueCommand inputs.
type IssueCommand struct {
	WorkerID      workforce.WorkerID
	ActorIdentity observability.Actor
}

// IssueResult — TokenValue is the plaintext returned to the caller ONCE.
type IssueResult struct {
	TokenID    workforce.BootstrapTokenID
	TokenValue string
	WorkerID   workforce.WorkerID
	ExpiresAt  time.Time
	EventID    observability.EventID
}

// Issue mints a fresh active BootstrapToken for the worker. Rejects if the
// worker already has an active token (callers should Reissue instead).
func (s *BootstrapTokenService) Issue(ctx context.Context, cmd IssueCommand) (IssueResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return IssueResult{}, fmt.Errorf("issue: %w", err)
	}
	if err := validateWorkerIDArg(cmd.WorkerID); err != nil {
		return IssueResult{}, err
	}
	plain, err := generateTokenPlaintext()
	if err != nil {
		return IssueResult{}, err
	}
	now := s.clock.Now()
	id := workforce.BootstrapTokenID(s.gen.NewULID())
	tok, err := workforce.NewBootstrapToken(workforce.NewBootstrapTokenInput{
		ID:        id,
		WorkerID:  cmd.WorkerID,
		ValueHash: workforce.HashTokenValue(plain),
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
		CreatedBy: cmd.ActorIdentity.String(),
	})
	if err != nil {
		return IssueResult{}, err
	}
	var eventID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, tok); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.bootstrap_token.issued",
			Refs:      observability.EventRefs{WorkerID: string(tok.WorkerID())},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"token_id":   string(tok.ID()),
				"worker_id":  string(tok.WorkerID()),
				"expires_at": tok.ExpiresAt().UTC().Format(time.RFC3339Nano),
				"created_by": tok.CreatedBy(),
			},
		})
		if err != nil {
			return err
		}
		eventID = evID
		return nil
	})
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{
		TokenID:    tok.ID(),
		TokenValue: plain,
		WorkerID:   tok.WorkerID(),
		ExpiresAt:  tok.ExpiresAt(),
		EventID:    eventID,
	}, nil
}

// ReissueCommand inputs.
type ReissueCommand struct {
	WorkerID      workforce.WorkerID
	ActorIdentity observability.Actor
}

// ReissueResult — NewTokenValue plaintext returned once.
type ReissueResult struct {
	NewTokenID         workforce.BootstrapTokenID
	NewTokenValue      string
	OldTokenID         workforce.BootstrapTokenID
	OldStatusAtReissue workforce.BootstrapTokenStatus
	WorkerID           workforce.WorkerID
	ExpiresAt          time.Time
	EventID            observability.EventID
}

// Reissue mints a fresh token, revoking any prior active token in the same
// transaction. Returns ErrBootstrapTokenAlreadyUsed if the most recent token
// is in `used` state (per ADR-0023 § 2 reissue rules table).
func (s *BootstrapTokenService) Reissue(ctx context.Context, cmd ReissueCommand) (ReissueResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return ReissueResult{}, fmt.Errorf("reissue: %w", err)
	}
	if err := validateWorkerIDArg(cmd.WorkerID); err != nil {
		return ReissueResult{}, err
	}
	plain, err := generateTokenPlaintext()
	if err != nil {
		return ReissueResult{}, err
	}
	now := s.clock.Now()
	newID := workforce.BootstrapTokenID(s.gen.NewULID())
	var result ReissueResult
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// Find prior active (most recent). If none, check whether worker's
		// most recent terminal state is `used` (reissue rules: used = reject).
		oldActive, err := s.repo.FindActiveByWorkerForUpdate(txCtx, cmd.WorkerID)
		if err != nil && !errors.Is(err, workforce.ErrBootstrapTokenNotFound) {
			return err
		}
		var oldID workforce.BootstrapTokenID
		var oldStatus workforce.BootstrapTokenStatus
		if oldActive != nil {
			// Revoke old active first (otherwise unique-active-per-worker
			// index will reject the new active insert).
			if err := oldActive.MarkRevoked(now, workforce.BootstrapTokenRevokedReasonReissueSuperseded, "superseded by reissue"); err != nil {
				return err
			}
			if err := s.repo.UpdateStatus(txCtx, oldActive, workforce.BootstrapTokenActive); err != nil {
				return err
			}
			oldID = oldActive.ID()
			oldStatus = workforce.BootstrapTokenActive
		} else {
			// No active. Check most recent terminal status; reject `used`.
			recent, err := s.repo.FindByWorkerID(txCtx, cmd.WorkerID,
				workforce.BootstrapTokenUsed, workforce.BootstrapTokenExpired, workforce.BootstrapTokenRevoked)
			if err != nil {
				return err
			}
			if len(recent) > 0 && recent[0].Status() == workforce.BootstrapTokenUsed {
				return workforce.ErrBootstrapTokenAlreadyUsed
			}
			if len(recent) > 0 {
				oldID = recent[0].ID()
				oldStatus = recent[0].Status()
			}
		}
		// Mint the new token.
		newTok, err := workforce.NewBootstrapToken(workforce.NewBootstrapTokenInput{
			ID:        newID,
			WorkerID:  cmd.WorkerID,
			ValueHash: workforce.HashTokenValue(plain),
			CreatedAt: now,
			ExpiresAt: now.Add(s.ttl),
			CreatedBy: cmd.ActorIdentity.String(),
		})
		if err != nil {
			return err
		}
		if err := s.repo.Save(txCtx, newTok); err != nil {
			return err
		}
		// Emit reissued event.
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.bootstrap_token.reissued",
			Refs:      observability.EventRefs{WorkerID: string(cmd.WorkerID)},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"new_token_id":          string(newTok.ID()),
				"old_token_id":          string(oldID),
				"worker_id":             string(cmd.WorkerID),
				"reissued_by":           cmd.ActorIdentity.String(),
				"old_status_at_reissue": string(oldStatus),
				"expires_at":            newTok.ExpiresAt().UTC().Format(time.RFC3339Nano),
			},
		})
		if err != nil {
			return err
		}
		result = ReissueResult{
			NewTokenID:         newTok.ID(),
			NewTokenValue:      plain,
			OldTokenID:         oldID,
			OldStatusAtReissue: oldStatus,
			WorkerID:           cmd.WorkerID,
			ExpiresAt:          newTok.ExpiresAt(),
			EventID:            evID,
		}
		return nil
	})
	if err != nil {
		return ReissueResult{}, err
	}
	return result, nil
}

// RevokeCommand inputs.
type RevokeCommand struct {
	TokenID       workforce.BootstrapTokenID
	Reason        workforce.BootstrapTokenRevokedReason
	Message       string
	ActorIdentity observability.Actor
}

// Revoke transitions an active token → revoked + emits event.
func (s *BootstrapTokenService) Revoke(ctx context.Context, cmd RevokeCommand) (observability.EventID, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return "", fmt.Errorf("revoke: %w", err)
	}
	var eventID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		tok, err := s.repo.FindByID(txCtx, cmd.TokenID)
		if err != nil {
			return err
		}
		if tok.Status() != workforce.BootstrapTokenActive {
			return workforce.ErrBootstrapTokenNotActive
		}
		now := s.clock.Now()
		if err := tok.MarkRevoked(now, cmd.Reason, cmd.Message); err != nil {
			return err
		}
		if err := s.repo.UpdateStatus(txCtx, tok, workforce.BootstrapTokenActive); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.bootstrap_token.revoked",
			Refs:      observability.EventRefs{WorkerID: string(tok.WorkerID())},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"token_id":       string(tok.ID()),
				"worker_id":      string(tok.WorkerID()),
				"revoked_by":     cmd.ActorIdentity.String(),
				"revoked_reason": string(cmd.Reason),
				"revoked_message": cmd.Message,
			},
		})
		if err != nil {
			return err
		}
		eventID = evID
		return nil
	})
	return eventID, err
}

// ScanExpiredResult reports the scan outcome.
type ScanExpiredResult struct {
	ExpiredTokenIDs []workforce.BootstrapTokenID
	EventIDs        []observability.EventID
}

// ScanExpired finds active tokens past TTL and transitions them to `expired`.
// Caller schedules this periodically (e.g. every minute).
func (s *BootstrapTokenService) ScanExpired(ctx context.Context, actor observability.Actor) (ScanExpiredResult, error) {
	if err := actor.Validate(); err != nil {
		return ScanExpiredResult{}, fmt.Errorf("scan_expired: %w", err)
	}
	var result ScanExpiredResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		now := s.clock.Now()
		expired, err := s.repo.FindExpired(txCtx, now)
		if err != nil {
			return err
		}
		for _, tok := range expired {
			if err := tok.MarkExpired(); err != nil {
				// concurrent transition raced us; skip
				continue
			}
			if err := s.repo.UpdateStatus(txCtx, tok, workforce.BootstrapTokenActive); err != nil {
				if errors.Is(err, workforce.ErrBootstrapTokenStatusConflict) {
					continue
				}
				return err
			}
			evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "workforce.worker.bootstrap_token.expired",
				Refs:      observability.EventRefs{WorkerID: string(tok.WorkerID())},
				Actor:     actor,
				Payload: map[string]any{
					"token_id":  string(tok.ID()),
					"worker_id": string(tok.WorkerID()),
				},
			})
			if err != nil {
				return err
			}
			result.ExpiredTokenIDs = append(result.ExpiredTokenIDs, tok.ID())
			result.EventIDs = append(result.EventIDs, evID)
		}
		return nil
	})
	return result, err
}

// generateTokenPlaintext returns a URL-safe base64 random string (32 bytes →
// 43 chars). High entropy; suitable for one-time enroll token.
func generateTokenPlaintext() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("bootstrap token: rand read: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func validateWorkerIDArg(id workforce.WorkerID) error {
	if string(id) == "" {
		return errors.New("bootstrap token service: worker_id required")
	}
	return nil
}
