package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
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
	size, offset = normalizeReadPage(size, offset)
	end := offset + size
	if end > total {
		end = total
	}
	if offset > total {
		offset = total
	}
	return offset, end
}

func normalizeReadPage(size, offset int) (int, int) {
	if size <= 0 {
		size = 50
	}
	if size > readModelMaxPage {
		size = readModelMaxPage
	}
	if offset < 0 {
		offset = 0
	}
	return size, offset
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
	pageSize, offset := normalizeReadPage(req.PageSize, req.Offset)
	logs, total, err := d.PMService.ListTaskActionLogs(r.Context(), pm.TaskID(req.TaskID), offset, pageSize)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(logs))
	for _, lg := range logs {
		items = append(items, map[string]any{
			"id": lg.ID, "action": lg.Action, "actor_ref": lg.ActorRef,
			"agent_ref": lg.AgentRef, "note": redactAuditNote(lg.Note),
			"occurred_at": lg.OccurredAt.UTC().Format(time.RFC3339Nano),
		})
	}
	if cmdItems, err := forkCommandAuditItems(r.Context(), d, string(a.ID()), a.WorkerID(), req.TaskID); err == nil {
		items = append(items, cmdItems...)
		total += len(cmdItems)
	}
	start, end := readPage(req.PageSize, req.Offset, total)
	if len(items) > pageSize {
		items = items[:pageSize]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": req.TaskID, "items": items, "total": total,
		"offset": start, "has_more": end < total,
	})
}

func forkCommandAuditItems(ctx context.Context, d HandlerDeps, agentID, workerID, taskID string) ([]map[string]any, error) {
	if d.EnvControlSvc == nil || agentID == "" || workerID == "" || taskID == "" {
		return nil, nil
	}
	cmds, err := d.EnvControlSvc.CommandsByAgentTask(ctx, environment.WorkerID(workerID), cmdTypeAgentForkExecutor, agentID, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(cmds))
	now := time.Now()
	for _, cmd := range cmds {
		status := forkCommandStatus(cmd)
		reason := cmd.StatusReason()
		detail := cmd.StatusDetail()
		at := cmd.StatusUpdatedAt()
		if at.IsZero() {
			at = cmd.CreatedAt()
		}
		if status == environment.CommandStatusPending && forkCommandExpired(cmd, now) {
			status = environment.CommandStatusExpired
			reason = "runtime_command_timeout"
			detail = "fork_executor command was not started before its pending timeout"
			at = now
		}
		note := "command_status=" + status
		if reason != "" {
			note += " reason=" + reason
		}
		if detail != "" {
			note += " detail=" + redactAuditNote(detail)
		}
		out = append(out, map[string]any{
			"id":          cmd.ID(),
			"action":      "fork_executor." + status,
			"actor_ref":   "system:environment",
			"agent_ref":   "agent:" + agentID,
			"note":        note,
			"occurred_at": at.UTC().Format(time.RFC3339Nano),
		})
	}
	return out, nil
}

type executionReadModel struct {
	ExecutionID             string              `json:"execution_id"`
	CommandID               string              `json:"command_id,omitempty"`
	CommandStatus           string              `json:"command_status,omitempty"`
	TaskID                  string              `json:"task_id"`
	AgentID                 string              `json:"agent_id"`
	CLI                     string              `json:"cli,omitempty"`
	Model                   string              `json:"model,omitempty"`
	State                   string              `json:"state"`
	HealthStatus            string              `json:"health_status"`
	RecoveryRequired        bool                `json:"recovery_required"`
	Outcome                 string              `json:"outcome,omitempty"`
	ErrorKind               string              `json:"error_kind,omitempty"`
	ErrorDetail             string              `json:"error_detail,omitempty"`
	StartedAt               string              `json:"started_at,omitempty"`
	FinishedAt              string              `json:"finished_at,omitempty"`
	LastEffectiveActivityAt string              `json:"last_effective_activity_at,omitempty"`
	LastCommitSHA           string              `json:"last_commit_sha,omitempty"`
	Branch                  string              `json:"branch,omitempty"`
	Pushed                  *bool               `json:"pushed,omitempty"`
	NonDeliveryReasons      []pm.DeliveryReason `json:"non_delivery_reasons,omitempty"`
	Recovered               bool                `json:"recovered"`
	Events                  int                 `json:"event_count"`
}

