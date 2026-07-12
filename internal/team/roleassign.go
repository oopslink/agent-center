package team

import (
	"sort"
	"strings"
)

// roleassign.go implements the author-time role→agent helper (design §7).
//
// Design premise: plans are authored AGAINST a concrete team roster, not late-
// bound at runtime. When building a plan the author tags a node with a ROLE
// (plus optional constraints); this helper resolves each role to a CONCRETE
// agent off the team's CURRENT roster and the plan stores that concrete agent.
// If the team changes, the author re-runs the helper against the new roster.
//
// Resolution rules (design §7):
//   - a role with a single agent member  → that agent;
//   - a role with several agent members  → pick by strategy (default: the least
//     busy; round-robin as an alternative). capability_tags matching is left for
//     the future.
//   - cross-review constraint Review ≠ Dev: a node may declare AvoidNodes; its
//     resolved agent must differ from the agents those referenced nodes resolve
//     to. Referenced nodes are resolved first.

// AssignStrategy selects among several agents staffing one role.
type AssignStrategy string

const (
	// StrategyLeastBusy picks the candidate with the lowest current load
	// (Busyness oracle + assignments already made in this batch). Default.
	StrategyLeastBusy AssignStrategy = "least_busy"
	// StrategyRoundRobin rotates through a role's members in roster order,
	// ignoring external busyness — useful for deterministic fan-out.
	StrategyRoundRobin AssignStrategy = "round_robin"
)

// Roster is the authoritative role→agents view of an instantiated team (design
// §7: "team = 权威名册"). Only agent members participate; humans are excluded.
type Roster struct {
	// byRole preserves membership order per role (roster order), which drives
	// round-robin and stable tie-breaks.
	byRole map[string][]MemberRef
}

// NewRoster builds a Roster from a team's members, keeping only agent members
// and preserving their given order within each role.
func NewRoster(members []*TeamMember) *Roster {
	r := &Roster{byRole: make(map[string][]MemberRef)}
	for _, m := range members {
		if m == nil || m.Kind != MemberKindAgent {
			continue
		}
		r.byRole[m.Role] = append(r.byRole[m.Role], m.Ref)
	}
	return r
}

// Agents returns the agent members staffing role (roster order). The returned
// slice is a copy; callers may mutate it freely.
func (r *Roster) Agents(role string) []MemberRef {
	src := r.byRole[role]
	out := make([]MemberRef, len(src))
	copy(out, src)
	return out
}

