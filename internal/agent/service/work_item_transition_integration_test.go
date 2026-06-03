package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TestWorkItemTransition_SameTxAtomicNoMissNoDouble is the locus-B §-1 net:
// the transition emit is bound to tx success (rollback drops BOTH the row and
// the event), a reloaded retry emits exactly once (no miss / no double), and a
// no-op Update never double-emits.
func TestWorkItemTransition_SameTxAtomicNoMissNoDouble(t *testing.T) {
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ob := outboxsql.NewOutboxRepo(d)
	sink := NewOutboxWorkItemTransitionSink(ob, idgen.NewGenerator(clock.SystemClock{}))
	wr := agentsql.NewWorkItemRepoWithSink(d, sink)

	transitionEvents := func() int {
		evs, err := ob.FetchUnprocessed(ctx, 1000)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range evs {
			if e.EventType == EvtAgentWorkItemTransitioned {
				n++
			}
		}
		return n
	}
	newWI := func() *agent.AgentWorkItem {
		w, err := agent.NewWorkItem(agent.NewWorkItemInput{ID: "WI1", AgentID: "A1", TaskRef: "pm://tasks/T1", CreatedAt: tNow})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}

	// Case A — rollback drops BOTH the row and the transition event (atomic).
	boom := errors.New("boom")
	err = persistence.RunInTx(ctx, d, func(txCtx context.Context) error {
		if e := wr.Save(txCtx, newWI()); e != nil {
			return e
		}
		return boom // force rollback after the row write + emit
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if _, e := wr.FindByID(ctx, "WI1"); e != agent.ErrWorkItemNotFound {
		t.Fatalf("rolled-back work item must not persist, got %v", e)
	}
	if n := transitionEvents(); n != 0 {
		t.Fatalf("rolled-back tx must leave NO transition event (no orphan), got %d", n)
	}

	// Case B — retry on a FRESH AR (the reload-per-attempt pattern) emits once.
	if e := persistence.RunInTx(ctx, d, func(txCtx context.Context) error {
		return wr.Save(txCtx, newWI())
	}); e != nil {
		t.Fatal(e)
	}
	if n := transitionEvents(); n != 1 {
		t.Fatalf("successful save must emit exactly one creation event (no miss/no double), got %d", n)
	}

	// Case C — a no-op Update (no new transition) must NOT double-emit.
	got, _ := wr.FindByID(ctx, "WI1") // fresh AR, pending empty
	if e := wr.Update(ctx, got); e != nil {
		t.Fatal(e)
	}
	if n := transitionEvents(); n != 1 {
		t.Fatalf("re-Update with no transition must not emit again, got %d", n)
	}

	// Case D — a real transition emits exactly one more.
	got2, _ := wr.FindByID(ctx, "WI1")
	if e := got2.Activate(tNow); e != nil {
		t.Fatal(e)
	}
	if e := persistence.RunInTx(ctx, d, func(txCtx context.Context) error {
		return wr.Update(txCtx, got2)
	}); e != nil {
		t.Fatal(e)
	}
	if n := transitionEvents(); n != 2 {
		t.Fatalf("activate must add exactly one event (total 2), got %d", n)
	}
}
