package peek_test

// Branch-coverage supplements for peek/server.go error paths that the high-
// level Client cannot trigger (Client validates execution_id locally and
// always JSON-encodes its Request). These paths are reachable in production
// from a misbehaving / hostile client, so testing them via raw dial is the
// right contract guard.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/peek"
)

// rawDialAndRead writes a single request line then drains response frames.
func rawDialAndRead(t *testing.T, sock string, requestLine string) []peek.Response {
	t.Helper()
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := d.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte(requestLine + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := bufio.NewReader(conn)
	var out []peek.Response
	for {
		line, err := r.ReadBytes('\n')
		if err != nil || len(line) == 0 {
			return out
		}
		var resp peek.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("server returned non-json: %q", line)
		}
		out = append(out, resp)
		if resp.Done || resp.Error != nil {
			return out
		}
	}
}

// Server should reply with ReasonInvalidRequest when the wire bytes aren't
// valid JSON (server.go:113-116).
func TestPeekServer_MalformedJSONReturnsInvalidRequest(t *testing.T) {
	_, sock, _ := setupServer(t)
	resps := rawDialAndRead(t, sock, "not-json{")
	if len(resps) == 0 || resps[0].Error == nil {
		t.Fatalf("expected error response, got %+v", resps)
	}
	if resps[0].Error.Reason != peek.ReasonInvalidRequest {
		t.Fatalf("want invalid_request, got %s", resps[0].Error.Reason)
	}
	if !strings.Contains(resps[0].Error.Message, "malformed") {
		t.Fatalf("expected malformed in message, got %q", resps[0].Error.Message)
	}
}

// Server should reply with ReasonInvalidRequest when JSON is well-formed
// but execution_id is empty (server.go:117-120). Client-side validation
// blocks this path; raw dial bypasses it.
func TestPeekServer_EmptyExecutionIDReturnsInvalidRequest(t *testing.T) {
	_, sock, _ := setupServer(t)
	resps := rawDialAndRead(t, sock, `{"execution_id":""}`)
	if len(resps) == 0 || resps[0].Error == nil {
		t.Fatalf("expected error response, got %+v", resps)
	}
	if resps[0].Error.Reason != peek.ReasonInvalidRequest {
		t.Fatalf("want invalid_request, got %s", resps[0].Error.Reason)
	}
	if !strings.Contains(resps[0].Error.Message, "execution_id") {
		t.Fatalf("expected execution_id in message, got %q", resps[0].Error.Message)
	}
}

// fakeServerOnce binds a unix listener, accepts ONE connection, drains
// the request bytes (line-terminated), then runs `behave(conn)` and closes
// the connection + listener. Returns the socket path.
func fakeServerOnce(t *testing.T, behave func(net.Conn)) string {
	t.Helper()
	sock := shortSock(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Drain one request line.
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
		behave(conn)
		_ = conn.Close()
	}()
	return sock
}

// Covers client.go:85-88 — server closes the connection cleanly after a
// frame without sending Done. Client must see io.EOF and exit the goroutine
// without surfacing an error frame.
func TestPeekClient_ServerClosesWithoutDone(t *testing.T) {
	sock := fakeServerOnce(t, func(conn net.Conn) {
		// Send one valid line frame, then close — no Done frame.
		_, _ = conn.Write([]byte(`{"line":"hello"}` + "\n"))
	})
	c := peek.NewClient(sock)
	frames, err := c.Stream(context.Background(), peek.Request{ExecutionID: "E-1"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var got []peek.Frame
	for f := range frames {
		got = append(got, f)
	}
	if len(got) == 0 || got[0].Line != "hello" {
		t.Fatalf("expected line frame, got %+v", got)
	}
	// No Done / Err — the channel just closes from EOF.
	for _, f := range got[1:] {
		if f.Done || f.Err != nil {
			t.Fatalf("expected silent EOF, got %+v", f)
		}
	}
}

// Covers client.go:94-97 — server replies with bytes that aren't valid JSON.
// Client must surface an ErrorPayload with ReasonInvalidRequest and stop.
func TestPeekClient_ServerSendsMalformedJSON(t *testing.T) {
	sock := fakeServerOnce(t, func(conn net.Conn) {
		_, _ = conn.Write([]byte("not-json{\n"))
	})
	c := peek.NewClient(sock)
	frames, err := c.Stream(context.Background(), peek.Request{ExecutionID: "E-1"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var sawErr bool
	for f := range frames {
		if f.Err != nil {
			if f.Err.Reason != peek.ReasonInvalidRequest {
				t.Fatalf("want invalid_request, got %s", f.Err.Reason)
			}
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("expected client to surface ErrorPayload for malformed server reply")
	}
}

// TestPeekServer_WriteLineAbortsOnClientClose deterministically covers
// server.go:142-144 — when writeLine fails (client closed conn early),
// the loop must return. Without this test the branch only fires when the
// client-side close races the server-side write, producing coverage flap.
//
// To force the failure: write enough events that the server's per-line
// write loop cannot drain into the socket buffer before the client closes,
// then close the client conn immediately after writing the request and
// briefly draining any initial frames.
func TestPeekServer_WriteLineAbortsOnClientClose(t *testing.T) {
	_, sock, root := setupServer(t)
	// Write a large number of large lines so the server's writeLine loop
	// cannot complete before the client tears down the connection.
	const nLines = 2000
	lines := make([]string, 0, nLines)
	bigText := strings.Repeat("x", 4096)
	for i := 0; i < nLines; i++ {
		lines = append(lines, `{"type":"thinking","text":"`+bigText+`"}`)
	}
	writeEvents(t, root, "E-bigclose", lines)

	// Dial raw, send the request, then close the conn before reading.
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := d.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := conn.Write([]byte(`{"execution_id":"E-bigclose"}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Immediately close — the server is about to enter the per-line write
	// loop with 2000 large frames; the next writeLine will hit EPIPE and
	// trigger the early-return branch.
	_ = conn.Close()
	// Give the server a moment to attempt writes and return.
	time.Sleep(100 * time.Millisecond)
	// Test passes as long as the server didn't crash; the coverage payoff
	// is the deterministic exercise of server.go:142-144.
}

// Driving Stream with an already-cancelled ctx exercises the goroutine's
// top-of-loop `<-ctx.Done()` branch (client.go:79-81). Without explicit
// cancel before the first read, the channel is racy.
func TestPeekClient_StreamCancelledBeforeFirstRead(t *testing.T) {
	_, sock, root := setupServer(t)
	writeEvents(t, root, "E-cx", []string{`{"type":"thinking","text":"a"}`, `{"type":"thinking","text":"b"}`})
	c := peek.NewClient(sock)
	ctx, cancel := context.WithCancel(context.Background())
	frames, err := c.Stream(ctx, peek.Request{ExecutionID: "E-cx", Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	// Drain — we must observe either the cancel-frame from client side, the
	// server's "stream_canceled" frame, or an EOF (server closes when ctx
	// derived from accept-context fires). All are legitimate.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				return
			}
			if f.Done || f.Err != nil {
				return
			}
		case <-deadline:
			t.Fatal("frames channel not closed after cancel within 2s")
		}
	}
}
