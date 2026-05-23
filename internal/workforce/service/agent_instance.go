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
	"github.com/oopslink/agent-center/internal/workforce"
)

// AgentInstanceManagementService handles AgentInstance create / config-update /
// archive (ADR-0024 § 1 + ADR-0029).
//
// State transitions driven by external events (worker offline/online,
// task_execution lifecycle) belong to AgentInstanceLifecycleService below.
type AgentInstanceManagementService struct {
	db                *sql.DB
	repo              workforce.AgentInstanceRepository
	gen               idgen.Generator
	sink              *observability.EventSink
	clock             clock.Clock
	identityRegistrar IdentityRegistrar
}

// IdentityRegistrar is the Conversation BC IdentityRegistration port the
// AgentInstance Create path uses to keep Identity[kind=agent].id ==
// AgentInstance.id (ADR-0033 § 4 cross-aggregate invariant). Implementations
// MUST write inside the supplied tx-ctx (no own RunInTx).
type IdentityRegistrar interface {
	RegisterAgentIdentityInTx(ctx context.Context, agentInstanceID string, displayName string, actor observability.Actor) error
}

// NewAgentInstanceManagementService wires the service.
func NewAgentInstanceManagementService(db *sql.DB, repo workforce.AgentInstanceRepository, gen idgen.Generator, sink *observability.EventSink, clk clock.Clock) *AgentInstanceManagementService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &AgentInstanceManagementService{db: db, repo: repo, gen: gen, sink: sink, clock: clk}
}

// WithIdentityRegistrar enables same-tx Identity[kind=agent] auto-register
// per ADR-0033. Returns the service for chaining.
func (s *AgentInstanceManagementService) WithIdentityRegistrar(r IdentityRegistrar) *AgentInstanceManagementService {
	s.identityRegistrar = r
	return s
}

// CreateCommand inputs for non-builtin AgentInstance.
type CreateAgentInstanceCommand struct {
	Name          string
	AgentCLI      string
	WorkerID      workforce.WorkerID
	Config        string // JSON; "" defaults to "{}"
	MaxConcurrent *int
	ActorIdentity observability.Actor
}

// CreateAgentInstanceResult — created instance id + emit event id.
type CreateAgentInstanceResult struct {
	ID      workforce.AgentInstanceID
	EventID observability.EventID
}

// Create persists a non-builtin AgentInstance + emits agent_instance.created.
// Built-in supervisor is created via EnsureBuiltinSupervisor (auto-provision
// at startup); CLI cannot create built-in.
func (s *AgentInstanceManagementService) Create(ctx context.Context, cmd CreateAgentInstanceCommand) (CreateAgentInstanceResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return CreateAgentInstanceResult{}, fmt.Errorf("agent create: %w", err)
	}
	if cmd.WorkerID == "" {
		return CreateAgentInstanceResult{}, errors.New("agent instance service: worker_id required for non-builtin")
	}
	wid := cmd.WorkerID
	now := s.clock.Now()
	id := workforce.AgentInstanceID(s.gen.NewULID())
	a, err := workforce.NewAgentInstance(workforce.NewAgentInstanceInput{
		ID:            id,
		Name:          cmd.Name,
		AgentCLI:      cmd.AgentCLI,
		WorkerID:      &wid,
		Config:        cmd.Config,
		MaxConcurrent: cmd.MaxConcurrent,
		IsBuiltin:     false,
		CreatedAt:     now,
	})
	if err != nil {
		return CreateAgentInstanceResult{}, err
	}
	var resp CreateAgentInstanceResult
	resp.ID = id
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, a); err != nil {
			return err
		}
		// ADR-0033 § 4: auto-register Identity[kind=agent] with
		// id="agent:<instance_id>" in the same tx (cross-aggregate
		// invariant).
		if s.identityRegistrar != nil {
			if err := s.identityRegistrar.RegisterAgentIdentityInTx(txCtx, string(id), a.Name(), cmd.ActorIdentity); err != nil {
				return fmt.Errorf("agent create: register identity: %w", err)
			}
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.agent_instance.created",
			Refs:      observability.EventRefs{WorkerID: string(wid)},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"id":             string(id),
				"name":           a.Name(),
				"agent_cli":      a.AgentCLI(),
				"worker_id":      string(wid),
				"max_concurrent": a.MaxConcurrent(),
				"config":         a.Config(),
				"is_builtin":     false,
			},
		})
		if err != nil {
			return err
		}
		resp.EventID = evID
		return nil
	})
	if err != nil {
		return CreateAgentInstanceResult{}, err
	}
	return resp, nil
}