// Roles returns the roles that have at least one agent member, sorted.
func (r *Roster) Roles() []string {
	out := make([]string, 0, len(r.byRole))
	for role := range r.byRole {
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}

// BusynessFunc reports the current load of an agent (e.g. count of active/queued
// nodes). Lower is freer. A nil oracle is treated as all-zero.
type BusynessFunc func(agent MemberRef) int

// NodeAssignRequest asks the helper to resolve one plan node's role to an agent.
type NodeAssignRequest struct {
	// NodeKey uniquely identifies the plan node within this batch (author's
	// handle; e.g. the node id or a stable slug).
	NodeKey string
	// Role is the team-declared role to resolve.
	Role string
	// AvoidNodes lists other NodeKeys in this batch whose resolved agent must be
	// avoided — this is how the Review ≠ Dev constraint is expressed (a Review
	// node lists the Dev node(s) it must not share an agent with).
	AvoidNodes []string
}

// NodeAssignment is the resolved binding written back onto the plan node.
type NodeAssignment struct {
	NodeKey string
	Role    string
	Agent   MemberRef
}

// ResolveRoles resolves every request to a concrete agent off the roster,
// honouring AvoidNodes constraints. Referenced nodes are resolved first (a
// cycle is rejected with ErrCyclicAvoid). strategy defaults to StrategyLeastBusy
// when empty. busyness may be nil (all agents equally free).
//
// The returned slice is ordered to match reqs. Errors:
//   - ErrRoleNotStaffed          role has no agent members;
//   - ErrConstraintUnsatisfiable constraints exclude every candidate;
//   - ErrUnknownNodeRef          an AvoidNodes entry is not in the batch;
//   - ErrCyclicAvoid             avoid references form a cycle.
func ResolveRoles(roster *Roster, reqs []NodeAssignRequest, busyness BusynessFunc, strategy AssignStrategy) ([]NodeAssignment, error) {
	if strategy == "" {
		strategy = StrategyLeastBusy
	}
	if busyness == nil {
		busyness = func(MemberRef) int { return 0 }
	}

	byKey := make(map[string]NodeAssignRequest, len(reqs))
	for _, req := range reqs {
		byKey[req.NodeKey] = req
	}

	resolved := make(map[string]MemberRef, len(reqs))
	// inBatch tracks how many nodes this batch has already handed to each agent,
	// so least-busy also spreads load WITHIN a single plan authoring pass.
	inBatch := make(map[MemberRef]int)
	// rrCursor advances round-robin per role across the batch.
	rrCursor := make(map[string]int)

	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(reqs))

	var resolve func(key string) error
	resolve = func(key string) error {
		switch state[key] {
		case done:
			return nil
		case visiting:
			return ErrCyclicAvoid
		}
		req, ok := byKey[key]
		if !ok {
			return ErrUnknownNodeRef
		}
		state[key] = visiting

		avoid := make(map[MemberRef]struct{})
		for _, ref := range req.AvoidNodes {
			if _, inSet := byKey[ref]; !inSet {
				return ErrUnknownNodeRef
			}
			if err := resolve(ref); err != nil {
				return err
			}
			avoid[resolved[ref]] = struct{}{}
		}

		agent, err := pick(roster.byRole[req.Role], avoid, busyness, inBatch, rrCursor, req.Role, strategy)
		if err != nil {
			return err
		}
		resolved[key] = agent
		inBatch[agent]++
		state[key] = done
		return nil
	}

	for _, req := range reqs {
		if err := resolve(req.NodeKey); err != nil {
			return nil, err
		}
	}

	out := make([]NodeAssignment, len(reqs))
	for i, req := range reqs {
		out[i] = NodeAssignment{NodeKey: req.NodeKey, Role: req.Role, Agent: resolved[req.NodeKey]}
	}
	return out, nil
}

// pick chooses one agent for a role from candidates, excluding avoid, per
// strategy. It returns ErrRoleNotStaffed when the role is empty and
// ErrConstraintUnsatisfiable when avoid removes every candidate.
func pick(candidates []MemberRef, avoid map[MemberRef]struct{}, busyness BusynessFunc, inBatch map[MemberRef]int, rrCursor map[string]int, role string, strategy AssignStrategy) (MemberRef, error) {
	if len(candidates) == 0 {
		return "", ErrRoleNotStaffed
	}
	eligible := make([]MemberRef, 0, len(candidates))
	for _, c := range candidates {
		if _, blocked := avoid[c]; blocked {
			continue
		}
		eligible = append(eligible, c)
	}
	if len(eligible) == 0 {
		return "", ErrConstraintUnsatisfiable
	}

	if strategy == StrategyRoundRobin {
		// Advance the role cursor until it lands on an eligible candidate,
		// scanning at most len(candidates) positions so a fully-blocked start
		// position still finds the next eligible one deterministically.
		n := len(candidates)
		for i := 0; i < n; i++ {
			idx := (rrCursor[role] + i) % n
			cand := candidates[idx]
			if _, blocked := avoid[cand]; blocked {
				continue
			}
			rrCursor[role] = idx + 1
			return cand, nil
		}
		return "", ErrConstraintUnsatisfiable
	}

	// least-busy: minimise (external busyness + in-batch load); tie-break by
	// roster order (stable) via the candidates slice ordering already captured
	// in eligible.
	best := eligible[0]
	bestLoad := busyness(best) + inBatch[best]
	for _, c := range eligible[1:] {
		load := busyness(c) + inBatch[c]
		if load < bestLoad {
			best, bestLoad = c, load
		}
	}
	return best, nil
}

// ResolveRole is a convenience for the common single-node case with no avoid
// constraints. It resolves one role to one agent off the roster.
func ResolveRole(roster *Roster, role string, busyness BusynessFunc, strategy AssignStrategy) (MemberRef, error) {
	out, err := ResolveRoles(roster, []NodeAssignRequest{{NodeKey: strings.TrimSpace(role) + "#0", Role: role}}, busyness, strategy)
	if err != nil {
		return "", err
	}
	return out[0].Agent, nil
}
