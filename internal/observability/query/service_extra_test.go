package query_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// v2.7 #107 Phase-2 (proj-A): query executions repointed to the work-item model.
func TestQuery_Executions_ByTaskID(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedWorkItem(t, "WI-1", "AG-1", "T-1")
	env.seedWorkItemProjection(t, "WI-1", "AG-1", "active")
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{TaskID: "T-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Items))
	}
}

func TestQuery_Executions_DefaultActive(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedWorkItem(t, "WI-1", "AG-1", "T-1")
	env.seedWorkItemProjection(t, "WI-1", "AG-1", "active")
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 active, got %d", len(res.Items))
	}
}

func TestQuery_Workers_HasMappingTrueFalse(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedWorker(t, "W-2", workforce.WorkerOnline)
	tr := true
	res, _ := env.svc.Query(context.Background(), "workers", query.QueryFilter{HasMapping: &tr})
	if len(res.Items) != 0 {
		t.Fatalf("expected 0 with mappings, got %d", len(res.Items))
	}
	fa := false
	res, _ = env.svc.Query(context.Background(), "workers", query.QueryFilter{HasMapping: &fa})
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 w/o mappings, got %d", len(res.Items))
	}
}

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
// (no filter) returns the non-terminal active set {open,assigned,running,blocked,
// reopened} and excludes terminal {completed,verified,canceled}. The old
// --priority and --blocked-by filters are removed (pm.Task has no priority and
// no dependency graph — no new-model equivalent).
func TestQuery_Tasks_DefaultActiveSet(t *testing.T) {
	env := newQEnv(t)
	env.seedTaskStatus(t, "T-open", "p", pm.TaskOpen)
	env.seedTaskStatus(t, "T-running", "p", pm.TaskRunning)
	env.seedTaskStatus(t, "T-blocked", "p", pm.TaskBlocked)
	env.seedTaskStatus(t, "T-completed", "p", pm.TaskCompleted)
	env.seedTaskStatus(t, "T-canceled", "p", pm.TaskCanceled)
	res, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range res.Items {
		got[it.(map[string]any)["id"].(string)] = true
	}
	if len(res.Items) != 3 || !got["T-open"] || !got["T-running"] || !got["T-blocked"] {
		t.Fatalf("default active set wrong (want open/running/blocked): %+v", res.Items)
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
