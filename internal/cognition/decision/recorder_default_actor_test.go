package decision_test

import (
	"os"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/decision"
)

func TestDefaultActor_UsesEnv(t *testing.T) {
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INVA")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	a := decision.DefaultActor("alice")
	if !a.IsSupervisor() {
		t.Error("env set should yield supervisor")
	}
}

func TestDefaultActor_FallsBackToUser(t *testing.T) {
	_ = os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	a := decision.DefaultActor("alice")
	if a.IsSupervisor() {
		t.Error("no env should not be supervisor")
	}
	if a.ActorString() != "user:alice" {
		t.Errorf("actor %q", a.ActorString())
	}
}
