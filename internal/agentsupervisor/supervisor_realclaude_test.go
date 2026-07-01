package agentsupervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// TestRealClaude_SupervisorInjectTurns is the MISSING real-claude exercise of the
// supervisor inject path (GATE-1 ship-blocker debug): s1/s2 only tested it with a
// stand-in claude; gate-1's PONG was a DIRECT pipe (not Supervisor.Inject). This
// spawns REAL claude via the Supervisor exactly as production does (BuildStreamingArgv
// + Supervisor.Start holding stdin open + the stdout drain), injects one user message
// via Supervisor.Inject (the held-open-stdin write under test), and asserts claude
// produces a turn into events.jsonl.
//
// Env-gated (AC_REAL_CLAUDE=1) so it never runs in normal CI (needs real claude +
// keychain /login). NO --mcp-config here — format/held-open/MCP were already proven
// to turn via direct pipe; this isolates Supervisor.Inject (socket/daemon layers
// excluded).
func TestRealClaude_SupervisorInjectTurns(t *testing.T) {
	if os.Getenv("AC_REAL_CLAUDE") == "" {
		t.Skip("set AC_REAL_CLAUDE=1 to run the real-claude supervisor-inject test")
	}
	home := t.TempDir()
	// Unique per run: claude derives a deterministic UUID-v5 session-id from this
	// string, and refuses to boot if that session already exists in ~/.claude (HOME
	// is NOT relocated). A fixed id collides on re-runs ("Session ID … already in
	// use") → claude exits → events=0 → Inject hits a closed supervisor.
	agentID := fmt.Sprintf("realclaude-inject-test-%d", time.Now().UnixNano())
	argv, _, err := claudestream.BuildStreamingArgv(agentID, "", "" /*no mcp*/, 0, 0, "" /*gen0,no-fork*/, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("argv: %v", argv)

	sup, err := New(Config{
		AgentID:  agentID,
		HomeDir:  home,
		ChildCmd: argv,
		Logger:   func(m string) { t.Logf("[sup] %s", m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sup.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sup.Stop(true)

	// Let claude finish startup/init (emits its system init line first).
	time.Sleep(6 * time.Second)
	initBytes, _ := os.ReadFile(filepath.Join(home, "events.jsonl"))
	t.Logf("events.jsonl after startup: %d bytes", len(initBytes))

	if err := sup.Inject("Reply with exactly the token PONGSUP and nothing else."); err != nil {
		t.Fatalf("inject: %v", err)
	}

	// Poll up to 30s for claude's turn to land in events.jsonl.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(home, "events.jsonl"))
		if strings.Contains(string(data), "PONGSUP") {
			t.Logf("PONGSUP turned (events=%d bytes) — Supervisor.Inject delivers to real claude", len(data))
			return
		}
		time.Sleep(1 * time.Second)
	}
	final, _ := os.ReadFile(filepath.Join(home, "events.jsonl"))
	t.Fatalf("no turn on Supervisor.Inject after 30s — events.jsonl (%d bytes):\n%s", len(final), string(final))
}
