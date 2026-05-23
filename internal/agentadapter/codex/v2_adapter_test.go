package codex

import (
	"context"
	"testing"
)

func TestCodex_SupportedFeatures_ConservativeDefaults(t *testing.T) {
	got := New("").SupportedFeatures()
	if got.SupportsMCP || got.SupportsSkills || got.SupportsSession {
		t.Fatalf("codex stub should default all features to false: %+v", got)
	}
}

func TestCodex_BuildMCPConfigArg_NotSupported(t *testing.T) {
	if _, err := New("").BuildMCPConfigArg("/x"); err == nil {
		t.Fatal("expected not-supported error")
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
