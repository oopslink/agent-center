package workerdaemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admin/dispatchq"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// shortSock returns a unix-socket-safe path. macOS caps at 104 bytes;
// drop directly under /tmp to keep the path short.
func shortSock(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ac-wac-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// fakeServer is a per-test in-memory recorder backed by an httptest
// server that listens on a unix socket. Mirrors the production admin
// server's URL surface but lets us assert on the exact request shape.
type fakeServer struct {
	t   *testing.T
	mux *http.ServeMux

	mu       sync.Mutex
	requests []recordedReq

	// queued payloads
	dispatches []dispatch.DispatchEnvelope
	kills      []dispatchq.KillRequest
}

type recordedReq struct {
	Method string
	Path   string
	Query  string
	Body   []byte
}

func newFakeServer(t *testing.T) (*fakeServer, *AdminClient, func()) {
	t.Helper()
	sock := shortSock(t, "fake.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{}
	fs := &fakeServer{t: t, mux: http.NewServeMux()}
	fs.registerRoutes()
	srv.Handler = fs.mux

	go func() { _ = srv.Serve(ln) }()

	// Wait for the socket to accept dial.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.Dial("unix", sock)
		if err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fakeServer: socket %s never ready: %v", sock, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	client := NewAdminClient(sock, 2*time.Second)
	cleanup := func() {
		_ = srv.Shutdown(context.Background())
		_ = ln.Close()
	}
	return fs, client, cleanup
}

func (fs *fakeServer) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.requests = append(fs.requests, recordedReq{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Body:   body,
	})
}

func (fs *fakeServer) reqs() []recordedReq {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cp := make([]recordedReq, len(fs.requests))
	copy(cp, fs.requests)
	return cp
}

func (fs *fakeServer) registerRoutes() {
	fs.mux.HandleFunc("/admin/workforce/worker/enroll", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"worker_id":"w-1","event_id":"E-1","version":1}`))
	})
	fs.mux.HandleFunc("/admin/dispatch/queue/pull", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		fs.mu.Lock()
		pending := fs.dispatches
		fs.dispatches = nil
		fs.mu.Unlock()
		if pending == nil {
			pending = []dispatch.DispatchEnvelope{}
		}
		_ = json.NewEncoder(w).Encode(pending)
	})
	fs.mux.HandleFunc("/admin/kill/queue/pull", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		fs.mu.Lock()
		pending := fs.kills
		fs.kills = nil
		fs.mu.Unlock()
		if pending == nil {
			pending = []dispatchq.KillRequest{}
		}
		_ = json.NewEncoder(w).Encode(pending)
	})
	fs.mux.HandleFunc("/admin/taskruntime/exec/report-progress", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	fs.mux.HandleFunc("/admin/taskruntime/exec/report-failure", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"failed"}`))
	})
	fs.mux.HandleFunc("/admin/taskruntime/artifact/append", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"artifact_id":"A-1"}`))
	})
}

func TestAdminClient_Enroll(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	if err := client.Enroll(context.Background(), "w-1", []string{"claude-code"}); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 {
		t.Fatalf("want 1 request, got %d", len(reqs))
	}
	if reqs[0].Method != "POST" || reqs[0].Path != "/admin/workforce/worker/enroll" {
		t.Fatalf("bad request: %+v", reqs[0])
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body["worker_id"] != "w-1" {
		t.Fatalf("worker_id=%v", body["worker_id"])
	}
	caps, _ := body["capabilities"].([]any)
	if len(caps) != 1 || caps[0] != "claude-code" {
		t.Fatalf("capabilities=%v", caps)
	}
}

func TestAdminClient_Enroll_EmptyWorkerIDFails(t *testing.T) {
	_, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.Enroll(context.Background(), "  ", nil); err == nil {
		t.Fatal("expected error for empty worker_id")
	}
}

func TestAdminClient_Heartbeat_AliasesEnroll(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.Heartbeat(context.Background(), "w-1", nil); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if len(fs.reqs()) != 1 || fs.reqs()[0].Path != "/admin/workforce/worker/enroll" {
		t.Fatalf("Heartbeat should hit enroll endpoint; got %+v", fs.reqs())
	}
}

func TestAdminClient_PullDispatches_Empty(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	envs, err := client.PullDispatches(context.Background(), "w-1")
	if err != nil {
		t.Fatalf("PullDispatches: %v", err)
	}
	if len(envs) != 0 {
		t.Fatalf("want empty, got %d", len(envs))
	}
	reqs := fs.reqs()
	if len(reqs) != 1 {
		t.Fatalf("want 1 req, got %d", len(reqs))
	}
	if reqs[0].Method != "GET" || reqs[0].Path != "/admin/dispatch/queue/pull" || reqs[0].Query != "worker_id=w-1" {
		t.Fatalf("bad request: %+v", reqs[0])
	}
}

func TestAdminClient_PullDispatches_Returns(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	fs.mu.Lock()
	fs.dispatches = []dispatch.DispatchEnvelope{
		{
			EnvelopeVersion: dispatch.EnvelopeVersionV2,
			ExecutionID:     "E-1",
			TaskID:          "T-1",
			WorkerID:        "w-1",
			ProjectID:       "P-1",
			AgentInstanceID: "A-1",
			AgentCLI:        "fakeagent",
			WorkspaceMode:   execution.WorkspaceDirect,
			TaskTitle:       "do thing",
			Priority:        "normal",
		},
	}
	fs.mu.Unlock()

	envs, err := client.PullDispatches(context.Background(), "w-1")
	if err != nil {
		t.Fatalf("PullDispatches: %v", err)
	}
	if len(envs) != 1 || envs[0].ExecutionID != "E-1" {
		t.Fatalf("unexpected envs: %+v", envs)
	}
}

func TestAdminClient_PullKills(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	fs.mu.Lock()
	fs.kills = []dispatchq.KillRequest{
		{ExecutionID: "E-2", Reason: execution.KilledUserRequest, Message: "user cancelled"},
	}
	fs.mu.Unlock()

	got, err := client.PullKills(context.Background())
	if err != nil {
		t.Fatalf("PullKills: %v", err)
	}
	if len(got) != 1 || got[0].ExecutionID != "E-2" {
		t.Fatalf("unexpected kills: %+v", got)
	}
}

func TestAdminClient_ReportProgress(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	if err := client.ReportProgress(context.Background(), "E-3", "step_1", "running"); err != nil {
		t.Fatalf("ReportProgress: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Path != "/admin/taskruntime/exec/report-progress" {
		t.Fatalf("bad request: %+v", reqs)
	}
	var body map[string]string
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["execution_id"] != "E-3" || body["kind"] != "step_1" || body["content"] != "running" {
		t.Fatalf("body=%v", body)
	}
}

func TestAdminClient_ReportFailure(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	if err := client.ReportFailure(context.Background(), "E-4", "test_reason", "test_msg"); err != nil {
		t.Fatalf("ReportFailure: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Path != "/admin/taskruntime/exec/report-failure" {
		t.Fatalf("bad path: %+v", reqs)
	}
	var body map[string]string
	_ = json.Unmarshal(reqs[0].Body, &body)
	if body["reason"] != "test_reason" || body["message"] != "test_msg" {
		t.Fatalf("body=%v", body)
	}
}

func TestAdminClient_ReportArtifact(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	if err := client.ReportArtifact(context.Background(), "E-5", []byte("blobby"), "log"); err != nil {
		t.Fatalf("ReportArtifact: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Path != "/admin/taskruntime/artifact/append" {
		t.Fatalf("bad path: %+v", reqs)
	}
	var body map[string]string
	_ = json.Unmarshal(reqs[0].Body, &body)
	if body["kind"] != "log" || body["execution_id"] != "E-5" {
		t.Fatalf("body=%v", body)
	}
}

func TestAdminClient_NonStatus2xx_ReturnsAdminError(t *testing.T) {
	sock := shortSock(t, "err.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/workforce/worker/enroll", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// Wait for socket.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.Dial("unix", sock)
		if err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket never ready: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	client := NewAdminClient(sock, 1*time.Second)
	err = client.Enroll(context.Background(), "w-1", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*AdminError)
	if !ok {
		t.Fatalf("want *AdminError, got %T: %v", err, err)
	}
	if ae.Status != http.StatusInternalServerError {
		t.Fatalf("status=%d", ae.Status)
	}
}

// Touch httptest import so it stays referenced even if a future refactor
// drops the inline server.
var _ = httptest.NewServer

// Touch taskruntime to ensure import alignment with envelope types.
var _ = taskruntime.TaskID("")