// UpdateConfigCommand inputs.
type UpdateAgentInstanceConfigCommand struct {
	ID            workforce.AgentInstanceID
	Config        *string // nil = unchanged
	MaxConcurrent *int    // nil = unchanged (caller passes existing)
	Version       int
	ActorIdentity observability.Actor
}

// UpdateConfig mutates config / max_concurrent + emits config_updated event.
func (s *AgentInstanceManagementService) UpdateConfig(ctx context.Context, cmd UpdateAgentInstanceConfigCommand) (observability.EventID, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return "", fmt.Errorf("agent update_config: %w", err)
	}
	if cmd.Config == nil && cmd.MaxConcurrent == nil {
		return "", errors.New("agent instance service: at least one of config / max_concurrent required")
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		a, err := s.repo.FindByID(txCtx, cmd.ID)
		if err != nil {
			return err
		}
		if a.State() == workforce.AgentInstanceArchived {
			return workforce.ErrAgentInstanceArchived
		}
		if a.Version() != cmd.Version {
			return workforce.ErrAgentInstanceVersionConflict
		}
		newConfig := a.Config()
		if cmd.Config != nil {
			newConfig = *cmd.Config
			if newConfig == "" {
				newConfig = "{}"
			}
		}
		newMax := a.MaxConcurrent()
		if cmd.MaxConcurrent != nil {
			newMax = cmd.MaxConcurrent
		}
		if err := s.repo.UpdateConfig(txCtx, cmd.ID, newConfig, newMax, cmd.Version); err != nil {
			return err
		}
		// Emit.
		fields := []string{}
		if cmd.Config != nil {
			fields = append(fields, "config")
		}
		if cmd.MaxConcurrent != nil {
			fields = append(fields, "max_concurrent")
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.agent_instance.config_updated",
			Refs:      observability.EventRefs{},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"id":             string(cmd.ID),
				"changed_fields": fields,
				"by":             cmd.ActorIdentity.String(),
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

// ArchiveCommand inputs.
type ArchiveAgentInstanceCommand struct {
	ID            workforce.AgentInstanceID
	Reason        workforce.AgentInstanceArchivedReason
	Message       string
	Version       int
	ActorIdentity observability.Actor
}

// Archive transitions the instance to archived (rejects built-in / active /
// sleeping per ADR-0024 § 9 + ADR-0029 § 5).
func (s *AgentInstanceManagementService) Archive(ctx context.Context, cmd ArchiveAgentInstanceCommand) (observability.EventID, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return "", fmt.Errorf("agent archive: %w", err)
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		now := s.clock.Now()
		// Use Repo.Archive which atomically checks state=idle + is_builtin=0
		// at the DB level; AR-level errors surface via Repo error mapping.
		if err := s.repo.Archive(txCtx, cmd.ID, now, cmd.Reason, cmd.Message, cmd.Version); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.agent_instance.archived",
			Refs:      observability.EventRefs{},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"id":              string(cmd.ID),
				"archived_by":     cmd.ActorIdentity.String(),
				"archived_reason": string(cmd.Reason),
				"archived_message": cmd.Message,
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

// EnsureBuiltinSupervisor is idempotent: inserts the built-in supervisor
// AgentInstance if not present. Called at center startup (per ADR-0029 § 2).
func (s *AgentInstanceManagementService) EnsureBuiltinSupervisor(ctx context.Context) (workforce.AgentInstanceID, error) {
	// Check existence first.
	existing, err := s.repo.FindByName(ctx, workforce.BuiltinSupervisorName)
	if err == nil {
		return existing.ID(), nil
	}
	if !errors.Is(err, workforce.ErrAgentInstanceNotFound) {
		return "", err
	}
	now := s.clock.Now()
	id := workforce.AgentInstanceID(s.gen.NewULID())
	a, err := workforce.NewAgentInstance(workforce.NewAgentInstanceInput{
		ID:        id,
		Name:      workforce.BuiltinSupervisorName,
		AgentCLI:  workforce.BuiltinSupervisorDefaultAgentCLI,
		WorkerID:  nil,
		Config:    "{}",
		IsBuiltin: true,
		CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, a); err != nil {
			// Concurrent create race: another goroutine inserted first;
			// treat as success — return existing id.
			if errors.Is(err, workforce.ErrAgentInstanceNameTaken) {
				return nil
			}
			return err
		}
		_, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.agent_instance.created",
			Refs:      observability.EventRefs{},
			Actor:     observability.Actor("system"),
			Payload: map[string]any{
				"id":         string(id),
				"name":       a.Name(),
				"agent_cli":  a.AgentCLI(),
				"is_builtin": true,
			},
		})
		return err
	})
	if err != nil {
		return "", err
	}
	// Re-find (handles race where another caller's insert won).
	final, err := s.repo.FindByName(ctx, workforce.BuiltinSupervisorName)
	if err != nil {
		return "", err
	}
	return final.ID(), nil
}

