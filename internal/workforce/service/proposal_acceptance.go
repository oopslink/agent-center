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

// ProposalAcceptanceService owns the Propose/Accept/Ignore/Unignore
// lifecycle of WorkerProjectProposal (plan § 3.4.3 + workforce/03 § 4).
//
// Accept is the cross-aggregate path: in one tx it writes Proposal +
// Project (if new) + Mapping + 3-4 events (ADR-0014 § 2).
type ProposalAcceptanceService struct {
	db           *sql.DB
	proposalRepo workforce.WorkerProjectProposalRepository
	mappingRepo  workforce.WorkerProjectMappingRepository
	projectRepo  workforce.ProjectRepository
	discovery    *ProjectDiscoveryService
	sink         *observability.EventSink
	idgen        idgen.Generator
	clock        clock.Clock
}

// NewProposalAcceptanceService constructs the service.
func NewProposalAcceptanceService(
	db *sql.DB,
	proposalRepo workforce.WorkerProjectProposalRepository,
	mappingRepo workforce.WorkerProjectMappingRepository,
	projectRepo workforce.ProjectRepository,
	discovery *ProjectDiscoveryService,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
) *ProposalAcceptanceService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ProposalAcceptanceService{
		db:           db,
		proposalRepo: proposalRepo,
		mappingRepo:  mappingRepo,
		projectRepo:  projectRepo,
		discovery:    discovery,
		sink:         sink,
		idgen:        gen,
		clock:        clk,
	}
}

// ProposeCommand is the CLI input for `worker proposal propose`.
// Phase 1 CLI directly triggers this (plan § 6 R6: no worker-side scan
// loop in Phase 1).
type ProposeCommand struct {
	WorkerID           workforce.WorkerID
	CandidatePath      string
	SuggestedProjectID workforce.ProjectID
	CandidateMetadata  workforce.CandidateMetadata
	Actor              observability.Actor
}

// ProposeResult is what the service returns.
type ProposeResult struct {
	ProposalID   workforce.ProposalID
	EventID      observability.EventID
	AlreadyExists bool // true when an active pending proposal for the
	// (worker, candidate_path) pair existed and we returned its id
	// without emitting a new event.
}

// Propose registers a new Proposal in pending state, or returns the
// existing pending one if (worker_id, candidate_path) already has one.
//
// Returns ProposalAlreadyExists=true when an existing active pending
// proposal is reused; no extra event is emitted in that case.
func (s *ProposalAcceptanceService) Propose(ctx context.Context, cmd ProposeCommand) (ProposeResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return ProposeResult{}, err
	}
	if existing, err := s.proposalRepo.FindByCandidatePath(ctx, cmd.WorkerID, cmd.CandidatePath); err == nil {
		return ProposeResult{ProposalID: existing.ID(), AlreadyExists: true}, nil
	} else if !errors.Is(err, workforce.ErrProposalNotFound) {
		return ProposeResult{}, err
	}
	p, err := workforce.NewWorkerProjectProposal(workforce.NewProposalInput{
		ID:                 workforce.ProposalID(s.idgen.NewULID()),
		WorkerID:           cmd.WorkerID,
		CandidatePath:      cmd.CandidatePath,
		SuggestedProjectID: cmd.SuggestedProjectID,
		CandidateMetadata:  cmd.CandidateMetadata,
		ProposedAt:         s.clock.Now(),
	})
	if err != nil {
		return ProposeResult{}, err
	}
	var evID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.proposalRepo.Save(txCtx, p); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker_project_proposal.proposed",
			Refs: observability.EventRefs{
				ProposalID: string(p.ID()),
				WorkerID:   string(p.WorkerID()),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"proposal_id":          string(p.ID()),
				"worker_id":            string(p.WorkerID()),
				"candidate_path":       p.CandidatePath(),
				"suggested_project_id": string(p.SuggestedProjectID()),
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	if err != nil {
		return ProposeResult{}, err
	}
	return ProposeResult{ProposalID: p.ID(), EventID: evID}, nil
}

// AcceptCommand is the CLI input.
//
// v2.5.5 dropped OverrideKind alongside ProjectKind. OverrideProjectID
// is empty by default (server-gen new id at Accept time); supplied to
// claim an existing project instead.
type AcceptCommand struct {
	ProposalID          workforce.ProposalID
	OverrideProjectID   workforce.ProjectID
	OverrideProjectName string
	Actor               observability.Actor
}

// AcceptResult captures what happened.
type AcceptResult struct {
	ProposalID     workforce.ProposalID
	MappingID      workforce.MappingID
	ProjectID      workforce.ProjectID
	ProjectCreated bool
	EventIDs       []observability.EventID
}

