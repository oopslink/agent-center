package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RecoverUnfinishedWork performs the post-control-loaded recovery pass. The disk-first
// boot reconcile adopts retained executors; this pass covers center-visible open/running
// work that still needs an executor after a restart or control reload. SpawnExecutor owns
// per-task single-flight, active-executor checks, admission, and audit-preserving fresh
// executor creation.
func (r *LocalRuntime) RecoverUnfinishedWork(ctx context.Context) error {
	caller := r.toolCaller()
	if caller == nil || r.execEngine() == nil {
		return nil
	}
	var raw json.RawMessage
	if err := caller.CallAgentTool(ctx, "list_my_tasks", map[string]any{"agent_id": r.cfg.AgentID}, &raw); err != nil {
		return fmt.Errorf("list unfinished tasks: %w", err)
	}
	var listed struct {
		Tasks []InflightTask `json:"tasks"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &listed); err != nil {
			return fmt.Errorf("decode unfinished tasks: %w", err)
		}
	}
	tasks := listed.Tasks
	for _, task := range tasks {
		taskID := strings.TrimSpace(task.TaskID)
		if taskID == "" || !unfinishedTaskStatusRecoverable(task.Status) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "running") {
			executionState, err := r.unfinishedTaskExecutionState(ctx, caller, taskID)
			if err != nil {
				r.log("agent=%s unfinished-work task=%s execution check: %v (continuing)", r.cfg.AgentID, taskID, err)
				continue // fail safe: an unknown execution must never cause a duplicate fork.
			}
			switch executionState {
			case unfinishedExecutionHealthy:
				r.log("agent=%s unfinished-work task=%s healthy execution present; supervising, no recovery fork", r.cfg.AgentID, taskID)
				continue
			case unfinishedExecutionPresentNoRecovery:
				r.log("agent=%s unfinished-work task=%s execution present but not recovery_required; no recovery fork", r.cfg.AgentID, taskID)
				continue
			}
		}
		if _, err := r.SpawnExecutor(ctx, SpawnRequest{TaskID: taskID}); err != nil {
			r.log("agent=%s unfinished-work task=%s recovery fork: %v (continuing)", r.cfg.AgentID, taskID, err)
		}
	}
	return nil
}

func unfinishedTaskStatusRecoverable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "open", "running", "reopened":
		return true
	default:
		return false
	}
}

type unfinishedExecutionState int

const (
	unfinishedExecutionMissing unfinishedExecutionState = iota
	unfinishedExecutionHealthy
	unfinishedExecutionRecoverable
	unfinishedExecutionPresentNoRecovery
)

func (r *LocalRuntime) unfinishedTaskExecutionState(ctx context.Context, caller ToolCaller, taskID string) (unfinishedExecutionState, error) {
	var raw json.RawMessage
	if err := caller.CallAgentTool(ctx, "list_task_executions", map[string]any{
		"agent_id": r.cfg.AgentID,
		"task_id":  taskID,
	}, &raw); err != nil {
		return unfinishedExecutionMissing, err
	}
	var listed struct {
		Items []struct {
			State            string `json:"state"`
			HealthStatus     string `json:"health_status"`
			RecoveryRequired bool   `json:"recovery_required"`
		} `json:"items"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &listed); err != nil {
			return unfinishedExecutionMissing, fmt.Errorf("decode task executions: %w", err)
		}
	}
	if len(listed.Items) == 0 {
		return unfinishedExecutionMissing, nil
	}
	recoverable := false
	for _, execution := range listed.Items {
		state := strings.ToLower(strings.TrimSpace(execution.State))
		health := strings.ToLower(strings.TrimSpace(execution.HealthStatus))
		needsRecovery := execution.RecoveryRequired || executionHealthRequiresRecovery(health)
		if state == "running" && !needsRecovery {
			return unfinishedExecutionHealthy, nil
		}
		if needsRecovery {
			recoverable = true
		}
	}
	if recoverable {
		return unfinishedExecutionRecoverable, nil
	}
	return unfinishedExecutionPresentNoRecovery, nil
}

func executionHealthRequiresRecovery(health string) bool {
	switch health {
	case "stale", "dead", "exhausted", "non_delivery":
		return true
	default:
		return false
	}
}
