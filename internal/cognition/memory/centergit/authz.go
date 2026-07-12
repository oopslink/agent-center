package centergit

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Operation is a smart-HTTP access mode: OpRead (git-upload-pack / clone / pull)
// or OpWrite (git-receive-pack / push).
type Operation int

const (
	// OpRead corresponds to git-upload-pack (fetch/clone/pull).
	OpRead Operation = iota
	// OpWrite corresponds to git-receive-pack (push).
	OpWrite
)

// String renders the operation for logs/errors.
func (o Operation) String() string {
	if o == OpWrite {
		return "write"
	}
	return "read"
}

// Access-control sentinels. Callers map these to HTTP 401 / 403.
var (
	// ErrUnauthenticated means no agent identity was resolved from the request.
	ErrUnauthenticated = errors.New("centergit: unauthenticated")
	// ErrForbidden means the agent is known but not permitted for this repo/op.
	ErrForbidden = errors.New("centergit: forbidden")
)

// TeamMembership is the seam onto S1's team service: the center maintains the
// agent→team mapping and this port answers "which team does this agent belong
// to" (§9 访问控制映射). An agent 独占一个 team, so at most one team id.
//
// S1's Team entity backs this with sqlite; S2 ships MapMembership for bootstrap
// and tests.
type TeamMembership interface {
	// TeamOfAgent returns the team the agent belongs to. ok=false means the
	// agent is in no team (yet). err is reserved for backing-store failures.
	TeamOfAgent(ctx context.Context, agentID string) (teamID string, ok bool, err error)
}

// Authorizer decides whether an authenticated agent may read/write a repo,
// implementing the §4.2/§9 rules:
//
//   - global repo : read = every authenticated agent; write = forbidden
//     (platform-level, not agent-writable).
//   - agent repo  : rw iff the requesting agent owns the repo.
//   - team  repo  : rw iff the agent's team == the repo's team.
type Authorizer struct {
	membership TeamMembership
}

// NewAuthorizer wires an Authorizer over a TeamMembership source.
func NewAuthorizer(m TeamMembership) *Authorizer {
	return &Authorizer{membership: m}
}

// Authorize returns nil when agentID may perform op on ref, else
// ErrUnauthenticated / ErrForbidden (or a backing-store error).
func (a *Authorizer) Authorize(ctx context.Context, agentID string, ref RepoRef, op Operation) error {
	if agentID == "" {
		return ErrUnauthenticated
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	switch ref.Kind {
	case RepoKindGlobal:
		if op == OpRead {
			return nil
		}
		return fmt.Errorf("%w: global repo is read-only for agents", ErrForbidden)

	case RepoKindAgent:
		if ref.ID == agentID {
			return nil
		}
		return fmt.Errorf("%w: agent %q may not %s agent repo %q", ErrForbidden, agentID, op, ref.ID)

	case RepoKindTeam:
		if a.membership == nil {
			return fmt.Errorf("%w: no team membership source configured", ErrForbidden)
		}
		team, ok, err := a.membership.TeamOfAgent(ctx, agentID)
		if err != nil {
			return err
		}
		if ok && team == ref.ID {
			return nil
		}
		return fmt.Errorf("%w: agent %q not a member of team %q", ErrForbidden, agentID, ref.ID)

	default:
		return ErrInvalidRepoRef
	}
}

// MapMembership is a concurrency-safe in-memory TeamMembership. It also models
// "实例化时给新 agent 授权其 team repo" (§9): instantiation calls Grant to record
// the new agent's team, which immediately unlocks rw on that team's repo.
type MapMembership struct {
	mu    sync.RWMutex
	teams map[string]string // agentID → teamID
}

// NewMapMembership returns an empty membership map.
func NewMapMembership() *MapMembership {
	return &MapMembership{teams: make(map[string]string)}
}

// Grant records that agentID belongs to teamID (agent 独占一个 team — this
// overwrites any prior team for the agent).
func (m *MapMembership) Grant(agentID, teamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teams[agentID] = teamID
}

// Revoke removes the agent's team membership.
func (m *MapMembership) Revoke(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.teams, agentID)
}

// TeamOfAgent implements TeamMembership.
func (m *MapMembership) TeamOfAgent(_ context.Context, agentID string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	team, ok := m.teams[agentID]
	return team, ok, nil
}
