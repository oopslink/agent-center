package workerdaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// sseFakeServer is an httptest SSE source mirroring the center's
// envWorkerCommandsStreamHandler frame shape (id:<offset> + data:<json>, the
// heartbeat data frame, and the terminal `event: error` frame). It lets the
// real AdminClient.StreamCommands transport be exercised end-to-end (SSE parse,
// bearer header, idle behavior) over a real network socket.
type sseFakeServer struct {
	mu sync.Mutex
	// frames written in order before (optionally) holding the connection open.
	cmds      []ControlCommand
	heartbeat bool   // emit a heartbeat frame before the commands
	errEvent  string // if non-empty, emit a terminal `event: error` frame
	hold      bool   // hold the connection open (no EOF) after writing frames
	holdFor   time.Duration
	gotAfter  int64  // captured ?after=
	gotAuth   string // captured Authorization header
}

func (s *sseFakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/environment/worker/commands/stream", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.gotAfter, _ = strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		s.gotAuth = r.Header.Get("Authorization")
		cmds := append([]ControlCommand(nil), s.cmds...)
		hb := s.heartbeat
		errEvent := s.errEvent
		hold := s.hold
		holdFor := s.holdFor
		s.mu.Unlock()

		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl.Flush()

		if hb {
			fmt.Fprint(w, "data: {\"type\":\"control.heartbeat\"}\n\n")
			fl.Flush()
		}
		for _, c := range cmds {
			body, _ := json.Marshal(c)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", c.Offset, body)
			fl.Flush()
		}
		if errEvent != "" {
			fmt.Fprintf(w, "event: error\ndata: {\"error\":%q}\n\n", errEvent)
			fl.Flush()
		}
		if hold {
			select {
			case <-time.After(holdFor):
			case <-r.Context().Done():
			}
		}
		// else: return → EOF (server-closed stream).
	})
	return mux
}

func newStreamTestClient(t *testing.T, s *sseFakeServer) *AdminClient {
	t.Helper()
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	c := NewAdminClient("", 2*time.Second).WithToken("test-token")
	c.baseURL = srv.URL
	c.httpc = srv.Client()
	c.httpc.Timeout = 2 * time.Second // proves StreamCommands bypasses this for the long stream
	return c
}

func cmd(off int64, id, idem, typ, payload string) ControlCommand {
	return ControlCommand{
		Offset: off, ID: id, IdempotencyKey: idem,
		CommandType: typ, Payload: payload,
		CreatedAt: time.Now().Format(time.RFC3339Nano),
	}
}

// TestStreamClient_ParsesFramesWithOffset proves the SSE client parses
// id:/data: frames into ControlCommands carrying the OFFSET + all payload
// fields, in order, and the stream ends (server EOF) with a non-ctx error so
// the loop knows to fall back.
func TestStreamClient_ParsesFramesWithOffset(t *testing.T) {
	s := &sseFakeServer{cmds: []ControlCommand{
		cmd(1, "c1", "k1", "agent.work", `{"brief":"hello"}`),
		cmd(2, "c2", "k2", "agent.stop", `{}`),
	}}
	client := newStreamTestClient(t, s)

	var got []ControlCommand
	err := client.StreamCommands(context.Background(), "w-1", 0, 200*time.Millisecond, func(c ControlCommand) error {
		got = append(got, c)
		return nil
	})
	// server EOF → non-nil (so loop falls back to poll), but NOT ctx error.
	if err == nil {
		t.Fatal("expected a non-nil stream-ended error on server EOF")
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d commands, want 2: %+v", len(got), got)
	}
	if got[0].Offset != 1 || got[0].ID != "c1" || got[0].CommandType != "agent.work" {
		t.Fatalf("frame0 wrong: %+v", got[0])
	}
	// #115: payload (work brief) must survive the stream parse identically.
	if got[0].Payload != `{"brief":"hello"}` {
		t.Fatalf("frame0 payload dropped/mangled: %q", got[0].Payload)
	}
	if got[1].Offset != 2 || got[1].IdempotencyKey != "k2" {
		t.Fatalf("frame1 wrong: %+v", got[1])
	}
	// Resume key + bearer rode through.
	if s.gotAfter != 0 {
		t.Fatalf("server saw after=%d, want 0", s.gotAfter)
	}
	if s.gotAuth != "Bearer test-token" {
		t.Fatalf("server saw auth %q, want Bearer test-token", s.gotAuth)
	}
}

// TestStreamClient_AfterCursorRidesThrough proves the OFFSET cursor is the SSE
// resume key (?after=<cursor>).
func TestStreamClient_AfterCursorRidesThrough(t *testing.T) {
	s := &sseFakeServer{cmds: []ControlCommand{cmd(6, "c6", "k6", "noop", "{}")}}
	client := newStreamTestClient(t, s)
	_ = client.StreamCommands(context.Background(), "w-1", 5, 200*time.Millisecond, func(ControlCommand) error { return nil })
	if s.gotAfter != 5 {
		t.Fatalf("server saw after=%d, want 5 (offset cursor is the resume key)", s.gotAfter)
	}
}

