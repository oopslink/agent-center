package service

// sole_running_task_test.go — issue-af03da2f / I54: SoleRunningTask backs the
// report_usage task-attribution fallback. The center fills an empty usage task_id
// from the agent's running task ONLY when there is EXACTLY ONE running-unblocked
// task — the unambiguous case. Zero (converse/idle) and >1 (concurrency, ambiguous)
// both return (nil, nil) so the usage event stays unattributed ("" non-task bucket).
// Reuses the capHarness/capFixture/mkAssigned fixtures from concurrency_cap_test.go.

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// Zero running → no attribution (a converse/idle agent stays in the non-task bucket).
func TestSoleRunningTask_NoneRunning(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	ag := pm.IdentityRef("agent:def")
	pid := capFixture(t, svc, ctx, ag)
	mkAssigned(t, svc, ctx, pid, "open-not-started", ag) // assigned but still open

	got, err := svc.SoleRunningTask(ctx, ag)
	if err != nil {
		t.Fatalf("SoleRunningTask: %v", err)
	}
	if got != nil {
		t.Fatalf("none running → want nil, got %s", got.ID())
	}
}

// Exactly one running → attributed to it (the production single-active case that
// revives the Top Cost Tasks panel). Verifies both the task id and its project.
func TestSoleRunningTask_ExactlyOne(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	ag := pm.IdentityRef("agent:def")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "running", ag)
	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}

	got, err := svc.SoleRunningTask(ctx, ag)
	if err != nil {
		t.Fatalf("SoleRunningTask: %v", err)
	}
	if got == nil || got.ID() != t1 {
		t.Fatalf("want sole running %s, got %v", t1, got)
	}
	if got.ProjectID() != pid {
		t.Fatalf("project = %s, want %s", got.ProjectID(), pid)
	}
}

// More than one running (a ≤max_concurrent agent) → ambiguous → abstain (nil): the
// center cannot know which of the N the tokens belong to.
func TestSoleRunningTask_MultipleRunningAmbiguous(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1", caps: map[string]int{"e1": 3}})
	ag := pm.IdentityRef("agent:e1")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "one", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "two", ag)
	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	if err := svc.StartTask(ctx, t2, ag); err != nil {
		t.Fatalf("start t2: %v", err)
	}

	got, err := svc.SoleRunningTask(ctx, ag)
	if err != nil {
		t.Fatalf("SoleRunningTask: %v", err)
	}
	if got != nil {
		t.Fatalf("two running → want nil (ambiguous), got %s", got.ID())
	}
}

// A blocked task is a legal pause that frees its run slot and is EXCLUDED — so an
// agent with one running + one blocked still resolves to the single running task
// (same RUN-SLOT predicate as the concurrency cap).
func TestSoleRunningTask_BlockedExcluded(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1", caps: map[string]int{"e1": 3}})
	ag := pm.IdentityRef("agent:e1")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "running", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "blocked", ag)
	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	if err := svc.StartTask(ctx, t2, ag); err != nil {
		t.Fatalf("start t2: %v", err)
	}
	if err := svc.BlockTask(ctx, t2, "waiting", pm.BlockReasonObstacle, ag); err != nil {
		t.Fatalf("block t2: %v", err)
	}

	got, err := svc.SoleRunningTask(ctx, ag)
	if err != nil {
		t.Fatalf("SoleRunningTask: %v", err)
	}
	if got == nil || got.ID() != t1 {
		t.Fatalf("blocked excluded → want sole running %s, got %v", t1, got)
	}
}

// An empty assignee short-circuits to (nil, nil) — never a wildcard scan.
func TestSoleRunningTask_EmptyAssignee(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	got, err := svc.SoleRunningTask(ctx, "")
	if err != nil {
		t.Fatalf("SoleRunningTask(\"\"): %v", err)
	}
	if got != nil {
		t.Fatalf("empty assignee → want nil, got %s", got.ID())
	}
}

// A repo/query error propagates (defensive path) instead of being swallowed — the
// caller (report_usage handler) treats an error as "no fallback" and still records
// the tokens. Forced via a closed DB.
func TestSoleRunningTask_RepoError(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: idgen.NewGenerator(clk), Clock: clk,
	})
	_ = db.Close() // force a query failure
	if _, err := svc.SoleRunningTask(context.Background(), pm.IdentityRef("agent:x")); err == nil {
		t.Fatal("want repo error on a closed DB, got nil")
	}
}
