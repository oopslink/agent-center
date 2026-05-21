package trace_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/trace"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

type stubStore struct {
	puts        map[string][]byte
	putErr      error
	existsErr   error
}

func newStubStore() *stubStore { return &stubStore{puts: map[string][]byte{}} }

func (s *stubStore) Put(_ context.Context, relPath string, content io.Reader, _ int64) error {
	if s.putErr != nil {
		return s.putErr
	}
	b, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	s.puts[relPath] = b
	return nil
}
func (s *stubStore) Get(_ context.Context, relPath string) (io.ReadCloser, error) {
	b, ok := s.puts[relPath]
	if !ok {
		return nil, blobstore.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (s *stubStore) Delete(_ context.Context, relPath string) error {
	if _, ok := s.puts[relPath]; !ok {
		return blobstore.ErrBlobNotFound
	}
	delete(s.puts, relPath)
	return nil
}
func (s *stubStore) Exists(_ context.Context, relPath string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	_, ok := s.puts[relPath]
	return ok, nil
}
func (s *stubStore) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for k := range s.puts {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}
func (s *stubStore) URL(relPath string) string { return "stub://" + relPath }

func setupTraceEnv(t *testing.T) (*trace.Service, *stubStore, *obsqlite.EventRepo) {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	store := newStubStore()
	svc := trace.NewService(store, sink, clk)
	return svc, store, er
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTraceArchive_HappyPath_UploadedEvent(t *testing.T) {
	svc, store, er := setupTraceEnv(t)
	tdir := t.TempDir()
	jsonlPath := writeFile(t, tdir, "events.jsonl", `{"type":"thinking"}`+"\n")
	logPath := writeFile(t, tdir, "agent.log", "hello world\n")
	res, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID:      "T-1",
		ExecutionID: taskruntime.TaskExecutionID("E-1"),
		SourceFiles: []trace.SourceFileSpec{
			{Path: jsonlPath, NameInTar: "events.jsonl"},
			{Path: logPath, NameInTar: "agent.log"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if res.BlobRef != "tasks/T-1/E-1/trace.jsonl.gz" {
		t.Fatalf("unexpected blob_ref: %s", res.BlobRef)
	}
	if res.Bytes <= 0 {
		t.Fatalf("expected positive bytes")
	}
	if _, ok := store.puts[res.BlobRef]; !ok {
		t.Fatal("blob not stored")
	}
	// uploaded event
	tup := observability.EventType("observability.trace_archive_uploaded")
	es, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &tup})
	if len(es) != 1 {
		t.Fatalf("expected 1 uploaded event, got %d", len(es))
	}
}

func TestTraceArchive_VerifyTarballContent(t *testing.T) {
	svc, store, _ := setupTraceEnv(t)
	tdir := t.TempDir()
	jsonlPath := writeFile(t, tdir, "events.jsonl", "line1\nline2\n")
	res, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID:      "T-1",
		ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: jsonlPath, NameInTar: "events.jsonl"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gz, _ := gzip.NewReader(bytes.NewReader(store.puts[res.BlobRef]))
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Next: %v", err)
	}
	if hdr.Name != "events.jsonl" {
		t.Fatalf("unexpected tar name: %q", hdr.Name)
	}
	body, _ := io.ReadAll(tr)
	if string(body) != "line1\nline2\n" {
		t.Fatalf("body mismatch: %q", body)
	}
}

func TestTraceArchive_CenterCallback_Invoked(t *testing.T) {
	svc, _, _ := setupTraceEnv(t)
	tdir := t.TempDir()
	jsonlPath := writeFile(t, tdir, "events.jsonl", "x")
	called := false
	cb := func(_ context.Context, info trace.TraceArchiveResult) error {
		called = true
		if info.BlobRef == "" {
			t.Fatal("blob ref empty")
		}
		return nil
	}
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID:      "T-1",
		ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: jsonlPath}},
	}, cb)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("center callback not invoked")
	}
}

func TestTraceArchive_CenterCallbackError_EmitsFailure(t *testing.T) {
	svc, _, er := setupTraceEnv(t)
	tdir := t.TempDir()
	jsonlPath := writeFile(t, tdir, "events.jsonl", "x")
	boom := errors.New("callback boom")
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID:      "T-1",
		ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: jsonlPath}},
	}, func(_ context.Context, _ trace.TraceArchiveResult) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("expected callback error propagated, got %v", err)
	}
	failType := observability.EventType("observability.trace_archive_failed")
	es, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &failType})
	if len(es) != 1 {
		t.Fatalf("expected failure event, got %d", len(es))
	}
}

func TestTraceArchive_SourceMissing_EmitsFailure(t *testing.T) {
	svc, _, er := setupTraceEnv(t)
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID:      "T-1",
		ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: "/no/such/file.jsonl"}},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	failType := observability.EventType("observability.trace_archive_failed")
	es, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &failType})
	if len(es) != 1 {
		t.Fatalf("expected 1 failure event, got %d", len(es))
	}
	p := es[0].Payload()
	if p["reason"] != "source_missing" {
		t.Fatalf("reason = %v", p["reason"])
	}
}

