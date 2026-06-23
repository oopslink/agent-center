package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

var t0 = time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)

func setup(t *testing.T) (context.Context, *ProjectRepo, *ProjectMemberRepo, *IssueRepo, *TaskRepo, *TaskSubscriberRepo, *IssueSubscriberRepo, *CodeRepoRefRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return context.Background(),
		NewProjectRepo(d), NewProjectMemberRepo(d), NewIssueRepo(d), NewTaskRepo(d),
		NewTaskSubscriberRepo(d), NewIssueSubscriberRepo(d), NewCodeRepoRefRepo(d)
}

func TestProjectRepo_RoundTrip(t *testing.T) {
	ctx, pr, _, _, _, _, _, _ := setup(t)
	p, _ := pm.NewProject(pm.NewProjectInput{ID: "P1", OrganizationID: "org-1", Name: "Acme", Description: "d", CreatedBy: "user:a", CreatedAt: t0})
	if err := pr.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	// duplicate
	if err := pr.Save(ctx, p); err != pm.ErrProjectExists {
		t.Fatalf("dup save want ErrProjectExists, got %v", err)
	}
	got, err := pr.FindByID(ctx, "P1")
	if err != nil || got.Name() != "Acme" || got.OrganizationID() != "org-1" {
		t.Fatalf("FindByID = %+v, %v", got, err)
	}
	if _, err := pr.FindByID(ctx, "nope"); err != pm.ErrProjectNotFound {
		t.Fatalf("want ErrProjectNotFound, got %v", err)
	}
	// update (rename + archive)
	_ = got.Rename("Acme Corp", t0)
	got.Archive(t0)
	if err := pr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := pr.FindByID(ctx, "P1")
	if re.Name() != "Acme Corp" || re.Status() != pm.ProjectArchived {
		t.Fatalf("update not persisted: %+v", re)
	}
	// list by org (second project in another org should not show)
	p2, _ := pm.NewProject(pm.NewProjectInput{ID: "P2", OrganizationID: "org-2", Name: "Other", CreatedBy: "user:a", CreatedAt: t0})
	_ = pr.Save(ctx, p2)
	list, _ := pr.ListByOrg(ctx, "org-1")
	if len(list) != 1 || list[0].ID() != "P1" {
		t.Fatalf("ListByOrg org-1 = %+v", list)
	}
	// update missing
	pmissing, _ := pm.NewProject(pm.NewProjectInput{ID: "PX", OrganizationID: "o", Name: "x", CreatedBy: "user:a", CreatedAt: t0})
	if err := pr.Update(ctx, pmissing); err != pm.ErrProjectNotFound {
		t.Fatalf("update missing want ErrProjectNotFound, got %v", err)
	}
}

// TestProjectRepo_ListAll covers the operator-global project list (v2.7 #131
// PR-3): ListAll returns ALL projects across ALL organizations (no org
// filter), stable-ordered (created_at, id), while ListByOrg stays org-scoped.
func TestProjectRepo_ListAll(t *testing.T) {
	ctx, pr, _, _, _, _, _, _ := setup(t)
	// empty DB → empty result, no error.
	if all, err := pr.ListAll(ctx); err != nil || len(all) != 0 {
		t.Fatalf("ListAll(empty) = %+v, %v", all, err)
	}
	// Two orgs, three projects, ascending created_at to pin stable order.
	p1, _ := pm.NewProject(pm.NewProjectInput{ID: "P1", OrganizationID: "org-1", Name: "Alpha", CreatedBy: "user:a", CreatedAt: t0})
	p2, _ := pm.NewProject(pm.NewProjectInput{ID: "P2", OrganizationID: "org-2", Name: "Beta", CreatedBy: "user:a", CreatedAt: t0.Add(time.Minute)})
	p3, _ := pm.NewProject(pm.NewProjectInput{ID: "P3", OrganizationID: "org-1", Name: "Gamma", CreatedBy: "user:a", CreatedAt: t0.Add(2 * time.Minute)})
	for _, p := range []*pm.Project{p1, p2, p3} {
		if err := pr.Save(ctx, p); err != nil {
			t.Fatalf("Save %s: %v", p.ID(), err)
		}
	}
	all, err := pr.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListAll len = %d, want 3 (cross-org global)", len(all))
	}
	for i, want := range []pm.ProjectID{"P1", "P2", "P3"} {
		if all[i].ID() != want {
			t.Errorf("ListAll[%d].ID = %s, want %s (stable created_at,id order)", i, all[i].ID(), want)
		}
	}
	// ListByOrg stays org-scoped: org-1 has only P1 + P3.
	if l, _ := pr.ListByOrg(ctx, "org-1"); len(l) != 2 {
		t.Fatalf("ListByOrg(org-1) = %d, want 2", len(l))
	}
}

