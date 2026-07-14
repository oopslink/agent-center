package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/team"
)

// fakeResolver is a test double for MemberResolver. It records the args it was
// called with and returns canned (ok, err).
type fakeResolver struct {
	ok     bool
	err    error
	calls  int
	gotOrg string
	gotRef team.MemberRef
}

func (f *fakeResolver) MemberExists(_ context.Context, orgID string, ref team.MemberRef) (bool, error) {
	f.calls++
	f.gotOrg = orgID
	f.gotRef = ref
	return f.ok, f.err
}

// pollutingRef mirrors the exact shape tester3 observed polluting team_members:
// a well-formed prefix (passes MemberRef.Kind) whose id is a truncated / bogus
// placeholder pointing at no real agent.
const pollutingRef = team.MemberRef("agent:04c1…")

// TestAddMember_RejectsUnresolvableRef locks the hardening: a well-formed but
// unresolvable ref is rejected with the typed error AND never persisted (the
// regression that let `agent:04c1…` reach team_members).
func TestAddMember_RejectsUnresolvableRef(t *testing.T) {
	svc, _ := newService(t)
	fr := &fakeResolver{ok: false}
	svc.WithMemberResolver(fr)
	a := createTeam(t, svc, "A", devRole())
	ctx := context.Background()

	_, err := svc.AddMember(ctx, a.ID(), pollutingRef, "dev")
	if !errors.Is(err, team.ErrMemberIdentityNotFound) {
		t.Fatalf("unresolvable ref: got %v want ErrMemberIdentityNotFound", err)
	}
	// The resolver was consulted with the team's OWN org + the exact ref.
	if fr.calls != 1 || fr.gotOrg != "org-1" || fr.gotRef != pollutingRef {
		t.Fatalf("resolver consulted with org=%q ref=%q calls=%d", fr.gotOrg, fr.gotRef, fr.calls)
	}
	// NOT persisted — no pollution.
	members, err := svc.ListMembers(ctx, a.ID())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("rejected ref must not persist, got %+v", members)
	}
}

// TestAddMember_AcceptsResolvableRef locks that tightening the contract does not
// break the happy path: a ref the resolver approves is added normally.
func TestAddMember_AcceptsResolvableRef(t *testing.T) {
	svc, _ := newService(t)
	svc.WithMemberResolver(&fakeResolver{ok: true})
	a := createTeam(t, svc, "A", devRole())
	ctx := context.Background()

	m, err := svc.AddMember(ctx, a.ID(), "agent:agent-real", "dev")
	if err != nil {
		t.Fatalf("resolvable ref should add: %v", err)
	}
	if m == nil || m.Ref != "agent:agent-real" {
		t.Fatalf("member = %+v", m)
	}
	members, err := svc.ListMembers(ctx, a.ID())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("resolvable ref must persist, got %+v", members)
	}
}

// TestAddMember_ResolverErrorPropagates locks that a real infra error from the
// resolver surfaces (fail-loud) rather than being swallowed as a reject — and
// nothing is persisted.
func TestAddMember_ResolverErrorPropagates(t *testing.T) {
	svc, _ := newService(t)
	boom := errors.New("directory unavailable")
	svc.WithMemberResolver(&fakeResolver{err: boom})
	a := createTeam(t, svc, "A", devRole())
	ctx := context.Background()

	_, err := svc.AddMember(ctx, a.ID(), "agent:agent-x", "dev")
	if !errors.Is(err, boom) {
		t.Fatalf("resolver error should surface: got %v", err)
	}
	if errors.Is(err, team.ErrMemberIdentityNotFound) {
		t.Fatal("infra error must not be masked as ErrMemberIdentityNotFound")
	}
	members, _ := svc.ListMembers(ctx, a.ID())
	if len(members) != 0 {
		t.Fatalf("error path must not persist, got %+v", members)
	}
}

