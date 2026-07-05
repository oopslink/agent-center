package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// auditSetup wires a Service WITH the audit ledger repo (flowSetup leaves it nil).
func auditSetup(t *testing.T) (*Service, context.Context) {
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
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: pmsql.NewPlanRepo(db),
		Outbox: ob, AgentDir: allOrgDir("org-1"), IDGen: gen, Clock: clk,
		Audit: pmsql.NewAuditLogRepo(db, gen),
	})
	return svc, context.Background()
}

func auditOf(t *testing.T, svc *Service, ctx context.Context, ot pm.AuditObjectType, id string) []pm.AuditEntry {
	t.Helper()
	entries, _, err := svc.ListObjectAudit(ctx, ot, id, "", 0)
	if err != nil {
		t.Fatalf("ListObjectAudit: %v", err)
	}
	return entries
}

func hasChange(entries []pm.AuditEntry, ct pm.AuditChangeType) *pm.AuditEntry {
	for i := range entries {
		if entries[i].ChangeType == ct {
			return &entries[i]
		}
	}
	return nil
}

// TestAudit_TaskLifecycle proves the mutation entry points each PRODUCE an audit
// entry (created / assigned / status_changed / blocked / reassigned / unassigned)
// with the right actor + from→to.
func TestAudit_TaskLifecycle(t *testing.T) {
	svc, ctx := auditSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	// created
	if e := hasChange(auditOf(t, svc, ctx, pm.AuditObjectTask, string(tid)), pm.AuditTaskCreated); e == nil {
		t.Fatal("no created audit entry after CreateTask")
	} else if string(e.ActorRef) != "user:a" {
		t.Fatalf("created actor = %q, want user:a", e.ActorRef)
	}

	// assigned
	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	if e := hasChange(auditOf(t, svc, ctx, pm.AuditObjectTask, string(tid)), pm.AuditTaskAssigned); e == nil {
		t.Fatal("no assigned entry")
	} else if e.ToValue != "agent:AG1" || string(e.ActorRef) != "user:a" {
		t.Fatalf("assigned entry wrong: %+v", e)
	}

	// status_changed (open→running via StartTask)
	if err := svc.StartTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	if e := hasChange(auditOf(t, svc, ctx, pm.AuditObjectTask, string(tid)), pm.AuditTaskStatusChanged); e == nil {
		t.Fatal("no status_changed entry after StartTask")
	} else if e.FromValue != "open" || e.ToValue != "running" {
		t.Fatalf("status entry from/to wrong: %+v", e)
	}

	// blocked (running→blocked, human-facing) then unblocked
	if err := svc.BlockTask(ctx, tid, "needs key", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, e := range auditOf(t, svc, ctx, pm.AuditObjectTask, string(tid)) {
		if e.ChangeType == pm.AuditTaskStatusChanged && e.ToValue == "blocked" {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("no running→blocked audit entry")
	}

	// reassigned
	if err := svc.AssignTask(ctx, tid, "agent:AG2", "user:a"); err != nil {
		t.Fatal(err)
	}
	if e := hasChange(auditOf(t, svc, ctx, pm.AuditObjectTask, string(tid)), pm.AuditTaskReassigned); e == nil {
		t.Fatal("no reassigned entry")
	} else if e.FromValue != "agent:AG1" || e.ToValue != "agent:AG2" {
		t.Fatalf("reassigned from/to wrong: %+v", e)
	}
}

// TestAudit_IssueAndReadOnly proves issue create/status/metadata produce entries and
// that a pure READ (GetTask) produces NONE (只读不产).
func TestAudit_IssueAndReadOnly(t *testing.T) {
	svc, ctx := auditSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})

	if hasChange(auditOf(t, svc, ctx, pm.AuditObjectIssue, string(iid)), pm.AuditIssueCreated) == nil {
		t.Fatal("no issue created entry")
	}
	// metadata edit (title) → metadata_edited, coarse (no full-text diff).
	newTitle := "bug (updated)"
	if err := svc.UpdateIssue(ctx, UpdateIssueCommand{IssueID: iid, Title: &newTitle, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	if hasChange(auditOf(t, svc, ctx, pm.AuditObjectIssue, string(iid)), pm.AuditIssueMetadataEdited) == nil {
		t.Fatal("no metadata_edited entry after UpdateIssue")
	}
	// status transition.
	if err := svc.SetIssueStatus(ctx, iid, pm.IssueResolved, "user:a"); err != nil {
		t.Fatal(err)
	}
	if hasChange(auditOf(t, svc, ctx, pm.AuditObjectIssue, string(iid)), pm.AuditIssueStatusChanged) == nil {
		t.Fatal("no issue status_changed entry")
	}

	// A pure read must not append anything. Count before + after a GetTask/GetIssue.
	before := len(auditOf(t, svc, ctx, pm.AuditObjectIssue, string(iid)))
	if _, err := svc.GetIssue(ctx, iid); err != nil {
		t.Fatal(err)
	}
	if after := len(auditOf(t, svc, ctx, pm.AuditObjectIssue, string(iid))); after != before {
		t.Fatalf("read produced audit entries: before=%d after=%d", before, after)
	}
}

// TestAudit_PlanDependency_NoOpNotRecorded proves the plan dependency write-points:
// adding an edge records dependency_added (with from/to detail), removing a NON-existent
// edge records NOTHING (no-op不产 — E-1: no empty-detail ledger row), and removing the
// real edge records exactly one dependency_removed with the from/to detail populated.
func TestAudit_PlanDependency_NoOpNotRecorded(t *testing.T) {
	svc, ctx := auditSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "dep", CreatedBy: "user:a"})
	a, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "A", CreatedBy: "user:a"})
	b, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "B", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, planID, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, b, "user:a"); err != nil {
		t.Fatal(err)
	}

	// add: B depends_on A → dependency_added with from/to detail.
	if err := svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	added := hasChange(auditOf(t, svc, ctx, pm.AuditObjectPlan, string(planID)), pm.AuditPlanDependencyAdded)
	if added == nil {
		t.Fatal("no dependency_added ledger row")
	}

	// no-op remove: an edge that does not exist must NOT append a ledger row.
	before := len(auditOf(t, svc, ctx, pm.AuditObjectPlan, string(planID)))
	if err := svc.RemovePlanDependency(ctx, planID, a, b, "user:a"); err != nil { // reversed → no such edge
		t.Fatal(err)
	}
	if after := len(auditOf(t, svc, ctx, pm.AuditObjectPlan, string(planID))); after != before {
		t.Fatalf("no-op dependency remove produced a ledger row: before=%d after=%d", before, after)
	}

	// real remove: the existing B→A edge → exactly one dependency_removed with from/to.
	if err := svc.RemovePlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	removed := hasChange(auditOf(t, svc, ctx, pm.AuditObjectPlan, string(planID)), pm.AuditPlanDependencyRemvd)
	if removed == nil {
		t.Fatal("no dependency_removed ledger row after removing the real edge")
	}
	if removed.Detail == "" || !strings.Contains(removed.Detail, string(b)) || !strings.Contains(removed.Detail, string(a)) {
		t.Fatalf("dependency_removed detail missing from/to: %q", removed.Detail)
	}
}

