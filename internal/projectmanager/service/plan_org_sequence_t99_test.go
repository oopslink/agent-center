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

// v2.10.1 [T99]: with an OrgSequence wired, CreatePlan allocates a per-org
// monotonic org_number (rendered P<n>) on its OWN 'plan' counter — INDEPENDENT
// of the task/issue T/I counters, and isolated per org. The hash id is
// unaffected.
func planSetupWithOrgSeq(t *testing.T) (*Service, *pmsql.PlanRepo, context.Context) {
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
	plans := pmsql.NewPlanRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans,
		Outbox: outboxsql.NewOutboxRepo(db), IDGen: idgen.NewGenerator(clk), Clock: clk,
		OrgSeq: pmsql.NewOrgSequenceRepo(db),
	})
	return svc, plans, context.Background()
}

func TestCreatePlan_T99_AllocatesOrgNumber(t *testing.T) {
	svc, plans, ctx := planSetupWithOrgSeq(t)

	p1, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "Alpha", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}

	// Two plans in org-1 → P1, P2.
	for want := 1; want <= 2; want++ {
		plid, err := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: p1, Name: "plan", CreatedBy: "user:a"})
		if err != nil {
			t.Fatal(err)
		}
		pl, ferr := plans.FindByID(ctx, plid)
		if ferr != nil {
			t.Fatal(ferr)
		}
		if pl.OrgNumber() != want {
			t.Fatalf("plan org_number = %d, want %d", pl.OrgNumber(), want)
		}
	}

	// A task in the SAME org → T1 (the plan counter is independent of the task counter).
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: p1, Title: "t", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if tk, _ := svc.GetTask(ctx, tid); tk.OrgNumber() != 1 {
		t.Fatalf("task org_number = %d, want 1 (independent of the plan counter)", tk.OrgNumber())
	}

	// A plan in a DIFFERENT org → P1 (per-org isolation).
	p2, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-2", Name: "Beta", CreatedBy: "user:b"})
	if err != nil {
		t.Fatal(err)
	}
	plid2, err := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: p2, Name: "p", CreatedBy: "user:b"})
	if err != nil {
		t.Fatal(err)
	}
	if pl, _ := plans.FindByID(ctx, plid2); pl.OrgNumber() != 1 {
		t.Fatalf("org-2 first plan org_number = %d, want 1 (per-org isolation)", pl.OrgNumber())
	}

	// org-1 plan counter continues at 3 (undisturbed by org-2 or the task).
	plid3, err := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: p1, Name: "p3", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if pl, _ := plans.FindByID(ctx, plid3); pl.OrgNumber() != 3 {
		t.Fatalf("org-1 next plan org_number = %d, want 3", pl.OrgNumber())
	}
}
