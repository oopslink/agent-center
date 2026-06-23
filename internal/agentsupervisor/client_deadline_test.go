package agentsupervisor_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

// TestAttachClient_RoundTripHonorsContextDeadline is the issue-9bd86b8f gap ①
// regression: a supervisor that is alive-but-unresponsive (假死) must NOT be able to
// block a roundTrip caller forever. Before the fix, roundTrip ignored the per-call
// ctx, so the control-loop goroutine that calls Inject would park on the socket read
// indefinitely → OnTick self-heal never ran → "卡死不能自动恢复". With the ctx
// deadline plumbed onto the conn, the call must return (with an error) at ~deadline.
func TestAttachClient_RoundTripHonorsContextDeadline(t *testing.T) {
	// Short dir under TempDir — t.TempDir() embeds the (long) test name and would
	// overflow the macOS AF_UNIX sun_path 104-byte limit (see SockPath rationale).
	dir, err := os.MkdirTemp("", "g1")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// A "hung supervisor": accept the connection and drain whatever the client
	// writes, but NEVER send a response frame back.
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		for {
			if _, rerr := conn.Read(buf); rerr != nil {
				return
			}
		}
	}()

	cli, err := agentsupervisor.Connect(context.Background(), sock)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- cli.Inject(ctx, "ping") }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a deadline error from a hung supervisor, got nil")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("Inject returned but took %v (> 1s) — deadline not respected promptly", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Inject blocked past the context deadline — roundTrip ignored ctx (gap ① not fixed)")
	}
}
