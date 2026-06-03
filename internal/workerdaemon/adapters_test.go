package workerdaemon

import (
	"context"
	"sort"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// v2.7 #177 (FINDING-D): the worker daemon must register ALL three agent-CLI
// adapters so ProbeAllAdapters discovers every installed CLI, not just
// claude-code. Each adapter package self-registers via init(); the daemon must
// import codex + opencode (claude-code arrives transitively via
// claude_session.go) or DefaultRegistry stays at 1 and auto-discovery silently
// reports only one CLI even when codex/opencode are installed + on PATH.
func TestWorkerDaemonRegistersAllAdapters(t *testing.T) {
	got := append([]string(nil), agentadapter.DefaultRegistry.Names()...)
	sort.Strings(got)
	want := []string{"claude-code", "codex", "opencode"}
	if len(got) != len(want) {
		t.Fatalf("registered adapters = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("registered adapters = %v, want %v", got, want)
		}
	}
}

// ProbeAllAdapters emits one entry per registered adapter (detected or not),
// so once all three register it yields three capability rows — the §5
// "caps=3" exit. Uses a canceled context so Probe fails fast without
// depending on the three CLIs actually being installed in the test env.
func TestProbeAllAdaptersCoversAllThree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caps := ProbeAllAdapters(ctx, nil)
	got := make([]string, 0, len(caps))
	for _, c := range caps {
		got = append(got, c.AgentCLI)
	}
	sort.Strings(got)
	want := []string{"claude-code", "codex", "opencode"}
	if len(got) != len(want) {
		t.Fatalf("ProbeAllAdapters covered %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ProbeAllAdapters covered %v, want %v", got, want)
		}
	}
}
