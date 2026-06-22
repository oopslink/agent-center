package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// v2.14.0 F7 (issue I14): TestDeleteAgent_ForceSkipsGuardsAndSweeps was removed —
// AgentWorkItem retired, so DeleteAgent no longer has the in-flight WorkItem guard
// (non-force) or the force-delete orphan-sweep. A deleted agent's stuck tasks are
// recovered by the F3 execution-lease checker, not an inline WorkItem sweep.

// TestUnbindAgentsFromWorker — worker force-delete unbind: every bound agent's
// worker_id is cleared (worker-less, retained, NOT archived) and the count is
// returned.
func TestUnbindAgentsFromWorker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, "w-1", testOrg)
	a1 := f.createAgent(t, "w-1")
	a2 := f.createAgent(t, "w-1")
	n, err := f.svc.UnbindAgentsFromWorker(ctx, "w-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("unbound count = %d, want 2", n)
	}
	for _, id := range []agent.AgentID{a1, a2} {
		a, err := f.svc.agents.FindByID(ctx, id)
		if err != nil {
			t.Fatalf("agent %s must be RETAINED (worker-less, not deleted): %v", id, err)
		}
		if a.WorkerID() != "" {
			t.Errorf("agent %s still bound to %q after unbind", id, a.WorkerID())
		}
	}
}
