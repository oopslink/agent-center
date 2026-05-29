package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// controlFakeServer is an in-memory implementation of the three control
// endpoints backed by an httptest server, with an append-only command log and
// a per-worker cumulative ack cursor. It proves the loop end-to-end over the
// real AdminClient/doJSON transport (bearer auth included).
type controlFakeServer struct {
	mu     sync.Mutex
	cmds   []ControlCommand // append-only, offsets ascending starting at 1
	acked  int64            // cumulative last-acked offset
	online bool
}

func newControlFakeServer() *controlFakeServer {
	return &controlFakeServer{}
}

// seed appends a command at the next offset.
func (s *controlFakeServer) seed(cmdType, payload, idemKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	off := int64(len(s.cmds) + 1)
	s.cmds = append(s.cmds, ControlCommand{
		ID:             "cmd-" + strconv.FormatInt(off, 10),
		Offset:         off,
		IdempotencyKey: idemKey,
		CommandType:    cmdType,
		Payload:        payload,
		CreatedAt:      time.Now().Format(time.RFC3339Nano),
	})
}

func (s *controlFakeServer) ackedOffset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acked
}

func (s *controlFakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/environment/worker/connect", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.online = true
		acked := s.acked
		s.mu.Unlock()
		writeControlJSON(w, map[string]any{
			"worker_id":         "w-1",
			"last_acked_offset": acked,
			"status":            "online",
		})
	})
	mux.HandleFunc("/admin/environment/worker/commands", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		s.mu.Lock()
		out := []ControlCommand{}
		for _, c := range s.cmds {
			if c.Offset > after {
				out = append(out, c)
			}
		}
		s.mu.Unlock()
		writeControlJSON(w, map[string]any{"commands": out})
	})
	mux.HandleFunc("/admin/environment/worker/ack", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WorkerID string `json:"worker_id"`
			Offset   int64  `json:"offset"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		if req.Offset > s.acked {
			s.acked = req.Offset // cumulative — never regress
		}
		acked := s.acked
		s.mu.Unlock()
		writeControlJSON(w, map[string]any{"worker_id": req.WorkerID, "last_acked_offset": acked})
	})
	return mux
}

func writeControlJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// recordingHandler records every command it handled (in order) for assertions.
type recordingHandler struct {
	mu      sync.Mutex
	handled []ControlCommand
	failOn  string // command ID to fail on (empty = never)
	failErr error
}

func (h *recordingHandler) Handle(_ context.Context, cmd ControlCommand) error {
	if h.failOn != "" && cmd.ID == h.failOn {
		return h.failErr
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handled = append(h.handled, cmd)
	return nil
}

func (h *recordingHandler) ids() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.handled))
	for i, c := range h.handled {
		out[i] = c.ID
	}
	return out
}

func newControlTestClient(t *testing.T, fs *controlFakeServer) *AdminClient {
	t.Helper()
	srv := httptest.NewServer(fs.handler())
	t.Cleanup(srv.Close)
	c := NewAdminClient("", 2*time.Second).WithToken("test-token")
	// Point at the httptest TCP server: override baseURL + swap the
	// unix-socket transport for the test server's default TCP client.
	c.baseURL = srv.URL
	c.httpc = srv.Client()
	c.httpc.Timeout = 2 * time.Second
	return c
}

// TestControlLoop_EndToEnd_ConnectPullHandleAck drives the loop end-to-end:
// connect → N seeded commands → loop pulls + handles (no-op recorder) + acks →
// server cursor advances to the highest offset, each command handled once.
func TestControlLoop_EndToEnd_ConnectPullHandleAck(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("noop", "{}", "k1")
	fs.seed("noop", "{}", "k2")
	fs.seed("noop", "{}", "k3")

	client := newControlTestClient(t, fs)
	rec := &recordingHandler{}
	loop := NewControlLoop(ControlLoopConfig{
		WorkerID:     "w-1",
		PollInterval: time.Millisecond, // short + deterministic
		Handler:      rec,
	}, client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Drive deterministically: connect once, then poll until all are acked.
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	if loop.Cursor() != 0 {
		t.Fatalf("initial cursor = %d, want 0", loop.Cursor())
	}
	loop.pollOnce(ctx)

	if got := rec.ids(); len(got) != 3 {
		t.Fatalf("handled %v, want 3 commands", got)
	}
	if fs.ackedOffset() != 3 {
		t.Fatalf("server acked offset = %d, want 3", fs.ackedOffset())
	}
	if loop.Cursor() != 3 {
		t.Fatalf("loop cursor = %d, want 3", loop.Cursor())
	}

	// Idempotent re-poll: nothing new, no re-handle.
	loop.pollOnce(ctx)
	if got := rec.ids(); len(got) != 3 {
		t.Fatalf("re-poll re-handled: %v", got)
	}
}

// TestControlLoop_ReconnectPullsOnlyUnacked proves that after acking a prefix,
// a fresh loop (simulated daemon reconnect) resumes from the server's
// last_acked_offset and pulls/handles ONLY the un-acked commands — acked
// commands are never re-handled. Across the whole run each command is handled
// exactly once.
func TestControlLoop_ReconnectPullsOnlyUnacked(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("noop", "{}", "k1")
	fs.seed("noop", "{}", "k2")

	client := newControlTestClient(t, fs)
	ctx := context.Background()

	// First loop: handle the 2 existing commands and ack.
	rec1 := &recordingHandler{}
	loop1 := NewControlLoop(ControlLoopConfig{WorkerID: "w-1", PollInterval: time.Millisecond, Handler: rec1}, client)
	if !loop1.connect(ctx) {
		t.Fatal("loop1 connect failed")
	}
	loop1.pollOnce(ctx)
	if got := rec1.ids(); len(got) != 2 {
		t.Fatalf("loop1 handled %v, want 2", got)
	}
	if fs.ackedOffset() != 2 {
		t.Fatalf("loop1 acked %d, want 2", fs.ackedOffset())
	}

	// Server gains 2 more commands while loop1 is gone.
	fs.seed("noop", "{}", "k3")
	fs.seed("noop", "{}", "k4")

	// Second loop (reconnect): MUST resume from acked offset (2) and only
	// pull the new commands (offsets 3,4). It must NOT re-handle 1,2.
	rec2 := &recordingHandler{}
	loop2 := NewControlLoop(ControlLoopConfig{WorkerID: "w-1", PollInterval: time.Millisecond, Handler: rec2}, client)
	if !loop2.connect(ctx) {
		t.Fatal("loop2 connect failed")
	}
	if loop2.Cursor() != 2 {
		t.Fatalf("loop2 resume cursor = %d, want 2 (server last_acked_offset)", loop2.Cursor())
	}
	loop2.pollOnce(ctx)

	got2 := rec2.ids()
	if len(got2) != 2 || got2[0] != "cmd-3" || got2[1] != "cmd-4" {
		t.Fatalf("loop2 handled %v, want [cmd-3 cmd-4] (only un-acked)", got2)
	}
	if fs.ackedOffset() != 4 {
		t.Fatalf("loop2 acked %d, want 4", fs.ackedOffset())
	}

	// Across the run: each of the 4 commands handled exactly once.
	all := append(rec1.ids(), rec2.ids()...)
	seen := map[string]int{}
	for _, id := range all {
		seen[id]++
	}
	for _, id := range []string{"cmd-1", "cmd-2", "cmd-3", "cmd-4"} {
		if seen[id] != 1 {
			t.Fatalf("command %s handled %d times, want exactly 1 (seen=%v)", id, seen[id], seen)
		}
	}
}

// TestControlLoop_HandlerErrorDoesNotAdvancePastFailure proves the ack-only-
// after-success contract D2 relies on: when the handler fails on a command,
// the cursor does not advance past it, so it is retried on the next pull.
func TestControlLoop_HandlerErrorDoesNotAdvancePastFailure(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("noop", "{}", "k1")
	fs.seed("noop", "{}", "k2") // cmd-2 will fail first time
	fs.seed("noop", "{}", "k3")

	client := newControlTestClient(t, fs)
	rec := &recordingHandler{failOn: "cmd-2", failErr: errors.New("boom")}
	loop := NewControlLoop(ControlLoopConfig{WorkerID: "w-1", PollInterval: time.Millisecond, Handler: rec}, client)

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.pollOnce(ctx)

	// cmd-1 handled + acked; cmd-2 failed → stop. Cursor at 1, server acked 1.
	if got := rec.ids(); len(got) != 1 || got[0] != "cmd-1" {
		t.Fatalf("handled %v, want [cmd-1] before failure", got)
	}
	if loop.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1 (not advanced past failed cmd-2)", loop.Cursor())
	}
	if fs.ackedOffset() != 1 {
		t.Fatalf("server acked = %d, want 1", fs.ackedOffset())
	}

	// Clear the failure → retry succeeds and drains the rest.
	rec.failOn = ""
	loop.pollOnce(ctx)
	got := rec.ids()
	if len(got) != 3 || got[1] != "cmd-2" || got[2] != "cmd-3" {
		t.Fatalf("after retry handled %v, want cmd-1,cmd-2,cmd-3", got)
	}
	if fs.ackedOffset() != 3 || loop.Cursor() != 3 {
		t.Fatalf("after retry acked=%d cursor=%d, want 3/3", fs.ackedOffset(), loop.Cursor())
	}
}

// TestControlLoop_RunHonorsCtxCancel proves Run returns promptly on ctx cancel
// (graceful shutdown), exercising the ticker/select path.
func TestControlLoop_RunHonorsCtxCancel(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("noop", "{}", "k1")
	client := newControlTestClient(t, fs)
	rec := &recordingHandler{}
	loop := NewControlLoop(ControlLoopConfig{WorkerID: "w-1", PollInterval: 2 * time.Millisecond, Handler: rec}, client)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	// Let it run a few iterations so it connects + handles the command.
	deadline := time.Now().Add(2 * time.Second)
	for fs.ackedOffset() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("loop never acked the seeded command")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// TestControlLoop_ConnectFailureDegradesGracefully proves the loop tolerates an
// unavailable control endpoint at start: connect returns false, Run keeps
// polling (and retries connect) without crashing.
func TestControlLoop_ConnectFailureDegradesGracefully(t *testing.T) {
	// A client pointed at a dead address — every call errors.
	client := NewAdminClient("", 200*time.Millisecond).WithToken("t")
	client.baseURL = "http://127.0.0.1:1" // unroutable
	loop := NewControlLoop(ControlLoopConfig{WorkerID: "w-1", PollInterval: time.Millisecond}, client)

	if loop.connect(context.Background()) {
		t.Fatal("connect should have failed against dead endpoint")
	}

	// Run must not panic/crash; it should return cleanly on ctx cancel.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("Run returned error on degraded path: %v", err)
	}
}

// TestNoopCommandHandler_NeverFails covers the D1 synthetic handler.
func TestNoopCommandHandler_NeverFails(t *testing.T) {
	var logged string
	h := NoopCommandHandler{Logger: func(m string) { logged = m }}
	if err := h.Handle(context.Background(), ControlCommand{CommandType: "x", Offset: 7, ID: "c7"}); err != nil {
		t.Fatal(err)
	}
	if logged == "" {
		t.Fatal("expected the no-op handler to log")
	}
}

// TestControlLoop_DefaultsToNoopHandler proves a nil Handler defaults to the
// D1 NoopCommandHandler (pluggable seam for D2) and the loop still acks.
func TestControlLoop_DefaultsToNoopHandler(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("noop", "{}", "k1")
	client := newControlTestClient(t, fs)
	loop := NewControlLoop(ControlLoopConfig{WorkerID: "w-1", PollInterval: time.Millisecond}, client)
	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.pollOnce(ctx)
	if fs.ackedOffset() != 1 {
		t.Fatalf("acked %d, want 1 (no-op handler still advances)", fs.ackedOffset())
	}
}
