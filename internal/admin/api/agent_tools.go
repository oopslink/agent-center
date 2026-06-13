package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// Agent MCP tool surface — per-agent authorization base (v2.7 D2-b1, ADR-0049).
//
// A Worker daemon runs MANY agents (one claude process = one agent). The Worker
// authenticates to this admin API with a bearer token whose owner is
// `worker:<id>` (AuthMiddleware populates AuthContext.Owner). An MCP tool call
// carries the OPERATING agent's id in the body; the center authorizes PER-AGENT
// (OQ4/OQ6), confined to the agent's own Project/Org domain.
//
// HARD GUARDRAIL (the security spine these tools build on): the authenticated
// worker is taken from the TOKEN OWNER, never from the request body, and the
// target Agent MUST be bound to that worker (agent.WorkerID() == worker-from-
// token), else 403 — otherwise a worker could impersonate any agent. All access
// goes through the Agent BC AppService (AppServices are the only entry); the
// handler never touches the DB directly.
//
// b1 ships the gate + ONE representative read tool (get_my_work) to prove it;
// b2 / D2-d tools reuse requireAgentOnWorker.
// =============================================================================

// requireAgentOnWorker is the per-agent auth gate. It returns the resolved
// Agent (the agent caller context — ID()/OrganizationID() — that b2's tools use
// for per-agent OQ4/OQ6) and true on success; on any failure it writes the
// error envelope and returns (nil, false).
//
// The check chain:
//  1. AgentService wired? else 501.
//  2. Authenticated owner is a `worker:` owner (from the TOKEN, via
//     AuthFromContext — NOT the body), else 403. workerID = strip prefix.
//  3. agentID present, else 400.
//  4. Load the Agent via the AppService; ErrAgentNotFound → 404, other → 500.
//  5. GUARDRAIL: agent.WorkerID() == workerID, else 403. (Authenticated worker,
//     wrong agent → 403, not 404: we don't leak whether the agent exists
//     elsewhere beyond the 404/403 distinction.)
func (s *Server) requireAgentOnWorker(w http.ResponseWriter, r *http.Request, d HandlerDeps, agentID string) (*agent.Agent, bool) {
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_svc_not_wired", "")
		return nil, false
	}
	auth, ok := AuthFromContext(r.Context())
	if !ok || !strings.HasPrefix(string(auth.Owner), "worker:") {
		writeError(w, http.StatusForbidden, "not_a_worker_token",
			"agent tools require a worker:<id> bearer")
		return nil, false
	}
	workerID := strings.TrimPrefix(string(auth.Owner), "worker:")
	if workerID == "" {
		writeError(w, http.StatusForbidden, "not_a_worker_token",
			"worker token has empty worker id")
		return nil, false
	}
	if strings.TrimSpace(agentID) == "" {
		writeError(w, http.StatusBadRequest, "missing_agent_id", "")
		return nil, false
	}
	a, err := d.AgentSvc.GetAgent(r.Context(), agent.AgentID(agentID))
	if err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return nil, false
	}
	// HARD GUARDRAIL: the target Agent must be bound to the worker proven by the
	// token. A worker may never operate another worker's agent.
	if a.WorkerID() != workerID {
		writeError(w, http.StatusForbidden, "agent_not_bound_to_worker",
			"agent is not bound to this worker")
		return nil, false
	}
	return a, true
}

// getMyWorkReq is the body for POST /admin/agent-tools/get_my_work.
type getMyWorkReq struct {
	AgentID string `json:"agent_id"`
}

// getMyWorkHandler is the representative read tool: it returns the OPERATING
// agent's own WorkItems. Inherently own-scoped — the agent reads only its own
// queue + history — demonstrating per-agent read scope on top of the guardrail.
func (s *Server) getMyWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getMyWorkReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	items, err := d.AgentSvc.ListWorkItems(r.Context(), a.ID())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i] = workItemMap(it)
	}
	resp := map[string]any{"work_items": out}

	// ADR-0047 PULL pool: the built-in "assignment pool" dispatches via pull — it
	// creates NO WorkItem and posts NO wake. Its claimable tasks would therefore be
	// invisible to the WorkItem-only surface above. So we ALSO query pm for the
	// agent's CLAIMABLE tasks (open + assigned-to-it + in a plan + node dispatched)
	// and surface them under "claimable_tasks". The agent pulls + claims these
	// (open→running) rather than being woken. Nil-safe: only when PMService is wired.
	if d.PMService != nil {
		claimable, cerr := d.PMService.ListClaimableTasks(r.Context(), pm.IdentityRef(agentActor(a)))
		if cerr != nil {
			mapDomainError(w, cerr)
			return
		}
		ct := make([]map[string]any, len(claimable))
		for i, c := range claimable {
			m := agentTaskMap(c.Task)
			m["node_status"] = string(c.NodeStatus)
			m["claimable"] = true
			ct[i] = m
		}
		resp["claimable_tasks"] = ct
	}

	writeJSON(w, http.StatusOK, resp)
}

// workItemMap projects an AgentWorkItem to the JSON wire shape.
func workItemMap(it *agent.AgentWorkItem) map[string]any {
	return map[string]any{
		"id":           it.ID(),
		"task_ref":     it.TaskRef(),
		"status":       string(it.Status()),
		"interactions": it.Interactions(),
		"created_at":   it.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":   it.UpdatedAt().Format(time.RFC3339Nano),
		"version":      it.Version(),
	}
}
