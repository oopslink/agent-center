package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// idoneSetup builds a Service over an in-memory DB and exposes the OutboxRepo so a
// test can assert which events were emitted (the T464 issue-derived-done event has no
// projector here, so it stays in FetchUnprocessed). Mirrors flowSetup (assign_flow_test).
func idoneSetup(t *testing.T) (*Service, *outboxsql.OutboxRepo, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	ob := outboxsql.NewOutboxRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, AgentDir: allOrgDir("org-1"), IDGen: gen, Clock: clk,
	})
	return svc, ob, context.Background()
}

// derivedDoneEvents returns the EvtIssueDerivedTasksDone payloads currently in the outbox.
func derivedDoneEvents(t *testing.T, ob *outboxsql.OutboxRepo, ctx context.Context) []issueDerivedTasksDonePayload {
	t.Helper()
	evs, err := ob.FetchUnprocessed(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	var out []issueDerivedTasksDonePayload
	for _, e := range evs {
		if e.EventType != EvtIssueDerivedTasksDone {
			continue
		}
		var pl issueDerivedTasksDonePayload
		if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
			t.Fatalf("bad payload: %v", err)
		}
		out = append(out, pl)
	}
	return out
}

// Headline: only the LAST derived task's conclusion emits the wake — earlier
// conclusions are silent, and the emit carries the right owner + counts.
func TestIssueDerivedDone_EmitsOnlyWhenAllTerminal(t *testing.T) {
	svc, ob, ctx := idoneSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:owner"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:owner"})
	t1, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "a", DerivedFromIssue: iid, CreatedBy: "user:owner"})
	t2, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "b", DerivedFromIssue: iid, CreatedBy: "user:owner"})

	// Conclude t1 (t2 still open) → NOT all terminal → no emit.
	if err := svc.SetTaskStatus(ctx, t1, pm.TaskCompleted, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if got := derivedDoneEvents(t, ob, ctx); len(got) != 0 {
		t.Fatalf("must not emit while a derived task is still in flight, got %d", len(got))
	}

	// Conclude t2 → all terminal → emit exactly once with the right shape.
	if err := svc.SetTaskStatus(ctx, t2, pm.TaskCompleted, "user:owner"); err != nil {
		t.Fatal(err)
	}
	got := derivedDoneEvents(t, ob, ctx)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 derived-done emit, got %d", len(got))
	}
	e := got[0]
	if e.IssueID != string(iid) || e.OwnerIdentity != "user:owner" || e.OwnerRef != "pm://issues/"+string(iid) {
		t.Fatalf("emit locus/owner wrong: %+v", e)
	}
	if e.Total != 2 || e.Completed != 2 || e.Discarded != 0 {
		t.Fatalf("counts wrong: %+v", e)
	}
}

// Discarded tasks count toward "all concluded" and are reported separately.
func TestIssueDerivedDone_DiscardedCounts(t *testing.T) {
	svc, ob, ctx := idoneSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:owner"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:owner"})
	t1, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "a", DerivedFromIssue: iid, CreatedBy: "user:owner"})
	t2, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "b", DerivedFromIssue: iid, CreatedBy: "user:owner"})

	if err := svc.SetTaskStatus(ctx, t1, pm.TaskCompleted, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DiscardTask(ctx, t2, "user:owner"); err != nil {
		t.Fatal(err)
	}
	got := derivedDoneEvents(t, ob, ctx)
	if len(got) != 1 || got[0].Total != 2 || got[0].Completed != 1 || got[0].Discarded != 1 {
		t.Fatalf("want 1 emit with 1 completed/1 discarded, got %+v", got)
	}
}

// An already-concluded issue (resolved/closed/discarded) is a no-op.
func TestIssueDerivedDone_NoEmitWhenIssueConcluded(t *testing.T) {
	svc, ob, ctx := idoneSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:owner"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:owner"})
	t1, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "a", DerivedFromIssue: iid, CreatedBy: "user:owner"})

	// Move the issue to resolved BEFORE the last task concludes.
	if err := svc.SetIssueStatus(ctx, iid, pm.IssueResolved, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTaskStatus(ctx, t1, pm.TaskCompleted, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if got := derivedDoneEvents(t, ob, ctx); len(got) != 0 {
		t.Fatalf("an already-resolved issue must not be nudged, got %d", len(got))
	}
}

// A task with NO derived_from_issue link never triggers the nudge.
func TestIssueDerivedDone_NoEmitForUnlinkedTask(t *testing.T) {
	svc, ob, ctx := idoneSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:owner"})
	t1, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "standalone", CreatedBy: "user:owner"})

	if err := svc.SetTaskStatus(ctx, t1, pm.TaskCompleted, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if got := derivedDoneEvents(t, ob, ctx); len(got) != 0 {
		t.Fatalf("an unlinked task must not nudge any issue, got %d", len(got))
	}
}

// Re-arm: after the set is all-terminal (one emit), a NEW derived task is non-terminal
// again; concluding it fires a fresh emit. And re-setting an already-terminal task does
// NOT emit (terminal→terminal is not a fresh conclusion).
func TestIssueDerivedDone_ReArmsOnNewTask(t *testing.T) {
	svc, ob, ctx := idoneSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:owner"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:owner"})
	t1, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "a", DerivedFromIssue: iid, CreatedBy: "user:owner"})

	if err := svc.SetTaskStatus(ctx, t1, pm.TaskCompleted, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if got := derivedDoneEvents(t, ob, ctx); len(got) != 1 {
		t.Fatalf("first fill must emit once, got %d", len(got))
	}

	// Re-set the already-completed task to discarded (terminal→terminal) → no new emit.
	if err := svc.SetTaskStatus(ctx, t1, pm.TaskDiscarded, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if got := derivedDoneEvents(t, ob, ctx); len(got) != 1 {
		t.Fatalf("a terminal→terminal re-set must not re-emit, got %d", len(got))
	}

	// Add a NEW derived task (non-terminal) then conclude it → a fresh emit (re-armed).
	t2, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "c", DerivedFromIssue: iid, CreatedBy: "user:owner"})
	if err := svc.SetTaskStatus(ctx, t2, pm.TaskCompleted, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if got := derivedDoneEvents(t, ob, ctx); len(got) != 2 {
		t.Fatalf("a newly-added+concluded task must re-emit, got %d", len(got))
	}
}
