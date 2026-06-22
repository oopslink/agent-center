package query_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// v2.14.0 F7 (issue I14): query executions repointed to pm_tasks. by-task → the
// agent-assigned task itself (work_item_id == task_id).
func TestQuery_Executions_ByTaskID(t *testing.T) {
	env := newQEnv(t)
	env.seedAgentTask(t, "WI-1", "AG-1", "p", "active")
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{TaskID: "WI-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Items))
	}
}

func TestQuery_Executions_DefaultActive(t *testing.T) {
	env := newQEnv(t)
	env.seedAgentTask(t, "WI-1", "AG-1", "p", "active")
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 active, got %d", len(res.Items))
	}
}

// TestQuery_Workers_HasMappingTrueFalse removed — the --has-mapping filter is
// deleted in v2.7 #131 (workforce WorkerProjectMapping model retired; workers
// are no longer project-scoped via mappings).

// TestQuery_Issues_ByOpener pins the v2.7 #125 --opener repoint: opener filters
// on pm issue created_by (the successor of discussion OpenedByIdentityID), in
// memory. seedPMIssue uses created_by "user:test".
func TestQuery_Issues_ByOpener(t *testing.T) {
	env := newQEnv(t)
	env.seedPMIssue(t, "I-1", "p", "x", pm.IssueOpen)
	env.seedPMIssue(t, "I-2", "p", "y", pm.IssueOpen)
	res, err := env.svc.Query(context.Background(), "issues", query.QueryFilter{Opener: "user:test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("opener=user:test: want 2, got %d", len(res.Items))
	}
	res, err = env.svc.Query(context.Background(), "issues", query.QueryFilter{Opener: "user:nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("opener=user:nobody: want 0, got %d", len(res.Items))
	}
}

// TestQuery_Tasks_DefaultActiveSet pins the proj-B口径: the default `query tasks`
// (no filter) returns the non-terminal active set {open,running,reopened} and
// excludes terminal {completed,discarded}. ADR-0046: "blocked"/"verified" deleted
// (blocked is now a running-task annotation). The old --priority and --blocked-by
// filters are removed (pm.Task has no priority and no dependency graph).
func TestQuery_Tasks_DefaultActiveSet(t *testing.T) {
	env := newQEnv(t)
	env.seedTaskStatus(t, "T-open", "p", pm.TaskOpen)
	env.seedTaskStatus(t, "T-running", "p", pm.TaskRunning)
	env.seedTaskStatus(t, "T-reopened", "p", pm.TaskReopened)
	env.seedTaskStatus(t, "T-completed", "p", pm.TaskCompleted)
	env.seedTaskStatus(t, "T-canceled", "p", pm.TaskDiscarded)
	res, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range res.Items {
		got[it.(map[string]any)["id"].(string)] = true
	}
	if len(res.Items) != 3 || !got["T-open"] || !got["T-running"] || !got["T-reopened"] {
		t.Fatalf("default active set wrong (want open/running/reopened): %+v", res.Items)
	}
	if got["T-completed"] || got["T-canceled"] {
		t.Fatalf("terminal task leaked into default set: %+v", res.Items)
	}
}

// TestQuery_Tasks_ByStatus filters to a single explicit status.
func TestQuery_Tasks_ByStatus(t *testing.T) {
	env := newQEnv(t)
	env.seedTaskStatus(t, "T-open", "p", pm.TaskOpen)
	env.seedTaskStatus(t, "T-completed", "p", pm.TaskCompleted)
	res, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].(map[string]any)["id"] != "T-completed" {
		t.Fatalf("by-status=completed wrong: %+v", res.Items)
	}
}

func TestQuery_Events_FullFlagSurface(t *testing.T) {
	env := newQEnv(t)
	for i := 0; i < 3; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task.created", Actor: "user:h",
		})
	}
	since := env.clk.Now().Add(-time.Hour)
	until := env.clk.Now().Add(time.Hour)
	res, err := env.svc.Query(context.Background(), "events", query.QueryFilter{
		Since: &since, Until: &until, Actor: "user:h",
		EventType: "task.created",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("expected 3, got %d", len(res.Items))
	}
}
