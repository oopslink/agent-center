package api

import (
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
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