func taskExecutions(ctx context.Context, d HandlerDeps, agentID, workerID, taskID string) ([]executionReadModel, error) {
	now := time.Now()
	if d.AgentActivityRepo == nil {
		return forkCommandExecutions(ctx, d, agentID, workerID, taskID, nil, now)
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
			run = &executionReadModel{ExecutionID: id, TaskID: taskID, AgentID: string(ev.AgentID()), State: "unknown", HealthStatus: "unknown"}
			byID[id] = run
			order = append(order, id)
		}
		run.Events++
		switch p["event"] {
		case "executor.start":
			run.State = "running"
			run.HealthStatus = "active"
			run.CLI, _ = p["cli"].(string)
			run.Model, _ = p["model"].(string)
			run.StartedAt = ev.OccurredAt().UTC().Format(time.RFC3339Nano)
			run.LastEffectiveActivityAt = run.StartedAt
		case "executor.progress":
			if state, ok := p["state"].(string); ok {
				run.State = state
			}
			run.HealthStatus = "active"
			if ts, ok := p["last_progress_at"].(string); ok && ts != "" {
				run.LastEffectiveActivityAt = ts
			} else {
				run.LastEffectiveActivityAt = ev.OccurredAt().UTC().Format(time.RFC3339Nano)
			}
		case "executor.stop":
			run.State = "terminal"
			run.Outcome, _ = p["outcome"].(string)
			run.ErrorKind, _ = p["reason"].(string)
			run.ErrorDetail, _ = p["detail"].(string)
			run.ErrorDetail = redactAuditNote(run.ErrorDetail)
			run.Recovered, _ = p["recovered"].(bool)
			run.FinishedAt = ev.OccurredAt().UTC().Format(time.RFC3339Nano)
			run.LastEffectiveActivityAt = run.FinishedAt
			if d := deliveryFromPayload(p["git"]); d != nil {
				run.LastCommitSHA = d.HeadSHA
				run.Branch = d.Branch
				pushed := d.Pushed
				run.Pushed = &pushed
				if !d.HasValidDelivery() {
					run.NonDeliveryReasons = d.InvalidReasons()
				}
			}
			run.HealthStatus = executionStopHealth(run.Outcome, run.ErrorKind, run.NonDeliveryReasons)
		}
	}
	out := make([]executionReadModel, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		run := *byID[order[i]]
		finalizeExecutionHealth(&run, now)
		out = append(out, run)
	}
	return forkCommandExecutions(ctx, d, agentID, workerID, taskID, out, now)
}

func forkCommandExecutions(ctx context.Context, d HandlerDeps, agentID, workerID, taskID string, runs []executionReadModel, now time.Time) ([]executionReadModel, error) {
	if d.EnvControlSvc == nil || agentID == "" || workerID == "" || taskID == "" {
		if runs == nil {
			return []executionReadModel{}, nil
		}
		return runs, nil
	}
	byExec := map[string]struct{}{}
	for _, run := range runs {
		if run.ExecutionID != "" {
			byExec[run.ExecutionID] = struct{}{}
		}
	}
	cmds, err := d.EnvControlSvc.CommandsByAgentTask(ctx, environment.WorkerID(workerID), cmdTypeAgentForkExecutor, agentID, taskID)
	if err != nil {
		return nil, err
	}
	added := make([]executionReadModel, 0, len(cmds))
	for _, cmd := range cmds {
		status := forkCommandStatus(cmd)
		execID := cmd.ExecutionID()
		if status == environment.CommandStatusStarted && execID != "" {
			if _, ok := byExec[execID]; ok {
				continue
			}
		}
		run := forkCommandExecution(cmd, now)
		if run.ExecutionID != "" {
			if _, dup := byExec[run.ExecutionID]; dup {
				continue
			}
			byExec[run.ExecutionID] = struct{}{}
		}
		added = append(added, run)
	}
	return append(added, runs...), nil
}

func forkCommandExecution(cmd *environment.WorkerControlEvent, now time.Time) executionReadModel {
	status := forkCommandStatus(cmd)
	reason := cmd.StatusReason()
	detail := redactAuditNote(cmd.StatusDetail())
	updated := cmd.StatusUpdatedAt()
	if updated.IsZero() {
		updated = cmd.CreatedAt()
	}
	if status == environment.CommandStatusPending && forkCommandExpired(cmd, now) {
		status = environment.CommandStatusExpired
		reason = "runtime_command_timeout"
		detail = "fork_executor command was not started before its pending timeout"
		updated = now
	}
	execID := cmd.ExecutionID()
	if execID == "" {
		execID = "command:" + cmd.ID()
	}
	run := executionReadModel{
		ExecutionID:             execID,
		CommandID:               cmd.ID(),
		CommandStatus:           status,
		TaskID:                  cmd.TaskID(),
		AgentID:                 cmd.AgentID(),
		State:                   "pending",
		HealthStatus:            "pending",
		StartedAt:               cmd.CreatedAt().UTC().Format(time.RFC3339Nano),
		LastEffectiveActivityAt: updated.UTC().Format(time.RFC3339Nano),
		Events:                  1,
	}
	switch status {
	case environment.CommandStatusStarted:
		run.State = "running"
		run.HealthStatus = "active"
	case environment.CommandStatusRejected, environment.CommandStatusFailed, environment.CommandStatusExpired:
		run.State = "terminal"
		run.Outcome = status
		run.ErrorKind = reason
		run.ErrorDetail = detail
		run.FinishedAt = updated.UTC().Format(time.RFC3339Nano)
		run.LastEffectiveActivityAt = run.FinishedAt
		run.HealthStatus = status
		if status == environment.CommandStatusFailed || status == environment.CommandStatusExpired {
			run.RecoveryRequired = true
		}
	}
	return run
}

