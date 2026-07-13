package workerdaemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime"
)

// leaseServer stands up a minimal admin server on a unix socket whose lease/heartbeat
// route returns the given JSON body, so RenewTaskLease's response parsing can be tested.
func leaseServer(t *testing.T, body string) *AdminClient {
	t.Helper()
	sock := shortSock(t, "lease.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/environment/agent/lease/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	deadline := time.Now().Add(2 * time.Second)
	for {
		c, derr := net.Dial("unix", sock)
		if derr == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket never ready: %v", derr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return NewAdminClient(sock, 2*time.Second)
}

// TestRenewTaskLease_RevokedReturnsSentinel (issue-88e32d98 P0 block-fuse): a
// revoked=true response is surfaced as agentruntime.ErrLeaseRevoked so the runtime's
// lease-renew sweep can circuit-break the in-flight executor (errors.Is match).
func TestRenewTaskLease_RevokedReturnsSentinel(t *testing.T) {
	client := leaseServer(t, `{"ok":true,"revoked":true,"reason":"blocked"}`)
	err := client.RenewTaskLease(context.Background(), "agent-1", "task-1", time.Unix(1700000000, 0))
	if err == nil || !errors.Is(err, agentruntime.ErrLeaseRevoked) {
		t.Fatalf("RenewTaskLease(revoked) = %v, want ErrLeaseRevoked", err)
	}
}

// A plain ok response (not revoked) renews cleanly — no sentinel.
func TestRenewTaskLease_OKNoRevoke(t *testing.T) {
	client := leaseServer(t, `{"ok":true}`)
	if err := client.RenewTaskLease(context.Background(), "agent-1", "task-1", time.Unix(1700000000, 0)); err != nil {
		t.Fatalf("RenewTaskLease(ok) = %v, want nil", err)
	}
}
