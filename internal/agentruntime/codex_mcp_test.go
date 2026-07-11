package agentruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The canonical runtime.json translates to codex config.toml [mcp_servers.<name>]
// tables carrying the SAME command/args + per-agent AC_MCP_* creds (a supervisor gets
// center creds — that's how it calls MCP tools). Deterministic output (sorted).
func TestCodexMCPConfigTOML(t *testing.T) {
	runtime := []byte(`{"mcpServers":{"agent_center":{` +
		`"command":"/opt/agent-center-worker",` +
		`"args":["mcp-host","--agent","agent-1"],` +
		`"env":{"AC_MCP_WORKER_TOKEN":"tok-secret","AC_MCP_AGENT_ID":"agent-1"}}}}`)
	got, err := codexMCPConfigTOML(runtime)
	if err != nil {
		t.Fatalf("codexMCPConfigTOML: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"[mcp_servers.agent_center]",
		`command = "/opt/agent-center-worker"`,
		`args = ["mcp-host", "--agent", "agent-1"]`,
		// The supervisor's per-agent center creds reach the codex-launched MCP host.
		`AC_MCP_AGENT_ID = "agent-1"`,
		`AC_MCP_WORKER_TOKEN = "tok-secret"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config.toml missing %q; got:\n%s", want, s)
		}
	}
	// env keys are deterministically sorted (AGENT_ID before WORKER_TOKEN).
	if strings.Index(s, "AC_MCP_AGENT_ID") > strings.Index(s, "AC_MCP_WORKER_TOKEN") {
		t.Errorf("env keys must be sorted deterministically; got:\n%s", s)
	}
}

// A no-MCP agent (empty mcpServers) yields a header-only doc with no server tables —
// not an error.
func TestCodexMCPConfigTOML_Empty(t *testing.T) {
	got, err := codexMCPConfigTOML([]byte(`{"mcpServers":{}}`))
	if err != nil {
		t.Fatalf("empty mcpServers should not error: %v", err)
	}
	if strings.Contains(string(got), "[mcp_servers.") {
		t.Errorf("empty config must have no server tables; got:\n%s", got)
	}
}

// WriteCodexMCPConfig writes the translated config.toml under a per-agent CODEX_HOME
// ("<home>/codex-home") and returns that dir — the value the daemon exports as
// $CODEX_HOME so codex loads the [mcp_servers.*] tables.
func TestWriteCodexMCPConfig(t *testing.T) {
	home := t.TempDir()
	runtime := []byte(`{"mcpServers":{"agent-center":{` +
		`"command":"/opt/agent-center-worker",` +
		`"args":["worker","mcp-host"],` +
		`"env":{"AC_MCP_AGENT_ID":"agent-1"}}}}`)
	codexHome, err := WriteCodexMCPConfig(home, runtime)
	if err != nil {
		t.Fatalf("WriteCodexMCPConfig: %v", err)
	}
	wantHome := filepath.Join(home, "codex-home")
	if codexHome != wantHome {
		t.Errorf("codexHome = %q, want %q", codexHome, wantHome)
	}
	cfgPath := filepath.Join(codexHome, "config.toml")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		"[mcp_servers.agent-center]",
		`command = "/opt/agent-center-worker"`,
		`AC_MCP_AGENT_ID = "agent-1"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config.toml missing %q; got:\n%s", want, s)
		}
	}
}

// An empty runtimeJSON yields a header-only config.toml (no servers), not an error —
// a codex agent with no MCP still gets a valid CODEX_HOME.
func TestWriteCodexMCPConfig_Empty(t *testing.T) {
	home := t.TempDir()
	codexHome, err := WriteCodexMCPConfig(home, nil)
	if err != nil {
		t.Fatalf("WriteCodexMCPConfig(nil): %v", err)
	}
	b, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if strings.Contains(string(b), "[mcp_servers.") {
		t.Errorf("empty config must have no server tables; got:\n%s", b)
	}
}

// A missing home is rejected (the caller must resolve the agent home first).
func TestWriteCodexMCPConfig_NoHome(t *testing.T) {
	if _, err := WriteCodexMCPConfig("", []byte(`{"mcpServers":{}}`)); err == nil {
		t.Fatal("WriteCodexMCPConfig(\"\") must error")
	}
}

// Special characters in command/env values are TOML-escaped (no broken config).
func TestCodexMCPConfigTOML_Escaping(t *testing.T) {
	runtime := []byte(`{"mcpServers":{"s":{"command":"a\"b\\c","args":[],"env":{"K":"line1\nline2"}}}}`)
	got, err := codexMCPConfigTOML(runtime)
	if err != nil {
		t.Fatalf("escaping: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, `command = "a\"b\\c"`) {
		t.Errorf("command not escaped; got:\n%s", s)
	}
	if !strings.Contains(s, `K = "line1\nline2"`) {
		t.Errorf("env value newline not escaped; got:\n%s", s)
	}
}
