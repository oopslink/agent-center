// Package service hosts Workforce BC domain services
// (workforce/00 § 3 + plan-1 § 3.4).
package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// WorkerEnrollService enrolls a new Worker into the system
// (workforce/00 § 3.1, plan § 3.4.1).
//
// Phase 1 simplification (plan § 6 R5): we skip real bootstrap/session
// token exchange. The CLI hands us a WorkerID + capabilities; we Save the
// Worker and emit `workforce.worker.enrolled` in the same tx.
type WorkerEnrollService struct {
	db        *sql.DB
	repo      workforce.WorkerRepository
	tokenRepo workforce.BootstrapTokenRepository // optional; nil disables Exchange path
	sink      *observability.EventSink
	clock     clock.Clock
}

// NewWorkerEnrollService constructs the service (v1 path; no Exchange).
func NewWorkerEnrollService(db *sql.DB, repo workforce.WorkerRepository, sink *observability.EventSink, clk clock.Clock) *WorkerEnrollService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WorkerEnrollService{db: db, repo: repo, sink: sink, clock: clk}
}

// NewWorkerEnrollServiceV2 wires the v2 exchange-based service
// (ADR-0023 § 1). tokenRepo is required for Exchange to function.
func NewWorkerEnrollServiceV2(db *sql.DB, repo workforce.WorkerRepository, tokenRepo workforce.BootstrapTokenRepository, sink *observability.EventSink, clk clock.Clock) *WorkerEnrollService {
	s := NewWorkerEnrollService(db, repo, sink, clk)
	s.tokenRepo = tokenRepo
	return s
}

// EnrollCommand captures the CLI input.
type EnrollCommand struct {
	WorkerID       workforce.WorkerID
	// Name is the operator-facing friendly label set at enroll time
	// (v2.4-D-X1 @oopslink). Empty falls back to WorkerID inside the
	// Worker AR.
	Name           string
	Capabilities   []string
	ActorIdentity  observability.Actor
}

// EnrollResult is what the service returns.
type EnrollResult struct {
	WorkerID workforce.WorkerID
	EventID  observability.EventID
	Version  int
}

// Enroll persists the worker and emits a domain event in one tx.
//
// v2.5-B1: idempotent when the worker was pre-created by Add() at
// mint-enroll time. The flow becomes:
//   - worker not found        → legacy path: NewWorker + Save (create)
//   - worker found, offline   → update capabilities only (claim path)
//   - worker found, online    → ErrWorkerAlreadyExists (real re-enroll
//                                of a live worker is rejected so a
//                                second daemon can't silently shadow
//                                the first; operator must Remove first)
//
// Either branch emits workforce.worker.enrolled — the event semantics
// is "daemon successfully checked in for the first time", independent
// of whether a row was pre-created at mint time.
func (s *WorkerEnrollService) Enroll(ctx context.Context, cmd EnrollCommand) (EnrollResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return EnrollResult{}, fmt.Errorf("enroll: %w", err)
	}
	existing, ferr := s.repo.FindByID(ctx, cmd.WorkerID)
	if ferr != nil && !errors.Is(ferr, workforce.ErrWorkerNotFound) {
		return EnrollResult{}, ferr
	}
	if existing != nil {
		if existing.Status() == workforce.WorkerOnline {
			return EnrollResult{}, workforce.ErrWorkerAlreadyExists
		}
		return s.claimPreEnrolled(ctx, existing, cmd)
	}
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:           cmd.WorkerID,
		Name:         cmd.Name,
		Capabilities: cmd.Capabilities,
		EnrolledAt:   s.clock.Now(),
	})
	if err != nil {
		return EnrollResult{}, err
	}
	var (
		eventID observability.EventID
	)
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, w); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.enrolled",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"worker_id":    string(w.ID()),
				"capabilities": w.Capabilities(),
			},
		})
		if err != nil {
			return err
		}
		eventID = evID
		return nil
	})
	if err != nil {
		return EnrollResult{}, err
	}
	return EnrollResult{
		WorkerID: w.ID(),
		EventID:  eventID,
		Version:  w.Version(),
	}, nil
}

