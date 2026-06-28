package supervisormanager

// Deterministic regression tests for waitComeUp's IDENTITY GUARD (task-f04a6ae1).
//
// ROOT CAUSE these pin: the per-agent supervisor socket path is DETERMINISTIC
// from agent-id (SockPath), so it is SHARED across supervisor incarnations. On a
// rapid restart a PREVIOUS supervisor for the same agent-id can still be alive on
// that socket (its kill/reap is async and not awaited) and answer THIS come-up's
// Hello while its supervisor.instance lives under a DIFFERENT home. If come-up
// accepts it, the caller gets a ref whose HomeDir has no matching record and then
// reads a missing/foreign instance file — observed as the flaky
// "supervisor.instance: no such file or directory" in
// TestSupervisorSession_DetachSurvives + TestDetach_NotKill (both use the SAME
// agent-id "agent-detach" and one leaves a SURVIVING supervisor for the next).
//
// These tests drive waitComeUp directly (internal package) against an in-process
// fake supervisor parked on the shared socket — no real subprocess, fully
// deterministic — so the foreign supervisor ALWAYS answers and the guard's
// behavior is pinned regardless of timing.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

// TestWaitComeUp_RejectsForeignSupervisor_HomeMissingInstance: a live supervisor
// on the shared socket whose home has NO instance file must NOT be attached to —
// come-up must keep polling and time out, not hand back a ref to a foreign home.
func TestWaitComeUp_RejectsForeignSupervisor_HomeMissingInstance(t *testing.T) {
	agentID := "agent-comeup-foreign-missing"
	sockPath := agentsupervisor.SockPath(agentID)
	stop := serveFakeHelloInternal(t, sockPath, "FOREIGN-INSTANCE-ID")
	defer stop()

	home := t.TempDir() // fresh home: the foreign supervisor wrote ITS record elsewhere
	ref, err := waitComeUp(context.Background(), agentID, home, sockPath, 500*time.Millisecond)
	if err == nil {
		closeRef(ref)
		t.Fatalf("attached to a foreign supervisor (home has no instance file); want timeout. ref=%+v", ref)
	}
	if ref != nil {
		closeRef(ref)
		t.Fatalf("expected nil ref on rejected come-up, got %+v", ref)
	}
}

// TestWaitComeUp_RejectsForeignSupervisor_InstanceIDMismatch: the home HAS an
// instance file, but it records a DIFFERENT instance-id than the live socket
// self-reports (a stale record / different incarnation). The PID-REUSE-SAFE
// identity check must reject it.
func TestWaitComeUp_RejectsForeignSupervisor_InstanceIDMismatch(t *testing.T) {
	agentID := "agent-comeup-foreign-mismatch"
	sockPath := agentsupervisor.SockPath(agentID)
	stop := serveFakeHelloInternal(t, sockPath, "LIVE-SOCKET-INSTANCE-A")
	defer stop()

	home := t.TempDir()
	writeInstanceRecordInternal(t, home, agentID, "RECORDED-INSTANCE-B") // != live id

	ref, err := waitComeUp(context.Background(), agentID, home, sockPath, 500*time.Millisecond)
	if err == nil {
		closeRef(ref)
		t.Fatalf("attached despite instance-id mismatch (home=B, socket=A); want timeout. ref=%+v", ref)
	}
}

// TestWaitComeUp_AcceptsMatchingInstance: the happy path is unchanged — when the
// home's record matches the socket's self-reported instance-id (the fresh
// supervisor writes its record before it serves), come-up attaches promptly.
func TestWaitComeUp_AcceptsMatchingInstance(t *testing.T) {
	agentID := "agent-comeup-match"
	sockPath := agentsupervisor.SockPath(agentID)
	const id = "MATCHING-INSTANCE-ID"
	stop := serveFakeHelloInternal(t, sockPath, id)
	defer stop()

	home := t.TempDir()
	writeInstanceRecordInternal(t, home, agentID, id)

	ref, err := waitComeUp(context.Background(), agentID, home, sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("rejected the MATCHING supervisor: %v", err)
	}
	if ref == nil || ref.InstanceID != id {
		var got string
		if ref != nil {
			got = ref.InstanceID
		}
		closeRef(ref)
		t.Fatalf("ref InstanceID=%q want %q", got, id)
	}
	if ref.HomeDir != home {
		t.Fatalf("ref HomeDir=%q want %q", ref.HomeDir, home)
	}
	closeRef(ref)
}

func closeRef(ref *SupervisorRef) {
	if ref != nil && ref.Client != nil {
		_ = ref.Client.Close()
	}
}

// writeInstanceRecordInternal writes a minimal <home>/supervisor.instance with the
// given instance-id (the field the identity guard cross-checks).
func writeInstanceRecordInternal(t *testing.T, home, agentID, instanceID string) {
	t.Helper()
	rec := instanceRecord{
		InstanceID:    instanceID,
		AgentID:       agentID,
		SupervisorPID: os.Getpid(),
		ChildPID:      os.Getpid(),
		StartedAt:     "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal instance record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, agentsupervisor.InstanceFileName), b, 0o600); err != nil {
		t.Fatalf("write instance record: %v", err)
	}
}

// serveFakeHelloInternal parks a minimal supervisor socket server at sockPath that
// answers every length-framed request with a Hello carrying instanceID. It speaks
// the exact s2 wire (4-byte BE length + JSON payload) the AttachClient expects.
func serveFakeHelloInternal(t *testing.T, sockPath, instanceID string) func() {
	t.Helper()
	_ = os.Remove(sockPath) // clear any stale file from a prior run
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen fake supervisor: %v", err)
	}
	resp, _ := json.Marshal(map[string]any{
		"ok":               true,
		"protocol_version": agentsupervisor.ProtocolVersion,
		"instance_id":      instanceID,
		"agent_id":         "fake",
		"child_pid":        os.Getpid(),
	})
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					select {
					case <-done:
						return
					default:
					}
					if _, err := readFrameInternal(c); err != nil {
						return
					}
					if err := writeFrameInternal(c, resp); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return func() {
		close(done)
		_ = ln.Close()
		_ = os.Remove(sockPath)
	}
}

func readFrameInternal(c net.Conn) ([]byte, error) {
	var hdr [4]byte
	if _, err := readFullInternal(c, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := readFullInternal(c, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeFrameInternal(c net.Conn, b []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := c.Write(hdr[:]); err != nil {
		return err
	}
	_, err := c.Write(b)
	return err
}

func readFullInternal(c net.Conn, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		n, err := c.Read(buf[got:])
		got += n
		if err != nil {
			return got, err
		}
	}
	return got, nil
}
