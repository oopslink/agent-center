package service

// concurrency_cap_test.go — v2.18.0 W4c (issue-b8687f2a): the application-layer
// ≤max_concurrent run-slot cap that replaced the dropped single-active UNIQUE index
// (migration 0072 → 0084). Covers the PD/tester1 acceptance points:
//   ① enabled agent runs ≤ EffectiveMaxConcurrentTasks (here 3);
//   ② default/unenabled agent does NOT regress — stays single-active (=1);
//   ③ the cap guards EVERY task→running transition, verified PER PATH
//      (start_task / unblock→running / reassign-of-running / owner SetStatus→running),
//      each with the "unenabled agent's 2nd→running is rejected (=1)" regression;
//   ③/① race-safety: a TRUE-CONCURRENT N+1 burst admits exactly N (the (N+1)th gets
//      ErrAgentHasActiveTask) on a WAL file DB, exercising RunInTx's real whole-tx
//      BUSY_SNAPSHOT replay — NOT a sequential stand-in;
//   ④ observability: CountRunningTasks reports the live run-slot count.

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// capAgentDir is an AgentDirectory whose run-slot cap is configurable per bare
// agent id (missing id ⇒ 1, the default single-active agent). Every agent resolves
// to one fixed org so the #5a cross-org assign guard passes.
type capAgentDir struct {
	org  string
	caps map[string]int
	errs map[string]bool // ids whose cap lookup fails (directory-error fail-safe test)
}

func (d capAgentDir) OrgOfAgent(_ context.Context, _ string) (string, error) { return d.org, nil }

func (d capAgentDir) ConcurrencyCapOfAgent(_ context.Context, id string) (int, error) {
	if d.errs[id] {
		return 0, errors.New("directory boom")
	}
	if c, ok := d.caps[id]; ok {
		return c, nil
	}
	return 1, nil // default agent: single-active (no regression)
}

// capHarness builds a Service over the given DSN (MemoryDSN for sequential tests,
// a WAL file DSN for the concurrent one) wired with the cap directory.
func capHarness(t *testing.T, dsn string, dir AgentDirectory) (*Service, context.Context) {
	t.Helper()
	db, err := persistence.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: idgen.NewGenerator(clk), Clock: clk, AgentDir: dir,
	})
	return svc, context.Background()
}

// capFixture creates a project (org-1) and registers the given agents as members.
func capFixture(t *testing.T, svc *Service, ctx context.Context, agents ...pm.IdentityRef) pm.ProjectID {
	t.Helper()
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: a, Actor: "user:a"}); err != nil {
			t.Fatalf("AddProjectMember %s: %v", a, err)
		}
	}
	return pid
}

// mkAssigned creates a task already assigned to `assignee` (open).
func mkAssigned(t *testing.T, svc *Service, ctx context.Context, pid pm.ProjectID, title string, assignee pm.IdentityRef) pm.TaskID {
	t.Helper()
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:a", Assignee: assignee})
	if err != nil {
		t.Fatalf("CreateTask %q: %v", title, err)
	}
	return tid
}

// ② default agent: a second start is rejected — single-active preserved.
func TestConcurrencyCap_DefaultAgentSingleActive(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	ag := pm.IdentityRef("agent:def")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "one", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "two", ag)

	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	if err := svc.StartTask(ctx, t2, ag); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("start t2 (default agent, slot full) = %v, want ErrAgentHasActiveTask", err)
	}
	// Freeing the slot lets the second run.
	if err := svc.SetTaskStatus(ctx, t1, pm.TaskCompleted, ag); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartTask(ctx, t2, ag); err != nil {
		t.Fatalf("start t2 after t1 done = %v, want nil", err)
	}
}

