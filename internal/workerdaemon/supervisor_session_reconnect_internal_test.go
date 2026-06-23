package workerdaemon

// Internal (white-box) test for issue-9bd86b8f gap ③: SupervisorSession.tryReconnect.
// It stands up a REAL in-process agentsupervisor on a SHORT socket path (no real
// claude — a tick stand-in child), drops the session's connection WITHOUT killing
// the supervisor, and proves tryReconnect re-dials the live supervisor and swaps in
// a working client (instead of the pump giving up → destructive reap+relaunch). The
// negative half proves a genuinely-gone supervisor yields false (so the truly-dead
// fallback still runs).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/supervisormanager"
)

const reconnectTickChild = `
cat >/dev/null &
i=0
while true; do
  i=$((i+1))
  printf '{"type":"system","subtype":"tick","n":%d}\n' "$i"
  sleep 0.02
done
`

func TestSupervisorSession_TryReconnect_RecoversLiveSupervisor(t *testing.T) {
	// Short home/sock — t.TempDir() embeds the long test name and would overflow the
	// macOS AF_UNIX sun_path 104-byte limit.
	home, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(home)
	sock := filepath.Join(home, "s.sock")
	const agentID = "agent-recon"

	sup, err := agentsupervisor.New(agentsupervisor.Config{
		AgentID:  agentID,
		HomeDir:  home,
		SockPath: sock,
		ChildCmd: []string{"sh", "-c", reconnectTickChild},
		Logger:   func(string) {},
	})
	if err != nil {
		t.Fatalf("New supervisor: %v", err)
	}
	if err := sup.Start(); err != nil {
		t.Fatalf("Start supervisor: %v", err)
	}
	t.Cleanup(func() { _ = sup.Stop(false) })

	serveCtx, serveCancel := context.WithCancel(context.Background())
	defer serveCancel()
	go func() { _ = sup.Serve(serveCtx, sock) }()

	cli := dialUntilUp(t, sock)

	// Build a session holding that client (white-box: newSession is package-internal).
	s := newSession(
		SupervisorSessionConfig{AgentID: agentID, HomeDir: home, Logger: func(string) {}},
		&supervisormanager.SupervisorRef{AgentID: agentID, HomeDir: home, SockPath: sock},
		cli,
	)

	// Simulate a DROPPED CONNECTION (supervisor stays alive): close the client conn.
	_ = cli.Close()
	if err := cli.Inject(context.Background(), "x"); err == nil {
		t.Fatal("precondition: a closed client must error on use")
	}

	// gap ③: the still-alive supervisor must be recovered by a re-dial.
	if !s.tryReconnect() {
		t.Fatal("tryReconnect returned false though the supervisor is alive")
	}
	s.mu.Lock()
	newCli := s.client
	s.mu.Unlock()
	if newCli == nil || newCli == cli {
		t.Fatal("tryReconnect did not swap in a fresh client")
	}
	if _, err := newCli.Hello(context.Background()); err != nil {
		t.Fatalf("reconnected client Hello failed: %v", err)
	}

	// Negative: a genuinely-gone supervisor must yield false (so the pump falls
	// through to the truly-dead reap+relaunch fallback rather than masking death).
	serveCancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if !s.tryReconnect() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tryReconnect kept succeeding after the supervisor's socket went away")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func dialUntilUp(t *testing.T, sock string) *agentsupervisor.AttachClient {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := agentsupervisor.Connect(context.Background(), sock)
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("could not connect to supervisor socket: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
