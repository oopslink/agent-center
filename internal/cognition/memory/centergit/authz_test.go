package centergit

import (
	"context"
	"errors"
	"testing"
)

func TestAuthorizeMatrix(t *testing.T) {
	ctx := context.Background()
	mem := NewMapMembership()
	mem.Grant("agent-1", "team-alpha") // 实例化时给新 agent 授权其 team repo
	authz := NewAuthorizer(mem)

	tests := []struct {
		name    string
		agent   string
		ref     RepoRef
		op      Operation
		wantErr error // nil / ErrForbidden / ErrUnauthenticated
	}{
		{"own agent repo read", "agent-1", AgentRepo("agent-1"), OpRead, nil},
		{"own agent repo write", "agent-1", AgentRepo("agent-1"), OpWrite, nil},
		{"other agent repo", "agent-1", AgentRepo("agent-2"), OpRead, ErrForbidden},
		{"team member rw read", "agent-1", TeamRepo("team-alpha"), OpRead, nil},
		{"team member rw write", "agent-1", TeamRepo("team-alpha"), OpWrite, nil},
		{"non-member team", "agent-1", TeamRepo("team-beta"), OpWrite, ErrForbidden},
		{"no-team agent on team", "stranger", TeamRepo("team-alpha"), OpRead, ErrForbidden},
		{"global read all", "stranger", GlobalRepo(), OpRead, nil},
		{"global write forbidden", "agent-1", GlobalRepo(), OpWrite, ErrForbidden},
		{"unauthenticated", "", AgentRepo("agent-1"), OpRead, ErrUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := authz.Authorize(ctx, tc.agent, tc.ref, tc.op)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want allow, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAuthorizeInvalidRef(t *testing.T) {
	authz := NewAuthorizer(NewMapMembership())
	if err := authz.Authorize(context.Background(), "a", RepoRef{Kind: "bogus", ID: "x"}, OpRead); err == nil {
		t.Fatal("expected error for invalid ref kind")
	}
}

func TestAuthorizeTeamWithoutMembershipSource(t *testing.T) {
	authz := NewAuthorizer(nil)
	err := authz.Authorize(context.Background(), "a", TeamRepo("t"), OpRead)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden when no membership source, got %v", err)
	}
}

func TestMapMembershipGrantRevoke(t *testing.T) {
	ctx := context.Background()
	m := NewMapMembership()
	if _, ok, _ := m.TeamOfAgent(ctx, "a"); ok {
		t.Fatal("expected no membership initially")
	}
	m.Grant("a", "t1")
	if team, ok, _ := m.TeamOfAgent(ctx, "a"); !ok || team != "t1" {
		t.Fatalf("Grant not reflected: team=%q ok=%v", team, ok)
	}
	m.Grant("a", "t2") // agent 独占一个 team — overwrites
	if team, _, _ := m.TeamOfAgent(ctx, "a"); team != "t2" {
		t.Fatalf("re-Grant should overwrite, got %q", team)
	}
	m.Revoke("a")
	if _, ok, _ := m.TeamOfAgent(ctx, "a"); ok {
		t.Fatal("Revoke should remove membership")
	}
}

// errMembership is a TeamMembership that always errors, to cover the
// backing-store error propagation path.
type errMembership struct{}

func (errMembership) TeamOfAgent(context.Context, string) (string, bool, error) {
	return "", false, errors.New("boom")
}

func TestAuthorizePropagatesMembershipError(t *testing.T) {
	authz := NewAuthorizer(errMembership{})
	err := authz.Authorize(context.Background(), "a", TeamRepo("t"), OpRead)
	if err == nil || errors.Is(err, ErrForbidden) {
		t.Fatalf("want raw backing-store error, got %v", err)
	}
}

func TestOperationString(t *testing.T) {
	if OpRead.String() != "read" || OpWrite.String() != "write" {
		t.Fatalf("unexpected Operation.String: %q %q", OpRead, OpWrite)
	}
}