// ① enabled agent: runs up to EffectiveMaxConcurrentTasks (3) and rejects the 4th.
func TestConcurrencyCap_EnabledAgentUpToN(t *testing.T) {
	const capN = 3
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1", caps: map[string]int{"e1": capN}})
	ag := pm.IdentityRef("agent:e1")
	pid := capFixture(t, svc, ctx, ag)
	ids := make([]pm.TaskID, 0, capN+1)
	for i := 0; i < capN+1; i++ {
		ids = append(ids, mkAssigned(t, svc, ctx, pid, "t", ag))
	}
	for i := 0; i < capN; i++ {
		if err := svc.StartTask(ctx, ids[i], ag); err != nil {
			t.Fatalf("start #%d (under capN %d) = %v, want nil", i+1, capN, err)
		}
	}
	if err := svc.StartTask(ctx, ids[capN], ag); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("start #%d (over capN %d) = %v, want ErrAgentHasActiveTask", capN+1, capN, err)
	}
	if n := mustRunningCount(t, svc, ctx, ag); n != capN {
		t.Fatalf("running count = %d, want %d", n, capN)
	}
	// Complete one → a slot frees → the 4th runs.
	if err := svc.SetTaskStatus(ctx, ids[0], pm.TaskCompleted, ag); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartTask(ctx, ids[capN], ag); err != nil {
		t.Fatalf("start #%d after a slot freed = %v, want nil", capN+1, err)
	}
}

// ③ PATH unblock→running: a blocked task frees its slot; unblocking it RE-ENTERS the
// cap (status stays running, blocked_reason cleared). For a default agent (=1) that
// has filled its slot meanwhile, the unblock is rejected — proving the unblock path
// re-checks the cap and does not leak a second active task behind the dropped index.
func TestConcurrencyCap_UnblockReentersCap_DefaultAgentRejected(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	ag := pm.IdentityRef("agent:def")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "one", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "two", ag)

	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	// Block t1 — a legal pause that frees its run slot.
	if err := svc.BlockTask(ctx, t1, "waiting", pm.BlockReasonObstacle, ag); err != nil {
		t.Fatalf("block t1: %v", err)
	}
	// The freed slot lets t2 run.
	if err := svc.StartTask(ctx, t2, ag); err != nil {
		t.Fatalf("start t2 after t1 blocked = %v, want nil", err)
	}
	// Unblocking t1 would make TWO running+unblocked tasks for a =1 agent → rejected.
	err := svc.UnblockTask(ctx, UnblockTaskCommand{TaskID: t1, Comment: "go", Actor: "user:a"})
	if !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("unblock t1 (slot already taken by t2) = %v, want ErrAgentHasActiveTask", err)
	}
}

// ③ PATH reassign-of-running (the crash-adopt / hand-off回流 path): moving a task
// that is already running+unblocked onto a NEW assignee who is at its cap must be
// rejected — the run slot transfers and would exceed the new owner's cap. Default
// agents (=1) on both sides.
func TestConcurrencyCap_ReassignRunningOntoFullAgentRejected(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	agA, agB := pm.IdentityRef("agent:a"), pm.IdentityRef("agent:b")
	pid := capFixture(t, svc, ctx, agA, agB)
	tA := mkAssigned(t, svc, ctx, pid, "ta", agA)
	tB := mkAssigned(t, svc, ctx, pid, "tb", agB)
	if err := svc.StartTask(ctx, tA, agA); err != nil {
		t.Fatalf("start tA: %v", err)
	}
	if err := svc.StartTask(ctx, tB, agB); err != nil {
		t.Fatalf("start tB: %v", err)
	}
	// Reassign tB (running) onto A, which already runs tA → A would hold 2 → reject.
	if err := svc.AssignTask(ctx, tB, agA, "user:a"); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("reassign running tB onto full agent A = %v, want ErrAgentHasActiveTask", err)
	}
}

// ③ PATH owner SetStatus→running (the free status-override menu): an owner forcing a
// second task to running for a =1 agent is rejected — the override path is guarded
// too (the dropped index would have rejected it at the DB).
func TestConcurrencyCap_SetStatusOverrideRespectsCap(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	ag := pm.IdentityRef("agent:def")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "one", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "two", ag)
	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	if err := svc.SetTaskStatus(ctx, t2, pm.TaskRunning, ag); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("SetStatus(t2, running) for full =1 agent = %v, want ErrAgentHasActiveTask", err)
	}
}

