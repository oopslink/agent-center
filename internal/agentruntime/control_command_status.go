package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	controlCommandStatusStarted  = "started"
	controlCommandStatusRejected = "rejected"
	controlCommandStatusFailed   = "failed"
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
