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

// MemberResolver validates that a member ref points at a real identity — of the
// kind the ref prefix claims — that belongs to the given org. It is the seam the
// application layer wires from the identity BC so AddMember can reject dangling
// refs. Optional: a nil resolver makes AddMember skip the existence check
// (degrade for tests / deployments that have not wired the identity directory —
// preserving the pre-hardening behavior rather than failing closed).
type MemberResolver interface {
	// MemberExists reports whether ref resolves to a real identity of the matching
	// kind that is a joined member of orgID. A well-formed but nonexistent,
	// cross-org, or kind-mismatched ref returns (false, nil).
	MemberExists(ctx context.Context, orgID string, ref team.MemberRef) (bool, error)
}

// Service implements the Team use cases over a team.Repository.
type Service struct {
	repo     team.Repository
	db       *sql.DB
	idgen    idgen.Generator
	clock    clock.Clock
	resolver MemberResolver
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

// WithMemberResolver wires the identity existence check used by AddMember and
// returns the receiver for chaining at construction. Left unset (nil), AddMember
// skips the check. Production wires it (admin_wiring); tests opt in per-case.
func (s *Service) WithMemberResolver(r MemberResolver) *Service {
	s.resolver = r
	return s
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
		// Write-path invariant (hardening): the ref must resolve to a real identity
		// of the matching kind that is a member of THIS team's org. Without it any
		// client (web facade OR the MCP add_member tool — both funnel through this
		// one method) could persist a dangling / cross-org / kind-mismatched ref
		// into team_members. The identity reads run on the tx executor (ctx-bound),
		// so they observe the same snapshot as the insert.
		if s.resolver != nil {
			exists, err := s.resolver.MemberExists(ctx, t.OrgID(), ref)
			if err != nil {
				return err
			}
			if !exists {
				return team.ErrMemberIdentityNotFound
			}
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

// MoveMember atomically migrates a member ref from fromTeam to toTeam under the
// destination role. The removal and the re-add happen in ONE tx: because the ref
// leaves fromTeam before the exclusivity check runs, a legitimate agent migration
// no longer self-trips ErrAgentAlreadyInTeam (the 409 the naive
// AddMember-into-second-team path hits). Atomicity is the point — a
// remove-then-add split across two calls could strand the agent in neither team
// if the add failed. Migration is held to the SAME hardening as AddMember (the ref
// must resolve to a real, matching-kind, same-org identity) and the SAME role
// declaration on the destination. Errors: ErrTeamNotFound (either team),
// ErrRoleNotDeclared (destination), ErrMemberIdentityNotFound (unresolvable ref),
// ErrMemberNotFound (ref not on fromTeam — e.g. a stale migrate_from),
// ErrAgentAlreadyInTeam (agent still bound to a THIRD team).
func (s *Service) MoveMember(ctx context.Context, fromTeam, toTeam team.TeamID, ref team.MemberRef, role string) (*team.TeamMember, error) {
	kind, err := ref.Kind()
	if err != nil {
		return nil, err
	}
	var member *team.TeamMember
	err = persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		to, err := s.repo.GetTeam(ctx, toTeam)
		if err != nil {
			return err
		}
		if !to.HasRole(role) {
			return team.ErrRoleNotDeclared
		}
		// Same write-path hardening as AddMember: reject a dangling / cross-org /
		// kind-mismatched ref BEFORE mutating anything.
		if s.resolver != nil {
			exists, err := s.resolver.MemberExists(ctx, to.OrgID(), ref)
			if err != nil {
				return err
			}
			if !exists {
				return team.ErrMemberIdentityNotFound
			}
		}
		// Free the ref from the old team first. Absent → ErrMemberNotFound (a stale
		// / wrong migrate_from), rolling the whole migration back rather than
		// silently duplicating the membership.
		if err := s.repo.RemoveMember(ctx, fromTeam, ref); err != nil {
			return err
		}
		// After removal the agent-exclusivity check should pass; if the agent is
		// somehow still bound to a THIRD team, refuse rather than duplicate.
		if kind == team.MemberKindAgent {
			if existing, ok, err := s.repo.FindAgentTeam(ctx, ref); err != nil {
				return err
			} else if ok && existing != toTeam {
				return team.ErrAgentAlreadyInTeam
			}
		}
		m := &team.TeamMember{
			TeamID:    toTeam,
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

// ListMembersByTeams returns the members of ALL the given teams in ONE batched
// read — the whole-org membership rollup behind the directory endpoints, which
// would otherwise cost one read per team.
func (s *Service) ListMembersByTeams(ctx context.Context, ids []team.TeamID) ([]*team.TeamMember, error) {
	return s.repo.ListMembersByTeams(ctx, ids)
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

// DisassociateProject unlinks a project from a team. ErrTeamNotFound if the team
// is absent, ErrProjectNotAssociated if the link does not exist.
func (s *Service) DisassociateProject(ctx context.Context, id team.TeamID, projectID string) error {
	if projectID == "" {
		return team.ErrInvalidProject
	}
	return persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		if _, err := s.repo.GetTeam(ctx, id); err != nil {
			return err
		}
		return s.repo.DisassociateProject(ctx, id, projectID)
	})
}

// ListProjects returns a team's associated projects.
func (s *Service) ListProjects(ctx context.Context, id team.TeamID) ([]*team.TeamProject, error) {
	return s.repo.ListProjects(ctx, id)
}
