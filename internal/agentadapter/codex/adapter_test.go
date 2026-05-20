package codex

import (
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

func TestCodex_StubReturnsNotImplemented(t *testing.T) {
	a := New()
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

func TestCodex_NotInDefaultRegistry(t *testing.T) {
	if _, ok := agentadapter.Get(AdapterName); ok {
		t.Fatal("codex stub must not auto-register")
	}
}