const executionStaleAfter = 15 * time.Minute

func finalizeExecutionHealth(run *executionReadModel, now time.Time) {
	if run == nil {
		return
	}
	if run.HealthStatus == "" {
		run.HealthStatus = "unknown"
	}
	if run.State != "running" {
		run.RecoveryRequired = run.HealthStatus == "dead" || run.HealthStatus == "exhausted" || run.HealthStatus == "non_delivery"
		return
	}
	at, ok := parseExecutionTime(run.LastEffectiveActivityAt)
	if ok && now.Sub(at) > executionStaleAfter {
		run.HealthStatus = "stale"
		run.RecoveryRequired = true
	}
}

func parseExecutionTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func executionStopHealth(outcome, reason string, nonDelivery []pm.DeliveryReason) string {
	if len(nonDelivery) > 0 {
		return "non_delivery"
	}
	switch outcome {
	case "succeeded":
		return "terminal"
	case "crashed":
		return "dead"
	case "failed":
		if reason == "stalled" || reason == "process_gone" || reason == "clean_exit_no_output" {
			return "dead"
		}
		if strings.Contains(reason, "exhaust") || reason == "reset_exhausted" || reason == "recovery_exhausted" {
			return "exhausted"
		}
		return "failed"
	default:
		return "terminal"
	}
}

func deliveryFromPayload(v any) *pm.Delivery {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return &pm.Delivery{
		Branch:      stringFromAny(m["branch"]),
		HeadSHA:     stringFromAny(m["head_sha"]),
		Dirty:       boolFromAny(m["dirty"]),
		Pushed:      boolFromAny(m["pushed"]),
		Probed:      boolFromAny(m["probed"]),
		BaseRef:     stringFromAny(m["base_ref"]),
		BaseKnown:   boolFromAny(m["base_known"]),
		AheadOfBase: intFromAny(m["ahead_of_base"]),
		PushError:   stringFromAny(m["push_error"]),
		Source:      stringFromAny(m["source"]),
		ExecutorID:  stringFromAny(m["executor_id"]),
		Worktree:    stringFromAny(m["worktree"]),
		Evidence:    stringFromAny(m["evidence"]),
		Reason:      stringFromAny(m["reason"]),
	}
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolFromAny(v any) bool {
	b, _ := v.(bool)
	return b
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
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
		runs, err := taskExecutions(r.Context(), d, string(a.ID()), a.WorkerID(), req.TaskID)
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
	effective := map[string]any{"status": "unknown", "reason": "worker has not reported an effective-config snapshot"}
	lastReconcileAt := any(nil)
	if d.LiveState != nil {
		if snap, age, ok := d.LiveState.Get(string(a.ID()), time.Now()); ok {
			effective = map[string]any{
				"status":                "applied",
				"config_version":        snap.ConfigVersion,
				"admission_cap":         snap.AdmissionCap,
				"slot_count":            snap.SlotCount,
				"active":                snap.Active,
				"snapshot_age_ms":       age.Milliseconds(),
				"executor_engine_ready": snap.AdmissionCap > 0,
			}
			switch {
			case age > forkCommandExpireAfter:
				effective["status"] = "stale"
				effective["reason"] = "agent runtime snapshot is stale"
			case snap.AdmissionCap <= 0 || snap.ConfigVersion <= 0:
				effective["status"] = "not_ready"
				effective["reason"] = "runtime has not attached an executor engine"
			case snap.ConfigVersion < a.Version():
				effective["status"] = "stale_version"
				effective["reason"] = "runtime has not applied desired config version"
			}
			lastReconcileAt = time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": string(a.ID()), "desired_version": a.Version(), "desired": desired,
		"effective": effective,
		"binary":    map[string]any{"status": "unknown"}, "last_reconcile_at": lastReconcileAt,
		"secrets_redacted": true, "env_var_count": len(p.EnvVars),
	})
}
