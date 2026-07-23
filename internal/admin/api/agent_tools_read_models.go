package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

const readModelMaxPage = 100

type taskReadReq struct {
	AgentID     string `json:"agent_id"`
	TaskID      string `json:"task_id"`
	ExecutionID string `json:"execution_id,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}

func readPage(size, offset, total int) (int, int) {
	if size <= 0 {
		size = 50
	}
	if size > readModelMaxPage {
		size = readModelMaxPage
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + size
	if end > total {
		end = total
	}
	if offset > total {
		offset = total
	}
	return offset, end
}

func redactAuditNote(note string) string {
	lower := strings.ToLower(note)
	for _, marker := range []string{"token=", "password=", "secret=", "authorization:", "bearer "} {
		if strings.Contains(lower, marker) {
			return "[redacted]"
		}
	}
	if len(note) > 1000 {
		return note[:1000] + "..."
	}
	return note
}

func (s *Server) getTaskAuditHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req taskReadReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if !s.requireTaskAccess(w, r, d, a, req.TaskID) {
		return
	}
	task, err := d.PMService.GetTask(r.Context(), pm.TaskID(req.TaskID))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	logs := task.ActionLogs()
	sort.SliceStable(logs, func(i, j int) bool {
		if logs[i].OccurredAt.Equal(logs[j].OccurredAt) {
			return logs[i].ID < logs[j].ID
		}
		return logs[i].OccurredAt.Before(logs[j].OccurredAt)
	})
	start, end := readPage(req.PageSize, req.Offset, len(logs))
	items := make([]map[string]any, 0, end-start)
	for _, lg := range logs[start:end] {
		items = append(items, map[string]any{
			"id": lg.ID, "action": lg.Action, "actor_ref": lg.ActorRef,
			"agent_ref": lg.AgentRef, "note": redactAuditNote(lg.Note),
			"occurred_at": lg.OccurredAt.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": req.TaskID, "items": items, "total": len(logs),
		"offset": start, "has_more": end < len(logs),
	})
}

type executionReadModel struct {
	ExecutionID string `json:"execution_id"`
	TaskID      string `json:"task_id"`
	AgentID     string `json:"agent_id"`
	CLI         string `json:"cli,omitempty"`
	Model       string `json:"model,omitempty"`
	State       string `json:"state"`
	Outcome     string `json:"outcome,omitempty"`
	ErrorKind   string `json:"error_kind,omitempty"`
	ErrorDetail string `json:"error_detail,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
	Recovered   bool   `json:"recovered"`
	Events      int    `json:"event_count"`
}

func taskExecutions(ctx context.Context, d HandlerDeps, taskID string) ([]executionReadModel, error) {
	if d.AgentActivityRepo == nil {
		return []executionReadModel{}, nil
	}
	events, err := d.AgentActivityRepo.ListByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	byID := map[string]*executionReadModel{}
	order := []string{}
	for _, ev := range events {
		if !strings.HasPrefix(ev.InteractionRef(), "executor:") {
			continue
		}
		id := strings.TrimPrefix(ev.InteractionRef(), "executor:")
		var p map[string]any
		if json.Unmarshal([]byte(ev.Payload()), &p) != nil || id == "" {
			continue
		}
		run := byID[id]
		if run == nil {
			run = &executionReadModel{ExecutionID: id, TaskID: taskID, AgentID: string(ev.AgentID()), State: "unknown"}
			byID[id] = run
			order = append(order, id)
		}
		run.Events++
		switch p["event"] {
		case "executor.start":
			run.State = "running"
			run.CLI, _ = p["cli"].(string)
			run.Model, _ = p["model"].(string)
			run.StartedAt = ev.OccurredAt().UTC().Format(time.RFC3339Nano)
		case "executor.progress":
			if state, ok := p["state"].(string); ok {
				run.State = state
			}
		case "executor.stop":
			run.State = "terminal"
			run.Outcome, _ = p["outcome"].(string)
			run.ErrorKind, _ = p["reason"].(string)
			run.ErrorDetail, _ = p["detail"].(string)
			run.ErrorDetail = redactAuditNote(run.ErrorDetail)
			run.Recovered, _ = p["recovered"].(bool)
			run.FinishedAt = ev.OccurredAt().UTC().Format(time.RFC3339Nano)
		}
	}
	out := make([]executionReadModel, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		out = append(out, *byID[order[i]])
	}
	return out, nil
}

func (s *Server) taskExecutionHandler(single bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := hd(r)
		var req taskReadReq
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
		if !ok || !s.requireTaskAccess(w, r, d, a, req.TaskID) {
			return
		}
		runs, err := taskExecutions(r.Context(), d, req.TaskID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "execution_read_failed", err.Error())
			return
		}
		if single {
			for _, run := range runs {
				if run.ExecutionID == req.ExecutionID {
					writeJSON(w, http.StatusOK, run)
					return
				}
			}
			writeError(w, http.StatusNotFound, "execution_not_found", "")
			return
		}
		start, end := readPage(req.PageSize, req.Offset, len(runs))
		writeJSON(w, http.StatusOK, map[string]any{
			"task_id": req.TaskID, "items": runs[start:end], "total": len(runs),
			"offset": start, "has_more": end < len(runs),
		})
	}
}

type effectiveConfigReq struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) getAgentRuntimeEffectiveConfigHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req effectiveConfigReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	p := a.Profile()
	desired := map[string]any{
		"cli": p.CLI, "model": p.Model, "reasoning": p.Reasoning, "mode": p.Mode,
		"provider": p.Provider, "orchestrator_model": p.OrchestratorModel,
		"default_executor_model": p.DefaultExecutorModel,
		"max_concurrent_tasks":   p.MaxConcurrentTasks, "judge_enabled": p.JudgeEnabled,
		"executor_git_worktree": p.ExecutorGitWorktree, "allowed_executors": p.AllowedExecutors,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": string(a.ID()), "desired_version": a.Version(), "desired": desired,
		"effective": map[string]any{"status": "unknown", "reason": "worker has not reported an effective-config snapshot"},
		"binary":    map[string]any{"status": "unknown"}, "last_reconcile_at": nil,
		"secrets_redacted": true, "env_var_count": len(p.EnvVars),
	})
}
