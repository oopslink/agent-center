package team

import "errors"

// Sentinel errors for the Team BC. Repository + service map DB / validation
// failures onto these so callers (and agent tools) can branch on a stable set.
var (
	// ErrTeamNotFound is returned when a team id does not resolve.
	ErrTeamNotFound = errors.New("team: not found")
	// ErrTeamNameTaken is returned when a team name collides within its org.
	ErrTeamNameTaken = errors.New("team: name already taken in org")
	// ErrInvalidTeam is returned when a Team fails validation (empty org/name).
	ErrInvalidTeam = errors.New("team: invalid team")

	// ErrInvalidMemberRef is returned for a malformed member ref (no
	// "agent:"/"user:" prefix, or an empty id).
	ErrInvalidMemberRef = errors.New("team: invalid member ref")
	// ErrAgentAlreadyInTeam is returned when adding an agent that already
	// belongs to a team — agents are exclusive to a single team (design §2/§4).
	ErrAgentAlreadyInTeam = errors.New("team: agent already belongs to a team")
	// ErrMemberAlreadyInTeam is returned when the same ref is added twice to one
	// team.
	ErrMemberAlreadyInTeam = errors.New("team: member already in team")
	// ErrMemberNotFound is returned when removing a member that is not present.
	ErrMemberNotFound = errors.New("team: member not found")
	// ErrRoleNotDeclared is returned when a member is added under a role the
	// team's template never declared (design §9).
	ErrRoleNotDeclared = errors.New("team: role not declared for team")
	// ErrInvalidRole is returned when a RoleConfig fails validation.
	ErrInvalidRole = errors.New("team: invalid role config")

	// ErrProjectAlreadyAssociated is returned when a project is associated to a
	// team twice.
	ErrProjectAlreadyAssociated = errors.New("team: project already associated")
	// ErrInvalidProject is returned for an empty project id.
	ErrInvalidProject = errors.New("team: invalid project id")

	// ErrRoleNotStaffed is returned by the role→agent resolver (design §7) when a
	// requested role has no agent members on the current team roster.
	ErrRoleNotStaffed = errors.New("team: role has no agent members")
	// ErrConstraintUnsatisfiable is returned when a resolution constraint (e.g.
	// Review ≠ Dev) leaves a role node with no eligible agent.
	ErrConstraintUnsatisfiable = errors.New("team: role assignment constraint unsatisfiable")
	// ErrCyclicAvoid is returned when node avoid-references form a cycle so no
	// resolution order exists.
	ErrCyclicAvoid = errors.New("team: cyclic avoid references")
	// ErrUnknownNodeRef is returned when an avoid reference names a node that is
	// not part of the resolution request set.
	ErrUnknownNodeRef = errors.New("team: unknown node reference")

	// ErrInvalidTemplate is returned when a team template fails validation.
	ErrInvalidTemplate = errors.New("team: invalid team template")
	// ErrTemplateNotCurated is returned when exporting / cross-org sharing a
	// template that has not passed the mandatory manual curation (design §9).
	ErrTemplateNotCurated = errors.New("team: template not curated (export requires manual curation)")
)
