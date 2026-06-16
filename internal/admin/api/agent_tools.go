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
	// WS2 (#issue-e346e5ec): get_my_work is the SINGLE "what do I have to do?"
	// query — it partitions the agent's work items into the actionable buckets
	// the loop needs (active / queued / paused / waiting_input), replacing the
	// former get_my_active_work + list_my_paused_work tools. Terminal items
	// (done/failed/canceled/superseded) are history and intentionally omitted.
	active := make([]map[string]any, 0)
	queued := make([]map[string]any, 0)
	paused := make([]map[string]any, 0)
	waiting := make([]map[string]any, 0)
	for _, it := range items {
		switch it.Status() {
		case agent.WorkItemActive:
			active = append(active, workItemMap(it))
		case agent.WorkItemQueued:
			queued = append(queued, workItemMap(it))
		case agent.WorkItemPaused:
			paused = append(paused, workItemMap(it))
		case agent.WorkItemWaitingInput:
			waiting = append(waiting, workItemMap(it))
		}
	}
	resp := map[string]any{
		"active":        active,
		"queued":        queued,
		"paused":        paused,
		"waiting_input": waiting,
	}

	// ADR-0047 PULL pool: the built-in "assignment pool" dispatches via pull — it
	// creates NO WorkItem and posts NO wake. Its claimable tasks would therefore be
	// invisible to the WorkItem-only surface above. So we ALSO query pm for what the
	// agent can claim and surface it under "claimable" (WS2 folds in the former
	// list_assignment_pool tool). The agent pulls + claims these (open→running) via
	// claim_task rather than being woken. Nil-safe: only when PMService is wired.
	//
	// Two disjoint sources, merged into one "claimable" bucket — each entry carries
	// `assignee` so the agent can tell them apart:
	//   - ListClaimableTasks  — tasks DISPATCHED to this agent (assigned + open + in
	//     a plan), i.e. claimable work meant for it.
	//   - ListClaimablePool   — the OPEN, UNASSIGNED shared pool the agent is eligible
	//     to grab across its member projects (the former list_assignment_pool surface).
	if d.PMService != nil {
		claimable := make([]map[string]any, 0)
		assigned, cerr := d.PMService.ListClaimableTasks(r.Context(), pm.IdentityRef(agentActor(a)))
		if cerr != nil {
			mapDomainError(w, cerr)
			return
		}
		for _, c := range assigned {
			m := agentTaskMap(c.Task)
			m["node_status"] = string(c.NodeStatus)
			m["claimable"] = true
			claimable = append(claimable, m)
		}
		pool, perr := d.PMService.ListClaimablePool(r.Context(), pm.IdentityRef(agentActor(a)))
		if perr != nil {
			mapDomainError(w, perr)
			return
		}
		for _, c := range pool {
			m := agentTaskMap(c.Task)
			m["node_status"] = string(c.NodeStatus)
			m["claimable"] = true
			claimable = append(claimable, m)
		}
		resp["claimable"] = claimable

		// T83: a CLAIMED pool task is running with NO WorkItem (pull/no-wake), so it
		// would otherwise vanish after the agent claims it. Surface the agent's
		// in-flight claimed pool work under "claimed_pool" so "my work" includes the
		// pool tasks already running on it (and survives wake/restart).
		held, herr := d.PMService.ListHeldPoolTasks(r.Context(), pm.IdentityRef(agentActor(a)))
		if herr != nil {
			mapDomainError(w, herr)
			return
		}
		mp := make([]map[string]any, 0, len(held))
		for _, c := range held {
			m := agentTaskMap(c.Task)
			m["node_status"] = string(c.NodeStatus)
			mp = append(mp, m)
		}
		resp["claimed_pool"] = mp
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
