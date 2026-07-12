// Package tool exposes the Team use cases as an agent-tool surface (design §4):
// stable tool names, JSON-tagged argument structs, and serializable result
// views over the Team application service. This is the data-layer form of the
// tools; wiring them onto the live MCP server / admin routes is a later phase.
package tool

import (
	"context"

	"github.com/oopslink/agent-center/internal/team"
	"github.com/oopslink/agent-center/internal/team/service"
)

// Tool name constants — the canonical identifiers an agent calls.
const (
	ToolCreateTeam       = "create_team"
	ToolUpdateTeam       = "update_team"
	ToolDeleteTeam       = "delete_team"
	ToolGetTeam          = "get_team"
	ToolListTeams        = "list_teams"
	ToolAddMember        = "add_member"
	ToolRemoveMember     = "remove_member"
	ToolAssociateProject = "associate_project"
)

// Definition is lightweight tool metadata (name + one-line description) for
// registration / discovery.
type Definition struct {
	Name        string
	Description string
}

// Definitions returns the Team agent-tool set, in a stable order.
func Definitions() []Definition {
	return []Definition{
		{ToolCreateTeam, "Create a team with its template-declared roles."},
		{ToolUpdateTeam, "Update a team's name and/or description."},
		{ToolDeleteTeam, "Delete a team and its members/projects/roles."},
		{ToolGetTeam, "Read a team, including its declared roles."},
		{ToolListTeams, "List teams, optionally scoped to an org."},
		{ToolAddMember, "Add an agent/human member under a declared role (agents are single-team)."},
		{ToolRemoveMember, "Remove a member from a team."},
		{ToolAssociateProject, "Associate a project with a team."},
	}
}

// Tools is the agent-facing facade over the Team service.
type Tools struct {
	svc *service.Service
}

// NewTools wraps a Team service as an agent-tool set.
func NewTools(svc *service.Service) *Tools { return &Tools{svc: svc} }

// ---- argument + result views ------------------------------------------------

// RoleArg is a role declaration in a create_team call.
type RoleArg struct {
	Role           string   `json:"role" jsonschema:"role name (template-defined, not hardcoded)"`
	CLI            string   `json:"cli,omitempty" jsonschema:"agent CLI the role runs on (e.g. claude-code)"`
	Model          string   `json:"model,omitempty" jsonschema:"model id the role uses"`
	CapabilityTags []string `json:"capability_tags,omitempty" jsonschema:"capability requirements for the role"`
	MaxConcurrency int      `json:"max_concurrency,omitempty" jsonschema:"max concurrent members of this role (default 1)"`
}

// CreateTeamArgs is the create_team payload.
type CreateTeamArgs struct {
	OrgID       string    `json:"org_id,omitempty" jsonschema:"owning org id (may be empty)"`
	Name        string    `json:"name" jsonschema:"team name (unique within the org)"`
	Description string    `json:"description,omitempty" jsonschema:"optional team description"`
	Roles       []RoleArg `json:"roles,omitempty" jsonschema:"template-declared roles for the team"`
}

// UpdateTeamArgs is the update_team payload; omit a field to leave it unchanged.
type UpdateTeamArgs struct {
	TeamID      string  `json:"team_id" jsonschema:"the team to update"`
	Name        *string `json:"name,omitempty" jsonschema:"new team name"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
}

// AddMemberArgs is the add_member payload.
type AddMemberArgs struct {
	TeamID    string `json:"team_id" jsonschema:"the team to add to"`
	MemberRef string `json:"member_ref" jsonschema:"identity ref: agent:<id> or user:<id>"`
	Role      string `json:"role" jsonschema:"a role the team declared (see create_team)"`
}

// RoleView is a serializable RoleConfig.
type RoleView struct {
	Role           string   `json:"role"`
	CLI            string   `json:"cli"`
	Model          string   `json:"model"`
	CapabilityTags []string `json:"capability_tags"`
	MaxConcurrency int      `json:"max_concurrency"`
}

// TeamView is a serializable Team.
type TeamView struct {
	ID          string     `json:"id"`
	OrgID       string     `json:"org_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Roles       []RoleView `json:"roles"`
	Version     int        `json:"version"`
}

