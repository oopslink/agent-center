package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStartCodex_WritesMCPConfigAndCodexHome pins the T972 supervisor-MCP wiring:
// Start(CLI=codex) generates the canonical mcp_config.runtime.json (agent-center host
// + per-agent creds), translates it into $CODEX_HOME/config.toml, and hands the codex
// starter the CODEX_HOME dir so codex loads the [mcp_servers.agent-center] table.
func TestStartCodex_WritesMCPConfigAndCodexHome(t *testing.T) {
	base := t.TempDir()
	var got CodexSpec
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		CodexBinary:   "/usr/local/bin/codex",
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			got = spec
			return &fakeSession{}, nil
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	if err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-x",
		Version: 1,
		CLI:     CLICodex,
		Model:   "gpt-5-codex",
	}); err != nil {
		t.Fatalf("Start(codex): %v", err)
	}

	home := filepath.Join(base, "agents", "agent-x")
	wantCodexHome := filepath.Join(home, "codex-home")
	if got.CodexHome != wantCodexHome {
		t.Fatalf("CodexSpec.CodexHome = %q, want %q", got.CodexHome, wantCodexHome)
	}
	// The codex binary + tasks dir are threaded through as well.
	if got.Binary != "/usr/local/bin/codex" {
		t.Errorf("CodexSpec.Binary = %q, want the configured codex binary", got.Binary)
	}

	cfgPath := filepath.Join(wantCodexHome, "config.toml")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	s := string(b)
	// The agent-center MCP server table is present so the codex supervisor can call
	// create_task/complete_task/post_message.
	for _, want := range []string{
		"[mcp_servers.agent-center]",
		`command = "/opt/agent-center-worker"`,
		`"worker"`, `"mcp-host"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config.toml missing %q; got:\n%s", want, s)
		}
	}
	// The per-agent worker token reaches the codex-launched MCP host (a supervisor
	// carries center creds — that is how it authenticates its tool calls).
	if !strings.Contains(s, "tok-secret") {
		t.Errorf("config.toml missing per-agent worker token; got:\n%s", s)
	}
}