func TestTraceArchive_BlobStoreError_EmitsBlobStoreUnavailable(t *testing.T) {
	svc, store, er := setupTraceEnv(t)
	store.putErr = errors.New("disk full")
	tdir := t.TempDir()
	jsonlPath := writeFile(t, tdir, "events.jsonl", "x")
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID:      "T-1",
		ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: jsonlPath}},
	}, nil)
	if err == nil {
		t.Fatal("expected error from Put failure")
	}
	failType := observability.EventType("observability.trace_archive_failed")
	es, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &failType})
	if len(es) != 1 {
		t.Fatalf("expected 1 failure event, got %d", len(es))
	}
	if es[0].Payload()["reason"] != "blob_store_unavailable" {
		t.Fatalf("reason mismatch")
	}
}

func TestTraceArchive_PayloadTooLarge(t *testing.T) {
	svc, _, er := setupTraceEnv(t)
	svc = svc.WithMaxBytes(50) // small
	tdir := t.TempDir()
	big := strings.Repeat("x", 1000)
	jsonlPath := writeFile(t, tdir, "events.jsonl", big)
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID:      "T-1",
		ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: jsonlPath}},
	}, nil)
	if !errors.Is(err, blobstore.ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}
	failType := observability.EventType("observability.trace_archive_failed")
	es, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &failType})
	if len(es) != 1 {
		t.Fatal("expected failure event")
	}
	if es[0].Payload()["reason"] != "payload_too_large" {
		t.Fatalf("reason mismatch")
	}
}

func TestTraceArchive_Required_Fields(t *testing.T) {
	svc, _, _ := setupTraceEnv(t)
	if _, err := svc.Archive(context.Background(), trace.ArchiveRequest{}, nil); err == nil {
		t.Fatal("expected empty req error")
	}
	if _, err := svc.Archive(context.Background(), trace.ArchiveRequest{ExecutionID: "E-1"}, nil); err == nil {
		t.Fatal("expected missing task_id error")
	}
	if _, err := svc.Archive(context.Background(), trace.ArchiveRequest{TaskID: "T-1", ExecutionID: "E-1"}, nil); err == nil {
		t.Fatal("expected missing source files error")
	}
}

func TestPendingScanner_RetriesUnfinishedArchives(t *testing.T) {
	svc, _, er := setupTraceEnv(t)
	execRoot := t.TempDir()
	// One execution dir: with terminal.json + events.jsonl, no uploaded.json.
	dir := filepath.Join(execRoot, "E-7")
	_ = os.MkdirAll(dir, 0o755)
	writeFile(t, dir, "terminal.json", `{"status":"completed"}`)
	writeFile(t, dir, "events.jsonl", "line\n")
	// Another execution dir: already uploaded (skip).
	dir2 := filepath.Join(execRoot, "E-8")
	_ = os.MkdirAll(dir2, 0o755)
	writeFile(t, dir2, "terminal.json", `{}`)
	writeFile(t, dir2, "events.jsonl", "x")
	writeFile(t, dir2, "uploaded.json", "ts")
	// Third dir: no terminal.json (still running).
	dir3 := filepath.Join(execRoot, "E-9")
	_ = os.MkdirAll(dir3, 0o755)
	writeFile(t, dir3, "events.jsonl", "running")

	scanner := trace.NewPendingScanner(execRoot, svc, clock.SystemClock{})
	res, err := scanner.Scan(context.Background(), func(execDir string) (trace.ArchiveRequest, bool, error) {
		execID := filepath.Base(execDir)
		return trace.ArchiveRequest{
			TaskID:      "T-x",
			ExecutionID: taskruntime.TaskExecutionID(execID),
			SourceFiles: []trace.SourceFileSpec{{Path: filepath.Join(execDir, "events.jsonl")}},
		}, true, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Retried != 1 || res.Succeeded != 1 || res.Failed != 0 {
		t.Fatalf("scan result mismatch: %+v", res)
	}
	// uploaded.json should now exist for E-7
	if _, err := os.Stat(filepath.Join(dir, "uploaded.json")); err != nil {
		t.Fatal("uploaded marker not written")
	}
	// uploaded event in events table
	tup := observability.EventType("observability.trace_archive_uploaded")
	es, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &tup})
	if len(es) != 1 {
		t.Fatalf("expected 1 uploaded event, got %d", len(es))
	}
}

func TestPendingScanner_MissingRoot_NoError(t *testing.T) {
	svc, _, _ := setupTraceEnv(t)
	scanner := trace.NewPendingScanner(filepath.Join(t.TempDir(), "no-such-root"), svc, clock.SystemClock{})
	res, err := scanner.Scan(context.Background(), func(string) (trace.ArchiveRequest, bool, error) {
		return trace.ArchiveRequest{}, false, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Retried != 0 {
		t.Fatalf("expected 0 retries, got %d", res.Retried)
	}
}
