package cognition_test

import (
	"testing"

	"github.com/oopslink/agent-center/internal/cognition"
)

func TestIDStringers(t *testing.T) {
	if cognition.InvocationID("INV").String() != "INV" {
		t.Error("InvocationID.String()")
	}
	if cognition.DecisionID("D").String() != "D" {
		t.Error("DecisionID.String()")
	}
	if cognition.ScopeTask.String() != "task" {
		t.Error("ScopeKind.String()")
	}
}
