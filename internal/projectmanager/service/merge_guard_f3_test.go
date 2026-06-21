package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// fakeMergeChecker is a recording stub for the F3 MergeChecker port. It returns a
// canned (merged, err) and counts calls so a test can assert the guard short-circuited
// (e.g. skip/role/nil cases must NOT call it).
type fakeMergeChecker struct {
	merged    bool
	err       error
	calls     int
	lastURL   string
	lastBr    string
	lastBase  string
}

func (f *fakeMergeChecker) BranchMergedToOrigin(_ context.Context, repoURL, branch, base string) (bool, error) {
	f.calls++
	f.lastURL, f.lastBr, f.lastBase = repoURL, branch, base
	return f.merged, f.err
}

// guardFixture builds a Service whose merge checker the test controls, plus a
// helper to create a member-gated project + a running task with a given role and
// cycle metadata, and (optionally) a CodeRepoRef on the project.
type guardFixture struct {
	svc   *Service
	tasks *pmsql.TaskRepo
	ctx   context.Context
	clk   *clock.FakeClock
}

func newGuardFixture(t *testing.T, mc MergeChecker) *guardFixture {
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
	tasks := pmsql.NewTaskRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: gen, Clock: clk, MergeChecker: mc,
	})
	return &guardFixture{svc: svc, tasks: tasks, ctx: context.Background(), clk: clk}
}

