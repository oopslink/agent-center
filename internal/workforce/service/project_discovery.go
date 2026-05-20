package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// ProjectDiscoveryService offers a thin helper used by
// ProposalAcceptanceService when accepting a Proposal whose target Project
// may or may not exist yet (plan § 3.4.2).
//
// Phase 1 scope: only `EnsureProject` is implemented. Worker-side scan loop
// + candidate metadata harvesting is out of scope (plan § 6 R6).
type ProjectDiscoveryService struct {
	repo  workforce.ProjectRepository
	sink  *observability.EventSink
	clock clock.Clock
}

// NewProjectDiscoveryService constructs the service.
func NewProjectDiscoveryService(repo workforce.ProjectRepository, sink *observability.EventSink, clk clock.Clock) *ProjectDiscoveryService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ProjectDiscoveryService{repo: repo, sink: sink, clock: clk}
}

// EnsureProjectInput captures the args.
type EnsureProjectInput struct {
	ID             workforce.ProjectID
	Name           string
	Kind           workforce.ProjectKind
	CreatedBy      observability.Actor
	DefaultAgentCLI string
}

// EnsureProjectResult tracks whether the project was created (true) or
// already existed (false).
type EnsureProjectResult struct {
	Project *workforce.Project
	Created bool
	EventID observability.EventID
}

// EnsureProject returns the existing Project if found; otherwise creates
// one and emits `workforce.project.created` in the same tx.
//
// IMPORTANT: ctx MUST carry a *sql.Tx (workforce/00 § 5 + ADR-0014 § 2).
// We don't BeginTx here — caller's tx boundary is required so we can share
// it with the wider proposal-accept flow.
func (s *ProjectDiscoveryService) EnsureProject(ctx context.Context, in EnsureProjectInput) (EnsureProjectResult, error) {
	if _, ok := persistence.TxFromCtx(ctx); !ok {
		return EnsureProjectResult{}, fmt.Errorf("EnsureProject: ctx must carry a tx (caller owns tx boundary)")
	}
	if err := in.CreatedBy.Validate(); err != nil {
		return EnsureProjectResult{}, err
	}
	existing, err := s.repo.FindByID(ctx, in.ID)
	if err == nil {
		return EnsureProjectResult{Project: existing, Created: false}, nil
	}
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		return EnsureProjectResult{}, err
	}
	// Create new project.
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID:                  in.ID,
		Name:                in.Name,
		Kind:                in.Kind,
		DefaultAgentCLI:     in.DefaultAgentCLI,
		CreatedByIdentityID: string(in.CreatedBy),
		CreatedAt:           s.clock.Now(),
	})
	if err != nil {
		return EnsureProjectResult{}, err
	}
	if err := s.repo.Save(ctx, p); err != nil {
		return EnsureProjectResult{}, err
	}
	evID, err := s.sink.Emit(ctx, observability.EmitCommand{
		EventType: "workforce.project.created",
		Refs:      observability.EventRefs{ProjectID: string(p.ID())},
		Actor:     in.CreatedBy,
		Payload: map[string]any{
			"project_id": string(p.ID()),
			"name":       p.Name(),
			"kind":       string(p.Kind()),
		},
	})
	if err != nil {
		return EnsureProjectResult{}, err
	}
	return EnsureProjectResult{Project: p, Created: true, EventID: evID}, nil
}
