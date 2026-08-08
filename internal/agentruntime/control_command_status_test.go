package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"
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
