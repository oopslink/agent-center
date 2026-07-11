package agentruntime

import (
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