// ③ PATH batch edit (BatchUpdateTask) status→running: the free batch editor can
// land a task in a run slot in one shot, so it is guarded too — a =1 agent's 2nd
// task patched to running is rejected (the dropped index would have rejected it at
// the DB). Backfills the 5th task→running entry point's per-path regression (the
// other four — start / unblock / reassign / SetStatus — are covered above), so a
// future refactor that drops the batch guard turns this test red.
func TestConcurrencyCap_BatchUpdateToRunningRespectsCap(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	ag := pm.IdentityRef("agent:def")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "one", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "two", ag)
	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	// Batch-patch t2 → running for the already-full =1 agent → it would hold 2 → reject.
	if err := svc.BatchUpdateTask(ctx, t2, BatchTaskPatch{Status: strptr("running")}, ag); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("BatchUpdate(t2, status=running) for full =1 agent = %v, want ErrAgentHasActiveTask", err)
	}
}

// ④ observability: CountRunningTasks reports live run slots; a blocked task is a
// legal pause and is NOT counted.
func TestConcurrencyCap_RunningCountObservability(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1", caps: map[string]int{"e1": 3}})
	ag := pm.IdentityRef("agent:e1")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "one", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "two", ag)
	if n := mustRunningCount(t, svc, ctx, ag); n != 0 {
		t.Fatalf("initial running = %d, want 0", n)
	}
	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartTask(ctx, t2, ag); err != nil {
		t.Fatal(err)
	}
	if n := mustRunningCount(t, svc, ctx, ag); n != 2 {
		t.Fatalf("running after 2 starts = %d, want 2", n)
	}
	// Blocking t1 frees its slot → count drops (blocked is a legal pause).
	if err := svc.BlockTask(ctx, t1, "pause", pm.BlockReasonObstacle, ag); err != nil {
		t.Fatal(err)
	}
	if n := mustRunningCount(t, svc, ctx, ag); n != 1 {
		t.Fatalf("running after blocking t1 = %d, want 1", n)
	}
}

// ①③ RACE-SAFE: a TRUE-CONCURRENT burst of N+1 StartTasks for one agent admits
// EXACTLY N (the (N+1)th gets ErrAgentHasActiveTask) — never a cap breakthrough.
// Runs on a WAL FILE DB (MemoryDSN pins MaxOpenConns=1 and cannot contend), so the
// goroutines genuinely race and exercise RunInTx's whole-tx BUSY_SNAPSHOT replay.
func TestConcurrencyCap_ConcurrentNPlus1_RaceSafe(t *testing.T) {
	cases := []struct {
		name string
		cap  int
	}{
		{"enabled_cap3", 3}, // ① enabled agent admits exactly 3 of 4
		{"default_cap1", 1}, // ② unenabled agent admits exactly 1 of 2 under real concurrency
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dsn := persistence.FileDSN(filepath.Join(t.TempDir(), "cap.db"))
			dir := capAgentDir{org: "org-1", caps: map[string]int{"race": tc.cap}}
			svc, ctx := capHarness(t, dsn, dir)
			ag := pm.IdentityRef("agent:race")
			pid := capFixture(t, svc, ctx, ag)

			n := tc.cap + 1
			ids := make([]pm.TaskID, 0, n)
			for i := 0; i < n; i++ {
				ids = append(ids, mkAssigned(t, svc, ctx, pid, "race", ag))
			}

			// Fire all N+1 starts at once; a barrier maximizes the real contention.
			start := make(chan struct{})
			errs := make([]error, n)
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func(i int) {
					defer wg.Done()
					<-start
					errs[i] = svc.StartTask(ctx, ids[i], ag)
				}(i)
			}
			close(start)
			wg.Wait()

			var ok, capped int
			for i, err := range errs {
				switch {
				case err == nil:
					ok++
				case errors.Is(err, pm.ErrAgentHasActiveTask):
					capped++
				default:
					t.Fatalf("start %d: unexpected error %v (want nil or ErrAgentHasActiveTask)", i, err)
				}
			}
			if ok != tc.cap || capped != 1 {
				t.Fatalf("concurrent N+1: admitted=%d capped=%d, want admitted=%d capped=1 (no cap breakthrough)", ok, capped, tc.cap)
			}
			if got := mustRunningCount(t, svc, ctx, ag); got != tc.cap {
				t.Fatalf("final running count = %d, want %d", got, tc.cap)
			}
		})
	}
}

