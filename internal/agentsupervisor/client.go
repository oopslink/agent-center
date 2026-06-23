package agentsupervisor

// AttachClient is the client side of the supervisor RPC (slice D2-f s2): it
// connects to a running supervisor's unix socket and drives the four ops. This
// is what the daemon-side re-attach uses. (v2.7: there is no version gate anymore
// — a live supervisor is always re-attachable; the protocol is assumed backward
// -compatible. See protocol.go's ProtocolVersion deferred-with-trigger note.)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// AttachClient is a single connection to a supervisor socket. It is
// SINGLE-FLIGHT per connection: all calls are serialized by mu, because the
// underlying socket carries one length-framed request → one response and
// interleaving would scramble the stream. One daemon owns one AttachClient; for
// more concurrency open more connections.
type AttachClient struct {
	mu   sync.Mutex
	conn net.Conn
}

// Connect dials the supervisor unix socket at sockPath. The ctx bounds the dial
// only; per-call deadlines on the long-lived conn are applied by roundTrip from
// each op's ctx (issue-9bd86b8f gap ①).
func Connect(ctx context.Context, sockPath string) (*AttachClient, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("agentsupervisor: dial %s: %w", sockPath, err)
	}
	return &AttachClient{conn: conn}, nil
}

// Close closes the underlying connection.
func (c *AttachClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// roundTrip writes one request frame and reads one response frame, serialized
// per connection. A response with ok=false is surfaced as an error so callers
// branch on err; the typed fields are returned for inspection (e.g.
// offset_truncated, base_offset).
//
// issue-9bd86b8f gap ①: ctx's deadline (if any) BOUNDS the socket I/O via a conn
// deadline. Without this, a hung-but-alive supervisor (假死) blocks the caller —
// and the control-loop goroutine behind Inject — forever, so the daemon's OnTick
// self-heal never runs ("卡死不能自动恢复"). The pump/Inject call sites already
// wrap their ctx with a 5s timeout; honoring it here is what makes that real. A
// ctx with NO deadline imposes no bound (behavior unchanged — e.g. tests using
// context.Background()).
//
// On a TIMEOUT the connection is POISONED (closed + nil'd): the request frame was
// already written, so a late response would arrive out-of-band and desync the
// length-framed stream if the conn were reused. A returning daemon must re-dial a
// fresh AttachClient (boot-reconcile reattach / pump reconnect) instead.
func (c *AttachClient) roundTrip(ctx context.Context, req Request) (Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return Response{}, errors.New("agentsupervisor: client closed")
	}
	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("agentsupervisor: encode request: %w", err)
	}
	conn := c.conn
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}
	if err := writeFrame(conn, b); err != nil {
		c.poisonIfTimeoutLocked(err)
		return Response{}, fmt.Errorf("agentsupervisor: write request: %w", err)
	}
	frame, err := readFrame(conn)
	if err != nil {
		c.poisonIfTimeoutLocked(err)
		return Response{}, fmt.Errorf("agentsupervisor: read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(frame, &resp); err != nil {
		return Response{}, fmt.Errorf("agentsupervisor: decode response: %w", err)
	}
	return resp, nil
}

// poisonIfTimeoutLocked closes + clears the conn when err is a deadline timeout, so
// a desynced (request-sent, response-pending) connection is never reused. Caller
// MUST hold c.mu. Non-timeout I/O errors are left alone — the caller's existing
// bounded-retry / reconnect logic decides what to do.
func (c *AttachClient) poisonIfTimeoutLocked(err error) {
	if errors.Is(err, os.ErrDeadlineExceeded) && c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// Hello performs the handshake and returns the supervisor's identity + offsets.
// resp.ProtocolVersion is informational (diagnostics + the deferred breaking-change
// trigger); it no longer gates re-attach (v2.7 — backward-compat assumed).
func (c *AttachClient) Hello(ctx context.Context) (HelloResp, error) {
	resp, err := c.roundTrip(ctx, Request{Op: OpHello})
	if err != nil {
		return HelloResp{}, err
	}
	if !resp.Ok {
		return HelloResp{}, fmt.Errorf("agentsupervisor: hello: %s", resp.Error)
	}
	return HelloResp{
		ProtocolVersion: resp.ProtocolVersion,
		InstanceID:      resp.InstanceID,
		AgentID:         resp.AgentID,
		ChildPID:        resp.ChildPID,
		StartedAt:       resp.StartedAt,
		BaseOffset:      resp.BaseOffset,
		CurrentOffset:   resp.CurrentOffset,
	}, nil
}

// Inject sends plain text the supervisor wraps as a stream-json user line and
// writes to claude's held-open stdin.
func (c *AttachClient) Inject(ctx context.Context, msg string) error {
	resp, err := c.roundTrip(ctx, Request{Op: OpInject, Message: msg})
	if err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("agentsupervisor: inject: %s", resp.Error)
	}
	return nil
}

// ReadFrom reads up to max bytes of events.jsonl starting at the ABSOLUTE
// offset, returning the data, the next absolute offset, and eof (caught up).
// A request below the supervisor's baseOffset returns an error whose message is
// the ErrCodeOffsetTruncated code (callers compare via errors.Is(err,
// ErrOffsetTruncated)).
func (c *AttachClient) ReadFrom(ctx context.Context, offset int64, max int) (data []byte, next int64, eof bool, err error) {
	resp, err := c.roundTrip(ctx, Request{Op: OpRead, Offset: offset, MaxBytes: max})
	if err != nil {
		return nil, offset, false, err
	}
	if !resp.Ok {
		if resp.Error == ErrCodeOffsetTruncated {
			return nil, offset, false, ErrOffsetTruncated
		}
		return nil, offset, false, fmt.Errorf("agentsupervisor: read: %s", resp.Error)
	}
	return resp.Data, resp.NextOffset, resp.EOF, nil
}

// Ack truncates events consumed up to the ABSOLUTE offset and returns the
// supervisor's new baseOffset.
func (c *AttachClient) Ack(ctx context.Context, offset int64) (base int64, err error) {
	resp, err := c.roundTrip(ctx, Request{Op: OpAck, AckOffset: offset})
	if err != nil {
		return 0, err
	}
	if !resp.Ok {
		return 0, fmt.Errorf("agentsupervisor: ack: %s", resp.Error)
	}
	return resp.BaseOffset, nil
}
