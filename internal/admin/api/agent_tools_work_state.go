package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
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
// pause_task / resume_task / fail_task below remain on the legacy work-item model
// (removed from the agent-facing surface in F5; their code is deleted with
// AgentWorkItem in F7).

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

type failWorkReq struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
}

func (s *Server) failWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req failWorkReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_svc_not_wired", "")
		return
	}
	if strings.TrimSpace(req.WorkItemID) == "" {
		writeError(w, http.StatusBadRequest, "missing_work_item_id", "")
		return
	}
	if err := d.AgentSvc.FailWork(r.Context(), a.ID(), req.WorkItemID); err != nil {
		writeWorkStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"work_item_id": req.WorkItemID,
		"status":       string(agent.WorkItemFailed),
	})
}

// pause_task / resume_task — v2.8.1 #278 PR4 scheduling autonomy (tool names
// task-lexicon since WS1/T197). The SELF half of the T200 WS4 pause/resume model:
// pause_task sets the agent's own active item aside (active→paused, releases the
// single-active slot); resume_task re-acquires the slot (paused→active,
// single-active-gated). (block_task is the external-dependency axis;
// resume_paused_node is the operator cross-agent resume — both are separate tools.)

type pauseWorkReq struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
	Reason     string `json:"reason"`
}

func (s *Server) pauseWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req pauseWorkReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_svc_not_wired", "")
		return
	}
	if strings.TrimSpace(req.WorkItemID) == "" {
		writeError(w, http.StatusBadRequest, "missing_work_item_id", "")
		return
	}
	if err := d.AgentSvc.PauseWork(r.Context(), a.ID(), req.WorkItemID, req.Reason); err != nil {
		writeWorkStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"work_item_id": req.WorkItemID,
		"status":       string(agent.WorkItemPaused),
	})
}

type resumeWorkReq struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
}

func (s *Server) resumeWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req resumeWorkReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_svc_not_wired", "")
		return
	}
	if strings.TrimSpace(req.WorkItemID) == "" {
		writeError(w, http.StatusBadRequest, "missing_work_item_id", "")
		return
	}
	if err := d.AgentSvc.ResumeWork(r.Context(), a.ID(), req.WorkItemID); err != nil {
		writeWorkStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"work_item_id": req.WorkItemID,
		"status":       string(agent.WorkItemActive),
	})
}

// writeWorkStateError maps the start_task / fail_task / pause_task /
// resume_task domain errors to HTTP.
func writeWorkStateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrAgentHasActiveWork):
		// single-active: the agent must finish its current item first; the
		// selected item stays queued (not dropped).
		writeError(w, http.StatusConflict, "agent_busy", "agent already has an active work item")
	case errors.Is(err, agent.ErrWorkItemReassigned):
		// v2.8.1 #278 PR4: CAS race lost (item moved since loaded) — the agent
		// goes back to step A (pull fresh).
		writeError(w, http.StatusConflict, "work_item_reassigned", "work item was reassigned (version conflict)")
	case errors.Is(err, agent.ErrWorkItemNotFound):
		writeError(w, http.StatusNotFound, "work_item_not_found", "")
	case errors.Is(err, agent.ErrWorkItemIllegalMove):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())
	case errors.Is(err, agent.ErrWorkItemTaskNotRunnable):
		// T130/T190: a backlog task (no real plan / not dispatched into the pool)
		// cannot be started. 409 — the work item stays queued; the remedy is to add
		// the task to a real plan (add_task_to_plan) or dispatch it into the pool.
		// Converged onto the UNIFIED task_backlog_not_actionable code (T190) so
		// claim/start/complete/block all speak with one voice.
		writeBacklogNotActionable(w, "starting")
	default:
		mapDomainError(w, err)
	}
}
