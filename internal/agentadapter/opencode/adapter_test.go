package opencode

import (
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

func TestOpenCode_StubReturnsNotImplemented(t *testing.T) {
	a := New("")
	if a.Name() != AdapterName {
		t.Fatalf("name: %s", a.Name())
	}
	if a.SupportsSession() {
		t.Fatal("expected false")
	}
	if _, err := a.BuildCommand(agentadapter.SpawnRequest{}); !errors.Is(err, agentadapter.ErrNotImplemented) {
		t.Fatalf("expected not_implemented: %v", err)
	}
	if _, err := a.ParseEvent(nil); !errors.Is(err, agentadapter.ErrNotImplemented) {
		t.Fatalf("expected not_implemented: %v", err)
	}
}

func TestOpenCode_RegisteredInDefaultRegistry(t *testing.T) {
	// v2 per ADR-0030 § 3: opencode self-registers on import (matches
	// claude-code / codex). v1 "must not auto-register" assertion flipped.
	a, ok := agentadapter.Get(AdapterName)
	if !ok {
		t.Fatal("opencode should be auto-registered (v2)")
	}
	if a.Name() != AdapterName {
		t.Fatalf("registered adapter name: %s", a.Name())
	}
}
