package team

import (
	"strings"
	"time"
)

// maxTeamNameLen bounds a team name (mirrors the org-name / template-name caps).
const maxTeamNameLen = 80

// Team is the Team aggregate root. It owns its declared RoleConfigs (design §9);
// members and project associations are separate records addressed by TeamID.
//
// Fields are unexported with accessors so the invariants (non-empty name, valid
// roles) can only be established through NewTeam / the mutators.
type Team struct {
	id          TeamID
	orgID       string
	name        string
	description string
	roles       []RoleConfig
	createdAt   time.Time
	updatedAt   time.Time
	version     int
}

// NewTeamInput carries the fields needed to construct a fresh Team.
type NewTeamInput struct {
	ID          TeamID
	OrgID       string
	Name        string
	Description string
	// Roles is the template-declared role set (design §9). May be empty at
	// creation; roles gate which members can later be added.
	Roles     []RoleConfig
	CreatedAt time.Time
}

// NewTeam validates input and returns a version-1 Team. CreatedAt doubles as the
// initial UpdatedAt.
func NewTeam(in NewTeamInput) (*Team, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > maxTeamNameLen {
		return nil, ErrInvalidTeam
	}
	if strings.TrimSpace(in.ID.String()) == "" {
		return nil, ErrInvalidTeam
	}
	roles, err := normalizeRoles(in.Roles)
	if err != nil {
		return nil, err
	}
	ts := in.CreatedAt
	return &Team{
		id:          in.ID,
		orgID:       strings.TrimSpace(in.OrgID),
		name:        name,
		description: in.Description,
		roles:       roles,
		createdAt:   ts,
		updatedAt:   ts,
		version:     1,
	}, nil
}

// normalizeRoles validates each RoleConfig and rejects duplicate role names.
func normalizeRoles(in []RoleConfig) ([]RoleConfig, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]RoleConfig, 0, len(in))
	for _, rc := range in {
		role := strings.TrimSpace(rc.Role)
		if role == "" {
			return nil, ErrInvalidRole
		}
		if _, dup := seen[role]; dup {
			return nil, ErrInvalidRole
		}
		if rc.MaxConcurrency < 0 {
			return nil, ErrInvalidRole
		}
		seen[role] = struct{}{}
		mc := rc.MaxConcurrency
		if mc == 0 {
			mc = 1 // default one concurrent slot per role
		}
		out = append(out, RoleConfig{
			Role:           role,
			CLI:            rc.CLI,
			Model:          rc.Model,
			CapabilityTags: append([]string(nil), rc.CapabilityTags...),
			MaxConcurrency: mc,
		})
	}
	return out, nil
}

// Rename updates the team name (validated) and bumps updatedAt.
func (t *Team) Rename(name string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxTeamNameLen {
		return ErrInvalidTeam
	}
	t.name = name
	t.touch(now)
	return nil
}

// SetDescription updates the description and bumps updatedAt.
func (t *Team) SetDescription(desc string, now time.Time) {
	t.description = desc
	t.touch(now)
}

// SetRoles replaces the team's declared role definitions after validating the
// complete set. Membership constraints are enforced by the application service.
func (t *Team) SetRoles(roles []RoleConfig, now time.Time) error {
	normalized, err := normalizeRoles(roles)
	if err != nil {
		return err
	}
	t.roles = normalized
	t.touch(now)
	return nil
}

func (t *Team) touch(now time.Time) {
	if !now.IsZero() {
		t.updatedAt = now
	}
	t.version++
}

// HasRole reports whether role is declared for this team.
func (t *Team) HasRole(role string) bool {
	for _, rc := range t.roles {
		if rc.Role == role {
			return true
		}
	}
	return false
}

// Accessors.
func (t *Team) ID() TeamID           { return t.id }
func (t *Team) OrgID() string        { return t.orgID }
func (t *Team) Name() string         { return t.name }
func (t *Team) Description() string  { return t.description }
func (t *Team) Roles() []RoleConfig  { return t.roles }
func (t *Team) CreatedAt() time.Time { return t.createdAt }
func (t *Team) UpdatedAt() time.Time { return t.updatedAt }
func (t *Team) Version() int         { return t.version }

// RehydrateInput reconstructs a Team from persisted state (repository use only).
type RehydrateInput struct {
	ID          TeamID
	OrgID       string
	Name        string
	Description string
	Roles       []RoleConfig
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

// Rehydrate rebuilds a Team from storage without re-running creation
// validation. Used by repositories when loading rows.
func Rehydrate(in RehydrateInput) *Team {
	return &Team{
		id:          in.ID,
		orgID:       in.OrgID,
		name:        in.Name,
		description: in.Description,
		roles:       in.Roles,
		createdAt:   in.CreatedAt,
		updatedAt:   in.UpdatedAt,
		version:     in.Version,
	}
}
