package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// ArchiveAgent soft-deletes a stopped agent: clears the worker binding, keeps the
// row (still GET-by-id resolvable), and is the sole user-facing delete (v2.8 #272).
func TestArchiveAgent_FromStopped_ClearsBindingAndStillResolves(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker) // stopped by default
	if err := f.svc.ArchiveAgent(context.Background(), id); err != nil {
		t.Fatalf("archive stopped: %v", err)
	}
	got, err := f.svc.GetAgent(context.Background(), id)
	if err != nil {
		t.Fatalf("archived agent must still GET-by-id (history): %v", err)
	}
	if got.Lifecycle() != agent.LifecycleArchived {
		t.Fatalf("lifecycle = %s want archived", got.Lifecycle())
	}
	if got.WorkerID() != "" {
		t.Fatalf("worker binding not cleared: %q", got.WorkerID())
	}
}

func TestArchiveAgent_Idempotent(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	if err := f.svc.ArchiveAgent(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	// Re-archive → no-op success (no error, no double version bump surfaced).
	if err := f.svc.ArchiveAgent(context.Background(), id); err != nil {
		t.Fatalf("re-archive must be idempotent no-op, got %v", err)
	}
}

func TestArchiveAgent_RunningRejected_MustStopFirst(t *testing.T) {
	f := newFixture(t)
	f.seedOnlineWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	if err := f.svc.StartAgent(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ArchiveAgent(context.Background(), id); err != agent.ErrAgentNotStoppedForArchive {
		t.Fatalf("archive running want ErrAgentNotStoppedForArchive, got %v", err)
	}
}

func TestArchiveAgent_StartRejectedAfterArchive(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	if err := f.svc.ArchiveAgent(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartAgent(context.Background(), id); err != agent.ErrAgentArchived {
		t.Fatalf("start archived want ErrAgentArchived, got %v", err)
	}
}