// MemberView is a serializable TeamMember.
type MemberView struct {
	TeamID    string `json:"team_id"`
	MemberRef string `json:"member_ref"`
	Kind      string `json:"kind"`
	Role      string `json:"role"`
}

// ---- tool handlers ----------------------------------------------------------

// CreateTeam handles the create_team tool.
func (t *Tools) CreateTeam(ctx context.Context, a CreateTeamArgs) (TeamView, error) {
	created, err := t.svc.CreateTeam(ctx, service.CreateTeamInput{
		OrgID:       a.OrgID,
		Name:        a.Name,
		Description: a.Description,
		Roles:       toRoleConfigs(a.Roles),
	})
	if err != nil {
		return TeamView{}, err
	}
	return toTeamView(created), nil
}

// UpdateTeam handles the update_team tool.
func (t *Tools) UpdateTeam(ctx context.Context, a UpdateTeamArgs) (TeamView, error) {
	updated, err := t.svc.UpdateTeam(ctx, team.TeamID(a.TeamID), service.UpdateTeamInput{
		Name:        a.Name,
		Description: a.Description,
	})
	if err != nil {
		return TeamView{}, err
	}
	return toTeamView(updated), nil
}

// DeleteTeam handles the delete_team tool.
func (t *Tools) DeleteTeam(ctx context.Context, teamID string) error {
	return t.svc.DeleteTeam(ctx, team.TeamID(teamID))
}

// GetTeam handles the get_team tool.
func (t *Tools) GetTeam(ctx context.Context, teamID string) (TeamView, error) {
	got, err := t.svc.GetTeam(ctx, team.TeamID(teamID))
	if err != nil {
		return TeamView{}, err
	}
	return toTeamView(got), nil
}

// ListTeams handles the list_teams tool ("" org → all teams).
func (t *Tools) ListTeams(ctx context.Context, orgID string) ([]TeamView, error) {
	teams, err := t.svc.ListTeams(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]TeamView, 0, len(teams))
	for _, tm := range teams {
		out = append(out, toTeamView(tm))
	}
	return out, nil
}

// AddMember handles the add_member tool.
func (t *Tools) AddMember(ctx context.Context, a AddMemberArgs) (MemberView, error) {
	m, err := t.svc.AddMember(ctx, team.TeamID(a.TeamID), team.MemberRef(a.MemberRef), a.Role)
	if err != nil {
		return MemberView{}, err
	}
	return MemberView{
		TeamID: m.TeamID.String(), MemberRef: m.Ref.String(),
		Kind: m.Kind.String(), Role: m.Role,
	}, nil
}

// RemoveMember handles the remove_member tool.
func (t *Tools) RemoveMember(ctx context.Context, teamID, memberRef string) error {
	return t.svc.RemoveMember(ctx, team.TeamID(teamID), team.MemberRef(memberRef))
}

// AssociateProject handles the associate_project tool.
func (t *Tools) AssociateProject(ctx context.Context, teamID, projectID string) error {
	return t.svc.AssociateProject(ctx, team.TeamID(teamID), projectID)
}

// ---- mapping helpers --------------------------------------------------------

func toRoleConfigs(in []RoleArg) []team.RoleConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]team.RoleConfig, 0, len(in))
	for _, r := range in {
		out = append(out, team.RoleConfig{
			Role:           r.Role,
			CLI:            r.CLI,
			Model:          r.Model,
			CapabilityTags: r.CapabilityTags,
			MaxConcurrency: r.MaxConcurrency,
		})
	}
	return out
}

func toTeamView(t *team.Team) TeamView {
	roles := make([]RoleView, 0, len(t.Roles()))
	for _, rc := range t.Roles() {
		tags := rc.CapabilityTags
		if tags == nil {
			tags = []string{}
		}
		roles = append(roles, RoleView{
			Role: rc.Role, CLI: rc.CLI, Model: rc.Model,
			CapabilityTags: tags, MaxConcurrency: rc.MaxConcurrency,
		})
	}
	return TeamView{
		ID:          t.ID().String(),
		OrgID:       t.OrgID(),
		Name:        t.Name(),
		Description: t.Description(),
		Roles:       roles,
		Version:     t.Version(),
	}
}
