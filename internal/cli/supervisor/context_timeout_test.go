package supervisor_test

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cli/supervisor"
	"github.com/oopslink/agent-center/internal/cognition"
)

func TestContextTimeout_PerScope(t *testing.T) {
	if got := supervisor.ContextTimeout(cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")); got != 180*time.Second {
		t.Errorf("task = %v", got)
	}
	if got := supervisor.ContextTimeout(cognition.MustNewInvocationScope(cognition.ScopeGlobal, "")); got != 600*time.Second {
		t.Errorf("global = %v", got)
	}
}