// project creates a member-gated project owned by user:pd and returns its id.
func (f *guardFixture) project(t *testing.T) pm.ProjectID {
	t.Helper()
	pid, err := f.svc.CreateProject(f.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// addRepo attaches a CodeRepoRef url to the project (the F3 guard resolves the
// project's primary repo from these).
func (f *guardFixture) addRepo(t *testing.T, pid pm.ProjectID, url string) {
	t.Helper()
	ref, err := pm.NewCodeRepoRef(pm.NewCodeRepoRefInput{
		ID: "repo-" + string(pid), ProjectID: pid, URL: url, AddedBy: "user:pd", CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.codeRepoRefs.Save(f.ctx, ref); err != nil {
		t.Fatal(err)
	}
}

// runningTask creates an Integrate-role (unless overridden) task with cycle meta,
// assigns + starts it so it is `running` (the only state CompleteTask accepts).
func (f *guardFixture) runningTask(t *testing.T, pid pm.ProjectID, role pm.CycleNodeRole, branch, base string, skip bool) pm.TaskID {
	t.Helper()
	tid, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{
		ProjectID: pid, Title: "node", CreatedBy: "user:pd",
		Role: role, Branch: branch, Base: base, SkipMergeCheck: skip,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AssignTask(f.ctx, tid, "user:pd", "user:pd"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartTask(f.ctx, tid, "user:pd"); err != nil {
		t.Fatal(err)
	}
	return tid
}

func (f *guardFixture) statusOf(t *testing.T, tid pm.TaskID) pm.TaskStatus {
	t.Helper()
	tk, err := f.tasks.FindByID(f.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status()
}

// (a) Integrate node + checker says merged → CompleteTask succeeds.
func TestF3Guard_IntegrateMerged_Completes(t *testing.T) {
	mc := &fakeMergeChecker{merged: true}
	f := newGuardFixture(t, mc)
	pid := f.project(t)
	f.addRepo(t, pid, "https://example.com/repo.git")
	tid := f.runningTask(t, pid, pm.CycleRoleIntegrate, "T7", "dev/v2.13.0", false)

	if err := f.svc.CompleteTask(f.ctx, tid, "user:pd"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if got := f.statusOf(t, tid); got != pm.TaskCompleted {
		t.Fatalf("status = %s, want completed", got)
	}
	if mc.calls != 1 {
		t.Fatalf("checker calls = %d, want 1", mc.calls)
	}
	if mc.lastBr != "T7" || mc.lastBase != "dev/v2.13.0" || mc.lastURL != "https://example.com/repo.git" {
		t.Fatalf("checker args = url:%q br:%q base:%q, want repo / T7 / dev/v2.13.0", mc.lastURL, mc.lastBr, mc.lastBase)
	}
}

// (b) Integrate + not merged → CompleteTask returns ErrIntegrateBranchNotMerged
// and the task stays running.
func TestF3Guard_IntegrateNotMerged_Blocked(t *testing.T) {
	mc := &fakeMergeChecker{merged: false}
	f := newGuardFixture(t, mc)
	pid := f.project(t)
	f.addRepo(t, pid, "https://example.com/repo.git")
	tid := f.runningTask(t, pid, pm.CycleRoleIntegrate, "T7", "dev/v2.13.0", false)

	err := f.svc.CompleteTask(f.ctx, tid, "user:pd")
	if !errors.Is(err, ErrIntegrateBranchNotMerged) {
		t.Fatalf("err = %v, want ErrIntegrateBranchNotMerged", err)
	}
	if got := f.statusOf(t, tid); got != pm.TaskRunning {
		t.Fatalf("status = %s, want running (blocked complete must not transition)", got)
	}
}

// (c) skip_merge_check Integrate → completes WITHOUT calling the checker.
func TestF3Guard_SkipMergeCheck_CompletesNoCheck(t *testing.T) {
	mc := &fakeMergeChecker{merged: false} // would block if consulted
	f := newGuardFixture(t, mc)
	pid := f.project(t)
	f.addRepo(t, pid, "https://example.com/repo.git")
	tid := f.runningTask(t, pid, pm.CycleRoleIntegrate, "T7", "dev/v2.13.0", true)

	if err := f.svc.CompleteTask(f.ctx, tid, "user:pd"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if got := f.statusOf(t, tid); got != pm.TaskCompleted {
		t.Fatalf("status = %s, want completed", got)
	}
	if mc.calls != 0 {
		t.Fatalf("checker calls = %d, want 0 (skip_merge_check exempts)", mc.calls)
	}
}

// (d) Dev/Review node (role != integrate) → completes WITHOUT calling the checker.
func TestF3Guard_NonIntegrateRole_CompletesNoCheck(t *testing.T) {
	mc := &fakeMergeChecker{merged: false}
	f := newGuardFixture(t, mc)
	pid := f.project(t)
	f.addRepo(t, pid, "https://example.com/repo.git")
	tid := f.runningTask(t, pid, pm.CycleRoleDev, "T7", "dev/v2.13.0", false)

	if err := f.svc.CompleteTask(f.ctx, tid, "user:pd"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if got := f.statusOf(t, tid); got != pm.TaskCompleted {
		t.Fatalf("status = %s, want completed", got)
	}
	if mc.calls != 0 {
		t.Fatalf("checker calls = %d, want 0 (Dev node is not a merge-check node)", mc.calls)
	}
}

// (e) nil checker → guard disabled → an Integrate node completes (pre-F3 behavior).
func TestF3Guard_NilChecker_Disabled(t *testing.T) {
	f := newGuardFixture(t, nil)
	pid := f.project(t)
	f.addRepo(t, pid, "https://example.com/repo.git")
	tid := f.runningTask(t, pid, pm.CycleRoleIntegrate, "T7", "dev/v2.13.0", false)

	if err := f.svc.CompleteTask(f.ctx, tid, "user:pd"); err != nil {
		t.Fatalf("CompleteTask (nil checker should be disabled): %v", err)
	}
	if got := f.statusOf(t, tid); got != pm.TaskCompleted {
		t.Fatalf("status = %s, want completed", got)
	}
}

// (f) checker error → CompleteTask blocked with the verify-failed error (fail closed).
func TestF3Guard_CheckerError_FailsClosed(t *testing.T) {
	mc := &fakeMergeChecker{err: errors.New("git: fetch timeout")}
	f := newGuardFixture(t, mc)
	pid := f.project(t)
	f.addRepo(t, pid, "https://example.com/repo.git")
	tid := f.runningTask(t, pid, pm.CycleRoleIntegrate, "T7", "dev/v2.13.0", false)

	err := f.svc.CompleteTask(f.ctx, tid, "user:pd")
	if !errors.Is(err, ErrIntegrateMergeUnverifiable) {
		t.Fatalf("err = %v, want ErrIntegrateMergeUnverifiable", err)
	}
	if got := f.statusOf(t, tid); got != pm.TaskRunning {
		t.Fatalf("status = %s, want running (unverifiable must not transition)", got)
	}
}

// Integrate node but the project has NO code repo → fail closed (unverifiable),
// task stays running (the PD must add a CodeRepoRef or set skip_merge_check).
func TestF3Guard_NoRepoConfigured_FailsClosed(t *testing.T) {
	mc := &fakeMergeChecker{merged: true}
	f := newGuardFixture(t, mc)
	pid := f.project(t) // NO addRepo
	tid := f.runningTask(t, pid, pm.CycleRoleIntegrate, "T7", "dev/v2.13.0", false)

	err := f.svc.CompleteTask(f.ctx, tid, "user:pd")
	if !errors.Is(err, ErrIntegrateMergeUnverifiable) {
		t.Fatalf("err = %v, want ErrIntegrateMergeUnverifiable (no repo)", err)
	}
	if mc.calls != 0 {
		t.Fatalf("checker calls = %d, want 0 (no repo to check against)", mc.calls)
	}
	if got := f.statusOf(t, tid); got != pm.TaskRunning {
		t.Fatalf("status = %s, want running", got)
	}
}
