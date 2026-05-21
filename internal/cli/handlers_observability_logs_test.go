package cli

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func TestLogsCmd_UnknownKind_ExitUsage(t *testing.T) {
	app := newTestApp(t)
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	app.BlobStore = bs
	app.LogsSvc = query.NewLogsService(query.Deps{Tasks: app.TaskRepo}, bs)
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	_, _, code := runHandler(t, cmd, []string{"blob", "X"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestLogsCmd_BlobNotFound_ExitNotFound(t *testing.T) {
	app := newTestApp(t)
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	app.BlobStore = bs
	app.LogsSvc = query.NewLogsService(query.Deps{Tasks: app.TaskRepo}, bs)
	// Seed task (but no blob).
	tk, _ := task.New(task.NewInput{
		ID: taskruntime.TaskID("T-1"), ProjectID: "p", Title: "x",
		CreatedBy: "user:t", Now: time.Now(),
	})
	_ = app.TaskRepo.Save(context.Background(), tk)
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	_, _, code := runHandler(t, cmd, []string{"task", "T-1"})
	if code != ExitNotFound {
		t.Fatalf("expected ExitNotFound, got %d", code)
	}
}

func TestLogsCmd_MissingArgs_ExitUsage(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	_, _, code := runHandler(t, cmd, []string{"task"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestLogsCmd_GenericFailure_ExitBusinessError(t *testing.T) {
	app := newTestApp(t)
	// Wire BlobStore but no Tasks repo → Open returns generic error
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	app.BlobStore = bs
	app.LogsSvc = query.NewLogsService(query.Deps{}, bs)
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	_, _, code := runHandler(t, cmd, []string{"task", "T-x"})
	if code != ExitBusinessError {
		t.Fatalf("expected ExitBusinessError, got %d", code)
	}
}