// claimPreEnrolled handles the v2.5-B1 "worker pre-created at mint
// time" path. The Worker AR already exists with status=offline; the
// daemon now reports its probed capabilities. We update the capability
// list (replacing whatever Add() seeded — typically empty) and emit
// workforce.worker.enrolled. The first Heartbeat after this will
// transition status offline → online (per the existing B8 fix).
func (s *WorkerEnrollService) claimPreEnrolled(ctx context.Context, w *workforce.Worker, cmd EnrollCommand) (EnrollResult, error) {
	caps := buildCapabilitiesForClaim(cmd.Capabilities)
	var eventID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if len(caps) > 0 {
			if err := s.repo.UpdateCapabilities(txCtx, w.ID(), caps, w.Version()); err != nil {
				return err
			}
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.enrolled",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"worker_id":    string(w.ID()),
				"capabilities": cmd.Capabilities,
			},
		})
		if err != nil {
			return err
		}
		eventID = evID
		return nil
	})
	if err != nil {
		return EnrollResult{}, err
	}
	version := w.Version()
	if len(caps) > 0 {
		version++ // UpdateCapabilities bumps version on success
	}
	return EnrollResult{
		WorkerID: w.ID(),
		EventID:  eventID,
		Version:  version,
	}, nil
}

// buildCapabilitiesForClaim promotes a []string list of CLI names to
// the Capability VO form the repo expects (detected + enabled). Empty
// input → nil so the claim path skips the no-op UpdateCapabilities
// write (avoids a version bump on a no-information claim).
func buildCapabilitiesForClaim(names []string) []workforce.Capability {
	if len(names) == 0 {
		return nil
	}
	out := make([]workforce.Capability, 0, len(names))
	for _, n := range names {
		out = append(out, workforce.Capability{AgentCLI: n, Detected: true, Enabled: true})
	}
	return out
}

// AddWorkerCommand is the input to AddWorker — the v2.5 "create
// worker row at mint time" path. status=offline,
// last_heartbeat_at=null. The Worker becomes visible in Fleet
// immediately; the daemon's later Enroll() claims this row.
type AddWorkerCommand struct {
	WorkerID       workforce.WorkerID
	Name           string
	OrganizationID string // v2.6: scopes the worker to an org
	ActorIdentity  observability.Actor
}

// AddWorkerResult mirrors EnrollResult for symmetry.
type AddWorkerResult struct {
	WorkerID workforce.WorkerID
	EventID  observability.EventID
	Version  int
}

// RemoveWorkerCommand drops a Worker AR and lets SSE notify Fleet
// to retire the row from the table. v2.5-B4 (#52). Caller is
// responsible for revoking any cross-BC tokens (admin tokens bound
// to the worker) before / after — the workforce BC only owns the
// Worker AR itself.
type RemoveWorkerCommand struct {
	WorkerID      workforce.WorkerID
	ActorIdentity observability.Actor
	// Reason is the operator-supplied audit string ("removed via Fleet
	// UI", "tenant teardown", etc.). Embedded in the emitted event
	// payload for auditability.
	Reason string
}

// RemoveWorkerResult mirrors the other service result shapes.
type RemoveWorkerResult struct {
	WorkerID workforce.WorkerID
	EventID  observability.EventID
}