// Accept consumes a pending Proposal: same tx writes Proposal status +
// Project (create if new) + Mapping + 3-4 events.
func (s *ProposalAcceptanceService) Accept(ctx context.Context, cmd AcceptCommand) (AcceptResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return AcceptResult{}, err
	}
	var res AcceptResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		p, err := s.proposalRepo.FindByID(txCtx, cmd.ProposalID)
		if err != nil {
			return err
		}
		if p.Status() != workforce.ProposalPending {
			return workforce.ErrProposalAlreadyTerminated
		}
		projectID := cmd.OverrideProjectID
		if projectID == "" {
			projectID = p.SuggestedProjectID()
		}
		name := cmd.OverrideProjectName
		if name == "" {
			name = string(projectID) // sensible default; user can update later
		}
		ensureRes, err := s.discovery.EnsureProject(txCtx, EnsureProjectInput{
			ID:        projectID,
			Name:      name,
			CreatedBy: cmd.Actor,
		})
		if err != nil {
			return err
		}
		projectID = ensureRes.Project.ID()
		// Pre-check: no active mapping for (worker, project) — enforced by
		// DB unique index too, but checking here lets us return a clean
		// domain error before INSERT.
		if existing, ferr := s.mappingRepo.FindByWorkerAndProject(txCtx, p.WorkerID(), projectID); ferr == nil && existing != nil {
			return workforce.ErrMappingAlreadyActive
		} else if ferr != nil && !errors.Is(ferr, workforce.ErrMappingNotFound) {
			return ferr
		}
		mappingID := workforce.MappingID(s.idgen.NewULID())
		m, err := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
			ID:               mappingID,
			WorkerID:         p.WorkerID(),
			ProjectID:        projectID,
			BasePath:         p.CandidatePath(),
			SourceProposalID: p.ID(),
			AddedAt:          s.clock.Now(),
		})
		if err != nil {
			return err
		}
		if err := s.mappingRepo.Save(txCtx, m); err != nil {
			return err
		}
		if err := p.Accept(s.clock.Now(), string(cmd.Actor), mappingID); err != nil {
			return err
		}
		if err := s.proposalRepo.Update(txCtx, p); err != nil {
			return err
		}
		mappingEvID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker_project_mapping.added",
			Refs: observability.EventRefs{
				MappingID:  string(m.ID()),
				WorkerID:   string(m.WorkerID()),
				ProjectID:  string(m.ProjectID()),
				ProposalID: string(m.SourceProposalID()),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"mapping_id": string(m.ID()),
				"worker_id":  string(m.WorkerID()),
				"project_id": string(m.ProjectID()),
				"base_path":  m.BasePath(),
			},
		})
		if err != nil {
			return err
		}
		acceptedEvID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker_project_proposal.accepted",
			Refs: observability.EventRefs{
				ProposalID: string(p.ID()),
				WorkerID:   string(p.WorkerID()),
				MappingID:  string(mappingID),
				ProjectID:  string(projectID),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"proposal_id":          string(p.ID()),
				"resulting_mapping_id": string(mappingID),
			},
		})
		if err != nil {
			return err
		}
		evs := []observability.EventID{mappingEvID, acceptedEvID}
		if ensureRes.Created && ensureRes.EventID != "" {
			evs = append([]observability.EventID{ensureRes.EventID}, evs...)
		}
		res = AcceptResult{
			ProposalID:     p.ID(),
			MappingID:      mappingID,
			ProjectID:      projectID,
			ProjectCreated: ensureRes.Created,
			EventIDs:       evs,
		}
		return nil
	})
	if err != nil {
		return AcceptResult{}, err
	}
	return res, nil
}

// IgnoreCommand is the CLI input.
type IgnoreCommand struct {
	ProposalID workforce.ProposalID
	Actor      observability.Actor
}

// Ignore transitions a pending Proposal to ignored.
func (s *ProposalAcceptanceService) Ignore(ctx context.Context, cmd IgnoreCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		p, err := s.proposalRepo.FindByID(txCtx, cmd.ProposalID)
		if err != nil {
			return err
		}
		if err := p.Ignore(s.clock.Now(), string(cmd.Actor)); err != nil {
			return err
		}
		if err := s.proposalRepo.Update(txCtx, p); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker_project_proposal.ignored",
			Refs: observability.EventRefs{
				ProposalID: string(p.ID()),
				WorkerID:   string(p.WorkerID()),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"proposal_id": string(p.ID()),
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

// Unignore transitions an ignored Proposal back to pending.
func (s *ProposalAcceptanceService) Unignore(ctx context.Context, cmd IgnoreCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		p, err := s.proposalRepo.FindByID(txCtx, cmd.ProposalID)
		if err != nil {
			return err
		}
		if err := p.Unignore(s.clock.Now()); err != nil {
			return err
		}
		if err := s.proposalRepo.Update(txCtx, p); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker_project_proposal.unignored",
			Refs: observability.EventRefs{
				ProposalID: string(p.ID()),
				WorkerID:   string(p.WorkerID()),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"proposal_id": string(p.ID()),
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

// ErrServiceMisconfigured signals missing deps.
var ErrServiceMisconfigured = errors.New("workforce: service missing dependency")

// guard ensures all collaborators are set. Used by tests / startup.
func (s *ProposalAcceptanceService) guard() error {
	if s == nil || s.db == nil || s.proposalRepo == nil || s.mappingRepo == nil ||
		s.projectRepo == nil || s.discovery == nil || s.sink == nil || s.idgen == nil {
		return fmt.Errorf("%w: ProposalAcceptanceService", ErrServiceMisconfigured)
	}
	return nil
}
