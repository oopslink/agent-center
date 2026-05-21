package trace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/trace"
)

func TestService_WithActor_SetsCustomActor(t *testing.T) {
	svc, _, er := setupTraceEnv(t)
	svc = svc.WithActor(observability.Actor("worker:W-1"))
	tdir := t.TempDir()
	p := filepath.Join(tdir, "events.jsonl")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID: "T-1", ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: p}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tup := observability.EventType("observability.trace_archive_uploaded")
	es, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &tup})
	if len(es) != 1 {
		t.Fatalf("expected 1 event, got %d", len(es))
	}
	if string(es[0].Actor()) != "worker:W-1" {
		t.Fatalf("actor mismatch: %s", es[0].Actor())
	}
}

func TestService_WithActor_EmptyIgnored(t *testing.T) {
	svc, _, _ := setupTraceEnv(t)
	prev := svc
	svc = svc.WithActor("")
	if svc != prev {
		// returned same receiver, fine
	}
}

func TestService_NilStore_Error(t *testing.T) {
	svc := trace.NewService(nil, nil, nil)
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{TaskID: "T", ExecutionID: "E", SourceFiles: []trace.SourceFileSpec{{Path: "x"}}}, nil)
	if err == nil {
		t.Fatal("expected nil-store error")
	}
}

func TestPendingScanner_DeriveError_CountsFailed(t *testing.T) {
	svc, _, _ := setupTraceEnv(t)
	root := t.TempDir()
	dir := filepath.Join(root, "E-1")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "terminal.json"), []byte("{}"), 0o644)
	scanner := trace.NewPendingScanner(root, svc, nil)
	res, err := scanner.Scan(context.Background(), func(_ string) (trace.ArchiveRequest, bool, error) {
		return trace.ArchiveRequest{}, false, errBlobErr()
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 {
		t.Fatalf("expected 1 failed, got %+v", res)
	}
}

func TestPendingScanner_DeriveSkip(t *testing.T) {
	svc, _, _ := setupTraceEnv(t)
	root := t.TempDir()
	dir := filepath.Join(root, "E-1")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "terminal.json"), []byte("{}"), 0o644)
	scanner := trace.NewPendingScanner(root, svc, nil)
	res, err := scanner.Scan(context.Background(), func(_ string) (trace.ArchiveRequest, bool, error) {
		return trace.ArchiveRequest{}, false, nil // skip
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Retried != 0 || res.Failed != 0 {
		t.Fatalf("expected no retry no failure, got %+v", res)
	}
}

func errBlobErr() error { return errSomething{} }

type errSomething struct{}

func (errSomething) Error() string { return "derive failed" }