// TestMoveMember_AtomicSwitch locks the migration happy path (dev1 lock #7 + #9):
// an agent in team-A migrates to team-B in one shot — it ends up in B, NOT in A,
// and the destination add does NOT self-trip the agent-exclusivity 409 (the bug
// the naive add-into-second-team path hit).
func TestMoveMember_AtomicSwitch(t *testing.T) {
	svc, _ := newService(t)
	svc.WithMemberResolver(&fakeResolver{ok: true})
	ctx := context.Background()
	a := createTeam(t, svc, "A", devRole())
	b := createTeam(t, svc, "B", devRole())
	if _, err := svc.AddMember(ctx, a.ID(), "agent:agent-ada", "dev"); err != nil {
		t.Fatalf("seed add to A: %v", err)
	}

	m, err := svc.MoveMember(ctx, a.ID(), b.ID(), "agent:agent-ada", "dev")
	if err != nil {
		t.Fatalf("MoveMember: %v (must not self-trip exclusivity)", err)
	}
	if m == nil || m.TeamID != b.ID() {
		t.Fatalf("moved member = %+v, want team %s", m, b.ID())
	}
	if ms, _ := svc.ListMembers(ctx, a.ID()); len(ms) != 0 {
		t.Fatalf("source team A must be empty after migration, got %+v", ms)
	}
	if ms, _ := svc.ListMembers(ctx, b.ID()); len(ms) != 1 || ms[0].Ref != "agent:agent-ada" {
		t.Fatalf("dest team B must hold the agent, got %+v", ms)
	}
}

// TestMoveMember_UnresolvableRefKeepsSource locks migration lock #8: an
// unresolvable ref is rejected AND the atomic tx rolls back — the agent is NOT
// removed from the source team (no half-migration / stranding).
func TestMoveMember_UnresolvableRefKeepsSource(t *testing.T) {
	svc, _ := newService(t)
	svc.WithMemberResolver(&fakeResolver{ok: true})
	ctx := context.Background()
	a := createTeam(t, svc, "A", devRole())
	b := createTeam(t, svc, "B", devRole())
	if _, err := svc.AddMember(ctx, a.ID(), "agent:agent-ada", "dev"); err != nil {
		t.Fatalf("seed add to A: %v", err)
	}

	// Flip the resolver to reject, then attempt migration.
	svc.WithMemberResolver(&fakeResolver{ok: false})
	_, err := svc.MoveMember(ctx, a.ID(), b.ID(), "agent:agent-ada", "dev")
	if !errors.Is(err, team.ErrMemberIdentityNotFound) {
		t.Fatalf("unresolvable migration: got %v want ErrMemberIdentityNotFound", err)
	}
	// Atomicity: source untouched, dest empty.
	if ms, _ := svc.ListMembers(ctx, a.ID()); len(ms) != 1 {
		t.Fatalf("failed migration must keep agent in source A, got %+v", ms)
	}
	if ms, _ := svc.ListMembers(ctx, b.ID()); len(ms) != 0 {
		t.Fatalf("failed migration must not add to dest B, got %+v", ms)
	}
}

// TestMoveMember_StaleSourceRejected locks that a wrong/stale migrate_from (the
// agent is not actually on the named source team) fails with ErrMemberNotFound and
// does not add to the destination — no silent duplicate membership.
func TestMoveMember_StaleSourceRejected(t *testing.T) {
	svc, _ := newService(t)
	svc.WithMemberResolver(&fakeResolver{ok: true})
	ctx := context.Background()
	a := createTeam(t, svc, "A", devRole())
	b := createTeam(t, svc, "B", devRole())

	// agent-ada was never added to A.
	_, err := svc.MoveMember(ctx, a.ID(), b.ID(), "agent:agent-ada", "dev")
	if !errors.Is(err, team.ErrMemberNotFound) {
		t.Fatalf("stale migrate_from: got %v want ErrMemberNotFound", err)
	}
	if ms, _ := svc.ListMembers(ctx, b.ID()); len(ms) != 0 {
		t.Fatalf("stale migration must not add to dest, got %+v", ms)
	}
}

// TestAddMember_NilResolverDegrades locks the opt-in degrade: with no resolver
// wired (tests / unwired deployments) AddMember keeps its pre-hardening behavior
// and does not fail closed or panic.
func TestAddMember_NilResolverDegrades(t *testing.T) {
	svc, _ := newService(t) // no WithMemberResolver
	a := createTeam(t, svc, "A", devRole())
	ctx := context.Background()

	if _, err := svc.AddMember(ctx, a.ID(), "agent:never-provisioned", "dev"); err != nil {
		t.Fatalf("nil resolver must degrade to pass-through, got %v", err)
	}
	members, _ := svc.ListMembers(ctx, a.ID())
	if len(members) != 1 {
		t.Fatalf("degrade path should add, got %+v", members)
	}
}