// RemoveWorker drops the Worker AR + emits workforce.worker.removed.
// Returns ErrWorkerNotFound if the id doesn't match. v2.5-B4 (#52).
func (s *WorkerEnrollService) RemoveWorker(ctx context.Context, cmd RemoveWorkerCommand) (RemoveWorkerResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return RemoveWorkerResult{}, fmt.Errorf("remove worker: %w", err)
	}
	if cmd.WorkerID == "" {
		return RemoveWorkerResult{}, errors.New("workforce.remove_worker: worker_id required")
	}
	var eventID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Delete(txCtx, cmd.WorkerID); err != nil {
			return err
		}
		payload := map[string]any{
			"worker_id": string(cmd.WorkerID),
		}
		// conventions § 16: payload.reason requires paired payload.message
		// (the message gives operators a human-readable string; the
		// reason is the machine-categorisable why-code). Skip the pair
		// when no operator-supplied reason was passed.
		if reason := strings.TrimSpace(cmd.Reason); reason != "" {
			payload["reason"] = "operator_removed"
			payload["message"] = reason
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.removed",
			Refs:      observability.EventRefs{WorkerID: string(cmd.WorkerID)},
			Actor:     cmd.ActorIdentity,
			Payload:   payload,
		})
		if err != nil {
			return err
		}
		eventID = evID
		return nil
	})
	if err != nil {
		return RemoveWorkerResult{}, err
	}
	return RemoveWorkerResult{
		WorkerID: cmd.WorkerID,
		EventID:  eventID,
	}, nil
}

// AddWorker creates a Worker row at mint-enroll time so Fleet sees
// it offline before the operator runs the install command on the
// worker machine. Emits workforce.worker.added (distinct from
// enrolled, which marks the daemon's first successful check-in).
//
// v2.5-B1 per #agent-center:5f8a6f7e — "添加是逻辑动作 = 创建记录
// status=offline；用户在机器上 install 后 worker 上线时 update status".
func (s *WorkerEnrollService) AddWorker(ctx context.Context, cmd AddWorkerCommand) (AddWorkerResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return AddWorkerResult{}, fmt.Errorf("add worker: %w", err)
	}
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             cmd.WorkerID,
		Name:           cmd.Name,
		OrganizationID: cmd.OrganizationID,
		EnrolledAt:     s.clock.Now(),
	})
	if err != nil {
		return AddWorkerResult{}, err
	}
	var eventID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, w); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.added",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"worker_id": string(w.ID()),
				"name":      w.Name(),
			},
		})
		if err != nil {
			return err
		}
		eventID = evID
		return nil
	})
	if err != nil {
		return AddWorkerResult{}, err
	}
	return AddWorkerResult{
		WorkerID: w.ID(),
		EventID:  eventID,
		Version:  w.Version(),
	}, nil
}

// Static error so callers can detect the "service uninitialised" case via
// errors.Is. Tests rely on this rather than string matching.
var ErrEnrollMisconfigured = errors.New("workforce: enroll service misconfigured (nil dep)")

// HeartbeatCommand is the input for a per-tick worker liveness ping.
// v2.3-1 (task #24): replaces the v2.2 workaround where the worker
// daemon re-called Enroll and swallowed the 409 already_exists as the
// success signal. This dedicated path lets the daemon assert "I'm
// alive" without abusing the create-only Enroll semantics.
type HeartbeatCommand struct {
	WorkerID                 workforce.WorkerID
	AdditionalWorkingSeconds int64
}

