package team

import "context"

// Repository is the persistence port for the Team BC. Implementations are
// tx-aware via persistence.ExecutorFromCtx (the service composes multi-step
// writes under persistence.RunInTx). Method contracts map storage failures onto
// the sentinels in errors.go.
type Repository interface {
	// CreateTeam inserts a team plus its declared roles atomically. Returns
	// ErrTeamNameTaken on an org+name collision.
	CreateTeam(ctx context.Context, t *Team) error
	// UpdateTeam persists name/description/version changes for an existing team.
	// Returns ErrTeamNotFound if the id is absent, ErrTeamNameTaken on collision.
	UpdateTeam(ctx context.Context, t *Team) error
	// DeleteTeam removes a team and cascades its roles/members/projects.
	// Idempotent: a missing id is a no-op.
	DeleteTeam(ctx context.Context, id TeamID) error
	// GetTeam loads a team and its declared roles. ErrTeamNotFound if absent.
	GetTeam(ctx context.Context, id TeamID) (*Team, error)
	// ListTeams returns all teams in an org (all orgs when orgID == "").
	ListTeams(ctx context.Context, orgID string) ([]*Team, error)

	// AddMember inserts a membership row. The DB enforces agent exclusivity (a
	// partial unique index) and dedup; the implementation maps those to
	// ErrAgentAlreadyInTeam / ErrMemberAlreadyInTeam.
	AddMember(ctx context.Context, m *TeamMember) error
	// RemoveMember deletes a membership row. Returns ErrMemberNotFound if absent.
	RemoveMember(ctx context.Context, id TeamID, ref MemberRef) error
	// ListMembers returns a team's members.
	ListMembers(ctx context.Context, id TeamID) ([]*TeamMember, error)
	// ListMembersByTeams returns the members of ALL the given teams in ONE query,
	// so a whole-org membership rollup costs a CONSTANT number of reads instead of
	// one per team. Each TeamMember carries its TeamID, so callers group in memory.
	// An empty id list yields no members and no query.
	ListMembersByTeams(ctx context.Context, ids []TeamID) ([]*TeamMember, error)
	// FindAgentTeam returns the team an agent currently belongs to, if any.
	FindAgentTeam(ctx context.Context, ref MemberRef) (TeamID, bool, error)

	// AssociateProject links a project to a team. Returns
	// ErrProjectAlreadyAssociated on a duplicate.
	AssociateProject(ctx context.Context, id TeamID, projectID string) error
	// DisassociateProject unlinks a project from a team. Returns
	// ErrProjectNotAssociated when the link is absent.
	DisassociateProject(ctx context.Context, id TeamID, projectID string) error
	// ListProjects returns a team's associated projects.
	ListProjects(ctx context.Context, id TeamID) ([]*TeamProject, error)
}
