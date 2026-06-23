package api

import (
	"net/http"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// start_task / heartbeat — v2.14.0 I14/F5 §五 (Task pull model; replaces the
// work-item start_task/fail_task and get_my_work). The agent, via the MCP
// start_task / heartbeat tools, drives its OWN Task queue by task_id:
//   - start_task — open→running on the Task (pm.StartTask), gated by §13.A
//     EnsureTaskRunnable (blockedBy deps satisfied) so an agent can't run ahead of
//     its upstream, and granting the §2.5 execution lease. Single-active is enforced
//     by the idx_tasks_one_active_per_agent UNIQUE index → pm.ErrAgentHasActiveTask
//     → 409 agent_busy (the task stays open, queue-drain not drop).
//   - heartbeat  — renew the running task's execution lease (pm.HeartbeatTask) so the
//     background lease-checker does not reclaim it; only the assignee may renew and a
//     blocked task has no lease (pm.ErrTaskBlocked → 409 task_blocked).
//
// v2.14.0 F7 (issue I14): the legacy work-item fail_task / pause_task / resume_task
// handlers (and writeWorkStateError) were removed with this file's rewrite —
// AgentWorkItem retired. Only the Task-model start_task / heartbeat tools remain.

type startTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

func (s *Server) startWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req startTaskReq
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
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	actor := pm.IdentityRef(agentActor(a))
	// §13.A run-ahead gate: an agent may start a task ONLY once its blockedBy
	// dependencies are satisfied (EnsureTaskRunnable) — the same gate the deleted
	// work-item start path enforced through the agentsvc.TaskRunGate port.
	if err := d.PMService.EnsureTaskRunnable(r.Context(), pm.TaskID(req.TaskID)); err != nil {
		mapDomainError(w, err)
		return
	}
	if err := d.PMService.StartTask(r.Context(), pm.TaskID(req.TaskID), actor); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": req.TaskID,
		"status":  string(pm.TaskRunning),
	})
}

type taskHeartbeatReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

func (s *Server) heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req taskHeartbeatReq
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
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	if err := d.PMService.HeartbeatTask(r.Context(), pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a))); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"task_id": req.TaskID,
	})
}
