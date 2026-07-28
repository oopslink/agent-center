package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
)

const cmdTypeAgentForkExecutor = "agent.fork_executor"

// forkExecutorReq is the body for POST /admin/agent-tools/fork_executor.
// The MCP host injects agent_id from its process config; the model never supplies it.
type forkExecutorReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Model   string `json:"model,omitempty"`
	Context string `json:"context,omitempty"`
}

// forkExecutorHandler enqueues an explicit supervisor decision to fork a task into
// this same Agent's runtime. Center→worker control is asynchronous, so the tool
// returns accepted once the command is durably appended; the runtime reports actual
// executor state through the existing task-execution/audit surfaces.
func (s *Server) forkExecutorHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req forkExecutorReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.TaskID = strings.TrimSpace(req.TaskID)
	if req.TaskID == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	// Reject a bad/foreign task before claiming the command was durably accepted.
	// Runtime repeats the assignee check as defense in depth because assignment may
	// change between enqueue and delivery.
	if !s.requireOwnTask(w, r, d, a, req.TaskID) {
		return
	}
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_not_wired", "")
		return
	}

	payload, err := json.Marshal(map[string]any{
		"agent_id": string(a.ID()),
		"task_id":  req.TaskID,
		"model":    strings.TrimSpace(req.Model),
		"context":  req.Context,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	evt, err := d.EnvControlSvc.EnqueueCommand(r.Context(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(a.WorkerID()),
		CommandType:    cmdTypeAgentForkExecutor,
		Payload:        string(payload),
		IdempotencyKey: fmt.Sprintf("fork_executor:%s:%s:%d", a.ID(), req.TaskID, time.Now().UnixNano()),
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":           true,
		"status":       "accepted",
		"task_id":      req.TaskID,
		"command_id":   evt.ID(),
		"worker_id":    string(evt.WorkerID()),
		"offset":       evt.Offset(),
		"command_type": evt.CommandType(),
	})
}
