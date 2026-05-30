package agentsupervisor

// Unix-socket RPC server for the persistent agent-supervisor (slice D2-f s2).
// Serve exposes the running supervisor over a length-framed JSON socket so a
// (future, s3) daemon can re-attach: say hello (version + identity + offsets),
// inject input into claude's held-open stdin, read output events from an
// absolute offset, and ack-truncate consumed bytes. It is purely additive and
// NOT wired into the daemon here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// DefaultSocketName is the unix socket filename under HomeDir.
const DefaultSocketName = "supervisor.sock"

// Serve listens on the unix socket at sockPath and serves attach clients until
// ctx is cancelled, the supervisor's child exits, or a fatal listen error.
// Each connection runs an independent readFrame → dispatch → writeFrame loop;
// the per-op handlers are mutex-safe (offMu for read/ack, stdinMu for inject),
// so multiple concurrent client connections are allowed.
//
// LIFECYCLE. A STALE socket file from a prior incarnation is unlinked before
// listen (in s2 this is sufficient; the s1 home lockfile [s3] is what actually
// guarantees a single live supervisor, so unlink-stale here cannot stomp a live
// peer). On ctx cancel (the subcommand's signal) or child exit, the listener is
// closed and the socket file removed. A bad/oversize request never crashes the
// supervisor — the handler replies {ok:false,error} or the connection is
// dropped, and Serve keeps accepting.
func (s *Supervisor) Serve(ctx context.Context, sockPath string) error {
	// Unlink a stale socket file so listen does not fail with EADDRINUSE.
	if err := removeIfSocket(sockPath); err != nil {
		return fmt.Errorf("agentsupervisor: remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("agentsupervisor: listen %s: %w", sockPath, err)
	}
	// inject drives the agent's claude stdin directly — a local process that can
	// connect can hijack the agent. Lock the socket to owner-only (0600). Single
	// trust domain today, but pin it. (chmod after listen; the brief window before
	// chmod is owner-created under the agent home which is itself 0700.)
	if err := os.Chmod(sockPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("agentsupervisor: chmod socket 0600: %w", err)
	}

	// Close the listener (unblocking Accept) on ctx cancel OR child exit, then
	// remove the socket file.
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-s.done:
		case <-stop:
		}
		_ = ln.Close()
		_ = os.Remove(sockPath)
	}()
	defer close(stop)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener closed (shutdown) → clean return; otherwise surface.
			select {
			case <-ctx.Done():
				return nil
			case <-s.done:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("agentsupervisor: accept: %w", err)
		}
		go s.serveConn(ctx, conn)
	}
}

// serveConn runs the per-connection request loop. It tolerates bad frames /
// decode errors per request (replies ok=false) and exits the loop only on a
// transport error (EOF / oversize frame / write failure).
func (s *Supervisor) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		req, err := readFrame(conn)
		if err != nil {
			// Clean EOF or an oversize/garbage frame: drop the connection. The
			// supervisor stays up; the client may reconnect. We do not try to
			// resync the stream after an oversize frame (the length prefix is
			// untrusted), which is the safe anti-abuse choice.
			return
		}
		resp := s.dispatch(req)
		b, mErr := json.Marshal(resp)
		if mErr != nil {
			b, _ = json.Marshal(Response{Ok: false, Error: "encode response: " + mErr.Error()})
		}
		if err := writeFrame(conn, b); err != nil {
			return
		}
	}
}

// dispatch decodes one request frame and executes the op, returning the typed
// response. It NEVER panics on malformed input — a decode failure or unknown op
// yields {ok:false,error}.
func (s *Supervisor) dispatch(frame []byte) Response {
	var req Request
	if err := json.Unmarshal(frame, &req); err != nil {
		return Response{Ok: false, Error: "decode request: " + err.Error()}
	}
	switch req.Op {
	case OpHello:
		return s.handleHello()
	case OpInject:
		return s.handleInject(req)
	case OpRead:
		return s.handleRead(req)
	case OpAck:
		return s.handleAck(req)
	default:
		return Response{Ok: false, Error: ErrCodeUnknownOp + ": " + req.Op}
	}
}

func (s *Supervisor) handleHello() Response {
	s.offMu.Lock()
	base, cur := s.baseOffset, s.offset
	s.offMu.Unlock()
	return Response{
		Ok:              true,
		ProtocolVersion: ProtocolVersion,
		InstanceID:      s.instanceID,
		AgentID:         s.cfg.AgentID,
		ChildPID:        s.ChildPID(),
		StartedAt:       s.startedAt.Format(time.RFC3339Nano),
		BaseOffset:      base,
		CurrentOffset:   cur,
	}
}

// handleInject forwards the message to the held-open child stdin. DECISION: the
// daemon sends PLAIN TEXT and the supervisor wraps it via Inject (== the same
// encodeUserMessage stream-json encoder s1 uses), so the wire format is decided
// in ONE place (the supervisor) and the daemon never re-encodes the schema.
func (s *Supervisor) handleInject(req Request) Response {
	if err := s.Inject(req.Message); err != nil {
		return Response{Ok: false, Error: err.Error()}
	}
	return Response{Ok: true}
}

func (s *Supervisor) handleRead(req Request) Response {
	data, next, eof, err := s.ReadAt(req.Offset, req.MaxBytes)
	if err != nil {
		if errors.Is(err, ErrOffsetTruncated) {
			return Response{Ok: false, Error: ErrCodeOffsetTruncated}
		}
		return Response{Ok: false, Error: err.Error()}
	}
	return Response{Ok: true, Data: data, NextOffset: next, EOF: eof}
}

func (s *Supervisor) handleAck(req Request) Response {
	base, err := s.Ack(req.AckOffset)
	if err != nil {
		return Response{Ok: false, Error: err.Error()}
	}
	return Response{Ok: true, BaseOffset: base}
}

// removeIfSocket unlinks path if it exists and is a socket. A non-socket file at
// the path is left alone and surfaced as an error (we will not clobber an
// unexpected file). A missing path is a no-op.
func removeIfSocket(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("path %s exists and is not a socket", path)
	}
	return os.Remove(path)
}
