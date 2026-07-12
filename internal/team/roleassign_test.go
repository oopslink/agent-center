package team_test

import (
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/team"
)

func members(pairs ...[2]string) []*team.TeamMember {
	out := make([]*team.TeamMember, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, &team.TeamMember{
			TeamID: "team-1", Ref: team.MemberRef(p[0]), Kind: team.MemberKindAgent, Role: p[1],
		})
	}
	return out
}

func TestRoster_ExcludesHumansAndPreservesOrder(t *testing.T) {
	ms := []*team.TeamMember{
		{TeamID: "t", Ref: "agent:d1", Kind: team.MemberKindAgent, Role: "dev"},
		{TeamID: "t", Ref: "user:alice", Kind: team.MemberKindHuman, Role: "dev"},
		{TeamID: "t", Ref: "agent:d2", Kind: team.MemberKindAgent, Role: "dev"},
	}
	r := team.NewRoster(ms)
	got := r.Agents("dev")
	if len(got) != 2 || got[0] != "agent:d1" || got[1] != "agent:d2" {
		t.Fatalf("dev roster = %v, want [agent:d1 agent:d2] (humans excluded, order kept)", got)
	}
}

func TestResolveRole_SingleMember(t *testing.T) {
	r := team.NewRoster(members([2]string{"agent:pd", "pd"}))
	got, err := team.ResolveRole(r, "pd", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "agent:pd" {
		t.Fatalf("got %q want agent:pd", got)
	}
}

func TestResolveRole_RoleNotStaffed(t *testing.T) {
	r := team.NewRoster(members([2]string{"agent:pd", "pd"}))
	_, err := team.ResolveRole(r, "tester", nil, "")
	if !errors.Is(err, team.ErrRoleNotStaffed) {
		t.Fatalf("want ErrRoleNotStaffed, got %v", err)
	}
}

func TestResolveRoles_LeastBusyPicksFreest(t *testing.T) {
	r := team.NewRoster(members(
		[2]string{"agent:d1", "dev"},
		[2]string{"agent:d2", "dev"},
		[2]string{"agent:d3", "dev"},
	))
	busy := map[team.MemberRef]int{"agent:d1": 5, "agent:d2": 1, "agent:d3": 9}
	got, err := team.ResolveRoles(r, []team.NodeAssignRequest{{NodeKey: "n1", Role: "dev"}}, func(a team.MemberRef) int { return busy[a] }, team.StrategyLeastBusy)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Agent != "agent:d2" {
		t.Fatalf("least busy = %q want agent:d2", got[0].Agent)
	}
}

func TestResolveRoles_LeastBusySpreadsWithinBatch(t *testing.T) {
	// Three dev nodes, three equally-idle devs → one each (in-batch load spread).
	r := team.NewRoster(members(
		[2]string{"agent:d1", "dev"},
		[2]string{"agent:d2", "dev"},
		[2]string{"agent:d3", "dev"},
	))
	reqs := []team.NodeAssignRequest{
		{NodeKey: "n1", Role: "dev"},
		{NodeKey: "n2", Role: "dev"},
		{NodeKey: "n3", Role: "dev"},
	}
	got, err := team.ResolveRoles(r, reqs, nil, team.StrategyLeastBusy)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[team.MemberRef]bool{}
	for _, a := range got {
		if seen[a.Agent] {
			t.Fatalf("agent %q assigned twice; batch did not spread: %+v", a.Agent, got)
		}
		seen[a.Agent] = true
	}
}

func TestResolveRoles_ReviewAvoidsDev(t *testing.T) {
	// Two agents both staff dev and review. Review must not land on the Dev agent.
	r := team.NewRoster([]*team.TeamMember{
		{TeamID: "t", Ref: "agent:a", Kind: team.MemberKindAgent, Role: "dev"},
		{TeamID: "t", Ref: "agent:b", Kind: team.MemberKindAgent, Role: "dev"},
		{TeamID: "t", Ref: "agent:a", Kind: team.MemberKindAgent, Role: "review"},
		{TeamID: "t", Ref: "agent:b", Kind: team.MemberKindAgent, Role: "review"},
	})
	// Force dev onto agent:a by making agent:b busy, then review must pick agent:b.
	busy := func(a team.MemberRef) int {
		if a == "agent:b" {
			return 10
		}
		return 0
	}
	reqs := []team.NodeAssignRequest{
		{NodeKey: "dev", Role: "dev"},
		{NodeKey: "review", Role: "review", AvoidNodes: []string{"dev"}},
	}
	got, err := team.ResolveRoles(r, reqs, busy, team.StrategyLeastBusy)
	if err != nil {
		t.Fatal(err)
	}
	var dev, rev team.MemberRef
	for _, a := range got {
		switch a.NodeKey {
		case "dev":
			dev = a.Agent
		case "review":
			rev = a.Agent
		}
	}
	if dev == rev {
		t.Fatalf("review (%q) must differ from dev (%q)", rev, dev)
	}
	if dev != "agent:a" || rev != "agent:b" {
		t.Fatalf("got dev=%q review=%q, want dev=agent:a review=agent:b", dev, rev)
	}
}

func TestResolveRoles_ConstraintUnsatisfiable(t *testing.T) {
	// Only one agent staffs both dev and review; Review≠Dev is impossible.
	r := team.NewRoster([]*team.TeamMember{
		{TeamID: "t", Ref: "agent:solo", Kind: team.MemberKindAgent, Role: "dev"},
		{TeamID: "t", Ref: "agent:solo", Kind: team.MemberKindAgent, Role: "review"},
	})
	reqs := []team.NodeAssignRequest{
		{NodeKey: "dev", Role: "dev"},
		{NodeKey: "review", Role: "review", AvoidNodes: []string{"dev"}},
	}
	_, err := team.ResolveRoles(r, reqs, nil, "")
	if !errors.Is(err, team.ErrConstraintUnsatisfiable) {
		t.Fatalf("want ErrConstraintUnsatisfiable, got %v", err)
	}
}

func TestResolveRoles_CyclicAvoid(t *testing.T) {
	r := team.NewRoster(members(
		[2]string{"agent:a", "dev"},
		[2]string{"agent:b", "dev"},
	))
	reqs := []team.NodeAssignRequest{
		{NodeKey: "n1", Role: "dev", AvoidNodes: []string{"n2"}},
		{NodeKey: "n2", Role: "dev", AvoidNodes: []string{"n1"}},
	}
	_, err := team.ResolveRoles(r, reqs, nil, "")
	if !errors.Is(err, team.ErrCyclicAvoid) {
		t.Fatalf("want ErrCyclicAvoid, got %v", err)
	}
}

func TestResolveRoles_UnknownNodeRef(t *testing.T) {
	r := team.NewRoster(members([2]string{"agent:a", "dev"}))
	reqs := []team.NodeAssignRequest{
		{NodeKey: "n1", Role: "dev", AvoidNodes: []string{"ghost"}},
	}
	_, err := team.ResolveRoles(r, reqs, nil, "")
	if !errors.Is(err, team.ErrUnknownNodeRef) {
		t.Fatalf("want ErrUnknownNodeRef, got %v", err)
	}
}

func TestResolveRoles_RoundRobin(t *testing.T) {
	r := team.NewRoster(members(
		[2]string{"agent:a", "dev"},
		[2]string{"agent:b", "dev"},
	))
	reqs := []team.NodeAssignRequest{
		{NodeKey: "n1", Role: "dev"},
		{NodeKey: "n2", Role: "dev"},
		{NodeKey: "n3", Role: "dev"},
	}
	got, err := team.ResolveRoles(r, reqs, nil, team.StrategyRoundRobin)
	if err != nil {
		t.Fatal(err)
	}
	want := []team.MemberRef{"agent:a", "agent:b", "agent:a"}
	for i, a := range got {
		if a.Agent != want[i] {
			t.Fatalf("rr[%d] = %q want %q (full=%+v)", i, a.Agent, want[i], got)
		}
	}
}
