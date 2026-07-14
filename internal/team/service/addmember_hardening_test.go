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
