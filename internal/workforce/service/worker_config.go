package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// WorkerConfigService handles Worker behavior config mutations (ADR-0023 § 3).
//
// The actual long-connection push to the worker daemon is decoupled via an
// EventSink subscription (workforce.worker.config.updated). P9 wires the
// daemon-side handler that re-pulls config on event receipt.
type WorkerConfigService struct {
	db    *sql.DB
	repo  workforce.WorkerRepository
	sink  *observability.EventSink
	clock clock.Clock
}

// NewWorkerConfigService constructs the service.
func NewWorkerConfigService(db *sql.DB, repo workforce.WorkerRepository, sink *observability.EventSink, clk clock.Clock) *WorkerConfigService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WorkerConfigService{db: db, repo: repo, sink: sink, clock: clk}
}

// SetConfigCommand inputs.
type SetConfigCommand struct {
	WorkerID      workforce.WorkerID
	Concurrency   *workforce.WorkerConcurrency
	Discovery     *workforce.WorkerDiscovery
	Version       int // optimistic lock (caller reads worker first)
	ActorIdentity observability.Actor
}

// SetConfigResult — EventID identifies the emitted worker.config.updated event.
type SetConfigResult struct {
	WorkerID workforce.WorkerID
	NewVersion int
	EventID  observability.EventID
}

// SetConfig persists new concurrency / discovery config and emits the
// `workforce.worker.config.updated` event in one tx. At least one of
// Concurrency / Discovery must be non-nil.
func (s *WorkerConfigService) SetConfig(ctx context.Context, cmd SetConfigCommand) (SetConfigResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return SetConfigResult{}, fmt.Errorf("set_config: %w", err)
	}
	if string(cmd.WorkerID) == "" {
		return SetConfigResult{}, errors.New("worker config service: worker_id required")
	}
	if cmd.Concurrency == nil && cmd.Discovery == nil {
		return SetConfigResult{}, errors.New("worker config service: at least one of concurrency / discovery required")
	}
	changed := changedConfigFields(cmd.Concurrency, cmd.Discovery)
	var resp SetConfigResult
	resp.WorkerID = cmd.WorkerID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.UpdateConfig(txCtx, cmd.WorkerID, workforce.WorkerConfigFields{
			Concurrency: cmd.Concurrency,
			Discovery:   cmd.Discovery,
		}, cmd.Version); err != nil {
			return err
		}
		// Re-read for new version.
		w, err := s.repo.FindByID(txCtx, cmd.WorkerID)
		if err != nil {
			return err
		}
		resp.NewVersion = w.Version()
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.config.updated",
			Refs:      observability.EventRefs{WorkerID: string(cmd.WorkerID)},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"worker_id":      string(cmd.WorkerID),
				"changed_fields": changed,
				"by":             cmd.ActorIdentity.String(),
			},
		})
		if err != nil {
			return err
		}
		resp.EventID = evID
		return nil
	})
	if err != nil {
		return SetConfigResult{}, err
	}
	return resp, nil
}

// SetCapabilityEnabledCommand inputs for toggling a capability's Enabled flag.
type SetCapabilityEnabledCommand struct {
	WorkerID      workforce.WorkerID
	AgentCLI      string
	Enabled       bool
	Version       int
	ActorIdentity observability.Actor
}

// SetCapabilityEnabled toggles `Enabled` for a capability. Returns
// ErrWorkerCapabilityNotFound if the CLI is not in the worker's detected list.
func (s *WorkerConfigService) SetCapabilityEnabled(ctx context.Context, cmd SetCapabilityEnabledCommand) (SetConfigResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return SetConfigResult{}, fmt.Errorf("set_capability_enabled: %w", err)
	}
	if string(cmd.WorkerID) == "" || cmd.AgentCLI == "" {
		return SetConfigResult{}, errors.New("worker config service: worker_id + agent_cli required")
	}
	var resp SetConfigResult
	resp.WorkerID = cmd.WorkerID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		w, err := s.repo.FindByID(txCtx, cmd.WorkerID)
		if err != nil {
			return err
		}
		if w.Version() != cmd.Version {
			return workforce.ErrWorkerVersionConflict
		}
		// Build modified capability list.
		caps := w.CapabilityList()
		found := false
		for i := range caps {
			if caps[i].AgentCLI == cmd.AgentCLI {
				caps[i].Enabled = cmd.Enabled
				found = true
				break
			}
		}
		if !found {
			return workforce.ErrWorkerCapabilityNotFound
		}
		if err := s.repo.ReplaceCapabilities(txCtx, cmd.WorkerID, caps, cmd.Version); err != nil {
			return err
		}
		// Re-read for version.
		w2, err := s.repo.FindByID(txCtx, cmd.WorkerID)
		if err != nil {
			return err
		}
		resp.NewVersion = w2.Version()
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.capability.updated",
			Refs:      observability.EventRefs{WorkerID: string(cmd.WorkerID)},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"worker_id": string(cmd.WorkerID),
				"agent_cli": cmd.AgentCLI,
				"enabled":   cmd.Enabled,
				"by":        cmd.ActorIdentity.String(),
			},
		})
		if err != nil {
			return err
		}
		resp.EventID = evID
		return nil
	})
	if err != nil {
		return SetConfigResult{}, err
	}
	return resp, nil
}

func changedConfigFields(c *workforce.WorkerConcurrency, d *workforce.WorkerDiscovery) []string {
	out := []string{}
	if c != nil {
		out = append(out, "concurrency")
	}
	if d != nil {
		out = append(out, "discovery")
	}
	return out
}
