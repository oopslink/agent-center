// Package service hosts the Team BC application service: the CRUD + membership
// + project-association use cases that back the agent tools (design §4). Each
// multi-step write runs under persistence.RunInTx so pre-check and mutation are
// atomic — critical for the agent-exclusivity rule, which must not race.
package service

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/team"
)

// Service implements the Team use cases over a team.Repository.
type Service struct {
	repo  team.Repository
	db    *sql.DB
	idgen idgen.Generator
	clock clock.Clock
}

// New constructs a Service. db is used to open transactions (RunInTx); pass the
// same *sql.DB the repo was built on. A nil clock defaults to SystemClock; a nil
// idgen defaults to a fresh ULID generator.
func New(repo team.Repository, db *sql.DB, gen idgen.Generator, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if gen == nil {
		gen = idgen.NewGenerator(clk)
	}
	return &Service{repo: repo, db: db, idgen: gen, clock: clk}
}

// CreateTeamInput is the create_team tool payload.
type CreateTeamInput struct {
	OrgID       string
	Name        string
	Description string
	// Roles are the template-declared roles for the team (design §9).
	Roles []team.RoleConfig
}

// CreateTeam creates a team and its declared roles atomically.
func (s *Service) CreateTeam(ctx context.Context, in CreateTeamInput) (*team.Team, error) {
	t, err := team.NewTeam(team.NewTeamInput{
		ID:          team.TeamID(s.idgen.NewEntityID("team")),
		OrgID:       in.OrgID,
		Name:        in.Name,
		Description: in.Description,
		Roles:       in.Roles,
		CreatedAt:   s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	if err := persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		return s.repo.CreateTeam(ctx, t)
	}); err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateTeamInput is the update_team tool payload. Nil fields are left unchanged.
type UpdateTeamInput struct {
	Name        *string
	Description *string
}

// UpdateTeam mutates name/description of an existing team.
func (s *Service) UpdateTeam(ctx context.Context, id team.TeamID, in UpdateTeamInput) (*team.Team, error) {
	var updated *team.Team
	err := persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		t, err := s.repo.GetTeam(ctx, id)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		if in.Name != nil {
			if err := t.Rename(*in.Name, now); err != nil {
				return err
			}
		}
		if in.Description != nil {
			t.SetDescription(*in.Description, now)
		}
		if err := s.repo.UpdateTeam(ctx, t); err != nil {
			return err
		}
		updated = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteTeam removes a team (cascading its roles/members/projects). Idempotent.
func (s *Service) DeleteTeam(ctx context.Context, id team.TeamID) error {
	return persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		return s.repo.DeleteTeam(ctx, id)
	})
}

// GetTeam loads a team (with roles). ErrTeamNotFound if absent.
func (s *Service) GetTeam(ctx context.Context, id team.TeamID) (*team.Team, error) {
	return s.repo.GetTeam(ctx, id)
}

// ListTeams lists teams in an org ("" → all orgs).
func (s *Service) ListTeams(ctx context.Context, orgID string) ([]*team.Team, error) {
	return s.repo.ListTeams(ctx, orgID)
}

// AddMember adds a member under a declared role, enforcing agent exclusivity.
// The whole check-then-insert runs in one tx so two concurrent adds of the same
// agent cannot both pass the pre-check.
func (s *Service) AddMember(ctx context.Context, id team.TeamID, ref team.MemberRef, role string) (*team.TeamMember, error) {
	kind, err := ref.Kind()
	if err != nil {
		return nil, err
	}
	var member *team.TeamMember
	err = persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		t, err := s.repo.GetTeam(ctx, id)
		if err != nil {
			return err
		}
		if !t.HasRole(role) {
			return team.ErrRoleNotDeclared
		}
		if kind == team.MemberKindAgent {
			existing, ok, err := s.repo.FindAgentTeam(ctx, ref)
			if err != nil {
				return err
			}
			if ok {
				if existing == id {
					return team.ErrMemberAlreadyInTeam
				}
				return team.ErrAgentAlreadyInTeam
			}
		}
		m := &team.TeamMember{
			TeamID:    id,
			Ref:       ref,
			Kind:      kind,
			Role:      role,
			CreatedAt: s.clock.Now(),
		}
		if err := s.repo.AddMember(ctx, m); err != nil {
			return err
		}
		member = m
		return nil
	})
	if err != nil {
		return nil, err
	}
	return member, nil
}

// RemoveMember removes a member from a team. ErrMemberNotFound if absent.
func (s *Service) RemoveMember(ctx context.Context, id team.TeamID, ref team.MemberRef) error {
	return persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		return s.repo.RemoveMember(ctx, id, ref)
	})
}

// ListMembers returns a team's members.
func (s *Service) ListMembers(ctx context.Context, id team.TeamID) ([]*team.TeamMember, error) {
	return s.repo.ListMembers(ctx, id)
}

// AssociateProject links a project to a team.
func (s *Service) AssociateProject(ctx context.Context, id team.TeamID, projectID string) error {
	if projectID == "" {
		return team.ErrInvalidProject
	}
	return persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		if _, err := s.repo.GetTeam(ctx, id); err != nil {
			return err
		}
		return s.repo.AssociateProject(ctx, id, projectID)
	})
}

// ListProjects returns a team's associated projects.
func (s *Service) ListProjects(ctx context.Context, id team.TeamID) ([]*team.TeamProject, error) {
	return s.repo.ListProjects(ctx, id)
}
