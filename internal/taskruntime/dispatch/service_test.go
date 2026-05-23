package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

type capturingSender struct {
	mu       sync.Mutex
	envs     []DispatchEnvelope
	wantFail bool
}

func (c *capturingSender) Send(_ context.Context, env DispatchEnvelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.envs = append(c.envs, env)
	if c.wantFail {
		return errors.New("simulated transport failure")
	}
	return nil
}

func (c *capturingSender) Snapshot() []DispatchEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]DispatchEnvelope(nil), c.envs...)
}

type testHarness struct {
	db       *sql.DB
	taskRepo *trsqlite.TaskRepo
	execRepo *trsqlite.TaskExecutionRepo
	sink     *observability.EventSink
	eventRepo *obsqlite.EventRepo
	clk      *clock.FakeClock
	idgen    idgen.Generator
	sender   *capturingSender
	svc      *Service
}

func setup(t *testing.T) *testHarness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Seed project + worker
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id) VALUES ('P-1', 'P', '2026-05-21T12:00:00Z', '2026-05-21T12:00:00Z', 'user:hayang')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO workers (id, status, capabilities_json, working_seconds, enrolled_at, created_at, updated_at) VALUES ('W-1', 'online', '[{"agent_cli":"claude-code","detected":true,"enabled":true}]', 0, '2026-05-21T12:00:00Z', '2026-05-21T12:00:00Z', '2026-05-21T12:00:00Z')`)
	if err != nil {
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
	sender := &capturingSender{}
	svc := NewService(db, taskRepo, execRepo, sink, sender, clk, gen, DispatchConfig{
		MaxExecutionsPerTask: 3,
		DispatchAckTimeout:   30 * time.Second,
	})
	return &testHarness{
		db: db, taskRepo: taskRepo, execRepo: execRepo, sink: sink,
		eventRepo: er, clk: clk, idgen: gen, sender: sender, svc: svc,
	}
}

func seedTask(t *testing.T, h *testHarness, id taskruntime.TaskID) *task.Task {
	t.Helper()
	tt, err := task.New(task.NewInput{
		ID:               id,
		ProjectID:        "P-1",
		Title:            "x",
		CreatedBy:        "user:hayang",
		RequiresWorktree: true,
		Now:              h.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.taskRepo.Save(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	return tt
}

func eventTypes(t *testing.T, h *testHarness) []string {
	t.Helper()
	got, err := h.eventRepo.Find(context.Background(), observability.EventQueryFilter{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, len(got))
	for i, e := range got {
		types[i] = e.Type().String()
	}
	return types
}

func TestDispatch_Happy(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	res, err := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID:   "T-1",
		WorkerID: "W-1",
		AgentCLI: "claude-code",
		Actor:    "user:hayang",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExecutionID == "" {
		t.Fatal("expected exec id")
	}
	if len(h.sender.Snapshot()) != 1 {
		t.Fatalf("send count: %d", len(h.sender.Snapshot()))
	}
	tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
	if tt.CurrentExecutionID() != res.ExecutionID {
		t.Fatalf("current_exec: %s", tt.CurrentExecutionID())
	}
	gotTypes := eventTypes(t, h)
	if !containsAll(gotTypes, "task_execution.submitted", "task_execution.dispatched") {
		t.Fatalf("events: %+v", gotTypes)
	}
}

func TestDispatch_SingleActiveViolation(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	ctx := context.Background()
	if _, err := h.svc.Dispatch(ctx, DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Dispatch(ctx, DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"}); !errors.Is(err, execution.ErrSingleActiveViolation) {
		t.Fatalf("expected single-active violation: %v", err)
	}
}

func TestDispatch_TerminalTaskRejected(t *testing.T) {
	h := setup(t)
	tt := seedTask(t, h, "T-1")
	if err := tt.MarkDone(h.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.taskRepo.Update(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Dispatch(context.Background(), DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"}); !errors.Is(err, execution.ErrInvalidTransition) {
		t.Fatalf("expected invalid transition: %v", err)
	}
}

func TestDispatch_NotFound(t *testing.T) {
	h := setup(t)
	if _, err := h.svc.Dispatch(context.Background(), DispatchInput{TaskID: "X", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"}); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestDispatch_MaxExecutionsLimit(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	ctx := context.Background()
	// Dispatch 3 times, each time fail the previous to free single-active
	for i := 0; i < 3; i++ {
		res, err := h.svc.Dispatch(ctx, DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"})
		if err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
		// Fail it so next can dispatch
		nack := DispatchNack{
			ExecutionID: res.ExecutionID, Accepted: false,
			Reason: execution.NackWorkerAtCapacity, Message: "boom",
			AckedAt: h.clk.Now(),
		}
		if err := h.svc.HandleNack(ctx, nack, "user:hayang"); err != nil {
			t.Fatalf("nack %d: %v", i, err)
		}
	}
	// 4th should fail with dispatch_limit_reached
	if _, err := h.svc.Dispatch(ctx, DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"}); err == nil {
		t.Fatal("expected dispatch limit")
	}
	if !contains(eventTypes(t, h), "task.dispatch_limit_reached") {
		t.Fatal("expected limit event")
	}
}

func TestHandleAck_Happy(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	res, err := h.svc.Dispatch(context.Background(), DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"})
	if err != nil {
		t.Fatal(err)
	}
	ack := DispatchAck{ExecutionID: res.ExecutionID, Accepted: true, AckedAt: h.clk.Now()}
	if err := h.svc.HandleAck(context.Background(), ack, "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	e, _ := h.execRepo.FindByID(context.Background(), res.ExecutionID)
	if e.DispatchState() != execution.DispatchAcked {
		t.Fatalf("expected acked, got %s", e.DispatchState())
	}
	if !contains(eventTypes(t, h), "task_execution.acked") {
		t.Fatal("expected acked event")
	}
}

func TestHandleAck_ValidatorAndNotFound(t *testing.T) {
	h := setup(t)
	if err := h.svc.HandleAck(context.Background(), DispatchAck{}, "user:hayang"); err == nil {
		t.Fatal("expected validation")
	}
	if err := h.svc.HandleAck(context.Background(), DispatchAck{ExecutionID: "E", Accepted: true, AckedAt: h.clk.Now()}, "BOGUS"); err == nil {
		t.Fatal("expected actor validation")
	}
	if err := h.svc.HandleAck(context.Background(), DispatchAck{ExecutionID: "E", Accepted: true, AckedAt: h.clk.Now()}, "user:hayang"); !errors.Is(err, execution.ErrTaskExecutionNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestHandleNack_AllSubReasons(t *testing.T) {
	reasons := []execution.NackSubReason{
		execution.NackWorkerAtCapacity,
		execution.NackMappingMissing,
		execution.NackAgentCliUnsupported,
		execution.NackWorktreePathBusy,
		execution.NackBaseBranchMissing,
		execution.NackEnvelopeVersionUnsupported,
	}
	for _, r := range reasons {
		t.Run(string(r), func(t *testing.T) {
			h := setup(t)
			seedTask(t, h, "T-1")
			res, _ := h.svc.Dispatch(context.Background(), DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"})
			nack := DispatchNack{ExecutionID: res.ExecutionID, Accepted: false, Reason: r, Message: "x", AckedAt: h.clk.Now()}
			if err := h.svc.HandleNack(context.Background(), nack, "worker:W-1"); err != nil {
				t.Fatal(err)
			}
			e, _ := h.execRepo.FindByID(context.Background(), res.ExecutionID)
			if e.Status() != execution.StatusFailed || e.FailedReason() != execution.DispatchNack(r) {
				t.Fatalf("status/reason: %s/%s", e.Status(), e.FailedReason())
			}
			tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
			if tt.HasActiveExecution() {
				t.Fatal("expected cleared current_execution")
			}
		})
	}
}

func TestHandleNack_Validators(t *testing.T) {
	h := setup(t)
	if err := h.svc.HandleNack(context.Background(), DispatchNack{}, "user:hayang"); err == nil {
		t.Fatal("expected validation")
	}
	if err := h.svc.HandleNack(context.Background(), DispatchNack{ExecutionID: "E", Reason: execution.NackWorkerAtCapacity, Message: "x", AckedAt: h.clk.Now()}, "BAD"); err == nil {
		t.Fatal("expected actor validation")
	}
}

func TestScanPendingAck_30sNoAck(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	res, _ := h.svc.Dispatch(context.Background(), DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"})
	// Advance > 30s
	h.clk.Advance(35 * time.Second)
	count, err := h.svc.ScanPendingAck(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count: %d", count)
	}
	e, _ := h.execRepo.FindByID(context.Background(), res.ExecutionID)
	if e.Status() != execution.StatusFailed || e.FailedReason() != execution.FailedDispatchNoAck {
		t.Fatalf("status/reason: %s/%s", e.Status(), e.FailedReason())
	}
	// Second scan idempotent (no more pending)
	count, _ = h.svc.ScanPendingAck(context.Background(), "system")
	if count != 0 {
		t.Fatalf("idempotent count: %d", count)
	}
}

func TestScanPendingAck_ActorValidation(t *testing.T) {
	h := setup(t)
	if _, err := h.svc.ScanPendingAck(context.Background(), "BAD"); err == nil {
		t.Fatal("expected actor validation")
	}
}

func TestDispatch_SenderFailureRecorded(t *testing.T) {
	h := setup(t)
	h.sender.wantFail = true
	seedTask(t, h, "T-1")
	if _, err := h.svc.Dispatch(context.Background(), DispatchInput{TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !contains(eventTypes(t, h), "task_execution.dispatch_send_failed") {
		t.Fatal("expected send failed event")
	}
}

func TestDispatch_ActorValidation(t *testing.T) {
	h := setup(t)
	if _, err := h.svc.Dispatch(context.Background(), DispatchInput{TaskID: "T", WorkerID: "W", AgentCLI: "c", Actor: "BAD"}); err == nil {
		t.Fatal("expected actor validation")
	}
}

func TestIssueConcludeSpawn_Happy(t *testing.T) {
	h := setup(t)
	stub := NewIssueConcludeSpawn(h.db, h.taskRepo, h.sink, h.idgen, h.clk)
	ids, err := stub.Spawn(context.Background(), IssueConcludeSpec{
		IssueID: "ISS-1", ProjectID: "P-1", Resolution: "done", ActorID: "user:hayang",
		Tasks: []IssueConcludeTaskSpec{
			{LocalID: "a", Title: "alpha", RequiresWorktree: true},
			{LocalID: "b", Title: "beta", DependsOnLocalIDs: []string{"a"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("len: %d", len(ids))
	}
	tA, _ := h.taskRepo.FindByID(context.Background(), ids[0])
	tB, _ := h.taskRepo.FindByID(context.Background(), ids[1])
	if tA.FromIssueID() != "ISS-1" {
		t.Fatalf("from_issue: %s", tA.FromIssueID())
	}
	if len(tB.DependsOnTaskIDs()) != 1 || tB.DependsOnTaskIDs()[0] != ids[0] {
		t.Fatalf("deps: %+v", tB.DependsOnTaskIDs())
	}
}

func TestIssueConcludeSpawn_CycleRollback(t *testing.T) {
	h := setup(t)
	stub := NewIssueConcludeSpawn(h.db, h.taskRepo, h.sink, h.idgen, h.clk)
	_, err := stub.Spawn(context.Background(), IssueConcludeSpec{
		IssueID: "ISS-1", ProjectID: "P-1", Resolution: "done", ActorID: "user:hayang",
		Tasks: []IssueConcludeTaskSpec{
			{LocalID: "a", Title: "alpha", DependsOnLocalIDs: []string{"b"}},
			{LocalID: "b", Title: "beta", DependsOnLocalIDs: []string{"a"}},
		},
	})
	if err == nil {
		t.Fatal("expected cycle error")
	}
	tasks, _ := h.taskRepo.FindByProject(context.Background(), "P-1", task.Filter{})
	if len(tasks) != 0 {
		t.Fatalf("expected rollback, got %d", len(tasks))
	}
}

func TestIssueConcludeSpawn_ExistingDepNotFoundRollsBack(t *testing.T) {
	h := setup(t)
	stub := NewIssueConcludeSpawn(h.db, h.taskRepo, h.sink, h.idgen, h.clk)
	_, err := stub.Spawn(context.Background(), IssueConcludeSpec{
		IssueID: "ISS-1", ProjectID: "P-1", Resolution: "done", ActorID: "user:hayang",
		Tasks: []IssueConcludeTaskSpec{
			{LocalID: "a", Title: "alpha", DependsOnTaskIDs: []taskruntime.TaskID{"T-X"}},
		},
	})
	if !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestIssueConcludeSpawn_BadSpec(t *testing.T) {
	h := setup(t)
	stub := NewIssueConcludeSpawn(h.db, h.taskRepo, h.sink, h.idgen, h.clk)
	if _, err := stub.Spawn(context.Background(), IssueConcludeSpec{}); err == nil {
		t.Fatal("expected spec validation")
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func containsAll(xs []string, vs ...string) bool {
	for _, v := range vs {
		if !contains(xs, v) {
			return false
		}
	}
	return true
}