// A non-agent (user:) assignee is capped at 1 too — the dropped index applied to
// every assignee. Exercises concurrencyCapOf's non-agent branch.
func TestConcurrencyCap_UserAssigneeSingleActive(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	user := pm.IdentityRef("user:u")
	pid := capFixture(t, svc, ctx) // creator user:a is a member; add user:u below
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: user, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	t1 := mkAssigned(t, svc, ctx, pid, "one", user)
	t2 := mkAssigned(t, svc, ctx, pid, "two", user)
	if err := svc.StartTask(ctx, t1, user); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	if err := svc.StartTask(ctx, t2, user); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("start t2 (user single-active) = %v, want ErrAgentHasActiveTask", err)
	}
}

// A directory lookup error fails SAFE: the cap falls back to 1 (single-active),
// never leaking an extra slot. Exercises concurrencyCapOf's error fallback.
func TestConcurrencyCap_DirectoryErrorFailsSafeToOne(t *testing.T) {
	dir := capAgentDir{org: "org-1", caps: map[string]int{"boom": 5}, errs: map[string]bool{"boom": true}}
	svc, ctx := capHarness(t, persistence.MemoryDSN(), dir)
	ag := pm.IdentityRef("agent:boom")
	pid := capFixture(t, svc, ctx, ag)
	t1 := mkAssigned(t, svc, ctx, pid, "one", ag)
	t2 := mkAssigned(t, svc, ctx, pid, "two", ag)
	if err := svc.StartTask(ctx, t1, ag); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	if err := svc.StartTask(ctx, t2, ag); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("start t2 (dir error → cap 1) = %v, want ErrAgentHasActiveTask", err)
	}
}

// An UNASSIGNED task forced to running carries no run slot to cap — the guard
// self-skips (assignee == ""). Exercises enforceConcurrencyCap's empty-assignee path.
func TestConcurrencyCap_UnassignedRunningNotCapped(t *testing.T) {
	svc, ctx := capHarness(t, persistence.MemoryDSN(), capAgentDir{org: "org-1"})
	pid := capFixture(t, svc, ctx)
	a := mkAssigned(t, svc, ctx, pid, "a", "user:a") // assign so we can then unassign
	if err := svc.UnassignTask(ctx, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	b := mkAssigned(t, svc, ctx, pid, "b", "user:a")
	if err := svc.UnassignTask(ctx, b, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Both unassigned → forcing either to running is uncapped (no owner to cap).
	if err := svc.SetTaskStatus(ctx, a, pm.TaskRunning, "user:a"); err != nil {
		t.Fatalf("set a running (unassigned, uncapped) = %v, want nil", err)
	}
	if err := svc.SetTaskStatus(ctx, b, pm.TaskRunning, "user:a"); err != nil {
		t.Fatalf("set b running (unassigned, uncapped) = %v, want nil", err)
	}
}

func mustRunningCount(t *testing.T, svc *Service, ctx context.Context, ag pm.IdentityRef) int {
	t.Helper()
	n, err := svc.CountRunningTasks(ctx, ag)
	if err != nil {
		t.Fatalf("CountRunningTasks: %v", err)
	}
	return n
}
