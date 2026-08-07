package workerdaemon

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// fakeAdapter is a programmable agentadapter.Adapter for probe tests.
type fakeAdapter struct {
	name      string
	available bool
	version   string
	probeErr  error
	features  agentadapter.FeatureSet
}

func (f *fakeAdapter) Name() string { return f.name }
func (f *fakeAdapter) BuildCommand(_ agentadapter.SpawnRequest) (agentadapter.CmdSpec, error) {
	return agentadapter.CmdSpec{}, nil
}
func (f *fakeAdapter) ParseEvent(_ []byte) (agentadapter.AgentTraceEvent, error) {
	return agentadapter.AgentTraceEvent{}, nil
}
func (f *fakeAdapter) SupportsSession() bool { return f.features.SupportsSession }
func (f *fakeAdapter) Probe(_ context.Context) (bool, string, error) {
	return f.available, f.version, f.probeErr
}
func (f *fakeAdapter) SupportedFeatures() agentadapter.FeatureSet { return f.features }
func (f *fakeAdapter) BuildMCPConfigArg(_ string) (agentadapter.MCPSetup, error) {
	return agentadapter.MCPSetup{}, nil
}
func (f *fakeAdapter) BuildSkillMountSetup(_, _ string) (agentadapter.SkillMountSetup, error) {
	return agentadapter.SkillMountSetup{}, nil
}

func newRegistryWith(adapters ...agentadapter.Adapter) *agentadapter.Registry {
	r := &agentadapter.Registry{}
	r.Reset()
	for _, a := range adapters {
		r.Register(a)
	}
	return r
}

func TestProbeAllAdapters_AllDetected(t *testing.T) {
	reg := newRegistryWith(
		&fakeAdapter{name: "claude-code", available: true, version: "1.0.0",
			features: agentadapter.FeatureSet{SupportsMCP: true, SupportsSkills: true, SupportsSession: true}},
		&fakeAdapter{name: "codex", available: true, version: "0.9.0",
			features: agentadapter.FeatureSet{}},
	)
	caps := ProbeAllAdapters(context.Background(), reg)
	if len(caps) != 2 {
		t.Fatalf("expected 2 caps, got %d", len(caps))
	}
	// Sorted: claude-code, codex
	if caps[0].AgentCLI != "claude-code" || caps[1].AgentCLI != "codex" {
		t.Fatalf("not sorted: %v", caps)
	}
	if !caps[0].Detected || !caps[0].Enabled || !caps[0].SupportsMCP {
		t.Fatalf("claude-code: %+v", caps[0])
	}
	if caps[0].Version != "1.0.0" {
		t.Fatalf("version: %s", caps[0].Version)
	}
	if !caps[1].Detected || caps[1].SupportsMCP {
		t.Fatalf("codex: %+v", caps[1])
	}
}

func TestProbeAllAdapters_BinaryNotAvailable(t *testing.T) {
	reg := newRegistryWith(
		&fakeAdapter{name: "opencode", available: false},
	)
	caps := ProbeAllAdapters(context.Background(), reg)
	if len(caps) != 1 {
		t.Fatal()
	}
	if caps[0].Detected || caps[0].Enabled {
		t.Fatalf("expected not detected: %+v", caps[0])
	}
}

func TestProbeAllAdapters_ProbeError(t *testing.T) {
	reg := newRegistryWith(
		&fakeAdapter{name: "claude-code", probeErr: errors.New("subprocess died")},
	)
	caps := ProbeAllAdapters(context.Background(), reg)
	if len(caps) != 1 {
		t.Fatal()
	}
	if caps[0].Detected {
		t.Fatalf("probe error should result in Detected=false: %+v", caps[0])
	}
	// CLI name still recorded so center knows it was checked
	if caps[0].AgentCLI != "claude-code" {
		t.Fatalf("agent_cli: %s", caps[0].AgentCLI)
	}
}

func TestProbeAllAdapters_NilRegistry_UsesDefault(t *testing.T) {
	// nil registry should fall back to agentadapter.DefaultRegistry. The
	// exact contents depend on which adapter packages have been imported
	// (and thus init()'d) in the test binary. workerdaemon imports
	// claudecode but not codex/opencode by default, so we only assert
	// claude-code is present.
	caps := ProbeAllAdapters(context.Background(), nil)
	got := map[string]bool{}
	for _, c := range caps {
		got[c.AgentCLI] = true
	}
	if !got["claude-code"] {
		t.Errorf("expected claude-code in default-registry probe result; got %+v", caps)
	}
}

func TestProbeAllAdapters_EmptyRegistry(t *testing.T) {
	reg := newRegistryWith()
	caps := ProbeAllAdapters(context.Background(), reg)
	if len(caps) != 0 {
		t.Fatalf("expected empty: %v", caps)
	}
}
