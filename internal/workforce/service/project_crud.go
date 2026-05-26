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

// ProjectCRUDService backs `agent-center project {add,update,remove}` CLI
// handlers. Each method runs in its own tx, emits the matching domain
// event, and enforces invariants.
//
// v2.5.5 (task #59) — Project.ID is now server-generated (proj-<8hex>)
// and the kind / default_agent_cli fields were dropped in favour of a
// free-text tags list. See migration 0032 + Project AR for the new
// shape.
type ProjectCRUDService struct {
	db          *sql.DB
	projectRepo workforce.ProjectRepository
	mappingRepo workforce.WorkerProjectMappingRepository
	sink        *observability.EventSink
	clock       clock.Clock
	newID       func() (workforce.ProjectID, error)
}

// NewProjectCRUDService constructs the service.
func NewProjectCRUDService(
	db *sql.DB,
	projectRepo workforce.ProjectRepository,
	mappingRepo workforce.WorkerProjectMappingRepository,
	sink *observability.EventSink,
	clk clock.Clock,
) *ProjectCRUDService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ProjectCRUDService{
		db:          db,
		projectRepo: projectRepo,
		mappingRepo: mappingRepo,
		sink:        sink,
		clock:       clk,
		newID:       workforce.NewProjectID,
	}
}

// WithIDGenerator swaps the project id generator (tests inject a
// deterministic source).
func (s *ProjectCRUDService) WithIDGenerator(gen func() (workforce.ProjectID, error)) *ProjectCRUDService {
	if gen != nil {
		s.newID = gen
	}
	return s
}

// AddCommand wraps `project add` flags.
//
// v2.5.5: ID is normally server-generated. The optional ID field is
// kept for the cross-aggregate Accept path (which may want to bind a
// proposal to a pre-named project id) and for tests with stable
// fixture ids. Empty ID → generate via NewProjectID.
type AddCommand struct {
	ID          workforce.ProjectID
	Name        string
	Description string
	Tags        []string
	Actor       observability.Actor
}

// AddResult returns the created project + emit event id.
type AddResult struct {
	Project *workforce.Project
	EventID observability.EventID
}

// Add creates a new Project and emits `workforce.project.created`.
func (s *ProjectCRUDService) Add(ctx context.Context, cmd AddCommand) (AddResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return AddResult{}, err
	}
	id := cmd.ID
	if id == "" {
		gen, err := s.newID()
		if err != nil {
			return AddResult{}, fmt.Errorf("project: generate id: %w", err)
		}
		id = gen
	}
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID:                  id,
		Name:                cmd.Name,
		Description:         cmd.Description,
		Tags:                cmd.Tags,
		CreatedByIdentityID: string(cmd.Actor),
		CreatedAt:           s.clock.Now(),
	})
	if err != nil {
		return AddResult{}, err
	}
	var evID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.projectRepo.Save(txCtx, p); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.project.created",
			Refs:      observability.EventRefs{ProjectID: string(p.ID())},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"project_id": string(p.ID()),
				"name":       p.Name(),
				"tags":       p.Tags(),
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	if err != nil {
		return AddResult{}, err
	}
	return AddResult{Project: p, EventID: evID}, nil
}

// UpdateCommand wraps `project update` flags.
type UpdateCommand struct {
	ID      workforce.ProjectID
	Version int
	Fields  workforce.ProjectUpdateFields
	Actor   observability.Actor
}

// UpdateResult returns the updated project.
type UpdateResult struct {
	Project *workforce.Project
	EventID observability.EventID
}

// Update applies the project update via CAS, emits `workforce.project.updated`.
func (s *ProjectCRUDService) Update(ctx context.Context, cmd UpdateCommand) (UpdateResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return UpdateResult{}, err
	}
	if cmd.Fields.IsEmpty() {
		return UpdateResult{}, errors.New("project update: no field changes")
	}
	var (
		updated *workforce.Project
		evID    observability.EventID
	)
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		p, err := s.projectRepo.Update(txCtx, cmd.ID, cmd.Fields, cmd.Version, s.clock.Now())
		if err != nil {
			return err
		}
		updated = p
		changed := changedFields(cmd.Fields)
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.project.updated",
			Refs:      observability.EventRefs{ProjectID: string(cmd.ID)},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"project_id":     string(cmd.ID),
				"changed_fields": changed,
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	if err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Project: updated, EventID: evID}, nil
}

// RemoveCommand wraps `project remove`.
type RemoveCommand struct {
	ID    workforce.ProjectID
	Actor observability.Actor
}

// Remove deletes a Project after checking it has no active mappings.
func (s *ProjectCRUDService) Remove(ctx context.Context, cmd RemoveCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if _, err := s.projectRepo.FindByID(txCtx, cmd.ID); err != nil {
			return err
		}
		n, err := s.mappingRepo.CountActiveByProjectID(txCtx, cmd.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: %d active mappings", workforce.ErrProjectHasActiveDeps, n)
		}
		if err := s.projectRepo.Delete(txCtx, cmd.ID); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.project.removed",
			Refs:      observability.EventRefs{ProjectID: string(cmd.ID)},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"project_id": string(cmd.ID),
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

func changedFields(f workforce.ProjectUpdateFields) []string {
	var out []string
	if f.Name != nil {
		out = append(out, "name")
	}
	if f.Description != nil {
		out = append(out, "description")
	}
	if f.Tags != nil {
		out = append(out, "tags")
	}
	return out
}
