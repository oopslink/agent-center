package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
)

// v2.8.1 #278 D PR5: the reconciler releases an agent's active WorkItem once the
// agent has been inactive for staleAge (hung/wedged), freeing the single-active
// slot. NO-FALSE-KILL: a normal long task with activity inside the window is never
// released. The release loses cleanly to a concurrent complete (version-CAS).
func TestWorkItemReconciler_ReleasesStuckAfterStaleAge(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")
	if err := f.svc.StartWork(ctx, id, "wi-1"); err != nil { // active at tNow
		t.Fatal(err)
	}
	if f.countActive(t, id) != 1 {
		t.Fatal("wi-1 should be active")
	}
	rec := NewWorkItemReconciler(f.workItems, agentsql.NewActivityEventRepo(f.db), f.clk, 30*time.Minute, time.Minute, nil)

	// Within the 30-min window → NOT released.
	f.clk.Advance(20 * time.Minute)
	if n, err := rec.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("within window: released=%d err=%v, want 0", n, err)
	}
	if f.countActive(t, id) != 1 {
		t.Fatal("must stay active within window")
	}

	// Past 30-min staleness (no activity) → released (active→failed, slot freed).
	f.clk.Advance(11 * time.Minute) // tNow+31m
	if n, err := rec.Tick(ctx); err != nil || n != 1 {
		t.Fatalf("stale: released=%d err=%v, want 1", n, err)
	}
	if f.countActive(t, id) != 0 {
		t.Fatal("single-active slot must be freed after release")
	}
	items, err := f.workItems.ListByAgent(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status() != agent.WorkItemFailed {
		t.Fatalf("released wi status=%s, want failed", items[0].Status())
	}
}

func TestWorkItemReconciler_NoFalseKill_RecentActivity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	f.seedQueuedWI(t, id, "wi-1")
	if err := f.svc.StartWork(ctx, id, "wi-1"); err != nil { // active at tNow
		t.Fatal(err)
	}
	rec := NewWorkItemReconciler(f.workItems, agentsql.NewActivityEventRepo(f.db), f.clk, 30*time.Minute, time.Minute, nil)

	// Agent IS working: an activity event at tNow+29m (inside the window).
	f.clk.Advance(29 * time.Minute)
	ev, _ := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID: "ev-1", AgentID: id, EventType: "tool_use", Payload: `{}`, OccurredAt: f.clk.Now(),
	})
	if err := agentsql.NewActivityEventRepo(f.db).Append(ctx, ev); err != nil {
		t.Fatal(err)
	}
	// Now tNow+31m overall, but last activity was 2m ago → NOT stale → NOT killed.
	f.clk.Advance(2 * time.Minute)
	if n, err := rec.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("recent activity: released=%d err=%v, want 0 (no false kill)", n, err)
	}
	if f.countActive(t, id) != 1 {
		t.Fatal("a working agent's item must not be released")
	}
}
