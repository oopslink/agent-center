package teammemory

import (
	"context"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/team"
)

// TeamPolicyAuthorization adapts Team membership/policy and Identity org roles
// to the Team Memory application service authorization port.
type TeamPolicyAuthorization struct {
	teams   team.Repository
	members identity.MemberRepository
}

// NewTeamPolicyAuthorization returns an authorization port backed by Team and
// Identity repositories. The Identity member repository is required for human
// owner/admin checks; agent-only proposal flows still work when it is nil.
func NewTeamPolicyAuthorization(teams team.Repository, members identity.MemberRepository) *TeamPolicyAuthorization {
	return &TeamPolicyAuthorization{teams: teams, members: members}
}

var _ AuthorizationPort = (*TeamPolicyAuthorization)(nil)

// ResolveActorTeam resolves the agent-facing MCP scope. Human users may belong
// to multiple teams and must use a trusted adapter that passes teamID explicitly.
func (a *TeamPolicyAuthorization) ResolveActorTeam(ctx context.Context, actorRef string) (string, bool, error) {
	if a == nil || a.teams == nil {
		return "", false, ErrNotWired
	}
	ref, kind, ok := parseActorRef(actorRef)
	if !ok || kind != team.MemberKindAgent {
		return "", false, nil
	}
	teamID, found, err := a.teams.FindAgentTeam(ctx, ref)
	if err != nil {
		return "", false, err
	}
	return teamID.String(), found, nil
}

// CanPropose allows current team agents and human owner/admins in the team's org.
func (a *TeamPolicyAuthorization) CanPropose(ctx context.Context, teamID, actorRef string) (bool, error) {
	ref, kind, ok := parseActorRef(actorRef)
	if !ok {
		return false, nil
	}
	switch kind {
	case team.MemberKindAgent:
		return a.agentInTeam(ctx, team.TeamID(strings.TrimSpace(teamID)), ref)
	case team.MemberKindHuman:
		m, ok, err := a.humanOrgMember(ctx, team.TeamID(strings.TrimSpace(teamID)), ref)
		if err != nil || !ok {
			return false, err
		}
		return m.Role().AtLeast(identity.RoleAdmin), nil
	default:
		return false, nil
	}
}

// CanRead allows current team agents and joined human members of the team's org.
func (a *TeamPolicyAuthorization) CanRead(ctx context.Context, teamID, actorRef string) (bool, error) {
	ref, kind, ok := parseActorRef(actorRef)
	if !ok {
		return false, nil
	}
	switch kind {
	case team.MemberKindAgent:
		return a.agentInTeam(ctx, team.TeamID(strings.TrimSpace(teamID)), ref)
	case team.MemberKindHuman:
		_, ok, err := a.humanOrgMember(ctx, team.TeamID(strings.TrimSpace(teamID)), ref)
		return ok, err
	default:
		return false, nil
	}
}

// CanReview allows human owner/admins, plus explicitly granted current agent
// members when the Team policy is curator_auto.
func (a *TeamPolicyAuthorization) CanReview(ctx context.Context, teamID, actorRef string) (bool, error) {
	ref, kind, ok := parseActorRef(actorRef)
	if !ok {
		return false, nil
	}
	id := team.TeamID(strings.TrimSpace(teamID))
	switch kind {
	case team.MemberKindAgent:
		inTeam, err := a.agentInTeam(ctx, id, ref)
		if err != nil || !inTeam {
			return false, err
		}
		policy, err := a.teams.GetMemoryPolicy(ctx, id)
		if err != nil {
			if errors.Is(err, team.ErrTeamNotFound) {
				return false, nil
			}
			return false, err
		}
		return policy.IsCurator(ref), nil
	case team.MemberKindHuman:
		m, ok, err := a.humanOrgMember(ctx, id, ref)
		if err != nil || !ok {
			return false, err
		}
		return m.Role().AtLeast(identity.RoleAdmin), nil
	default:
		return false, nil
	}
}

func (a *TeamPolicyAuthorization) agentInTeam(ctx context.Context, teamID team.TeamID, ref team.MemberRef) (bool, error) {
	if a == nil || a.teams == nil || strings.TrimSpace(teamID.String()) == "" {
		return false, nil
	}
	got, ok, err := a.teams.FindAgentTeam(ctx, ref)
	if err != nil {
		return false, err
	}
	return ok && got == teamID, nil
}

func (a *TeamPolicyAuthorization) humanOrgMember(ctx context.Context, teamID team.TeamID, ref team.MemberRef) (*identity.Member, bool, error) {
	if a == nil || a.teams == nil || a.members == nil || strings.TrimSpace(teamID.String()) == "" {
		return nil, false, nil
	}
	tm, err := a.teams.GetTeam(ctx, teamID)
	if err != nil {
		if errors.Is(err, team.ErrTeamNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	m, err := a.members.GetByOrganizationAndIdentity(ctx, tm.OrgID(), ref.BareID())
	if err != nil {
		if errors.Is(err, identity.ErrMemberNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return m, m != nil && m.IsJoined(), nil
}

func parseActorRef(actorRef string) (team.MemberRef, team.MemberKind, bool) {
	ref := team.MemberRef(strings.TrimSpace(actorRef))
	kind, err := ref.Kind()
	if err != nil {
		return "", "", false
	}
	return ref, kind, true
}
