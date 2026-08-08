package team

import (
	"errors"
	"sort"
	"strings"
)

// TeamMemoryPolicyMode gates controlled writes to Team Memory. proposal_only is
// the zero-trust default: agents may propose, owner/admin humans review.
type TeamMemoryPolicyMode string

const (
	TeamMemoryProposalOnly TeamMemoryPolicyMode = "proposal_only"
	TeamMemoryCuratorAuto  TeamMemoryPolicyMode = "curator_auto"
)

// TeamMemoryPolicy is owned by the Team aggregate. CuratorAgentRefs must be
// explicit agent member refs ("agent:<id>"); capability tags are not grants.
type TeamMemoryPolicy struct {
	Mode             TeamMemoryPolicyMode
	CuratorAgentRefs []MemberRef
}

// DefaultTeamMemoryPolicy returns the ADR-0057 default.
func DefaultTeamMemoryPolicy() TeamMemoryPolicy {
	return TeamMemoryPolicy{Mode: TeamMemoryProposalOnly}
}

// Normalize validates and canonicalizes a TeamMemoryPolicy.
func (p TeamMemoryPolicy) Normalize() (TeamMemoryPolicy, error) {
	mode := p.Mode
	if mode == "" {
		mode = TeamMemoryProposalOnly
	}
	switch mode {
	case TeamMemoryProposalOnly, TeamMemoryCuratorAuto:
	default:
		return TeamMemoryPolicy{}, ErrInvalidTeamMemoryPolicy
	}
	seen := map[string]struct{}{}
	refs := make([]MemberRef, 0, len(p.CuratorAgentRefs))
	for _, raw := range p.CuratorAgentRefs {
		ref := MemberRef(strings.TrimSpace(raw.String()))
		if ref == "" {
			return TeamMemoryPolicy{}, ErrInvalidTeamMemoryPolicy
		}
		kind, err := ref.Kind()
		if err != nil || kind != MemberKindAgent {
			return TeamMemoryPolicy{}, ErrInvalidTeamMemoryPolicy
		}
		if _, ok := seen[ref.String()]; ok {
			continue
		}
		seen[ref.String()] = struct{}{}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i] < refs[j] })
	return TeamMemoryPolicy{Mode: mode, CuratorAgentRefs: refs}, nil
}

// IsCurator reports whether ref is explicitly granted under curator_auto mode.
func (p TeamMemoryPolicy) IsCurator(ref MemberRef) bool {
	n, err := p.Normalize()
	if err != nil || n.Mode != TeamMemoryCuratorAuto {
		return false
	}
	want := strings.TrimSpace(ref.String())
	for _, got := range n.CuratorAgentRefs {
		if got.String() == want {
			return true
		}
	}
	return false
}

// RevokeCurator removes a member ref from the policy grant set.
func (p TeamMemoryPolicy) RevokeCurator(ref MemberRef) TeamMemoryPolicy {
	n, err := p.Normalize()
	if err != nil {
		return DefaultTeamMemoryPolicy()
	}
	want := strings.TrimSpace(ref.String())
	out := n.CuratorAgentRefs[:0]
	for _, got := range n.CuratorAgentRefs {
		if got.String() != want {
			out = append(out, got)
		}
	}
	n.CuratorAgentRefs = append([]MemberRef(nil), out...)
	return n
}

// ValidateCuratorRefs asserts every curator grant points at a current agent
// member of the same team.
func ValidateCuratorRefs(policy TeamMemoryPolicy, members []*TeamMember) error {
	n, err := policy.Normalize()
	if err != nil {
		return err
	}
	memberSet := make(map[MemberRef]struct{}, len(members))
	for _, m := range members {
		if m == nil || m.Kind != MemberKindAgent {
			continue
		}
		memberSet[m.Ref] = struct{}{}
	}
	for _, ref := range n.CuratorAgentRefs {
		if _, ok := memberSet[ref]; !ok {
			return errors.Join(ErrInvalidTeamMemoryPolicy, ErrMemberNotFound)
		}
	}
	return nil
}
