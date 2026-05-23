package opencode

import (
	"context"
	"testing"
)

func TestOpenCode_SupportedFeatures_ConservativeDefaults(t *testing.T) {
	got := New("").SupportedFeatures()
	if got.SupportsMCP || got.SupportsSkills || got.SupportsSession {
		t.Fatalf("opencode stub should default all features to false: %+v", got)
	}
}

func TestOpenCode_BuildMCPConfigArg_NotSupported(t *testing.T) {
	if _, err := New("").BuildMCPConfigArg("/x"); err == nil {
		t.Fatal()
	}
}

func TestOpenCode_BuildSkillMountSetup_NotSupported(t *testing.T) {
	if _, err := New("").BuildSkillMountSetup("/x", "/y"); err == nil {
		t.Fatal()
	}
}

func TestOpenCode_Probe_BinaryMissing(t *testing.T) {
	a := New("/no/such/opencode/__nope__")
	avail, ver, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if avail || ver != "" {
		t.Fatalf("avail=%v ver=%s", avail, ver)
	}
}

func TestOpenCode_Probe_BinaryExistsButVersionFails(t *testing.T) {
	a := New("/usr/bin/false")
	avail, _, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if avail {
		t.Fatal()
	}
}

func TestOpenCode_Probe_BinaryExistsVersionOK(t *testing.T) {
	a := New("/usr/bin/true")
	avail, _, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !avail {
		t.Fatal()
	}
}
