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
	// v2.3-1: dedicated heartbeat endpoint (replaces the v2.2 re-enroll
	// hack that swallowed 409 already_exists as the success signal).
	fs.mux.HandleFunc("/admin/workforce/worker/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"worker_id":"w-1"}`))
	})
	// v2.7 #147: worker capability report (auto-probe upload).
	fs.mux.HandleFunc("/admin/workforce/worker/capabilities", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"worker_id":"w-1","version":2}`))
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
	// v2.3-3b: blob_put is the new pre-step for ReportArtifact when the
	// agent emits inline blob bytes.
	fs.mux.HandleFunc("/admin/blob/put", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rel_path":"ok"}`))
	})
	// v2.3-3b: secret resolve handler — echoes a deterministic base64
	// plaintext so AdminClient.ResolveSecret can round-trip.
	fs.mux.HandleFunc("/admin/secret/user-secret/resolve", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"S-1","name":"db_password","plaintext_base64":"c2VjcmV0LXZhbA=="}`))
	})
	// T341 reply-guardrail: echoes two prompts so FetchReplyNudges can round-trip.
	fs.mux.HandleFunc("/admin/environment/agent/reply-nudges", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"prompts":["reply in DM dm-1","reply in channel ch-2"]}`))
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

// TestAdminClient_FetchReplyNudges is the worker-client↔center critical-path
// seam (T341): the client POSTs {agent_id} to the reply-nudges endpoint and
// parses the returned prompts.
func TestAdminClient_FetchReplyNudges(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	prompts, err := client.FetchReplyNudges(context.Background(), "AG1")
	if err != nil {
		t.Fatalf("FetchReplyNudges: %v", err)
	}
	if len(prompts) != 2 || prompts[0] != "reply in DM dm-1" {
		t.Fatalf("unexpected prompts: %+v", prompts)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/admin/environment/agent/reply-nudges" {
		t.Fatalf("bad request: %+v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body["agent_id"] != "AG1" {
		t.Fatalf("agent_id=%v", body["agent_id"])
	}
}

// TestAdminClient_FetchReplyNudges_EmptyAgentIDFails pins the client-side guard.
func TestAdminClient_FetchReplyNudges_EmptyAgentIDFails(t *testing.T) {
	_, client, cleanup := newFakeServer(t)
	defer cleanup()
	if _, err := client.FetchReplyNudges(context.Background(), "  "); err == nil {
		t.Fatal("want error for blank agent_id, got nil")
	}
}

func TestAdminClient_Enroll_EmptyWorkerIDFails(t *testing.T) {
	_, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.Enroll(context.Background(), "  ", nil); err == nil {
		t.Fatal("expected error for empty worker_id")
	}
}

// v2.3-1: Heartbeat now hits the dedicated endpoint (was an Enroll alias
// in v2.2 that swallowed 409 already_exists).
func TestAdminClient_Heartbeat_PostsToHeartbeatEndpoint(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.Heartbeat(context.Background(), "w-1", nil, nil); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/admin/workforce/worker/heartbeat" {
		t.Fatalf("Heartbeat should POST to /heartbeat endpoint; got %+v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body["worker_id"] != "w-1" {
		t.Fatalf("worker_id=%v", body["worker_id"])
	}
}

func TestAdminClient_Heartbeat_EmptyWorkerIDFails(t *testing.T) {
	_, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.Heartbeat(context.Background(), "  ", nil, nil); err == nil {
		t.Fatal("expected error for empty worker_id")
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
