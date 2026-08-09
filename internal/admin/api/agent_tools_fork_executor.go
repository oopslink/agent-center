package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/environment"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

const cmdTypeAgentForkExecutor = "agent.fork_executor"

const forkCommandExpireAfter = 15 * time.Minute

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
	task, err := d.PMService.GetTask(r.Context(), pm.TaskID(req.TaskID))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if ok := requireRuntimeReadyForFork(w, r, d, a); !ok {
		return
	}
	if existing, err := d.EnvControlSvc.LatestNonTerminalByAgentTask(
		r.Context(), environment.WorkerID(a.WorkerID()), cmdTypeAgentForkExecutor, string(a.ID()), req.TaskID,
	); err != nil {
		mapDomainError(w, err)
		return
	} else if existing != nil {
		if forkCommandExpired(existing, time.Now()) {
			_, err := d.EnvControlSvc.UpdateCommandStatus(r.Context(), environment.UpdateCommandStatusInput{
				WorkerID:        environment.WorkerID(a.WorkerID()),
				CommandID:       existing.ID(),
				AgentID:         string(a.ID()),
				TaskID:          req.TaskID,
				Status:          environment.CommandStatusExpired,
				StatusReason:    "runtime_command_timeout",
				StatusDetail:    "fork_executor command was not started before its pending timeout",
				StatusUpdatedAt: time.Now(),
			})
			if err != nil {
				mapDomainError(w, err)
				return
			}
		} else {
			writeForkAccepted(w, existing, req.TaskID)
			return
		}
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
		IdempotencyKey: fmt.Sprintf("fork_executor:%s:%s:v%d", a.ID(), req.TaskID, task.Version()),
		AgentID:        string(a.ID()),
		TaskID:         req.TaskID,
		Status:         environment.CommandStatusPending,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if environment.CommandStatusTerminal(evt.Status()) {
		evt, err = d.EnvControlSvc.EnqueueCommand(r.Context(), environment.AppendCommandInput{
			WorkerID:       environment.WorkerID(a.WorkerID()),
			CommandType:    cmdTypeAgentForkExecutor,
			Payload:        string(payload),
			IdempotencyKey: fmt.Sprintf("fork_executor:%s:%s:v%d:%d", a.ID(), req.TaskID, task.Version(), time.Now().UnixNano()),
			AgentID:        string(a.ID()),
			TaskID:         req.TaskID,
			Status:         environment.CommandStatusPending,
		})
		if err != nil {
			mapDomainError(w, err)
			return
		}
	}
	writeForkAccepted(w, evt, req.TaskID)
}

func writeForkAccepted(w http.ResponseWriter, evt *environment.WorkerControlEvent, taskID string) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":             true,
		"status":         "accepted",
		"task_id":        taskID,
		"command_id":     evt.ID(),
		"worker_id":      string(evt.WorkerID()),
		"offset":         evt.Offset(),
		"command_type":   evt.CommandType(),
		"command_status": forkCommandStatus(evt),
	})
}

func requireRuntimeReadyForFork(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent) bool {
	if d.WorkerRepo != nil {
		wk, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(a.WorkerID()))
		if err != nil {
			mapDomainError(w, err)
			return false
		}
		if wk.Status() != workforce.WorkerOnline {
			writeError(w, http.StatusServiceUnavailable, "runtime_not_ready", "worker is not online")
			return false
		}
	}
	if d.LiveState == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_not_ready", "worker has not reported an agent runtime snapshot")
		return false
	}
	snap, age, ok := d.LiveState.Get(string(a.ID()), time.Now())
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "runtime_not_ready", "worker has not reported an effective-config snapshot")
		return false
	}
	if age > forkCommandExpireAfter {
		writeError(w, http.StatusServiceUnavailable, "runtime_not_ready", "agent runtime snapshot is stale")
		return false
	}
	if snap.AdmissionCap <= 0 || snap.ConfigVersion <= 0 {
		writeError(w, http.StatusServiceUnavailable, "runtime_executor_unavailable", "agent runtime has not attached an executor engine")
		return false
	}
	if snap.ConfigVersion < a.Version() {
		writeError(w, http.StatusServiceUnavailable, "runtime_config_not_applied", "agent runtime has not applied the desired config version")
		return false
	}
	return true
}

func forkCommandExpired(evt *environment.WorkerControlEvent, now time.Time) bool {
	if evt == nil {
		return false
	}
	status := forkCommandStatus(evt)
	if status != environment.CommandStatusPending {
		return false
	}
	at := evt.StatusUpdatedAt()
	if at.IsZero() {
		at = evt.CreatedAt()
	}
	return !at.IsZero() && now.Sub(at) > forkCommandExpireAfter
}

func forkCommandStatus(evt *environment.WorkerControlEvent) string {
	if evt == nil {
		return ""
	}
	status := strings.TrimSpace(evt.Status())
	if status == "" {
		return environment.CommandStatusPending
	}
	return status
}
