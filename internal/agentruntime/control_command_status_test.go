package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/runtimefs"
)

type commandStatusTestReporter struct {
	nopReporter
	got  []commandStatusReport
	fail error
}

type commandStatusReport struct {
	agentID     string
	commandID   string
	taskID      string
	status      string
	reason      string
	detail      string
	executionID string
	at          time.Time
}

func (r *commandStatusTestReporter) ReportControlCommandStatus(_ context.Context, agentID, commandID, taskID, status, reason, detail, executionID string, at time.Time) error {
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, commandStatusReport{
		agentID: agentID, commandID: commandID, taskID: taskID,
		status: status, reason: reason, detail: detail, executionID: executionID, at: at,
	})
	return nil
}

type sandboxStatusReporter struct {
	activityCaptureReporter
	statuses []string
	reasons  []string
}

func (r *sandboxStatusReporter) ReportAgentLifecycle(context.Context, string, string, string, time.Time) error {
	return nil
}
func (r *sandboxStatusReporter) ReportMarkSeen(context.Context, string, string, string, time.Time) error {
	return nil
}
func (r *sandboxStatusReporter) ReportConverseError(context.Context, string, string, string, time.Time) error {
	return nil
}
func (r *sandboxStatusReporter) FetchReplyNudges(context.Context, string) ([]string, error) {
	return nil, nil
}
func (r *sandboxStatusReporter) ReportUsage(context.Context, UsageReport) error { return nil }
func (r *sandboxStatusReporter) RenewTaskLease(context.Context, string, string, time.Time) error {
	return nil
}
func (r *sandboxStatusReporter) ReportRuntimeFsResponse(context.Context, runtimefs.Response) error {
	return nil
}
func (r *sandboxStatusReporter) ReportControlCommandStatus(_ context.Context, _, _, _, status, reason, _, _ string, _ time.Time) error {
	r.statuses = append(r.statuses, status)
	r.reasons = append(r.reasons, reason)
	return nil
}

func TestReportForkCommandStatus_ReportsStartedBeforeAck(t *testing.T) {
	now := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	rep := &commandStatusTestReporter{}
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "agent-1", Reporter: rep, Now: func() time.Time { return now },
	}, &SessionState{})

	if err := rt.ReportForkCommandStatus(context.Background(), "cmd-1", "task-1", &SpawnResult{ExecutorID: "exec-1"}); err != nil {
		t.Fatalf("ReportForkCommandStatus: %v", err)
	}
	if len(rep.got) != 1 || rep.got[0].agentID != "agent-1" || rep.got[0].commandID != "cmd-1" ||
		rep.got[0].taskID != "task-1" || rep.got[0].status != controlCommandStatusStarted ||
		rep.got[0].executionID != "exec-1" || !rep.got[0].at.Equal(now) {
		t.Fatalf("status report = %+v", rep.got)
	}
}

func TestReportForkCommandStatus_ErrorPreventsAck(t *testing.T) {
	rep := &commandStatusTestReporter{fail: errors.New("center unavailable")}
	rt := NewLocalRuntime(LocalRuntimeConfig{AgentID: "agent-1", Reporter: rep}, &SessionState{})

	err := rt.ReportForkCommandStatus(context.Background(), "cmd-1", "task-1", &SpawnResult{
		CommandStatus: controlCommandStatusFailed, Reason: "runtime_executor_unavailable",
	})
	if err == nil {
		t.Fatal("status report failure must surface so the worker does not ack the command")
	}
}

func TestReportSandboxCommandStatus_AppendsActivity(t *testing.T) {
	rep := &sandboxStatusReporter{}
	now := time.Date(2026, 9, 11, 9, 30, 0, 0, time.UTC)
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID:  "agent-1",
		Reporter: rep,
		Now:      func() time.Time { return now },
	}, &SessionState{})

	err := rt.ReportSandboxCommandStatus(context.Background(), "cmd-1", "start", SandboxBinding{
		SandboxID: "sbx-1",
		Provider:  SandboxProviderTartMacOSVM,
		VMName:    "ac-agent-1",
		State:     SandboxStateRunning,
	}, nil)
	if err != nil {
		t.Fatalf("ReportSandboxCommandStatus: %v", err)
	}
	if len(rep.statuses) != 1 || rep.statuses[0] != controlCommandStatusSucceeded {
		t.Fatalf("statuses = %#v", rep.statuses)
	}
	if len(rep.payloads) != 1 {
		t.Fatalf("activity payload count = %d", len(rep.payloads))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rep.payloads[0]), &payload); err != nil {
		t.Fatalf("activity payload json: %v", err)
	}
	if payload["event"] != "sandbox.start" || payload["status"] != controlCommandStatusSucceeded || payload["vm_name"] != "ac-agent-1" {
		t.Fatalf("payload = %#v", payload)
	}
}
