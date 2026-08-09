package teammemory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/team"
)

func TestTeamPolicyAuthorizationAgentPolicy(t *testing.T) {
	ctx := context.Background()
	teams := newAuthTeamRepo(t)
	members := &authMemberRepo{}
	auth := NewTeamPolicyAuthorization(teams, members)

	resolved, ok, err := auth.ResolveActorTeam(ctx, "agent:member")
	if err != nil || !ok || resolved != "team-1" {
		t.Fatalf("ResolveActorTeam=%q,%v,%v", resolved, ok, err)
	}
	if ok, err := auth.CanPropose(ctx, "team-1", "agent:member"); err != nil || !ok {
		t.Fatalf("member CanPropose=%v,%v", ok, err)
	}
	if ok, err := auth.CanRead(ctx, "team-1", "agent:member"); err != nil || !ok {
		t.Fatalf("member CanRead=%v,%v", ok, err)
	}
	if ok, err := auth.CanReview(ctx, "team-1", "agent:member"); err != nil || ok {
		t.Fatalf("proposal_only CanReview=%v,%v, want false,nil", ok, err)
	}

	curatorPolicy := team.TeamMemoryPolicy{
		Mode:             team.TeamMemoryCuratorAuto,
		CuratorAgentRefs: []team.MemberRef{"agent:curator"},
	}
	teams.setPolicy(t, "team-1", curatorPolicy)
	if ok, err := auth.CanReview(ctx, "team-1", "agent:curator"); err != nil || !ok {
		t.Fatalf("curator CanReview=%v,%v", ok, err)
	}
	if ok, err := auth.CanReview(ctx, "team-1", "agent:member"); err != nil || ok {
		t.Fatalf("ungranted agent CanReview=%v,%v, want false,nil", ok, err)
	}

	teams.setPolicy(t, "team-1", team.TeamMemoryPolicy{Mode: team.TeamMemoryCuratorAuto})
	if ok, err := auth.CanReview(ctx, "team-1", "agent:curator"); err != nil || ok {
		t.Fatalf("revoked curator CanReview=%v,%v, want false,nil", ok, err)
	}
	teams.setPolicy(t, "team-1", curatorPolicy)
	teams.removeAgent("agent:curator")
	if ok, err := auth.CanReview(ctx, "team-1", "agent:curator"); err != nil || ok {
		t.Fatalf("removed curator CanReview=%v,%v, want false,nil", ok, err)
	}
}

func TestTeamPolicyAuthorizationHumanOrgRoles(t *testing.T) {
	ctx := context.Background()
	teams := newAuthTeamRepo(t)
	members := &authMemberRepo{members: map[string]*identity.Member{
		"org-1/user:owner":  mustIdentityMember(t, "org-1", "owner", identity.RoleOwner),
		"org-1/user:admin":  mustIdentityMember(t, "org-1", "admin", identity.RoleAdmin),
		"org-1/user:member": mustIdentityMember(t, "org-1", "member", identity.RoleMember),
		"org-2/user:other":  mustIdentityMember(t, "org-2", "other", identity.RoleOwner),
	}}
	auth := NewTeamPolicyAuthorization(teams, members)

	if _, ok, err := auth.ResolveActorTeam(ctx, "user:owner"); err != nil || ok {
		t.Fatalf("human ResolveActorTeam ok=%v err=%v, want false,nil", ok, err)
	}
	for _, ref := range []string{"user:owner", "user:admin"} {
		if ok, err := auth.CanPropose(ctx, "team-1", ref); err != nil || !ok {
			t.Fatalf("%s CanPropose=%v,%v", ref, ok, err)
		}
		if ok, err := auth.CanReview(ctx, "team-1", ref); err != nil || !ok {
			t.Fatalf("%s CanReview=%v,%v", ref, ok, err)
		}
	}
	if ok, err := auth.CanRead(ctx, "team-1", "user:member"); err != nil || !ok {
		t.Fatalf("human member CanRead=%v,%v", ok, err)
	}
	if ok, err := auth.CanPropose(ctx, "team-1", "user:member"); err != nil || ok {
		t.Fatalf("human member CanPropose=%v,%v, want false,nil", ok, err)
	}
	if ok, err := auth.CanReview(ctx, "team-1", "user:member"); err != nil || ok {
		t.Fatalf("human member CanReview=%v,%v, want false,nil", ok, err)
	}
	if ok, err := auth.CanReview(ctx, "team-1", "user:other"); err != nil || ok {
		t.Fatalf("cross-org owner CanReview=%v,%v, want false,nil", ok, err)
	}
}

type authTeamRepo struct {
	teams      map[team.TeamID]*team.Team
	members    map[team.TeamID][]*team.TeamMember
	agentTeams map[team.MemberRef]team.TeamID
}

