package claudecode

import (
	"context"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

func TestClaudeCode_SupportedFeatures(t *testing.T) {
	a := New("")
	got := a.SupportedFeatures()
	if !got.SupportsMCP || !got.SupportsSkills || !got.SupportsSession {
		t.Fatalf("expected all features: %+v", got)
	}
}

func TestClaudeCode_BuildMCPConfigArg_Happy(t *testing.T) {
	a := New("")
	mcp, err := a.BuildMCPConfigArg("/tmp/runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(mcp.Args) != 2 || mcp.Args[0] != "--mcp-config" || mcp.Args[1] != "/tmp/runtime.json" {
		t.Fatalf("args: %v", mcp.Args)
	}
}

func TestClaudeCode_BuildMCPConfigArg_EmptyPath(t *testing.T) {
	a := New("")
	if _, err := a.BuildMCPConfigArg(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestClaudeCode_BuildSkillMountSetup_Happy(t *testing.T) {
	a := New("")
	setup, err := a.BuildSkillMountSetup("/home/x/.agent-center-worker/agents/01/skills/", "/tmp/exec/")
	if err != nil {
		t.Fatal(err)
	}
	if setup.Mode != agentadapter.SkillMountCLIArg {
		t.Fatalf("mode: %v", setup.Mode)
	}
	if len(setup.Args) != 2 || setup.Args[0] != "--skill-path" {
		t.Fatalf("args: %v", setup.Args)
	}
	if !strings.HasPrefix(setup.Args[1], "/home/x/.agent-center-worker/agents/01/skills") {
		t.Fatalf("path: %s", setup.Args[1])
	}
}

func TestClaudeCode_BuildSkillMountSetup_EmptyHomeDir(t *testing.T) {
	a := New("")
	if _, err := a.BuildSkillMountSetup("", "/tmp/exec/"); err == nil {
		t.Fatal("expected error for empty homeDirSkills")
	}
}

// Probe: invoke against an obviously-bogus binary to exercise the not-found
// path. The lookpath returns ENOENT → Probe returns (false, "", nil) per
// the contract.
func TestClaudeCode_Probe_BinaryMissing(t *testing.T) {
	a := New("/no/such/binary/anywhere/__nope__")
	avail, ver, err := a.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if avail {
		t.Fatal("expected not available")
	}
	if ver != "" {
		t.Fatalf("ver should be empty: %s", ver)
	}
}

// Registry exposes claude-code by default
func TestClaudeCode_RegisteredInDefault(t *testing.T) {
	a, ok := agentadapter.Get(AdapterName)
	if !ok {
		t.Fatal()
	}
	if a.Name() != AdapterName {
		t.Fatalf("name: %s", a.Name())
	}
}

func TestClaudeCode_Probe_BinaryExistsButVersionFails(t *testing.T) {
	a := New("/usr/bin/false")
	avail, _, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if avail {
		t.Fatal()
	}
}

func TestClaudeCode_Probe_BinaryExistsVersionOK(t *testing.T) {
	a := New("/usr/bin/true")
	avail, _, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !avail {
		t.Fatal()
	}
}
