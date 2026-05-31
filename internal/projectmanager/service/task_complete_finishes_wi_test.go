package service

import (
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
)

// v2.7 #111 ④ (Q1 WI-stale): when a Task is COMPLETED, the WorkItemProjector must
// finish the live WorkItem so it does not linger `active` forever (the #1
// projection would otherwise show a completed task's WI as stale-active = lying).
// active → done (work finished); waiting_input → canceled (waiting_input→done is
// illegal). Both leave 0 live items.

func TestCompleteTask_ActiveWorkItemBecomesDone(t *testing.T) {
	svc, wiRepo, relay, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	taskRef := "pm://tasks/" + string(tid)
	list := func() []*agentpkg.AgentWorkItem {
		items, err := wiRepo.ListByTask(ctx, taskRef)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	_ = svc.AssignTask(ctx, tid, "agent:AG1", "user:a")
	_ = svc.StartTask(ctx, tid, "user:a")
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	// Agent picks up the work → WI active.
	live := list()[0]
	if err := live.Activate(time.Unix(1_700_000_100, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if err := wiRepo.Update(ctx, live); err != nil {
		t.Fatal(err)
	}
	// Task completes (e.g. agent complete_task).
	if err := svc.CompleteTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if lc, sc := liveCount(list()), statusCount(list()); lc != 0 || sc[agentpkg.WorkItemDone] != 1 {
		t.Fatalf("after complete: want 0 live + 1 done (no stale-active), got live=%d %v", lc, sc)
	}
}

func TestCompleteTask_WaitingInputWorkItemBecomesCanceled(t *testing.T) {
	svc, wiRepo, relay, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	taskRef := "pm://tasks/" + string(tid)
	list := func() []*agentpkg.AgentWorkItem {
		items, err := wiRepo.ListByTask(ctx, taskRef)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	_ = svc.AssignTask(ctx, tid, "agent:AG1", "user:a")
	_ = svc.StartTask(ctx, tid, "user:a")
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	live := list()[0]
	_ = live.Activate(time.Unix(1_700_000_100, 0).UTC())
	if err := live.WaitInput(time.Unix(1_700_000_200, 0).UTC()); err != nil { // active → waiting_input
		t.Fatal(err)
	}
	if err := wiRepo.Update(ctx, live); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	// waiting_input → done is illegal, so it must be canceled (not stale, not error).
	if lc, sc := liveCount(list()), statusCount(list()); lc != 0 || sc[agentpkg.WorkItemCanceled] != 1 {
		t.Fatalf("after complete on waiting_input WI: want 0 live + 1 canceled, got live=%d %v", lc, sc)
	}
}