func newAuthTeamRepo(t *testing.T) *authTeamRepo {
	t.Helper()
	tm, err := team.NewTeam(team.NewTeamInput{
		ID: "team-1", OrgID: "org-1", Name: "Team", CreatedAt: time.Unix(1, 0),
		Roles: []team.RoleConfig{{Role: "dev", CLI: "codex", Model: "gpt-5", MaxConcurrency: 1}},
	})
	if err != nil {
		t.Fatalf("NewTeam: %v", err)
	}
	return &authTeamRepo{
		teams: map[team.TeamID]*team.Team{"team-1": tm},
		members: map[team.TeamID][]*team.TeamMember{"team-1": {
			{TeamID: "team-1", Ref: "agent:member", Kind: team.MemberKindAgent, Role: "dev"},
			{TeamID: "team-1", Ref: "agent:curator", Kind: team.MemberKindAgent, Role: "dev"},
		}},
		agentTeams: map[team.MemberRef]team.TeamID{"agent:member": "team-1", "agent:curator": "team-1"},
	}
}

func (r *authTeamRepo) setPolicy(t *testing.T, id team.TeamID, policy team.TeamMemoryPolicy) {
	t.Helper()
	if err := r.teams[id].SetMemoryPolicy(policy, time.Unix(2, 0)); err != nil {
		t.Fatalf("SetMemoryPolicy: %v", err)
	}
}

func (r *authTeamRepo) removeAgent(ref team.MemberRef) {
	delete(r.agentTeams, ref)
	ms := r.members["team-1"][:0]
	for _, m := range r.members["team-1"] {
		if m.Ref != ref {
			ms = append(ms, m)
		}
	}
	r.members["team-1"] = ms
}

func (r *authTeamRepo) CreateTeam(context.Context, *team.Team) error { panic("unused") }
func (r *authTeamRepo) UpdateTeam(context.Context, *team.Team) error { panic("unused") }
func (r *authTeamRepo) ReplaceRoles(context.Context, *team.Team) error {
	panic("unused")
}
func (r *authTeamRepo) SetMemoryPolicy(context.Context, *team.Team) error { panic("unused") }
func (r *authTeamRepo) GetMemoryPolicy(_ context.Context, id team.TeamID) (team.TeamMemoryPolicy, error) {
	tm, ok := r.teams[id]
	if !ok {
		return team.TeamMemoryPolicy{}, team.ErrTeamNotFound
	}
	return tm.MemoryPolicy(), nil
}
func (r *authTeamRepo) DeleteTeam(context.Context, team.TeamID) error { panic("unused") }
func (r *authTeamRepo) GetTeam(_ context.Context, id team.TeamID) (*team.Team, error) {
	tm, ok := r.teams[id]
	if !ok {
		return nil, team.ErrTeamNotFound
	}
	return tm, nil
}
func (r *authTeamRepo) ListTeams(context.Context, string) ([]*team.Team, error) { panic("unused") }
func (r *authTeamRepo) AddMember(context.Context, *team.TeamMember) error       { panic("unused") }
func (r *authTeamRepo) RemoveMember(context.Context, team.TeamID, team.MemberRef) error {
	panic("unused")
}
func (r *authTeamRepo) ListMembers(_ context.Context, id team.TeamID) ([]*team.TeamMember, error) {
	return r.members[id], nil
}
func (r *authTeamRepo) ListMembersByTeams(context.Context, []team.TeamID) ([]*team.TeamMember, error) {
	panic("unused")
}
func (r *authTeamRepo) FindAgentTeam(_ context.Context, ref team.MemberRef) (team.TeamID, bool, error) {
	id, ok := r.agentTeams[ref]
	return id, ok, nil
}
func (r *authTeamRepo) AssociateProject(context.Context, team.TeamID, string) error {
	panic("unused")
}
func (r *authTeamRepo) DisassociateProject(context.Context, team.TeamID, string) error {
	panic("unused")
}
func (r *authTeamRepo) ListProjects(context.Context, team.TeamID) ([]*team.TeamProject, error) {
	panic("unused")
}

type authMemberRepo struct {
	members map[string]*identity.Member
}

func (r *authMemberRepo) Save(context.Context, *identity.Member) error { panic("unused") }
func (r *authMemberRepo) GetByID(context.Context, string) (*identity.Member, error) {
	panic("unused")
}
func (r *authMemberRepo) GetByOrganizationAndIdentity(_ context.Context, orgID, identityID string) (*identity.Member, error) {
	m, ok := r.members[orgID+"/user:"+identityID]
	if !ok {
		return nil, identity.ErrMemberNotFound
	}
	return m, nil
}
func (r *authMemberRepo) ListByOrganization(context.Context, string) ([]*identity.Member, error) {
	panic("unused")
}
func (r *authMemberRepo) CountActiveOwners(context.Context, string) (int, error) {
	panic("unused")
}
func (r *authMemberRepo) Delete(context.Context, string) error { panic("unused") }

func mustIdentityMember(t *testing.T, orgID, identityID string, role identity.MemberRole) *identity.Member {
	t.Helper()
	m, err := identity.MemberFactory{}.New(orgID, identityID, role, nil)
	if err != nil {
		t.Fatalf("MemberFactory.New: %v", err)
	}
	return m
}

func TestTeamPolicyAuthorizationNotWired(t *testing.T) {
	_, _, err := (*TeamPolicyAuthorization)(nil).ResolveActorTeam(context.Background(), "agent:a")
	if !errors.Is(err, ErrNotWired) {
		t.Fatalf("ResolveActorTeam err=%v want not wired", err)
	}
}