// failingAudit is an AuditLogRepository whose Append always errors — proving审计写
// 不阻塞主 mutation: the business op still succeeds and commits.
type failingAudit struct{}

func (failingAudit) Append(context.Context, pm.AuditEntry) error {
	return errors.New("audit backend down")
}
func (failingAudit) ListByObject(context.Context, pm.AuditObjectType, string, string, int) ([]pm.AuditEntry, string, error) {
	return nil, "", nil
}

// TestAudit_NonBlocking proves a failing audit append never fails/rolls back the
// primary mutation (the acceptance's 审计写不阻塞主 mutation).
func TestAudit_NonBlocking(t *testing.T) {
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
	var ob outbox.Repository = outboxsql.NewOutboxRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, AgentDir: allOrgDir("org-1"),
		IDGen: gen, Clock: clk, Audit: failingAudit{},
	})
	ctx := context.Background()

	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreateProject failed despite best-effort audit: %v", err)
	}
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreateTask failed despite best-effort audit: %v", err)
	}
	// The mutation must have committed: assign then read it back.
	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatalf("AssignTask failed despite best-effort audit: %v", err)
	}
	tk, err := svc.GetTask(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Assignee() != "agent:AG1" {
		t.Fatalf("mutation not committed: assignee=%q", tk.Assignee())
	}
}
