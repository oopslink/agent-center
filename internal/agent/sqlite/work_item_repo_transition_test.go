package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newWIRawDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

type fakeTransitionSink struct {
	calls [][]agent.WorkItemTransition
	sawTx []bool
	err   error
}

func (f *fakeTransitionSink) AppendTransitions(ctx context.Context, ts []agent.WorkItemTransition) error {
	cp := make([]agent.WorkItemTransition, len(ts))
	copy(cp, ts)
	f.calls = append(f.calls, cp)
	_, ok := persistence.TxFromCtx(ctx)
	f.sawTx = append(f.sawTx, ok)
	return f.err
}

func mkWI(t *testing.T, id, agentID string) *agent.AgentWorkItem {
	t.Helper()
	w, err := agent.NewWorkItem(agent.NewWorkItemInput{ID: id, AgentID: agent.AgentID(agentID), TaskRef: "pm://tasks/T1", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWorkItemRepo_SaveEmitsCreationTransition(t *testing.T) {
	d := newWIRawDB(t)
	sink := &fakeTransitionSink{}
	wr := NewWorkItemRepoWithSink(d, sink)
	if err := wr.Save(context.Background(), mkWI(t, "WI1", "A1")); err != nil {
		t.Fatal(err)
	}
	if len(sink.calls) != 1 || len(sink.calls[0]) != 1 {
		t.Fatalf("Save should drain 1 creation transition to sink, got calls=%v", sink.calls)
	}
	if got := sink.calls[0][0]; got.PrevStatus != "" || got.Status != agent.WorkItemQueued || got.Version != 1 {
		t.Fatalf("creation transition wrong: %+v", got)
	}
}

func TestWorkItemRepo_UpdateEmitsDrainedTransition(t *testing.T) {
	d := newWIRawDB(t)
	sink := &fakeTransitionSink{}
	wr := NewWorkItemRepoWithSink(d, sink)
	ctx := context.Background()
	w := mkWI(t, "WI1", "A1")
	if err := wr.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, _ := wr.FindByID(ctx, "WI1") // fresh AR, no pending
	if err := got.Activate(t0); err != nil {
		t.Fatal(err)
	}
	if err := wr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	// calls[0] = creation (from Save), calls[1] = activate (from Update)
	if len(sink.calls) != 2 {
		t.Fatalf("want 2 sink calls (save+update), got %d", len(sink.calls))
	}
	upd := sink.calls[1]
	if len(upd) != 1 || upd[0].PrevStatus != agent.WorkItemQueued || upd[0].Status != agent.WorkItemActive || upd[0].Version != 2 {
		t.Fatalf("update transition wrong: %+v", upd)
	}
}

func TestWorkItemRepo_SinkSeesCallerTx(t *testing.T) {
	d := newWIRawDB(t)
	sink := &fakeTransitionSink{}
	wr := NewWorkItemRepoWithSink(d, sink)
	err := persistence.RunInTx(context.Background(), d, func(txCtx context.Context) error {
		return wr.Save(txCtx, mkWI(t, "WI1", "A1"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.sawTx) != 1 || !sink.sawTx[0] {
		t.Fatalf("sink must be invoked within the caller's tx (same-tx atomicity), sawTx=%v", sink.sawTx)
	}
}

func TestWorkItemRepo_NilSinkSafe(t *testing.T) {
	d := newWIRawDB(t)
	wr := NewWorkItemRepo(d) // no sink
	ctx := context.Background()
	w := mkWI(t, "WI1", "A1")
	if err := wr.Save(ctx, w); err != nil {
		t.Fatalf("Save with nil sink must not fail/panic: %v", err)
	}
	got, _ := wr.FindByID(ctx, "WI1")
	_ = got.Activate(t0)
	if err := wr.Update(ctx, got); err != nil {
		t.Fatalf("Update with nil sink must not fail/panic: %v", err)
	}
}
