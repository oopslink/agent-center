package team

import (
	"errors"
	"testing"
	"time"
)

func TestMemberRef_Kind(t *testing.T) {
	cases := []struct {
		ref  string
		want MemberKind
		err  bool
	}{
		{"agent:01H", MemberKindAgent, false},
		{"user:jane", MemberKindHuman, false},
		{"agent:", "", true},
		{"user:", "", true},
		{"nobody", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := MemberRef(c.ref).Kind()
		if c.err {
			if !errors.Is(err, ErrInvalidMemberRef) {
				t.Errorf("%q: got err %v want ErrInvalidMemberRef", c.ref, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got (%v, %v) want (%v, nil)", c.ref, got, err, c.want)
		}
	}
}

func TestNewTeam_Validation(t *testing.T) {
	now := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	if _, err := NewTeam(NewTeamInput{ID: "t1", Name: "  ", CreatedAt: now}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("blank name: got %v want ErrInvalidTeam", err)
	}
	if _, err := NewTeam(NewTeamInput{ID: "", Name: "ok", CreatedAt: now}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("blank id: got %v want ErrInvalidTeam", err)
	}
	// Duplicate role declarations rejected.
	_, err := NewTeam(NewTeamInput{ID: "t1", Name: "ok", CreatedAt: now, Roles: []RoleConfig{{Role: "dev"}, {Role: "dev"}}})
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("dup role: got %v want ErrInvalidRole", err)
	}
	// MaxConcurrency defaults to 1 when unset.
	tm, err := NewTeam(NewTeamInput{ID: "t1", Name: "ok", CreatedAt: now, Roles: []RoleConfig{{Role: "dev"}}})
	if err != nil {
		t.Fatal(err)
	}
	if tm.Roles()[0].MaxConcurrency != 1 {
		t.Fatalf("default concurrency: got %d want 1", tm.Roles()[0].MaxConcurrency)
	}
	if tm.Version() != 1 {
		t.Fatalf("initial version: got %d want 1", tm.Version())
	}
}
