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

// listMyTasksReq is the body for POST /admin/agent-tools/list_my_tasks.
type listMyTasksReq struct {
	AgentID string `json:"agent_id"`
}

// listMyTasksHandler is the agent's "what do I have to do?" query in the Task model
// (v2.14.0 I14/F5 §五, replacing get_my_work). It returns the open/running tasks
// assigned to the calling agent that are RUNNABLE now (§13.A — their blockedBy
// dependencies are satisfied), each projected to the §5.2 shape (task_id, title,
// status, blocked_reason, blocked_reason_type, blocked_comment, lease_expires_at).
// Inherently own-scoped; the runnable filter is the same gate start_task enforces,
// so the list never offers a task the agent can't actually start.
func (s *Server) listMyTasksHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listMyTasksReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	tasks, err := d.PMService.ListRunnableAgentTasks(r.Context(), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, agentRunnableTaskMap(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

// listMyInflightTasksHandler is the RUNTIME-facing reconcile query (design §4.2):
// it returns the agent's DISPATCHABLE active tasks — ListAssignedAgentTasks minus the
// ADR-0054 parked states, NOT ListRunnableAgentTasks. "In-flight" means "an executor may
// be relaunched for this"; a parked (blocked) task is active but has nothing
// in flight, and the runtime treats membership here as licence to relaunch. The
// DEPENDENCY set is still unfiltered, which is the reason this is not
// ListRunnableAgentTasks. The difference matters for
// self-recovery: a running-but-deps-unsatisfied task (e.g. a resumed executor whose
// upstream is not yet done) is dropped by the runnable filter but MUST appear here so
// the runtime's boot reconcile reconciles every in-flight executor Record against the
// agent's true in-flight set (adopt / recover / cancel). Same own-scoped auth + wire
// shape as list_my_tasks; it is an admin-only agent-tool route (called by the runtime's
// center client, not exposed as an MCP tool to the supervisor).
func (s *Server) listMyInflightTasksHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listMyTasksReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	tasks, err := d.PMService.ListAssignedAgentTasks(r.Context(), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		// ADR-0054: drop PARKED tasks (blocked).
		// "In-flight" here means "work an executor may be relaunched for", which is
		// exactly TaskStatus.IsDispatchable. A parked task is still active and still on
		// every board — it just has nothing in flight — and leaving it in this set makes
		// boot self-reconcile fork a fresh empty-context executor onto work that is
		// deliberately paused.
		//
		// This stays UNFILTERED in the DEPENDENCY sense (the reason this endpoint is not
		// ListRunnableAgentTasks): IsDispatchable is a pure STATUS predicate, so a
		// running-but-deps-unsatisfied task is still returned.
		if !t.Status().IsDispatchable() {
			continue
		}
		out = append(out, agentRunnableTaskMap(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

// agentRunnableTaskMap projects a pm.Task to the list_my_tasks §5.2 wire shape:
// the identity + status + the blocked annotation (so the agent sees what an Unblock
// left in blocked_comment) + the execution lease deadline (null when none).
func agentRunnableTaskMap(t *pm.Task) map[string]any {
	m := map[string]any{
		"task_id":             string(t.ID()),
		"title":               t.Title(),
		"status":              string(t.Status()),
		"blocked_reason":      t.BlockedReason(),
		"blocked_reason_type": string(t.BlockedReasonType()),
		"blocked_comment":     t.BlockedComment(),
		"lease_expires_at":    nil,
	}
	if exp := t.ExecutionLeaseExpiresAt(); exp != nil {
		m["lease_expires_at"] = exp.Format(time.RFC3339Nano)
	}
	return m
}
