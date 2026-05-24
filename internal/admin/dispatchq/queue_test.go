package dispatchq

import (
	"context"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

func TestQueue_PushDrainDispatch(t *testing.T) {
	q := New()
	if got := q.DrainDispatches("w-1"); len(got) != 0 {
		t.Fatalf("empty drain returns nil-equiv slice, got %d", len(got))
	}
	q.PushDispatch(dispatch.DispatchEnvelope{WorkerID: "w-1"})
	q.PushDispatch(dispatch.DispatchEnvelope{WorkerID: "w-1"})
	q.PushDispatch(dispatch.DispatchEnvelope{WorkerID: "w-2"})
	if got := q.DrainDispatches("w-1"); len(got) != 2 {
		t.Fatalf("w-1 drain got %d want 2", len(got))
	}
	if got := q.DrainDispatches("w-1"); len(got) != 0 {
		t.Fatalf("post-drain w-1 should be empty, got %d", len(got))
	}
	if got := q.DrainDispatches("w-2"); len(got) != 1 {
		t.Fatalf("w-2 drain got %d want 1", len(got))
	}
}

func TestQueue_PushDrainKill_PerWorker(t *testing.T) {
	q := New()
	q.PushKill(KillRequest{WorkerID: "w-1", ExecutionID: "e-1"})
	q.PushKill(KillRequest{WorkerID: "w-2", ExecutionID: "e-2"})
	w1 := q.DrainKills("w-1")
	if len(w1) != 1 || w1[0].ExecutionID != "e-1" {
		t.Fatalf("w-1 drain unexpected: %+v", w1)
	}
	if got := q.DrainKills("w-1"); len(got) != 0 {
		t.Fatalf("post-drain w-1 should be empty")
	}
	w2 := q.DrainKills("w-2")
	if len(w2) != 1 || w2[0].ExecutionID != "e-2" {
		t.Fatalf("w-2 drain unexpected: %+v", w2)
	}
}

func TestKillSender_StoresUnderEmptyKey(t *testing.T) {
	q := New()
	s := KillSender{Q: q}
	if err := s.SendKill(context.Background(), "e-1", execution.KilledSupervisorRequest, "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.SendKill(context.Background(), "e-2", execution.KilledSupervisorRequest, "test"); err != nil {
		t.Fatal(err)
	}
	if got := q.AllKills(); len(got) != 2 {
		t.Fatalf("AllKills got %d want 2", len(got))
	}
	if got := q.AllKills(); len(got) != 0 {
		t.Fatalf("post-drain AllKills should be empty")
	}
}

func TestDispatchSender_PushesEnvelope(t *testing.T) {
	q := New()
	s := DispatchSender{Q: q}
	env := dispatch.DispatchEnvelope{
		WorkerID:    "w-x",
		ExecutionID: taskruntime.TaskExecutionID("exec-1"),
	}
	if err := s.Send(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	got := q.DrainDispatches("w-x")
	if len(got) != 1 || got[0].ExecutionID != "exec-1" {
		t.Fatalf("unexpected drain: %+v", got)
	}
}

func TestQueue_ConcurrentSafe(t *testing.T) {
	q := New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); q.PushDispatch(dispatch.DispatchEnvelope{WorkerID: "w"}) }()
		go func() { defer wg.Done(); q.DrainDispatches("w") }()
	}
	wg.Wait()
	// Final drain — total pushed - drained = pending.
	// Don't assert exact count (race-dependent), just no panic / no
	// data race (run with -race).
	_ = q.DrainDispatches("w")
}

func TestQueue_PendingCounts(t *testing.T) {
	q := New()
	q.PushDispatch(dispatch.DispatchEnvelope{WorkerID: "w-1"})
	q.PushKill(KillRequest{WorkerID: "w-1", ExecutionID: "e"})
	d, k := q.Pending("w-1")
	if d != 1 || k != 1 {
		t.Fatalf("pending counts got %d/%d want 1/1", d, k)
	}
	_ = q.DrainDispatches("w-1")
	d, k = q.Pending("w-1")
	if d != 0 || k != 1 {
		t.Fatalf("after dispatch drain got %d/%d want 0/1", d, k)
	}
}
