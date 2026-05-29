package sqlite

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newWIDB(t *testing.T) (*WorkItemRepo, *ActivityEventRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewWorkItemRepo(d), NewActivityEventRepo(d)
}

func TestWorkItemRepo_RoundTripAndActive(t *testing.T) {
	wr, _ := newWIDB(t)
	ctx := context.Background()
	w, _ := agent.NewWorkItem(agent.NewWorkItemInput{ID: "WI1", AgentID: "A1", TaskRef: "pm://tasks/T1", CreatedAt: t0})
	if err := wr.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := wr.Save(ctx, w); err != agent.ErrWorkItemExists {
		t.Fatalf("dup save want ErrWorkItemExists, got %v", err)
	}
	// no active work yet (queued)
	if ok, _ := wr.HasActiveWorkItem(ctx, "A1"); ok {
		t.Fatal("queued should not count as active")
	}
	got, _ := wr.FindByID(ctx, "WI1")
	_ = got.Activate(t0)
	if err := wr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if ok, _ := wr.HasActiveWorkItem(ctx, "A1"); !ok {
		t.Fatal("active work item should be detected (availability=busy input)")
	}
	re, _ := wr.FindByID(ctx, "WI1")
	if re.Status() != agent.WorkItemActive || re.Interactions() != 1 {
		t.Fatalf("round-trip lost status/interactions: %+v", re)
	}
	if _, err := wr.FindByID(ctx, "nope"); err != agent.ErrWorkItemNotFound {
		t.Fatalf("want ErrWorkItemNotFound, got %v", err)
	}

	// reassignment: supersede old + new item for the same task
	_ = re.Supersede(t0)
	_ = wr.Update(ctx, re)
	w2, _ := agent.NewWorkItem(agent.NewWorkItemInput{ID: "WI2", AgentID: "A2", TaskRef: "pm://tasks/T1", CreatedAt: t0})
	_ = wr.Save(ctx, w2)
	byTask, _ := wr.ListByTask(ctx, "pm://tasks/T1")
	if len(byTask) != 2 {
		t.Fatalf("ListByTask should show the superseded chain (2), got %d", len(byTask))
	}
	if l, _ := wr.ListByAgent(ctx, "A1"); len(l) != 1 {
		t.Fatalf("ListByAgent A1 = %d", len(l))
	}
}

func TestActivityEventRepo_AppendAndList(t *testing.T) {
	_, ar := newWIDB(t)
	ctx := context.Background()
	mk := func(id, wi string) *agent.AgentActivityEvent {
		e, err := agent.NewActivityEvent(agent.NewActivityEventInput{ID: id, AgentID: "A1", WorkItemRef: wi, EventType: "tool_use", OccurredAt: t0})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if err := ar.Append(ctx, mk("E1", "WI1")); err != nil {
		t.Fatal(err)
	}
	_ = ar.Append(ctx, mk("E2", "WI1"))
	_ = ar.Append(ctx, mk("E3", "WI2"))

	byAgent, _ := ar.ListByAgent(ctx, "A1", 10)
	if len(byAgent) != 3 {
		t.Fatalf("ListByAgent = %d, want 3", len(byAgent))
	}
	byWI, _ := ar.ListByWorkItem(ctx, "WI1")
	if len(byWI) != 2 {
		t.Fatalf("ListByWorkItem WI1 = %d, want 2", len(byWI))
	}
}
