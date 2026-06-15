package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// stubRunGate is a test double for the T130 TaskRunGate port: it returns a fixed
// verdict for every work item, so the test controls whether start_work sees the
// task as runnable.
type stubRunGate struct{ err error }

func (g stubRunGate) EnsureWorkItemRunnable(_ context.Context, _ string) error { return g.err }

// T130: start_work consults the run gate; a backlog task (gate rejects) is
// refused and the work item STAYS queued (not activated → its task never flips
// open→running). This is the open→running gate the T83 claim guard's sibling,
// closing the direct-assign→start_work path. A nil-verdict gate (or no gate)
// preserves the normal activation.
func TestStartWork_BacklogTaskRejected_T130(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")

	// Gate rejects (backlog) → start_work refused, item stays queued (active=0).
	f.svc.SetTaskRunGate(stubRunGate{err: agent.ErrWorkItemTaskNotRunnable})
	if err := f.svc.StartWork(ctx, id, "wi-1"); !errors.Is(err, agent.ErrWorkItemTaskNotRunnable) {
		t.Fatalf("start backlog = %v, want ErrWorkItemTaskNotRunnable", err)
	}
	if got := f.countActive(t, id); got != 0 {
		t.Fatalf("rejected start left active=%d, want 0 (item stays queued)", got)
	}

	// Gate now allows (task became a real-plan/pool member) → the SAME item starts.
	f.svc.SetTaskRunGate(stubRunGate{err: nil})
	if err := f.svc.StartWork(ctx, id, "wi-1"); err != nil {
		t.Fatalf("start after runnable = %v, want nil", err)
	}
	if got := f.countActive(t, id); got != 1 {
		t.Fatalf("after runnable start active=%d, want 1", got)
	}
}

// A real (driver) error from the gate is propagated verbatim (not swallowed,
// not mis-mapped to the not-runnable sentinel) and the item stays queued.
func TestStartWork_GateError_Propagated_T130(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")

	boom := errors.New("gate db error")
	f.svc.SetTaskRunGate(stubRunGate{err: boom})
	if err := f.svc.StartWork(ctx, id, "wi-1"); !errors.Is(err, boom) {
		t.Fatalf("start with gate error = %v, want the gate error", err)
	}
	if got := f.countActive(t, id); got != 0 {
		t.Fatalf("gate error left active=%d, want 0", got)
	}
}
