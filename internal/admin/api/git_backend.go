package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/team"
)

// git_backend.go wires the center-hosted git smart-HTTP endpoint (design
// §4.2/§4.3) onto the admin API. The centergit.Handler is mounted at
// /admin/git/ behind the SAME bearer-auth + deps middleware as every other
// admin route (server.go routes → gitPassthrough → ServerDeps.GitHandler).
//
// Auth is per-agent, mirroring requireAgentOnWorker: the bearer proves the
// WORKER (token owner worker:<id>), and the request names the OPERATING agent
// via the X-Agent-Id header. The resolver asserts that agent is bound to the
// token's worker (a worker may never act as another worker's agent) before the
// centergit Authorizer decides read/write against the requested repo.

// gitAgentHeader carries the operating agent id on a git smart-HTTP request.
const gitAgentHeader = "X-Agent-Id"

// NewGitHandler builds the /admin/git/ handler over host + membership. The
// AgentResolver is this package's per-agent gate (bearer worker → X-Agent-Id →
// agent-bound-to-worker check). It is mounted with prefix /admin/git so the
// handler recovers the bare-repo path. Returns an error only when git tooling
// (git-http-backend) cannot be located.
func NewGitHandler(host *centergit.Host, membership centergit.TeamMembership) (http.Handler, error) {
	authz := centergit.NewAuthorizer(membership)
	return centergit.NewHandler(host, authz, gitAgentResolver, centergit.WithMountPrefix("/admin/git"))
}

// gitAgentResolver extracts + authorizes the operating agent for a git request.
// It returns ok=false (→ HTTP 401) when the deps/auth chain cannot prove an
// agent bound to the authenticated worker.
func gitAgentResolver(r *http.Request) (string, bool) {
	d := hd(r)
	if d.AgentSvc == nil {
		return "", false
	}
	auth, ok := AuthFromContext(r.Context())
	if !ok || !strings.HasPrefix(string(auth.Owner), "worker:") {
		return "", false
	}
	workerID := strings.TrimPrefix(string(auth.Owner), "worker:")
	if workerID == "" {
		return "", false
	}
	agentID := strings.TrimSpace(r.Header.Get(gitAgentHeader))
	if agentID == "" {
		return "", false
	}
	a, err := d.AgentSvc.GetAgent(r.Context(), agent.AgentID(agentID))
	if err != nil {
		return "", false
	}
	// HARD GUARDRAIL: the target agent must be bound to the worker proven by the
	// token — identical to requireAgentOnWorker's spine.
	if a.WorkerID() != workerID {
		return "", false
	}
	return agentID, true
}

// =============================================================================
// Team membership adapter (design §9 访问控制映射). Backs the centergit Authorizer
// with the LIVE team + agent tables so an add_member / instantiate_team
// immediately unlocks git rw on the team's shared repo.
// =============================================================================

// teamMembershipRepo is the slice of the team repository the adapter needs.
type teamMembershipRepo interface {
	FindAgentTeam(ctx context.Context, ref team.MemberRef) (team.TeamID, bool, error)
}

// agentDirectory resolves a runtime execution-agent id to its Agent — the adapter
// reads IdentityMemberID off it to cross the id-namespace boundary (see below).
type agentDirectory interface {
	FindByID(ctx context.Context, id agent.AgentID) (*agent.Agent, error)
}

// teamMembership answers "which team does this agent belong to" for the centergit
// Authorizer, backed by the live team + agent repositories.
type teamMembership struct {
	teams  teamMembershipRepo
	agents agentDirectory
}

// NewTeamMembership adapts the S1 team repository onto the centergit
// TeamMembership seam. agents bridges the two id namespaces (see TeamOfAgent) and
// may be nil in degraded wiring, in which case every agent resolves to "no team".
func NewTeamMembership(teams teamMembershipRepo, agents agentDirectory) centergit.TeamMembership {
	return teamMembership{teams: teams, agents: agents}
}

// TeamOfAgent maps a runtime agent id to its team. gitAgentResolver authorizes on
// the RUNTIME execution-agent id (a ULID — the SAME value agent-repo ownership
// keys on, so the resolver must keep returning it), but team member refs are
// stored in the identity-member namespace ("agent:agent-<...>" — the ref
// add_member/instantiate_team persist). Querying the team tables with the raw
// ULID therefore NEVER matches, so every real member — including instantiate_team's
// freshly minted agents — would get 403 on its own team repo. Bridge the two
// namespaces via the agent's IdentityMemberID before the lookup. (id-namespace
// mismatch family — same class of ref-vs-id error as the taskReassigned
// ULID-vs-agent-ref bug.)
func (m teamMembership) TeamOfAgent(ctx context.Context, agentID string) (string, bool, error) {
	ref, ok, err := m.memberRef(ctx, agentID)
	if err != nil || !ok {
		return "", false, err
	}
	id, ok, err := m.teams.FindAgentTeam(ctx, ref)
	if err != nil {
		return "", false, err
	}
	return id.String(), ok, nil
}

// memberRef resolves the runtime execution-agent id to the identity-member-facing
// team ref ("agent:agent-<...>"). ok=false (no error) when the agent is unknown or
// carries no identity-member id — an agent with no member identity can hold no
// team membership, so it correctly resolves to "no team" rather than erroring.
func (m teamMembership) memberRef(ctx context.Context, runtimeAgentID string) (team.MemberRef, bool, error) {
	if m.agents == nil {
		return "", false, nil
	}
	a, err := m.agents.FindByID(ctx, agent.AgentID(runtimeAgentID))
	if err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	memberID := strings.TrimSpace(a.IdentityMemberID())
	if memberID == "" {
		return "", false, nil
	}
	return team.MemberRef("agent:" + memberID), true, nil
}

// gitPassthrough is the /admin/git/ route handler. It rides the bearer-auth +
// deps middleware (so an unauthenticated request is already 401'd upstream) and
// delegates to the wired centergit handler. Unwired git → 501.
func (s *Server) gitPassthrough(w http.ResponseWriter, r *http.Request) {
	if s.deps.GitHandler == nil {
		writeError(w, http.StatusNotImplemented, "git_not_wired", "center-hosted git storage is not enabled")
		return
	}
	s.deps.GitHandler.ServeHTTP(w, r)
}
