package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RecoverUnfinishedWork performs the one-shot, post-control-loaded recovery pass.
// It complements the disk-first executor reconcile: the latter can adopt retained
// executors, while this pass also repairs a running center task whose executor never
// reached durable local state. SpawnExecutor owns the per-task single-flight and live
// executor checks, so repeated runtime starts converge without parallel forks.
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
		Tasks []struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return fmt.Errorf("decode unfinished tasks: %w", err)
	}
	for _, task := range listed.Tasks {
		if strings.TrimSpace(task.TaskID) == "" {
			continue
		}
		if strings.TrimSpace(task.Status) == "running" {
			healthy, err := r.hasHealthyTaskExecution(ctx, caller, task.TaskID)
			if err != nil {
				r.log("agent=%s unfinished-work task=%s execution check: %v (continuing)", r.cfg.AgentID, task.TaskID, err)
				continue // fail safe: an unknown execution must never cause a duplicate fork.
			}
			if healthy {
				continue
			}
		}
		if _, err := r.SpawnExecutor(ctx, SpawnRequest{TaskID: task.TaskID}); err != nil {
			r.log("agent=%s unfinished-work task=%s recovery fork: %v (continuing)", r.cfg.AgentID, task.TaskID, err)
		}
	}
	return nil
}

func (r *LocalRuntime) hasHealthyTaskExecution(ctx context.Context, caller ToolCaller, taskID string) (bool, error) {
	var raw json.RawMessage
	if err := caller.CallAgentTool(ctx, "list_task_executions", map[string]any{
		"agent_id": r.cfg.AgentID, "task_id": taskID,
	}, &raw); err != nil {
		return false, err
	}
	var listed struct {
		Items []struct {
			State            string `json:"state"`
			HealthStatus     string `json:"health_status"`
			RecoveryRequired bool   `json:"recovery_required"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return false, err
	}
	for _, execution := range listed.Items {
		if execution.RecoveryRequired {
			continue
		}
		health := strings.ToLower(strings.TrimSpace(execution.HealthStatus))
		state := strings.ToLower(strings.TrimSpace(execution.State))
		if state == "running" && health != "stale" && health != "dead" && health != "exhausted" && health != "non_delivery" {
			return true, nil
		}
	}
	return false, nil
}
