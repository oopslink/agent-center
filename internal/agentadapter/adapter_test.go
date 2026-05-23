package agentadapter

import (
	"context"
	"errors"
	"testing"
)

type stubAdapter struct {
	name string
}

func (s *stubAdapter) Name() string { return s.name }
func (s *stubAdapter) BuildCommand(_ SpawnRequest) (CmdSpec, error) {
	return CmdSpec{Binary: s.name}, nil
}
func (s *stubAdapter) ParseEvent(_ []byte) (AgentTraceEvent, error) { return AgentTraceEvent{}, nil }
func (s *stubAdapter) SupportsSession() bool                         { return false }

// v2 ADR-0030 § 2 no-ops for stub.
func (s *stubAdapter) Probe(context.Context) (bool, string, error) { return false, "", nil }
func (s *stubAdapter) SupportedFeatures() FeatureSet                { return FeatureSet{} }
func (s *stubAdapter) BuildMCPConfigArg(string) (MCPSetup, error)   { return MCPSetup{}, nil }
func (s *stubAdapter) BuildSkillMountSetup(string, string) (SkillMountSetup, error) {
	return SkillMountSetup{}, nil
}

func TestRegistry_RegisterGetNames(t *testing.T) {
	r := &Registry{adapters: map[string]Adapter{}}
	r.Register(&stubAdapter{name: "a"})
	r.Register(&stubAdapter{name: "b"})
	a, ok := r.Get("a")
	if !ok || a.Name() != "a" {
		t.Fatalf("get a: %v / %v", ok, a)
	}
	names := r.Names()
	if len(names) != 2 || names[0] != "a" {
		t.Fatalf("names: %+v", names)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("missing should not be found")
	}
}

func TestRegistry_NilAndReset(t *testing.T) {
	r := &Registry{adapters: map[string]Adapter{}}
	r.Register(nil) // no panic
	r.Register(&stubAdapter{name: "x"})
	r.Reset()
	if len(r.Names()) != 0 {
		t.Fatal("expected reset")
	}
}

func TestDefaultRegistry_PackageLevelShortcuts(t *testing.T) {
	defer DefaultRegistry.Reset()
	DefaultRegistry.Reset()
	Register(&stubAdapter{name: "z"})
	a, ok := Get("z")
	if !ok {
		t.Fatal("missing z")
	}
	if a.Name() != "z" {
		t.Fatal("bad name")
	}
	if len(Names()) != 1 {
		t.Fatal("names len")
	}
}

func TestCmdSpecWithoutStdin(t *testing.T) {
	stripped := CmdSpecWithoutStdin(CmdSpec{Binary: "x"})
	if stripped.Stdin != nil {
		t.Fatal("expected nil stdin")
	}
}

func TestErrNotImplemented(t *testing.T) {
	if !errors.Is(ErrNotImplemented, ErrNotImplemented) {
		t.Fatal("self equality")
	}
}

func TestEventType_IsValid(t *testing.T) {
	known := []EventType{EventThinking, EventToolCall, EventToolResult,
		EventTokensReport, EventTurnEnd, EventError, EventUnknown}
	for _, k := range known {
		if !k.IsValid() {
			t.Fatalf("expected valid: %s", k)
		}
	}
	if EventType("bogus").IsValid() {
		t.Fatal("bogus should be invalid")
	}
}
