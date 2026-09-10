package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/workforce"
)

const cmdTypeAgentSandboxAction = "agent.sandbox_action"

var allowedSandboxActions = map[string]bool{
	"ensure":       true,
	"provision":    true,
	"start":        true,
	"resume":       true,
	"suspend":      true,
	"reset":        true,
	"delete":       true,
	"health":       true,
	"open_console": true,
	"open_browser": true,
}

func (s *Server) agentSandboxActionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	action := strings.TrimSpace(strings.ToLower(r.PathValue("action")))
	if action == "" || !allowedSandboxActions[action] {
		writeError(w, http.StatusBadRequest, "invalid_sandbox_action", "unsupported sandbox action")
		return
	}
	if !a.Profile().SandboxEnabled {
		writeError(w, http.StatusConflict, "sandbox_disabled", "enable sandbox before running sandbox actions")
		return
	}
	workerID := strings.TrimSpace(a.WorkerID())
	if workerID == "" {
		writeError(w, http.StatusConflict, "worker_required", "agent must be bound to a worker")
		return
	}
	if d.WorkerRepo != nil {
		wk, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(workerID))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		if wk != nil && wk.Status() != workforce.WorkerOnline {
			writeError(w, http.StatusConflict, "worker_offline", "agent worker is offline")
			return
		}
	}
	if d.EnvControl == nil {
		writeError(w, http.StatusNotImplemented, "env_control_not_wired", "")
		return
	}
	payload, err := json.Marshal(map[string]any{
		"agent_id": string(a.ID()),
		"action":   action,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	evt, err := d.EnvControl.EnqueueCommand(r.Context(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    cmdTypeAgentSandboxAction,
		Payload:        string(payload),
		IdempotencyKey: fmt.Sprintf("sandbox_action:%s:%s:%d", a.ID(), action, time.Now().UnixNano()),
		AgentID:        string(a.ID()),
		Status:         environment.CommandStatusPending,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":             true,
		"status":         "accepted",
		"agent_id":       agentFacingID(a),
		"worker_id":      workerID,
		"action":         action,
		"command_id":     evt.ID(),
		"offset":         evt.Offset(),
		"command_type":   evt.CommandType(),
		"command_status": evt.Status(),
	})
}