// TestStreamClient_HeartbeatIgnored proves a heartbeat data frame is swallowed
// (not delivered as a command) yet keeps the stream alive.
func TestStreamClient_HeartbeatIgnored(t *testing.T) {
	s := &sseFakeServer{heartbeat: true, cmds: []ControlCommand{cmd(1, "c1", "k1", "noop", "{}")}}
	client := newStreamTestClient(t, s)
	var got []ControlCommand
	_ = client.StreamCommands(context.Background(), "w-1", 0, 200*time.Millisecond, func(c ControlCommand) error {
		got = append(got, c)
		return nil
	})
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("heartbeat not ignored: delivered %+v, want only c1", got)
	}
}

// TestStreamClient_ErrorEventSurfaces proves a terminal SSE `event: error`
// frame returns a non-nil error (fall back to poll).
func TestStreamClient_ErrorEventSurfaces(t *testing.T) {
	s := &sseFakeServer{cmds: []ControlCommand{cmd(1, "c1", "k1", "noop", "{}")}, errEvent: "catch_up_failed"}
	client := newStreamTestClient(t, s)
	var got []ControlCommand
	err := client.StreamCommands(context.Background(), "w-1", 0, 200*time.Millisecond, func(c ControlCommand) error {
		got = append(got, c)
		return nil
	})
	if err == nil {
		t.Fatal("expected error on SSE error event")
	}
	if len(got) != 1 {
		t.Fatalf("commands before the error event should still be delivered, got %d", len(got))
	}
}

// TestStreamClient_DisconnectDetected proves a held-then-EOF stream returns a
// non-nil error (disconnect-detect → fall back).
func TestStreamClient_DisconnectDetected(t *testing.T) {
	s := &sseFakeServer{cmds: []ControlCommand{cmd(1, "c1", "k1", "noop", "{}")}, hold: true, holdFor: 20 * time.Millisecond}
	client := newStreamTestClient(t, s)
	err := client.StreamCommands(context.Background(), "w-1", 0, time.Second, func(ControlCommand) error { return nil })
	if err == nil {
		t.Fatal("expected non-nil error on disconnect (server closed after hold)")
	}
}

// TestStreamClient_HeartbeatTimeout proves the idle watchdog fires when NO frame
// (no command, no heartbeat) arrives within idleTimeout — prompt fallback, not a
// hang. The server holds the connection open silently; the client must give up
// at ~idleTimeout (well under the held duration AND under httpc.Timeout).
func TestStreamClient_HeartbeatTimeout(t *testing.T) {
	s := &sseFakeServer{hold: true, holdFor: 5 * time.Second} // silent, long hold
	client := newStreamTestClient(t, s)

	start := time.Now()
	err := client.StreamCommands(context.Background(), "w-1", 0, 60*time.Millisecond, func(ControlCommand) error { return nil })
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected idle-timeout error")
	}
	if elapsed > time.Second {
		t.Fatalf("idle timeout took %v — did not fall back promptly (hung)", elapsed)
	}
}

// TestStreamClient_CtxCancelReturnsCtxErr proves ctx cancellation returns the
// ctx error (graceful shutdown — NOT treated as a fallback-worthy stream error).
func TestStreamClient_CtxCancelReturnsCtxErr(t *testing.T) {
	s := &sseFakeServer{hold: true, holdFor: 5 * time.Second}
	client := newStreamTestClient(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	err := client.StreamCommands(ctx, "w-1", 0, time.Second, func(ControlCommand) error { return nil })
	if err == nil || err != context.Canceled {
		t.Fatalf("ctx-cancel should return context.Canceled, got %v", err)
	}
}

// TestStreamClient_MissingOffsetFrameSurfaced proves a non-heartbeat frame with
// no offset is surfaced as an error (never silently delivered as offset 0, which
// would corrupt the cursor).
func TestStreamClient_MissingOffsetFrameSurfaced(t *testing.T) {
	s := &sseFakeServer{cmds: []ControlCommand{cmd(0, "bad", "k", "noop", "{}")}}
	client := newStreamTestClient(t, s)
	var got []ControlCommand
	err := client.StreamCommands(context.Background(), "w-1", 0, 200*time.Millisecond, func(c ControlCommand) error {
		got = append(got, c)
		return nil
	})
	if err == nil {
		t.Fatal("expected error on zero-offset frame")
	}
	if len(got) != 0 {
		t.Fatalf("zero-offset frame must NOT be delivered, got %+v", got)
	}
}