// Heartbeat advances last_seen_at + accumulated working seconds for an
// already-enrolled worker. No event is emitted on the steady-state
// tick (heartbeats are noisy by design — per-tick events would flood
// the audit log).
//
// v2.4-D-X1 fix B8: if the worker is currently `offline` (either
// because it was just enrolled — `NewWorker` defaults to offline —
// or because the reconciler marked it offline after a heartbeat
// stall) we transition to `online` and emit `workforce.worker.online`
// in the same tx. PD's acceptance saw `last_heartbeat_at` advancing
// while status stuck on offline; the Modal said Online and the Fleet
// table said offline. Without an explicit transition there was no
// path from the initial offline state to online.
func (s *WorkerEnrollService) Heartbeat(ctx context.Context, cmd HeartbeatCommand) error {
	if cmd.WorkerID == "" {
		return errors.New("workforce.heartbeat: worker_id required")
	}
	if cmd.AdditionalWorkingSeconds < 0 {
		return errors.New("workforce.heartbeat: working_seconds delta must be >= 0")
	}
	w, err := s.repo.FindByID(ctx, cmd.WorkerID)
	if err != nil {
		// Surface ErrWorkerNotFound (or the sqlite equivalent) untouched
		// so the caller can drive the re-enroll branch on a cold center.
		return err
	}
	now := s.clock.Now()
	if w.Status() != workforce.WorkerOnline {
		// Status transition: CAS via UpdateStatus + event emit, all
		// in one tx with the heartbeat update. Save() can't be used
		// here — it's INSERT-only for fresh AR creation; existing
		// rows go through the dedicated Update* hot paths.
		return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			if err := s.repo.UpdateStatus(txCtx, w.ID(), w.Status(), workforce.WorkerOnline, w.Version()); err != nil {
				return err
			}
			if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "workforce.worker.online",
				Refs:      observability.EventRefs{WorkerID: string(w.ID())},
				Actor:     observability.Actor("worker:" + string(w.ID())),
				Payload: map[string]any{
					"worker_id": string(w.ID()),
					"online_at": now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
				},
			}); err != nil {
				return err
			}
			return s.repo.UpdateLastHeartbeatAt(txCtx, cmd.WorkerID, now, cmd.AdditionalWorkingSeconds)
		})
	}
	return s.repo.UpdateLastHeartbeatAt(ctx, cmd.WorkerID, now, cmd.AdditionalWorkingSeconds)
}

// RenameCommand carries the inputs to Rename. Actor identifies who
// performed the rename for the audit event.
type RenameCommand struct {
	WorkerID workforce.WorkerID
	Name     string
	Actor    observability.Actor
}

// Rename mutates the worker's friendly label. Emits
// `workforce.worker.renamed` so SSE consumers (Fleet view) refresh.
// Returns workforce.ErrWorkerNotFound if id is unknown.
// v2.4-D-X1 (@oopslink ask).
func (s *WorkerEnrollService) Rename(ctx context.Context, cmd RenameCommand) error {
	if cmd.WorkerID == "" {
		return errors.New("workforce.rename: worker_id required")
	}
	w, err := s.repo.FindByID(ctx, cmd.WorkerID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	if err := w.SetName(now, cmd.Name); err != nil {
		return err
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.UpdateName(txCtx, w.ID(), w.Name(), w.Version()-1); err != nil {
			return err
		}
		_, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.renamed",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"worker_id": string(w.ID()),
				"name":      w.Name(),
			},
		})
		return err
	})
}

// =============================================================================
// v2 Exchange-based Enroll (ADR-0023 § 1)
// =============================================================================

// ExchangeRequest is the v2 enroll payload from worker daemon → center
// (per ADR-0023 § 1).
type ExchangeRequest struct {
	TokenValue    string
	WorkerID      workforce.WorkerID
	Capabilities  []workforce.Capability // worker auto-probe at enroll time
	ActorIdentity observability.Actor
}

// ExchangeResponse is what the worker daemon receives on successful enroll.
// SessionToken is opaque to the center; the worker persists it locally and
// presents it on subsequent long-connect heartbeats.
type ExchangeResponse struct {
	WorkerID     workforce.WorkerID
	SessionToken string
	Version      int
	// EventID of the worker.enrolled event (workforce.worker.enrolled).
	EnrolledEventID observability.EventID
	// UsedEventID of the workforce.worker.bootstrap_token.used event.
	UsedEventID observability.EventID
}

// Sentinel errors specific to exchange.
var (
	// ErrExchangeWorkerIDMismatch is returned when the supplied worker_id does
	// not match the worker_id the token was issued for.
	ErrExchangeWorkerIDMismatch = errors.New("workforce: enroll exchange worker_id does not match token's worker_id")
	// ErrExchangeTokenExpired is returned when the token TTL has elapsed.
	ErrExchangeTokenExpired = errors.New("workforce: enroll exchange token expired")
	// ErrEnrollServiceNoTokenRepo signals that Exchange was called on a
	// service that wasn't wired with a BootstrapTokenRepository.
	ErrEnrollServiceNoTokenRepo = errors.New("workforce: enroll service has no token repository (v1-only construction)")
)

