// Package workerdaemon: stream_client.go is the daemon-side SSE STREAM client
// for the Environment BC worker control channel (v2.7 D5 slice-2, ADR-0050,
// task #108). It is the low-latency down-push counterpart to PullCommands
// (the poll path) and rides the SAME authed transport (bearer token) +
// the SAME offset-driven resume (?after=<cursor>).
//
// §-1 CONTRACT: the stream client is ONLY a transport. It does NOT own delivery
// guarantees. It parses SSE frames into ControlCommand values (carrying the
// log-assigned OFFSET) and hands each one to a callback IN ORDER, exactly as
// the poll path hands the PullCommands batch to the same handle/ack/cursor
// logic in control_loop.go. At-least-once / offset-ordering / reconnect-by-
// offset are owned by the LOG + the control loop's cursor, NOT by this client.
// On any disconnect / decode error / heartbeat-timeout it returns an error so
// the control loop falls back to the poll path (catch-up backfills from the
// offset cursor).
package workerdaemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// StreamClient is the subset of AdminClient the ControlLoop needs for the
// stream-first path. Defined as an interface so control_loop_test.go can plug a
// fake SSE source and so the loop stays decoupled from the concrete transport.
// Production wires *AdminClient (its StreamCommands satisfies this).
type StreamClient interface {
	// StreamCommands opens the SSE down-push GET
	// /admin/environment/worker/commands/stream?worker_id=X&after=<after> and
	// invokes onCommand for EACH parsed command frame IN ORDER (offset-ascending,
	// as the server writes them). Heartbeat frames are swallowed (they only reset
	// the idle timer). It blocks until the stream ends:
	//   - ctx cancelled            → returns ctx.Err()
	//   - server closes / network  → returns a non-nil transport error
	//   - heartbeat/idle timeout    → returns a non-nil timeout error
	//   - an SSE `event: error`    → returns a non-nil stream error
	//   - onCommand returns an err  → returns that error (stops streaming)
	// In every non-ctx case the caller (ControlLoop) FALLS BACK to PullCommands
	// (?after=cursor), which backfills from the offset cursor — no command is
	// silently lost.
	StreamCommands(ctx context.Context, workerID string, after int64, idleTimeout time.Duration, onCommand func(ControlCommand) error) error
}

// var _ StreamClient asserts *AdminClient satisfies the loop's stream seam.
var _ StreamClient = (*AdminClient)(nil)

// errStreamHeartbeatTimeout is returned by StreamCommands when no frame (command
// OR heartbeat) arrives within idleTimeout. The control loop treats it like any
// other stream error: prompt fall back to poll (don't hang waiting for frames).
type streamError struct{ msg string }

func (e *streamError) Error() string { return e.msg }

