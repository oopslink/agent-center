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

// writeWorkStateError maps the start_work / fail_work domain errors to HTTP.
func writeWorkStateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrAgentHasActiveWork):
		// single-active: the agent must finish its current item first; the
		// selected item stays queued (not dropped).
		writeError(w, http.StatusConflict, "agent_busy", "agent already has an active work item")
	case errors.Is(err, agent.ErrWorkItemNotFound):
		writeError(w, http.StatusNotFound, "work_item_not_found", "")
	case errors.Is(err, agent.ErrWorkItemIllegalMove):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())
	default:
		mapDomainError(w, err)
	}
}