// Exchange validates the supplied BootstrapToken and atomically:
//   1. marks the token used,
//   2. creates the Worker (first-time enroll) — re-enroll of an existing
//      worker_id is rejected because each token is single-use and bound to a
//      specific worker_id,
//   3. emits workforce.worker.bootstrap_token.used + workforce.worker.enrolled,
//   4. returns an opaque session_token for the worker to persist locally.
//
// Validation order (per ADR-0023 § 2 + plan § 3.3 step-1):
//   - token exists (FindByValueHash) → not found → ErrBootstrapTokenNotFound
//   - token.status == active → otherwise ErrBootstrapTokenNotActive
//   - token.expires_at > now → otherwise ErrExchangeTokenExpired
//   - token.worker_id == cmd.WorkerID → otherwise ErrExchangeWorkerIDMismatch
//   - worker not already enrolled → otherwise ErrWorkerAlreadyExists
//
// All side-effects happen in one tx.
func (s *WorkerEnrollService) Exchange(ctx context.Context, req ExchangeRequest) (ExchangeResponse, error) {
	if s.tokenRepo == nil {
		return ExchangeResponse{}, ErrEnrollServiceNoTokenRepo
	}
	if err := req.ActorIdentity.Validate(); err != nil {
		return ExchangeResponse{}, fmt.Errorf("exchange: %w", err)
	}
	if req.TokenValue == "" {
		return ExchangeResponse{}, errors.New("workforce: enroll exchange token_value required")
	}
	if string(req.WorkerID) == "" {
		return ExchangeResponse{}, errors.New("workforce: enroll exchange worker_id required")
	}
	hash := workforce.HashTokenValue(req.TokenValue)
	now := s.clock.Now()
	sessionToken, err := generateSessionToken()
	if err != nil {
		return ExchangeResponse{}, err
	}
	var resp ExchangeResponse
	resp.WorkerID = req.WorkerID
	resp.SessionToken = sessionToken
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		tok, err := s.tokenRepo.FindByValueHash(txCtx, hash)
		if err != nil {
			return err
		}
		if tok.Status() != workforce.BootstrapTokenActive {
			return workforce.ErrBootstrapTokenNotActive
		}
		if tok.IsExpiredAt(now) {
			return ErrExchangeTokenExpired
		}
		if tok.WorkerID() != req.WorkerID {
			return ErrExchangeWorkerIDMismatch
		}
		// Mark token used.
		if err := tok.MarkUsed(now); err != nil {
			return err
		}
		if err := s.tokenRepo.UpdateStatus(txCtx, tok, workforce.BootstrapTokenActive); err != nil {
			return err
		}
		// Create Worker.
		w, err := workforce.NewWorker(workforce.NewWorkerInput{
			ID:             req.WorkerID,
			CapabilityList: req.Capabilities,
			EnrolledAt:     now,
		})
		if err != nil {
			return err
		}
		if err := s.repo.Save(txCtx, w); err != nil {
			return err
		}
		resp.Version = w.Version()
		// Emit events.
		usedEvID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.bootstrap_token.used",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     req.ActorIdentity,
			Payload: map[string]any{
				"token_id":  string(tok.ID()),
				"worker_id": string(w.ID()),
				"used_at":   now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
			},
		})
		if err != nil {
			return err
		}
		enrolledEvID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.enrolled",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     req.ActorIdentity,
			Payload: map[string]any{
				"worker_id":    string(w.ID()),
				"capabilities": w.Capabilities(),
			},
		})
		if err != nil {
			return err
		}
		resp.UsedEventID = usedEvID
		resp.EnrolledEventID = enrolledEvID
		return nil
	})
	if err != nil {
		return ExchangeResponse{}, err
	}
	return resp, nil
}

// generateSessionToken returns a high-entropy opaque string.
func generateSessionToken() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("workforce: session_token rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
