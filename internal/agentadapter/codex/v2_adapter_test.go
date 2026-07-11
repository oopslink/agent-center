package codex

import (
	"context"
	"testing"
)

// T972: codex SupportsMCP is now TRUE (a codex supervisor reaches the MCP host via
// $CODEX_HOME/config.toml). Skills/Session stay false (skills→AGENTS.md separately;
// codex has no pre-assignable session id — resume is via captured thread_id).
func TestCodex_SupportedFeatures_MCPEnabled(t *testing.T) {
	got := New("").SupportedFeatures()
	if !got.SupportsMCP {
		t.Errorf("codex SupportsMCP should be true (T972): %+v", got)
	}
	if got.SupportsSkills || got.SupportsSession {
		t.Errorf("codex Skills/Session should stay false: %+v", got)
	}
}

// BuildMCPConfigArg is not on the codex path (codex reads config.toml, not --mcp-config)
// — it returns a clean zero MCPSetup (no error) now that SupportsMCP is true.
func TestCodex_BuildMCPConfigArg_NoOp(t *testing.T) {
	setup, err := New("").BuildMCPConfigArg("/x")
	if err != nil {
		t.Fatalf("codex BuildMCPConfigArg should no-op (config.toml path), got err: %v", err)
	}
	if len(setup.Args) != 0 {
		t.Errorf("codex must not inject --mcp-config args: %+v", setup)
	}
}

func TestCodex_BuildSkillMountSetup_NotSupported(t *testing.T) {
	if _, err := New("").BuildSkillMountSetup("/x", "/y"); err == nil {
		t.Fatal()
	}
}

func TestCodex_Probe_BinaryMissing(t *testing.T) {
	a := New("/no/such/codex/__nope__")
	avail, ver, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if avail || ver != "" {
		t.Fatalf("avail=%v ver=%s", avail, ver)
	}
}

// Probe binary exists but --version fails (exit code != 0) → treat as
// not-available per the safety contract.
func TestCodex_Probe_BinaryExistsButVersionFails(t *testing.T) {
	// /usr/bin/false exits 1 on any args, including --version.
	a := New("/usr/bin/false")
	avail, _, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if avail {
		t.Fatal("expected not available when --version fails")
	}
}

// Probe binary exists AND --version succeeds → reports available.
func TestCodex_Probe_BinaryExistsVersionOK(t *testing.T) {
	a := New("/usr/bin/true")
	avail, _, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !avail {
		t.Fatal("expected available")
	}
}
