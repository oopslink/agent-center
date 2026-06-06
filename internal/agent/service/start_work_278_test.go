package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// countActive counts an agent's active|waiting_input work items (the single-
// active invariant target).
func (f *fixture) countActive(t *testing.T, agentID agent.AgentID) int {
	t.Helper()
	items, err := f.workItems.ListByAgent(context.Background(), agentID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range items {
		if w.Status() == agent.WorkItemActive || w.Status() == agent.WorkItemWaitingInput {
			n++
		}
	}
	return n
}

func (f *fixture) seedQueuedWI(t *testing.T, agentID agent.AgentID, id string) {
	t.Helper()
	wi, err := agent.NewWorkItem(agent.NewWorkItemInput{
		ID: id, AgentID: agentID, TaskRef: "task:" + id, CreatedAt: tNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.workItems.Save(context.Background(), wi); err != nil {
		t.Fatal(err)
	}
}

// #278 D pull model: agent selects a queued work item via start_work; only one
// may be active at a time (queue-drain, not drop).
func TestStartWork_SingleActiveSequential(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")
	f.seedQueuedWI(t, id, "wi-2")

	if err := f.svc.StartWork(ctx, id, "wi-1"); err != nil {
		t.Fatalf("start wi-1: %v", err)
	}
	if got := f.countActive(t, id); got != 1 {
		t.Fatalf("after wi-1: active=%d want 1", got)
	}

	// second start_work is rejected (agent already busy) — wi-2 stays queued, not dropped
	if err := f.svc.StartWork(ctx, id, "wi-2"); !errors.Is(err, agent.ErrAgentHasActiveWork) {
		t.Fatalf("start wi-2 err=%v want ErrAgentHasActiveWork", err)
	}
	if got := f.countActive(t, id); got != 1 {
		t.Fatalf("after rejected wi-2: active=%d want 1", got)
	}

	// finish wi-1 → wi-2 may now drain
	if err := f.svc.MarkWorkItemState(ctx, id, "wi-1", WorkItemFeedbackDone, tNow); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartWork(ctx, id, "wi-2"); err != nil {
		t.Fatalf("start wi-2 after done: %v", err)
	}
	if got := f.countActive(t, id); got != 1 {
		t.Fatalf("after wi-2: active=%d want 1", got)
	}
}

// The race regression (criteria 1/3/4): N concurrent start_work on different
// queued items for the SAME agent → exactly ONE becomes active (pre-check + DB
// UNIQUE partial index close the race; losers stay queued).
func TestStartWork_ConcurrentOnlyOneWins(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	const n = 8
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("wi-%d", i)
		f.seedQueuedWI(t, id, ids[i])
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int, wiID string) {
			defer wg.Done()
			errs[idx] = f.svc.StartWork(ctx, id, wiID)
		}(i, ids[i])
	}
	wg.Wait()

	if got := f.countActive(t, id); got != 1 {
		t.Fatalf("after %d concurrent start_work: active=%d want exactly 1 (race not closed)", n, got)
	}
	// exactly one winner; every loser gets the CLEAN domain error (not a raw
	// driver UNIQUE error) — both the pre-check path and the race path (Update
	// hit the unique index) map to ErrAgentHasActiveWork (Dev2 #194 review).
	wins := 0
	for _, e := range errs {
		if e == nil {
			wins++
			continue
		}
		if !errors.Is(e, agent.ErrAgentHasActiveWork) {
			t.Fatalf("race-loser got non-clean error %v, want ErrAgentHasActiveWork", e)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent start_work winners=%d want exactly 1", wins)
	}
}

// PR1 ACTUAL activation path (Tester #194 finding): the controller push
// report-active (MarkWorkItemState active) must also map the single-active race
// to the clean ErrAgentHasActiveWork (→ 409, not a raw 500/UNIQUE) — this is the
// path in use until PR4's pull-loop.
func TestMarkWorkItemState_ReportActive_SingleActive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")
	f.seedQueuedWI(t, id, "wi-2")
	if err := f.svc.MarkWorkItemState(ctx, id, "wi-1", WorkItemFeedbackActive, tNow); err != nil {
		t.Fatalf("report active wi-1: %v", err)
	}
	// excess report-active for wi-2 → clean ErrAgentHasActiveWork (not raw UNIQUE)
	err := f.svc.MarkWorkItemState(ctx, id, "wi-2", WorkItemFeedbackActive, tNow)
	if !errors.Is(err, agent.ErrAgentHasActiveWork) {
		t.Fatalf("excess report-active err=%v, want clean ErrAgentHasActiveWork", err)
	}
	if got := f.countActive(t, id); got != 1 {
		t.Fatalf("active=%d want 1", got)
	}
}

func TestStartWork_OwnershipGuard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")
	if err := f.svc.StartWork(ctx, agent.AgentID("other-agent"), "wi-1"); !errors.Is(err, ErrWorkItemNotForAgent) {
		t.Fatalf("cross-agent start_work err=%v want ErrWorkItemNotForAgent", err)
	}
}

func TestFailWork_FreesActiveSlot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")
	f.seedQueuedWI(t, id, "wi-2")
	if err := f.svc.StartWork(ctx, id, "wi-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.FailWork(ctx, id, "wi-1"); err != nil {
		t.Fatalf("fail wi-1: %v", err)
	}
	if got := f.countActive(t, id); got != 0 {
		t.Fatalf("after fail: active=%d want 0", got)
	}
	// slot freed → next can start
	if err := f.svc.StartWork(ctx, id, "wi-2"); err != nil {
		t.Fatalf("start wi-2 after fail: %v", err)
	}
	if got := f.countActive(t, id); got != 1 {
		t.Fatalf("after wi-2: active=%d want 1", got)
	}
}