// =============================================================================
// AgentInstanceLifecycleService — state transitions driven by external events
// =============================================================================

// AgentInstanceLifecycleService transitions agent state in response to
// task_execution.* / worker.* events (ADR-0024 § 3). Caller wires this as a
// subscriber to the relevant event types.
//
// For P8 we expose the transition methods; event subscription wiring is
// done in P9 alongside dispatch / reconcile.
type AgentInstanceLifecycleService struct {
	db    *sql.DB
	repo  workforce.AgentInstanceRepository
	sink  *observability.EventSink
	clock clock.Clock
}

// NewAgentInstanceLifecycleService wires the service.
func NewAgentInstanceLifecycleService(db *sql.DB, repo workforce.AgentInstanceRepository, sink *observability.EventSink, clk clock.Clock) *AgentInstanceLifecycleService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &AgentInstanceLifecycleService{db: db, repo: repo, sink: sink, clock: clk}
}

// OnExecutionStarted: first execution begins → idle → active.
// Idempotent: no-op if already active.
func (s *AgentInstanceLifecycleService) OnExecutionStarted(ctx context.Context, id workforce.AgentInstanceID, actor observability.Actor) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		a, err := s.repo.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if a.State() != workforce.AgentInstanceIdle {
			return nil // already active or sleeping
		}
		if err := s.repo.UpdateState(txCtx, id, workforce.AgentInstanceIdle, workforce.AgentInstanceActive, a.Version()); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.agent_instance.activated",
			Refs:      observability.EventRefs{},
			Actor:     actor,
			Payload:   map[string]any{"id": string(id)},
		})
		return err
	})
}

// OnExecutionEnded: last execution ends → active → idle (only if no other
// active executions remain).
func (s *AgentInstanceLifecycleService) OnExecutionEnded(ctx context.Context, id workforce.AgentInstanceID, actor observability.Actor) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		a, err := s.repo.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if a.State() != workforce.AgentInstanceActive {
			return nil
		}
		count, err := s.repo.CountActiveExecutions(txCtx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return nil // still busy with other executions
		}
		if err := s.repo.UpdateState(txCtx, id, workforce.AgentInstanceActive, workforce.AgentInstanceIdle, a.Version()); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.agent_instance.idle",
			Refs:      observability.EventRefs{},
			Actor:     actor,
			Payload:   map[string]any{"id": string(id)},
		})
		return err
	})
}

// OnWorkerOffline: bulk transition all agents on the worker idle/active → sleeping.
func (s *AgentInstanceLifecycleService) OnWorkerOffline(ctx context.Context, workerID workforce.WorkerID, actor observability.Actor) (int, error) {
	var total int
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// Active → sleeping
		n1, err := s.repo.BulkUpdateStateByWorker(txCtx, workerID, workforce.AgentInstanceActive, workforce.AgentInstanceSleeping)
		if err != nil {
			return err
		}
		// Idle → sleeping
		n2, err := s.repo.BulkUpdateStateByWorker(txCtx, workerID, workforce.AgentInstanceIdle, workforce.AgentInstanceSleeping)
		if err != nil {
			return err
		}
		total = n1 + n2
		if total > 0 {
			_, err = s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "workforce.agent_instance.sleeping",
				Refs:      observability.EventRefs{WorkerID: string(workerID)},
				Actor:     actor,
				Payload: map[string]any{
					"worker_id":    string(workerID),
					"agent_count":  total,
				},
			})
		}
		return err
	})
	return total, err
}

// OnWorkerOnline: bulk transition all sleeping agents on the worker → idle.
func (s *AgentInstanceLifecycleService) OnWorkerOnline(ctx context.Context, workerID workforce.WorkerID, actor observability.Actor) (int, error) {
	var total int
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		n, err := s.repo.BulkUpdateStateByWorker(txCtx, workerID, workforce.AgentInstanceSleeping, workforce.AgentInstanceIdle)
		if err != nil {
			return err
		}
		total = n
		if total > 0 {
			_, err = s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "workforce.agent_instance.awakened",
				Refs:      observability.EventRefs{WorkerID: string(workerID)},
				Actor:     actor,
				Payload: map[string]any{
					"worker_id":   string(workerID),
					"agent_count": total,
				},
			})
		}
		return err
	})
	return total, err
}
