package team

import (
	"errors"
	"testing"
)

func TestTeamMemoryPolicyNormalize(t *testing.T) {
	got, err := (TeamMemoryPolicy{
		Mode:             TeamMemoryCuratorAuto,
		CuratorAgentRefs: []MemberRef{"agent:b", "agent:a", "agent:a"},
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.Mode != TeamMemoryCuratorAuto {
		t.Fatalf("mode=%q", got.Mode)
	}
	if len(got.CuratorAgentRefs) != 2 || got.CuratorAgentRefs[0] != "agent:a" || got.CuratorAgentRefs[1] != "agent:b" {
		t.Fatalf("refs=%v", got.CuratorAgentRefs)
	}
	if _, err := (TeamMemoryPolicy{Mode: "open_write"}).Normalize(); !errors.Is(err, ErrInvalidTeamMemoryPolicy) {
		t.Fatalf("invalid mode err=%v", err)
	}
	if _, err := (TeamMemoryPolicy{Mode: TeamMemoryCuratorAuto, CuratorAgentRefs: []MemberRef{"user:u1"}}).Normalize(); !errors.Is(err, ErrInvalidTeamMemoryPolicy) {
		t.Fatalf("human curator err=%v", err)
	}
}

func TestValidateCuratorRefsRequireAgentMember(t *testing.T) {
	policy := TeamMemoryPolicy{Mode: TeamMemoryCuratorAuto, CuratorAgentRefs: []MemberRef{"agent:a1"}}
	if err := ValidateCuratorRefs(policy, []*TeamMember{{Ref: "agent:a1", Kind: MemberKindAgent}}); err != nil {
		t.Fatalf("valid curator: %v", err)
	}
	if err := ValidateCuratorRefs(policy, []*TeamMember{{Ref: "agent:other", Kind: MemberKindAgent}}); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("missing curator err=%v", err)
	}
}
