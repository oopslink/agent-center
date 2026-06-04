package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// v2.7.1 #245: with an OrgSequence wired, CreateTask/CreateIssue allocate a
// per-org, per-type monotonic org_number (rendered T<n>/I<n>). Independent
// counters per org and per type; the hash id is unaffected.
func setupWithOrgSeq(t *testing.T) (*Service, context.Context) {
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
	svc := New(Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Outbox:       outboxsql.NewOutboxRepo(db),
		IDGen:        idgen.NewGenerator(clk),
		Clock:        clk,
		OrgSeq:       pmsql.NewOrgSequenceRepo(db), // #245
	})
	return svc, context.Background()
}

func TestCreateTaskIssue_245_AllocatesOrgNumber(t *testing.T) {
	svc, ctx := setupWithOrgSeq(t)

	// org-1 with a project (creator becomes owner member → passes the write-gate).
	p1, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "Alpha", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}

	// Two tasks in org-1 → T1, T2.
	for want := 1; want <= 2; want++ {
		tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: p1, Title: "task", CreatedBy: "user:a"})
		if err != nil {
			t.Fatal(err)
		}
		tk, _ := svc.GetTask(ctx, tid)
		if tk.OrgNumber() != want {
			t.Fatalf("task org_number = %d, want %d", tk.OrgNumber(), want)
		}
	}
	// An issue in org-1 → I1 (issue counter independent of task counter).
	iid, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: p1, Title: "issue", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if iss, _ := svc.GetIssue(ctx, iid); iss.OrgNumber() != 1 {
		t.Fatalf("issue org_number = %d, want 1 (independent of task counter)", iss.OrgNumber())
	}

	// A task in a DIFFERENT org → T1 (per-org counter, independent of org-1).
	p2, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-2", Name: "Beta", CreatedBy: "user:b"})
	if err != nil {
		t.Fatal(err)
	}
	tid2, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: p2, Title: "t", CreatedBy: "user:b"})
	if err != nil {
		t.Fatal(err)
	}
	if tk, _ := svc.GetTask(ctx, tid2); tk.OrgNumber() != 1 {
		t.Fatalf("org-2 first task org_number = %d, want 1 (per-org isolation)", tk.OrgNumber())
	}

	// org-1 task counter continues at 3 (not disturbed by org-2 or the issue).
	tid3, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: p1, Title: "t3", CreatedBy: "user:a"})
	if tk, _ := svc.GetTask(ctx, tid3); tk.OrgNumber() != 3 {
		t.Fatalf("org-1 next task org_number = %d, want 3", tk.OrgNumber())
	}
}
