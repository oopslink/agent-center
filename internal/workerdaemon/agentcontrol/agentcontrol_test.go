package agentcontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// shortSockDir returns a SHORT temp dir (under /tmp) — a unix socket path must fit
// the OS sun_path limit (~104 on darwin), which t.TempDir()'s long path blows.
func shortSockDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "act")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func startServer(t *testing.T, h Handler) (sock string, stop func()) {
	t.Helper()
	sock = filepath.Join(shortSockDir(t), "a.sock")
	s, err := NewServer(sock, h, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = s.Serve() }()
	// Wait until the socket accepts a connection.
	c := NewClient(sock, time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.Deliver(context.Background(), Command{Type: "ping"}); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return sock, func() { _ = s.Close(context.Background()) }
}

func TestDeliver_SuccessCallsHandler(t *testing.T) {
	var mu sync.Mutex
	var got []Command
	h := HandlerFunc(func(_ context.Context, cmd Command) error {
		mu.Lock()
		got = append(got, cmd)
		mu.Unlock()
		return nil
	})
	sock, stop := startServer(t, h)
	defer stop()

	c := NewClient(sock, time.Second)
	err := c.Deliver(context.Background(), Command{Type: "work", AgentID: "a", Seq: 7, Payload: []byte(`{"task_id":"t1"}`)})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	// got includes the readiness "ping" from startServer + our "work".
	var work *Command
	for i := range got {
		if got[i].Type == "work" {
			work = &got[i]
		}
	}
	if work == nil || work.AgentID != "a" || work.Seq != 7 || string(work.Payload) != `{"task_id":"t1"}` {
		t.Errorf("handler got %+v, want the work command intact", got)
	}
}

func TestDeliver_HandlerErrorSurfacesAsUndelivered(t *testing.T) {
	h := HandlerFunc(func(_ context.Context, _ Command) error { return errors.New("runtime busy") })
	sock, stop := startServer(t, h)
	defer stop()

	c := NewClient(sock, time.Second)
	if err := c.Deliver(context.Background(), Command{Type: "work"}); err == nil {
		t.Error("a handler error must surface as a Deliver error (so the cursor is NOT advanced)")
	}
}

func TestDeliver_AgentDownIsError(t *testing.T) {
	// No server on this socket → dial fails → Deliver errors (agent down/restarting).
	sock := filepath.Join(shortSockDir(t), "absent.sock")
	c := NewClient(sock, 200*time.Millisecond)
	if err := c.Deliver(context.Background(), Command{Type: "work"}); err == nil {
		t.Error("delivering to a down agent must error (worker must retry, not drop the command)")
	}
}

func TestNewServer_Validation(t *testing.T) {
	if _, err := NewServer("", HandlerFunc(func(context.Context, Command) error { return nil }), nil); err == nil {
		t.Error("empty socket path must error")
	}
	if _, err := NewServer(filepath.Join(shortSockDir(t), "x.sock"), nil, nil); err == nil {
		t.Error("nil handler must error")
	}
}

func TestServer_RebindsOverStaleSocket(t *testing.T) {
	sock := filepath.Join(shortSockDir(t), "a.sock")
	h := HandlerFunc(func(context.Context, Command) error { return nil })
	s1, err := NewServer(sock, h, nil)
	if err != nil {
		t.Fatalf("NewServer 1: %v", err)
	}
	go func() { _ = s1.Serve() }()
	_ = s1.Close(context.Background())
	// A prior incarnation left the socket file; a fresh Server must rebind over it
	// (the crash-recovery case — a killed agent process never cleaned its socket).
	s2, err := NewServer(sock, h, nil)
	if err != nil {
		t.Fatalf("NewServer over stale socket: %v", err)
	}
	_ = s2.Close(context.Background())
}