// StreamCommands implements StreamClient. It rides the SAME bearer + baseURL as
// doJSON/PullCommands. Because the AdminClient's shared *http.Client carries a
// per-request Timeout (≈30s) that would kill a long-lived SSE stream, the
// request is issued with that overall deadline DISABLED and liveness is instead
// enforced by an idle watchdog: if no frame (command or 30s server heartbeat)
// arrives within idleTimeout the request context is cancelled → prompt fallback.
func (c *AdminClient) StreamCommands(ctx context.Context, workerID string, after int64, idleTimeout time.Duration, onCommand func(ControlCommand) error) error {
	if strings.TrimSpace(workerID) == "" {
		return &streamError{msg: "adminclient: worker_id required"}
	}
	if idleTimeout <= 0 {
		idleTimeout = defaultStreamIdleTimeout
	}

	base := c.baseURL
	if base == "" {
		base = "http://unix"
	}
	path := fmt.Sprintf("/admin/environment/worker/commands/stream?worker_id=%s&after=%d", workerID, after)

	// Per-stream cancellable context: the idle watchdog cancels it when no frame
	// arrives within idleTimeout (the shared client.Timeout is bypassed below).
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, base+path, nil)
	if err != nil {
		return &streamError{msg: fmt.Sprintf("adminclient: build stream req: %v", err)}
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	// Clone the http.Client with Timeout disabled: the per-request Timeout would
	// abort the long-lived SSE stream after ≈30s even while healthy. Idle
	// liveness is enforced by the watchdog below instead. The Transport (dialer,
	// TLS pinning, unix socket) is REUSED unchanged.
	streamClient := &http.Client{
		Transport: c.httpc.Transport,
		Timeout:   0,
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return &streamError{msg: fmt.Sprintf("adminclient: open stream: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &AdminError{Method: http.MethodGet, Path: path, Status: resp.StatusCode, Body: "stream open"}
	}

	// Idle watchdog: the parse loop stamps lastActivity (atomically) on every
	// line read; a poller goroutine cancels the request when no activity has
	// occurred for idleTimeout → Read unblocks with an error → prompt fallback to
	// poll. Using an atomic timestamp (not a Timer Reset) avoids any concurrent
	// Timer Stop/Reset/fire race between the two goroutines.
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	var timedOut atomic.Bool
	go func() {
		// Poll at a fraction of the idle window so the fallback is prompt.
		check := idleTimeout / 4
		if check <= 0 {
			check = time.Millisecond
		}
		t := time.NewTicker(check)
		defer t.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-t.C:
				idleFor := time.Duration(time.Now().UnixNano() - lastActivity.Load())
				if idleFor >= idleTimeout {
					timedOut.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	resetIdle := func() { lastActivity.Store(time.Now().UnixNano()) }

	return parseSSE(resp.Body, resetIdle, ctx, &timedOut, onCommand)
}

// parseSSE reads the SSE byte stream frame-by-frame. An SSE frame is a block of
// lines terminated by a blank line; we care about `id:` (the OFFSET, secondary —
// the command JSON carries the authoritative offset), `data:` (the JSON
// command OR a heartbeat marker), and `event:` (an `error` event → stream
// error). Heartbeat data frames (`{"type":"control.heartbeat"}`) are swallowed
// after resetting the idle timer. Every line resets the idle watchdog.
func parseSSE(
	body interface{ Read([]byte) (int, error) },
	onFrameByte func(),
	parentCtx context.Context,
	timedOut *atomic.Bool,
	onCommand func(ControlCommand) error,
) error {
	sc := bufio.NewScanner(body)
	// SSE frames can be large (a command payload carries the work brief, #115).
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var dataLines []string
	var eventType string

	dispatch := func() error {
		defer func() { dataLines = nil; eventType = "" }()
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		if eventType == "error" {
			return &streamError{msg: "stream error event: " + data}
		}
		// Heartbeat frame: swallow (idle timer already reset by the line read).
		if isHeartbeat(data) {
			return nil
		}
		var cmd ControlCommand
		if err := json.Unmarshal([]byte(data), &cmd); err != nil {
			return &streamError{msg: fmt.Sprintf("stream decode frame: %v (data=%s)", err, data)}
		}
		// A non-heartbeat frame with no offset is a malformed command — surface it
		// rather than silently delivering a zero-offset command that would corrupt
		// the cursor.
		if cmd.Offset <= 0 {
			return &streamError{msg: fmt.Sprintf("stream frame missing offset (data=%s)", data)}
		}
		return onCommand(cmd)
	}

	for sc.Scan() {
		onFrameByte() // reset idle watchdog on any activity
		line := sc.Text()
		switch {
		case line == "":
			// Frame boundary → dispatch the accumulated frame.
			if err := dispatch(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// SSE comment — ignore (also a liveness signal; idle already reset).
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "id:"):
			// The `id:` carries the offset for native EventSource resume; the
			// daemon tracks the offset inside the command JSON, so this is
			// informational. Ignore.
		default:
			// Unknown field — ignore per SSE spec.
		}
	}
	// Scan ended: either ctx cancel (graceful / idle-timeout) or transport EOF.
	if err := sc.Err(); err != nil {
		if timedOut.Load() {
			return &streamError{msg: "stream idle timeout (no frame within window)"}
		}
		if parentCtx.Err() != nil {
			return parentCtx.Err()
		}
		return &streamError{msg: fmt.Sprintf("stream read: %v", err)}
	}
	// Clean EOF (server closed the stream).
	if timedOut.Load() {
		return &streamError{msg: "stream idle timeout (no frame)"}
	}
	if parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	return &streamError{msg: "stream closed by server"}
}

// isHeartbeat reports whether the SSE data frame is the server's liveness
// heartbeat (`{"type":"control.heartbeat"}`) rather than a command. Parsed
// leniently: any JSON object whose `type` == "control.heartbeat".
func isHeartbeat(data string) bool {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &probe); err != nil {
		return false
	}
	return probe.Type == "control.heartbeat"
}
