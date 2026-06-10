package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// TestDeleteAgent_ForceSkipsGuardsAndSweeps — v2.8.1 force-delete (@oopslink): a
// non-force delete of an agent with active work is blocked by a guard; force skips
// the guards, sweeps the agent's non-terminal WorkItems (orphan-sweep: in-flight→
// failed, queued→canceled), and deletes the agent.
func TestDeleteAgent_ForceSkipsGuardsAndSweeps(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, "w-1", testOrg)
	id := f.createAgent(t, "w-1")
	f.seedQueuedWI(t, id, "wi-active")
	f.seedQueuedWI(t, id, "wi-queued")
	if err := f.svc.StartWork(ctx, id, "wi-active"); err != nil {
		t.Fatal(err)
	}
	// Non-force: a guard (not-stopped / active-work) must block the delete.
	if err := f.svc.DeleteAgent(ctx, id, false); err == nil {
		t.Fatal("non-force delete of an agent with active work must error (guard)")
	}
	// Force: skips guards + sweeps WorkItems + deletes the agent row.
	if err := f.svc.DeleteAgent(ctx, id, true); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	if _, err := f.svc.agents.FindByID(ctx, id); err == nil {
		t.Fatal("agent row must be gone after force delete")
	}
	// Orphan-sweep: no non-terminal WorkItem may reference the deleted agent.
	items, _ := f.workItems.ListByAgent(ctx, id)
	if len(items) == 0 {
		t.Fatal("expected the swept WorkItems to still exist (terminal), got none")
	}
	for _, w := range items {
		if !w.Status().IsTerminal() {
			t.Errorf("WorkItem %s left non-terminal (%s) after force delete = orphan", w.ID(), w.Status())
		}
	}
}

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
