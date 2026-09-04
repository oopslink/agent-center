package agentruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type executionStateCaller struct {
	resp map[string]string
}

func (c *executionStateCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	if out != nil {
		*out = json.RawMessage(c.resp[tool])
	}
	return nil
}

func TestSnapshotExecutionState_AvailableTasksFromCenterAuthority(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "agent-1",
		Now:     func() time.Time { return time.Unix(1, 0) },
		ToolCaller: func() ToolCaller {
			return &executionStateCaller{resp: map[string]string{
				"list_my_inflight_tasks": `{"tasks":[]}`,
				"list_my_tasks":          `{"tasks":[{"task_id":"task-open","title":"Open","status":"open"}]}`,
			}}
		},
	}, &SessionState{})

	snap, err := rt.SnapshotExecutionState(context.Background())
	if err != nil {
		t.Fatalf("SnapshotExecutionState: %v", err)
	}
	if len(snap.AvailableTasks) != 1 || snap.AvailableTasks[0].TaskID != "task-open" || !snap.AvailableTasks[0].Runnable {
		t.Fatalf("available_tasks = %+v", snap.AvailableTasks)
	}
	if snap.AvailableTasks[0].RequiredNextAction != "fork_executor" {
		t.Fatalf("required_next_action = %q, want fork_executor", snap.AvailableTasks[0].RequiredNextAction)
	}
	if len(snap.ActiveTasks) != 0 {
		t.Fatalf("active_tasks = %+v, want none", snap.ActiveTasks)
	}
}

func TestSnapshotExecutionState_PendingNonDeliveryRequiresRepair(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID:       "agent-1",
		AgentHomeBase: t.TempDir(),
		Now:           func() time.Time { return time.Unix(2, 0) },
		ToolCaller: func() ToolCaller {
			return &executionStateCaller{resp: map[string]string{
				"list_my_inflight_tasks": `{"tasks":[{"task_id":"task-bad","title":"Bad","status":"running"}]}`,
				"list_my_tasks":          `{"tasks":[{"task_id":"task-bad","title":"Bad","status":"running"}]}`,
			}}
		},
	}, &SessionState{})
	rt.pending.record("task-bad", "[executor finished] outcome=non_delivery dirty files", time.Unix(1, 0))

	snap, err := rt.SnapshotExecutionState(context.Background())
	if err != nil {
		t.Fatalf("SnapshotExecutionState: %v", err)
	}
	if len(snap.ActiveTasks) != 1 {
		t.Fatalf("active_tasks = %+v", snap.ActiveTasks)
	}
	row := snap.ActiveTasks[0]
	if row.ExecutorState != "non_delivery" || row.DeliveryState != "invalid" || row.RequiredNextAction != "repair_non_delivery" {
		t.Fatalf("row = %+v, want non_delivery/invalid/repair_non_delivery", row)
	}
}

func TestSnapshotExecutionState_CenterRunningWithoutRuntimeExecutorIsGap(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "agent-1",
		Now:     func() time.Time { return time.Unix(3, 0) },
		ToolCaller: func() ToolCaller {
			return &executionStateCaller{resp: map[string]string{
				"list_my_inflight_tasks": `{"tasks":[{"task_id":"task-gap","title":"Gap","status":"running"}]}`,
				"list_my_tasks":          `{"tasks":[{"task_id":"task-gap","title":"Gap","status":"running"}]}`,
			}}
		},
	}, &SessionState{})

	snap, err := rt.SnapshotExecutionState(context.Background())
	if err != nil {
		t.Fatalf("SnapshotExecutionState: %v", err)
	}
	if len(snap.ActiveTasks) != 1 {
		t.Fatalf("active_tasks = %+v", snap.ActiveTasks)
	}
	row := snap.ActiveTasks[0]
	if row.ExecutorState != "stale" || row.RequiredNextAction != "reset_stale_executor" {
		t.Fatalf("row = %+v, want stale/reset_stale_executor", row)
	}
	if len(row.Evidence) == 0 || row.Evidence[0].Kind != "runtime_gap" {
		t.Fatalf("evidence = %+v, want runtime_gap", row.Evidence)
	}
}