func TestProjectMemberRepo_RoundTrip(t *testing.T) {
	ctx, _, mr, _, _, _, _, _ := setup(t)
	m, _ := pm.NewProjectMember(pm.NewProjectMemberInput{ID: "M1", ProjectID: "P1", IdentityID: "user:a", AddedBy: "user:owner", CreatedAt: t0})
	if err := mr.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	// dup (same project+identity) rejected by unique index
	dup, _ := pm.NewProjectMember(pm.NewProjectMemberInput{ID: "M2", ProjectID: "P1", IdentityID: "user:a", CreatedAt: t0})
	if err := mr.Save(ctx, dup); err != pm.ErrMemberExists {
		t.Fatalf("dup member want ErrMemberExists, got %v", err)
	}
	got, err := mr.FindByProjectAndIdentity(ctx, "P1", "user:a")
	if err != nil || got.ID() != "M1" {
		t.Fatalf("FindByProjectAndIdentity = %+v, %v", got, err)
	}
	if _, err := mr.FindByProjectAndIdentity(ctx, "P1", "user:none"); err != pm.ErrMemberNotFound {
		t.Fatalf("want ErrMemberNotFound, got %v", err)
	}
	list, _ := mr.ListByProject(ctx, "P1")
	if len(list) != 1 {
		t.Fatalf("ListByProject = %d", len(list))
	}
	if err := mr.Delete(ctx, "M1"); err != nil {
		t.Fatal(err)
	}
	if err := mr.Delete(ctx, "M1"); err != pm.ErrMemberNotFound {
		t.Fatalf("delete missing want ErrMemberNotFound, got %v", err)
	}
}

func TestIssueRepo_RoundTrip(t *testing.T) {
	ctx, _, _, ir, _, _, _, _ := setup(t)
	i, _ := pm.NewIssue(pm.NewIssueInput{ID: "I1", ProjectID: "P1", Title: "bug", CreatedBy: "user:a", CreatedAt: t0})
	if err := ir.Save(ctx, i); err != nil {
		t.Fatal(err)
	}
	_ = i.Transition(pm.IssueInProgress, t0)
	if err := ir.Update(ctx, i); err != nil {
		t.Fatal(err)
	}
	got, _ := ir.FindByID(ctx, "I1")
	if got.Status() != pm.IssueInProgress {
		t.Fatalf("issue status not persisted: %s", got.Status())
	}
	if _, err := ir.FindByID(ctx, "nope"); err != pm.ErrIssueNotFound {
		t.Fatalf("want ErrIssueNotFound, got %v", err)
	}
	list, _ := ir.ListByProject(ctx, "P1")
	if len(list) != 1 {
		t.Fatalf("ListByProject = %d", len(list))
	}
}

func TestTaskRepo_RoundTripWithAllFields(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	tk, _ := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "do", DerivedFromIssue: "I1", CreatedBy: "user:a", CreatedAt: t0})
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	// drive through assignment + block + complete and persist each
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	_ = tk.Block("waiting", pm.BlockReasonObstacle, "agent:c", t0)
	if err := tr.Update(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, _ := tr.FindByID(ctx, "T1")
	// ADR-0046: Block is an annotation; status stays running, reason persisted.
	if got.Status() != pm.TaskRunning || got.Assignee() != "agent:c" || got.BlockedReason() != "waiting" || got.DerivedFromIssue() != "I1" {
		t.Fatalf("task round-trip lost fields: %+v", got)
	}
	_ = got.Unblock("", "agent:c", t0)
	_ = got.Complete("agent:c", t0)
	_ = tr.Update(ctx, got)
	re, _ := tr.FindByID(ctx, "T1")
	if re.Status() != pm.TaskCompleted || re.CompletedBy() != "agent:c" || re.BlockedReason() != "" {
		t.Fatalf("completed round-trip wrong: %+v", re)
	}
	// list by project + assignee
	if l, _ := tr.ListByProject(ctx, "P1"); len(l) != 1 {
		t.Fatalf("ListByProject = %d", len(l))
	}
	if l, _ := tr.ListByAssignee(ctx, "agent:c"); len(l) != 1 {
		t.Fatalf("ListByAssignee = %d", len(l))
	}
	if _, err := tr.FindByID(ctx, "nope"); err != pm.ErrTaskNotFound {
		t.Fatalf("want ErrTaskNotFound, got %v", err)
	}
}

