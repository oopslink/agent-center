package query_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/observability/query"
)

func TestLogs_UnknownKind(t *testing.T) {
	env := newQEnv(t)
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	svc := query.NewLogsService(env.deps, bs)
	_, _, err := svc.Open(context.Background(), query.LogsRequest{Kind: "blob", ID: "x"})
	if !errors.Is(err, query.ErrLogsKindUnknown) {
		t.Fatalf("expected ErrLogsKindUnknown, got %v", err)
	}
}

func TestLogs_ArchivedFollowExplicitError(t *testing.T) {
	env := newQEnv(t)
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	svc := query.NewLogsService(env.deps, bs)
	_, _, err := svc.Open(context.Background(), query.LogsRequest{Kind: query.LogsTask, ID: "T-1", Follow: true})
	if !errors.Is(err, query.ErrLogsArchivedFollow) {
		t.Fatalf("expected ErrLogsArchivedFollow, got %v", err)
	}
}

func TestLogs_Task_Happy(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	body := []byte("some log content\n")
	if err := bs.Put(context.Background(), "tasks/T-1/log.log.gz", bytes.NewReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	svc := query.NewLogsService(env.deps, bs)
	rc, ref, err := svc.Open(context.Background(), query.LogsRequest{Kind: query.LogsTask, ID: "T-1"})
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if !strings.HasSuffix(ref, "log.log.gz") {
		t.Fatalf("ref: %s", ref)
	}
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, body) {
		t.Fatalf("body mismatch: %q", got)
	}
}

func TestLogs_TargetMissing(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	svc := query.NewLogsService(env.deps, bs)
	_, _, err := svc.Open(context.Background(), query.LogsRequest{Kind: query.LogsTask, ID: "T-1"})
	if !errors.Is(err, query.ErrLogsTargetMissing) {
		t.Fatalf("expected ErrLogsTargetMissing, got %v", err)
	}
}

// TestLogs_Execution_Happy removed — the `logs execution` kind is deleted in
// v2.7 #131 (the retired task_execution model is no longer read; only
// `logs task` survives).

func TestLogs_EmptyID(t *testing.T) {
	env := newQEnv(t)
	bs, _ := blobstore.NewLocalDir(t.TempDir())
	svc := query.NewLogsService(env.deps, bs)
	_, _, err := svc.Open(context.Background(), query.LogsRequest{Kind: query.LogsTask})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}
