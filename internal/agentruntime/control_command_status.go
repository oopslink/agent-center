package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

const (
	controlCommandStatusStarted   = "started"
	controlCommandStatusSucceeded = "succeeded"
	controlCommandStatusRejected  = "rejected"
	controlCommandStatusFailed    = "failed"
)

type controlCommandStatusReporter interface {
	ReportControlCommandStatus(ctx context.Context, agentID, commandID, taskID, status, reason, detail, executionID string, at time.Time) error
}

// ReportForkCommandStatus writes the durable outcome for a fork_executor command
// before the worker acks the control offset. Empty command IDs are tolerated for
// older tests/envelopes.
func (r *LocalRuntime) ReportForkCommandStatus(ctx context.Context, commandID, taskID string, res *SpawnResult) error {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" || res == nil {
		return nil
	}
	rep, ok := r.cfg.Reporter.(controlCommandStatusReporter)
	if !ok || rep == nil {
		r.log("fork_executor agent=%s task=%s command=%s status not reported: reporter lacks command-status endpoint",
			r.cfg.AgentID, taskID, commandID)
		return nil
	}
	status := strings.TrimSpace(res.CommandStatus)
	reason := strings.TrimSpace(res.Reason)
	detail := strings.TrimSpace(res.Detail)
	executionID := strings.TrimSpace(res.ExecutorID)
	if status == "" && executionID != "" {
		status = controlCommandStatusStarted
	}
	if status == "" {
		return nil
	}
	if err := rep.ReportControlCommandStatus(ctx, r.cfg.AgentID, commandID, taskID, status, reason, detail, executionID, r.now()); err != nil {
		return fmt.Errorf("report fork_executor command status command=%s status=%s: %w", commandID, status, err)
	}
	return nil
}

func (r *LocalRuntime) ReportSandboxCommandStatus(ctx context.Context, commandID, action string, b SandboxBinding, actionErr error) error {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return nil
	}
	rep, ok := r.cfg.Reporter.(controlCommandStatusReporter)
	if !ok || rep == nil {
		r.log("sandbox action agent=%s action=%s command=%s status not reported: reporter lacks command-status endpoint",
			r.cfg.AgentID, action, commandID)
		return nil
	}
	status := controlCommandStatusSucceeded
	reason := strings.TrimSpace(b.LastError)
	detail := strings.TrimSpace(b.LastError)
	if actionErr != nil {
		status = controlCommandStatusFailed
		reason = "sandbox_action_failed"
		detail = actionErr.Error()
	} else if b.ComputerUseStatus() == concurrency.ComputerUseDegraded {
		status = controlCommandStatusFailed
		if reason == "" {
			reason = "sandbox_degraded"
		}
	}
	if err := rep.ReportControlCommandStatus(ctx, r.cfg.AgentID, commandID, "", status, reason, detail, "", r.now()); err != nil {
		return fmt.Errorf("report sandbox action command status command=%s status=%s: %w", commandID, status, err)
	}
	return nil
}