func TestTaskRepo_ListByStatuses(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	open, _ := pm.NewTask(pm.NewTaskInput{ID: "T-open", ProjectID: "P1", Title: "o", CreatedBy: "user:a", CreatedAt: t0})
	if err := tr.Save(ctx, open); err != nil {
		t.Fatal(err)
	}
	done, _ := pm.NewTask(pm.NewTaskInput{ID: "T-done", ProjectID: "P1", Title: "d", CreatedBy: "user:a", CreatedAt: t0})
	if err := tr.Save(ctx, done); err != nil {
		t.Fatal(err)
	}
	_ = done.Assign("agent:c", t0)
	_ = done.Start(t0)
	_ = done.Complete("agent:c", t0)
	if err := tr.Update(ctx, done); err != nil {
		t.Fatal(err)
	}
	// single status
	if l, _ := tr.ListByStatuses(ctx, []pm.TaskStatus{pm.TaskCompleted}); len(l) != 1 || l[0].ID() != "T-done" {
		t.Fatalf("ListByStatuses(completed) = %+v", l)
	}
	// multi status (the non-terminal active set excludes the completed task)
	active := []pm.TaskStatus{pm.TaskOpen, pm.TaskRunning, pm.TaskReopened}
	if l, _ := tr.ListByStatuses(ctx, active); len(l) != 1 || l[0].ID() != "T-open" {
		t.Fatalf("ListByStatuses(active) = %+v", l)
	}
	// empty input → empty result (no all-rows scan)
	if l, _ := tr.ListByStatuses(ctx, nil); len(l) != 0 {
		t.Fatalf("ListByStatuses(nil) should be empty, got %+v", l)
	}
}

func TestTaskSubscriberRepo(t *testing.T) {
	ctx, _, _, _, _, ts, _, _ := setup(t)
	s, _ := pm.NewTaskSubscriber("T1", "user:a", "user:owner", t0)
	if err := ts.Add(ctx, s); err != nil {
		t.Fatal(err)
	}
	// idempotent re-add
	if err := ts.Add(ctx, s); err != nil {
		t.Fatalf("re-add should be no-op, got %v", err)
	}
	list, _ := ts.ListByTask(ctx, "T1")
	if len(list) != 1 || list[0].IdentityID() != "user:a" {
		t.Fatalf("ListByTask = %+v", list)
	}
	if err := ts.Remove(ctx, "T1", "user:a"); err != nil {
		t.Fatal(err)
	}
	if l, _ := ts.ListByTask(ctx, "T1"); len(l) != 0 {
		t.Fatalf("after remove = %d", len(l))
	}
}

func TestIssueSubscriberRepo(t *testing.T) {
	ctx, _, _, _, _, _, is, _ := setup(t)
	s, _ := pm.NewIssueSubscriber("I1", "user:a", "user:owner", t0)
	if err := is.Add(ctx, s); err != nil {
		t.Fatal(err)
	}
	list, _ := is.ListByIssue(ctx, "I1")
	if len(list) != 1 {
		t.Fatalf("ListByIssue = %d", len(list))
	}
	_ = is.Remove(ctx, "I1", "user:a")
	if l, _ := is.ListByIssue(ctx, "I1"); len(l) != 0 {
		t.Fatalf("after remove = %d", len(l))
	}
}

