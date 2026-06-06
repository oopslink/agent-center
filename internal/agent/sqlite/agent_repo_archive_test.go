package sqlite

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// Archive persists lifecycle=archived AND clears the worker binding (the one
// place worker_id changes), so the worker is freed to re-bind (v2.8 #272).
func TestAgentRepo_Archive_ClearsBinding(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	a := mkAgent(t, "A1", "W1")
	if err := r.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	// Bound to W1 before archive.
	if bound, _ := r.ListByWorker(ctx, "W1"); len(bound) != 1 {
		t.Fatalf("pre-archive bound to W1 = %d want 1", len(bound))
	}
	if err := a.Archive(t0); err != nil { // stopped → archived (clears workerID in-mem)
		t.Fatal(err)
	}
	if err := r.Archive(ctx, a); err != nil {
		t.Fatalf("repo archive: %v", err)
	}
	got, err := r.FindByID(ctx, "A1")
	if err != nil {
		t.Fatalf("archived agent must still resolve by id: %v", err)
	}
	if got.Lifecycle() != agent.LifecycleArchived {
		t.Fatalf("lifecycle = %s want archived", got.Lifecycle())
	}
	if got.WorkerID() != "" {
		t.Fatalf("worker binding not cleared: %q", got.WorkerID())
	}
	// Worker W1 is now free — no agents bound to it (re-bindable).
	if bound, _ := r.ListByWorker(ctx, "W1"); len(bound) != 0 {
		t.Fatalf("post-archive bound to W1 = %d want 0 (worker released)", len(bound))
	}
}

func TestAgentRepo_Archive_Absent(t *testing.T) {
	r := newDB(t)
	a := mkAgent(t, "ghost", "W1")
	_ = a.Archive(t0)
	if err := r.Archive(context.Background(), a); err != agent.ErrAgentNotFound {
		t.Fatalf("archive absent want ErrAgentNotFound, got %v", err)
	}
}
