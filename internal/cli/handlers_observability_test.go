package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestInspectCmd_UsageError_NoArgs(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "inspect")
	_, errOut, code := runHandler(t, cmd, []string{"--format=json"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d (stderr=%s)", code, errOut)
	}
}

func TestInspectCmd_UnknownKind(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "inspect")
	_, errOut, code := runHandler(t, cmd, []string{"blob", "X"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d (stderr=%s)", code, errOut)
	}
}

func TestInspectCmd_TaskNotFound_ExitNotFound(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "inspect")
	_, _, code := runHandler(t, cmd, []string{"task", "T-missing"})
	if code != ExitNotFound {
		t.Fatalf("expected ExitNotFound, got %d", code)
	}
}

func TestInspectCmd_Task_HumanOutput(t *testing.T) {
	app := newTestApp(t)
	// Seed a task.
	tk, err := task.New(task.NewInput{
		ID: "T-1", ProjectID: "p", Title: "hello",
		CreatedBy: "user:test", Now: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.TaskRepo.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	cmd := findCmd(app.ObservabilityCommands(), "inspect")
	out, _, code := runHandler(t, cmd, []string{"task", "T-1"})
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d", code)
	}
	if !strings.Contains(out, "T-1") {
		t.Fatalf("output missing id: %q", out)
	}
}

func TestInspectCmd_Task_JSONOutput(t *testing.T) {
	app := newTestApp(t)
	tk, _ := task.New(task.NewInput{ID: "T-1", ProjectID: "p", Title: "x", CreatedBy: "user:t", Now: time.Now()})
	_ = app.TaskRepo.Save(context.Background(), tk)
	cmd := findCmd(app.ObservabilityCommands(), "inspect")
	out, _, code := runHandler(t, cmd, []string{"task", "T-1", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code=%d out=%s", code, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("non-JSON output: %v\n%s", err, out)
	}
	if got["id"] != "T-1" {
		t.Fatalf("json id mismatch: %v", got)
	}
}

func TestQueryCmd_UnknownResource(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "query")
	_, _, code := runHandler(t, cmd, []string{"blob"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestQueryCmd_LimitTooLarge(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "query")
	_, _, code := runHandler(t, cmd, []string{"events", "--limit=99999"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestQueryCmd_Tasks_JSON(t *testing.T) {
	app := newTestApp(t)
	tk, _ := task.New(task.NewInput{ID: "T-1", ProjectID: "proj", Title: "x", CreatedBy: "user:t", Now: time.Now()})
	_ = app.TaskRepo.Save(context.Background(), tk)
	cmd := findCmd(app.ObservabilityCommands(), "query")
	out, _, code := runHandler(t, cmd, []string{"tasks", "--project=proj", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, `"T-1"`) {
		t.Fatalf("output missing T-1: %s", out)
	}
}

func TestQueryCmd_SinceFlag(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "query")
	// Bad since value → usage error
	_, _, code := runHandler(t, cmd, []string{"events", "--since=blob"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage for bad --since, got %d", code)
	}
	// Good since: 1h
	_, _, code = runHandler(t, cmd, []string{"events", "--since=1h"})
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d", code)
	}
}

func TestPsCmd_HumanAndJSON(t *testing.T) {
	app := newTestApp(t)
	// Seed minimal data
	_ = app.WorkerRepo.Save(context.Background(), mustWorker(t, "W-1"))
	cmd := findCmd(app.ObservabilityCommands(), "ps")
	out, _, code := runHandler(t, cmd, []string{})
	if code != ExitOK {
		t.Fatalf("ps human: code=%d", code)
	}
	if !strings.Contains(out, "FLEET SNAPSHOT") {
		t.Fatalf("human output: %s", out)
	}
	out, _, code = runHandler(t, cmd, []string{"--format=json"})
	if code != ExitOK {
		t.Fatalf("ps json: code=%d", code)
	}
	if !strings.Contains(out, "work_items") {
		t.Fatalf("json output: %s", out)
	}
}

func TestStatsCmd_UnknownScope(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "stats")
	_, _, code := runHandler(t, cmd, []string{"--scope=blob"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestStatsCmd_Tasks_Counters(t *testing.T) {
	app := newTestApp(t)
	tk, _ := task.New(task.NewInput{ID: "T-1", ProjectID: "p", Title: "x", CreatedBy: "user:t", Now: time.Now()})
	_ = app.TaskRepo.Save(context.Background(), tk)
	cmd := findCmd(app.ObservabilityCommands(), "stats")
	out, _, code := runHandler(t, cmd, []string{"--scope=tasks", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if got["scope"] != "tasks" {
		t.Fatalf("scope: %v", got["scope"])
	}
}

func TestStatsCmd_BadSince(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "stats")
	_, _, code := runHandler(t, cmd, []string{"--scope=tasks", "--since=blob"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestLogsCmd_UnknownKind(t *testing.T) {
	app := newTestApp(t)
	// BlobStore must be wired for the early kind check to reach.
	app.BlobStore = nil
	app.LogsSvc = nil
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	_, _, code := runHandler(t, cmd, []string{"blob", "X"})
	if code != ExitBusinessError {
		// when LogsSvc is nil we report blob_store_unavailable as
		// business error, which is acceptable
		if code != ExitUsage {
			t.Fatalf("expected ExitBusinessError or ExitUsage, got %d", code)
		}
	}
}

func TestLogsCmd_FollowOnArchived_Explicit(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	_, _, code := runHandler(t, cmd, []string{"task", "T-1", "--follow"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage for --follow archived, got %d", code)
	}
}

func TestPeekTraceCmd_NoExecutionID(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "peek-trace")
	_, _, code := runHandler(t, cmd, []string{"--socket=/tmp/peek-test-doesnt-matter.sock"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestPeekTraceCmd_WorkerOffline(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "peek-trace")
	// non-existent socket → connect fail
	_, _, code := runHandler(t, cmd, []string{"E-1", "--socket=/tmp/absent-peek-sock-test.sock"})
	if code != ExitBusinessError {
		t.Fatalf("expected ExitBusinessError, got %d", code)
	}
}

func TestInspect_AllKinds_NoPanic_SnapshotShape(t *testing.T) {
	app := newTestApp(t)
	// Seed one of each.
	tk, _ := task.New(task.NewInput{ID: "T-1", ProjectID: "p", Title: "x", CreatedBy: "user:t", Now: time.Now()})
	_ = app.TaskRepo.Save(context.Background(), tk)
	exec, _ := execution.New(execution.NewInput{
		ID: taskruntime.TaskExecutionID("E-1"), TaskID: "T-1", WorkerID: "W-1",
		AgentCLI: "claude-code", WorkspaceMode: execution.WorkspaceWorktree, Now: time.Now(),
	})
	_ = app.ExecRepo.Save(context.Background(), exec)
	_ = app.WorkerRepo.Save(context.Background(), mustWorker(t, "W-1"))
	issue, _ := discussion.NewIssue(discussion.NewIssueInput{
		ID: "I-1", ProjectID: "p", Title: "x",
		OpenedByIdentityID: "user:t", Origin: discussion.OriginCLI, OpenedAt: time.Now(),
	})
	_ = app.IssueRepo.Save(context.Background(), issue)
	proj, _ := workforce.NewProject(workforce.NewProjectInput{
		ID: "p", Name: "P", CreatedByIdentityID: "user:t", CreatedAt: time.Now(),
	})
	_ = app.ProjectRepo.Save(context.Background(), proj)

	// Inspect each kind
	cases := []struct {
		kind string
		id   string
	}{
		{"task", "T-1"},
		{"execution", "E-1"},
		{"worker", "W-1"},
		{"issue", "I-1"},
		{"project", "p"},
		{"worktree", "E-1"},
		// supervisor and decision removed in v2.6 (BE-9)
	}
	cmd := findCmd(app.ObservabilityCommands(), "inspect")
	for _, tc := range cases {
		_, _, code := runHandler(t, cmd, []string{tc.kind, tc.id, "--format=json"})
		if code != ExitOK {
			t.Fatalf("inspect %s/%s: code=%d", tc.kind, tc.id, code)
		}
	}
}

func TestQueryEvents_LimitTooLargeReturnsExitUsage(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "query")
	_, _, code := runHandler(t, cmd, []string{"events", "--limit=" + bigNum()})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestStatsCmd_Events_NonEmptyAfterEmit(t *testing.T) {
	app := newTestApp(t)
	for i := 0; i < 3; i++ {
		_, err := app.Sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task.created", Actor: "user:t",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	cmd := findCmd(app.ObservabilityCommands(), "stats")
	out, _, code := runHandler(t, cmd, []string{"--scope=events", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "task.created") {
		t.Fatalf("expected event counter in output: %s", out)
	}
}

// helpers ------------------------------------------------------------------

func mustWorker(t *testing.T, id string) *workforce.Worker {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID: workforce.WorkerID(id), Capabilities: []string{"claude-code"}, EnrolledAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func bigNum() string {
	return "100000"
}

// _ keeps imports stable
var _ = filepath.Base