func TestCodeRepoRefRepo(t *testing.T) {
	ctx, _, _, _, _, _, _, cr := setup(t)
	c, _ := pm.NewCodeRepoRef(pm.NewCodeRepoRefInput{ID: "R1", ProjectID: "P1", URL: "https://x/y.git", Label: "main", AddedBy: "user:a", CreatedAt: t0})
	if err := cr.Save(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := cr.FindByID(ctx, "R1")
	if err != nil || got.URL() != "https://x/y.git" || got.Label() != "main" {
		t.Fatalf("FindByID = %+v, %v", got, err)
	}
	if l, _ := cr.ListByProject(ctx, "P1"); len(l) != 1 {
		t.Fatalf("ListByProject = %d", len(l))
	}
	if err := cr.Delete(ctx, "R1"); err != nil {
		t.Fatal(err)
	}
	if err := cr.Delete(ctx, "R1"); err != pm.ErrCodeRepoRefNotFound {
		t.Fatalf("delete missing want ErrCodeRepoRefNotFound, got %v", err)
	}
}

// TestTaskRepo_CountByStatus covers the v2.7 #107 Phase-2 stats repoint: a
// global grouped count across ALL projects, optional since filter, and — the
// §-1 gate — no silently dropped status class.
func TestTaskRepo_CountByStatus(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	save := func(id, project string, status pm.TaskStatus, created time.Time) {
		tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
			ID: pm.TaskID(id), ProjectID: pm.ProjectID(project), Title: id,
			Status: status, CreatedBy: "user:a", CreatedAt: created, UpdatedAt: created, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	late := t0.Add(48 * time.Hour)
	// Tasks span MULTIPLE projects + statuses — the global count must aggregate
	// across all projects and must not drop a status class.
	save("T1", "PA", pm.TaskRunning, t0)
	save("T2", "PB", pm.TaskRunning, late)
	save("T3", "PA", pm.TaskCompleted, late)
	save("T4", "PC", pm.TaskReopened, t0)

	got, err := tr.CountByStatus(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[pm.TaskStatus]int{pm.TaskRunning: 2, pm.TaskCompleted: 1, pm.TaskReopened: 1}
	if len(got) != len(want) {
		t.Fatalf("status classes mismatch: got %v want %v", got, want)
	}
	for st, n := range want {
		if got[st] != n {
			t.Fatalf("status %s: got %d want %d (full=%v)", st, got[st], n, got)
		}
	}

	sinceLate := late
	got2, err := tr.CountByStatus(ctx, &sinceLate)
	if err != nil {
		t.Fatal(err)
	}
	if got2[pm.TaskRunning] != 1 || got2[pm.TaskCompleted] != 1 {
		t.Fatalf("since-filtered counts wrong: got %v", got2)
	}
	if got2[pm.TaskReopened] != 0 {
		t.Fatalf("since must exclude the early reopened task: got %v", got2)
	}
}

// TestTaskRepo_CountActiveByAssignee pins the agent-load metric source (T342):
// per-assignee Running ("doing") + Open ("pending") counts across all projects,
// terminal + unassigned rows excluded.
func TestTaskRepo_CountActiveByAssignee(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	save := func(id, project string, status pm.TaskStatus, assignee string) {
		tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
			ID: pm.TaskID(id), ProjectID: pm.ProjectID(project), Title: id,
			Status: status, Assignee: pm.IdentityRef(assignee),
			CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	const a1 = "agent:agent-aaaa1111"
	const a2 = "agent:agent-bbbb2222"
	save("T1", "PA", pm.TaskRunning, a1)   // a1 doing
	save("T2", "PB", pm.TaskOpen, a1)      // a1 pending (other project)
	save("T3", "PA", pm.TaskOpen, a1)      // a1 pending
	save("T4", "PA", pm.TaskRunning, a2)   // a2 doing
	save("T5", "PA", pm.TaskCompleted, a1) // terminal — excluded
	save("T6", "PA", pm.TaskOpen, "")      // unassigned — excluded

	got, err := tr.CountActiveByAssignee(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[a1].Running != 1 || got[a1].Pending != 2 {
		t.Fatalf("a1: got %+v want {Running:1 Pending:2}", got[a1])
	}
	if got[a2].Running != 1 || got[a2].Pending != 0 {
		t.Fatalf("a2: got %+v want {Running:1 Pending:0}", got[a2])
	}
	if _, ok := got[""]; ok {
		t.Fatalf("unassigned must be excluded: %v", got)
	}
}

// TestIssueRepo_FindByStatuses_GlobalNonTerminal pins the additive global
// issue-by-status scan (v2.7 #107 #119 fleet issues-repoint): the fleet
// pending-issues segment's global-admin path needs all non-terminal issues
// {open,in_progress,reopened} across ALL projects, mirroring the retired
// discussion FindByStatus full scan. Terminal {resolved,closed,withdrawn}
// must be excluded.
func TestIssueRepo_FindByStatuses_GlobalNonTerminal(t *testing.T) {
	ctx, pr, _, ir, _, _, _, _ := setup(t)
	mkProj := func(id, org string) {
		p, err := pm.NewProject(pm.NewProjectInput{ID: pm.ProjectID(id), OrganizationID: org, Name: id, CreatedBy: "user:a", CreatedAt: t0})
		if err != nil {
			t.Fatal(err)
		}
		if err := pr.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	mkProj("P1", "org-1")
	mkProj("P2", "org-2")
	mkIssue := func(id, proj string, st pm.IssueStatus) {
		i, err := pm.RehydrateIssue(pm.RehydrateIssueInput{
			ID: pm.IssueID(id), ProjectID: pm.ProjectID(proj), Title: id, Status: st,
			CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ir.Save(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	mkIssue("I-open", "P1", pm.IssueOpen)
	mkIssue("I-inprog", "P2", pm.IssueInProgress) // cross-project/org + in_progress must be included
	mkIssue("I-reopened", "P1", pm.IssueReopened)
	mkIssue("I-resolved", "P1", pm.IssueResolved) // terminal — excluded
	mkIssue("I-closed", "P2", pm.IssueClosed)     // terminal — excluded

	got, err := ir.FindByStatuses(ctx, []pm.IssueStatus{pm.IssueOpen, pm.IssueInProgress, pm.IssueReopened}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 non-terminal across projects (open/in_progress/reopened), got %d", len(got))
	}
	for _, i := range got {
		if i.Status() == pm.IssueResolved || i.Status() == pm.IssueClosed || i.Status() == pm.IssueDiscarded {
			t.Fatalf("terminal issue leaked: %s status=%s", i.ID(), i.Status())
		}
	}
}
