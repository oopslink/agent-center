package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
)

// start_work / fail_work — v2.8.1 #278 D (@oopslink's pull model). The agent,
// via the MCP start_work / fail_work tools, drives its OWN work-item queue:
//   - start_work — select a queued work item and mark it running (queued→active).
//     Single-active is enforced (service StartWork + DB UNIQUE partial index):
//     if the agent already has an active/waiting_input item → 409 agent_busy and
//     the item stays queued (queue-drain, not drop).
//   - fail_work  — report the in-flight work item failed (active|waiting_input→
//     failed); frees the active slot so the next queued item can drain.
// Own-scope: the work item must belong to the calling agent (StartWork/FailWork
// ownership-guard → ErrWorkItemNotFound → 404).

type startWorkReq struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
}

func (s *Server) startWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req startWorkReq
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
	if err := d.AgentSvc.StartWork(r.Context(), a.ID(), req.WorkItemID); err != nil {
		writeWorkStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"work_item_id": req.WorkItemID,
		"status":       string(agent.WorkItemActive),
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

// pause_work / resume_paused_work — v2.8.1 #278 PR4 scheduling autonomy. pause_work
// sets the active item aside (active→paused, releases the single-active slot);
// resume_paused_work re-acquires the slot (paused→active, single-active-gated).

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

// get_my_active_work / list_my_paused_work — v2.8.1 #278 PR4 read tools. The
// agent's loop-boundary "do I have an active task?" check + its paused-resume
// candidates. Both reuse workItemMap (agent_tools.go).

type getMyActiveWorkReq struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) getMyActiveWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getMyActiveWorkReq
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
	items, err := d.AgentSvc.GetMyActiveWork(r.Context(), a.ID())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i] = workItemMap(it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"work_items": out})
}

type listMyPausedWorkReq struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) listMyPausedWorkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listMyPausedWorkReq
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
	items, err := d.AgentSvc.ListMyPausedWork(r.Context(), a.ID())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i] = workItemMap(it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"work_items": out})
}

// writeWorkStateError maps the start_work / fail_work / pause_work /
// resume_paused_work domain errors to HTTP.
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
