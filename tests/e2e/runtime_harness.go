// runtime_harness.go — in-process harness for the Phase 2 runtime e2e
// suite. Drives DispatchService / KillCoordinator / TimeoutScanner /
// ShimSupervisor / Reconcile end-to-end against a real SQLite DB (real
// file, real migrations) with a fake clock for time travel.
//
// Real OS process spawning is exercised via internal/shim with real
// OSSpawner + bash scripts in testdata/fake-agents (see
// runtimeFakeAgent). The harness is intentionally local to the tests/e2e
// package and does not export anything for outside callers.
package e2e

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/kill"
	"github.com/oopslink/agent-center/internal/taskruntime/reconcile"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/taskruntime/timeoutscan"
)

// runtimeRig bundles services + repos so individual tests stay terse.
type runtimeRig struct {
	t         *testing.T
	db        *sql.DB
	dbPath    string
	clk       *clock.FakeClock
	gen       idgen.Generator
	sink      *observability.EventSink
	eventRepo *obsqlite.EventRepo

	taskRepo task.Repository
	execRepo execution.Repository
	irRepo   inputrequest.Repository

	taskSvc     *trservice.TaskService
	irSvc       *trservice.InputRequestService
	execSvc     *trservice.ExecutionService
	dispatchSvc *dispatch.Service
	killCoord   *kill.Coordinator
	scanner     *timeoutscan.Scanner
	reconcSvc   *reconcile.Service
}

// newRuntimeRig wires the full Phase 2 runtime against a temp SQLite DB.
func newRuntimeRig(t *testing.T) *runtimeRig {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "runtime.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	// Seed project + worker.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id)
		 VALUES ('p-1','P','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','user:hayang')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO workers (id, status, capabilities_json, working_seconds, enrolled_at, created_at, updated_at)
		 VALUES ('W-1','online','[{"agent_cli":"claude-code","detected":true,"enabled":true}]',0,'2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	irRepo := trsqlite.NewInputRequestRepo(db)
	artifactRepo := trsqlite.NewArtifactRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)

	taskSvc := trservice.NewTaskService(db, taskRepo, convRepo, execRepo, msgRepo, sink, gen, clk)
	irSvc := trservice.NewInputRequestService(db, irRepo, execRepo, taskRepo, convRepo, msgRepo, sink, gen, clk, "")
	execSvc := trservice.NewExecutionService(db, execRepo, taskRepo, convRepo, msgRepo, sink, gen, clk)
	_ = artifactRepo // wired into App in production; not needed for these tests

	dispatchSvc := dispatch.NewService(db, taskRepo, execRepo, sink, dispatch.NoopSender{}, clk, gen, dispatch.DefaultConfig())
	killCoord := kill.NewCoordinator(db, execRepo, taskRepo, irRepo, sink, kill.NoopKillSender{}, clk)
	scanner := timeoutscan.NewScanner(db, execRepo, taskRepo, irRepo, sink, killCoord, clk, timeoutscan.DefaultConfig())
	reconcSvc := reconcile.NewService(execRepo)

	return &runtimeRig{
		t: t, db: db, dbPath: dbPath, clk: clk, gen: gen, sink: sink, eventRepo: er,
		taskRepo: taskRepo, execRepo: execRepo, irRepo: irRepo,
		taskSvc: taskSvc, irSvc: irSvc, execSvc: execSvc,
		dispatchSvc: dispatchSvc, killCoord: killCoord, scanner: scanner, reconcSvc: reconcSvc,
	}
}

// createAndDispatch creates a Task + Dispatches an Execution; returns IDs.
func (r *runtimeRig) createAndDispatch(t *testing.T) (taskruntime.TaskID, taskruntime.TaskExecutionID) {
	t.Helper()
	ctx := context.Background()
	taskRes, err := r.taskSvc.Create(ctx, trservice.TaskCreateInput{
		ProjectID:        "p-1",
		Title:            "e2e task",
		WithConversation: false,
		Actor:            "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	disp, err := r.dispatchSvc.Dispatch(ctx, dispatch.DispatchInput{
		TaskID:   taskRes.TaskID,
		WorkerID: "W-1",
		AgentCLI: "claude-code",
		Actor:    "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	return taskRes.TaskID, disp.ExecutionID
}

// ackAndStartWorking advances an execution from submitted → working in the
// same way DispatchService.HandleAck + Worker.NotifyWorking would.
func (r *runtimeRig) ackAndStartWorking(t *testing.T, execID taskruntime.TaskExecutionID, cwd string) {
	t.Helper()
	ctx := context.Background()
	if err := r.dispatchSvc.HandleAck(ctx, dispatch.DispatchAck{
		ExecutionID: execID,
		Accepted:    true,
		AckedAt:     r.clk.Now(),
	}, observability.Actor("user:hayang")); err != nil {
		t.Fatal(err)
	}
	if err := persistence.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		e, err := r.execRepo.FindByID(txCtx, execID)
		if err != nil {
			return err
		}
		if err := e.StartWorking(cwd, r.clk.Now()); err != nil {
			return err
		}
		return r.execRepo.Update(txCtx, e)
	}); err != nil {
		t.Fatal(err)
	}
}

// findExecution refetches the execution row.
func (r *runtimeRig) findExecution(t *testing.T, id taskruntime.TaskExecutionID) *execution.TaskExecution {
	t.Helper()
	e, err := r.execRepo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("find execution %s: %v", id, err)
	}
	return e
}

// events reads events in chronological order from the rig's DB.
func (r *runtimeRig) events(t *testing.T) []eventRow {
	t.Helper()
	return readEvents(t, r.dbPath)
}

// containsEvent reports whether events include the given type at least once.
func containsEvent(events []eventRow, eventType string) bool {
	for _, e := range events {
		if e.EventType == eventType {
			return true
		}
	}
	return false
}

// fakeAgentPath resolves the absolute path to a script in
// tests/e2e/testdata/fake-agents.
func fakeAgentPath(t *testing.T, name string) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	p := filepath.Join(filepath.Dir(here), "testdata", "fake-agents", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fake agent script missing: %v", err)
	}
	return p
}
