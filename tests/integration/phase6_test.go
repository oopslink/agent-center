package integration_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/decision"
	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
)

func openCognitionDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := persistence.Open(persistence.FileDSN(dir + "/test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestPhase6_FullPipeline drives the full event → coalesce → spawn → exit
// → emit pipeline using a fake ProcessRunner (so no real subprocess).
func TestPhase6_FullPipeline(t *testing.T) {
	db := openCognitionDB(t)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(1))
	sink := observability.NewEventSink(er, er, gen, clk)
	invRepo := cognitiondb.NewInvocationRepo(db)
	queue := scheduler.NewInMemoryQueue(5)
	coalCfg := scheduler.CoalescerConfig{
		RollingWindow: 30 * time.Second, HardWindow: 5 * time.Minute,
		BatchSize: 100, MaxConcurrentInvocations: 5, TickInterval: time.Second,
	}
	coal, err := scheduler.NewCoalescer(coalCfg, scheduler.CoalescerDeps{
		EventRepo: er, InvocationRepo: invRepo, Clock: clk,
	}, queue)
	if err != nil {
		t.Fatal(err)
	}
	runner := &phase6Runner{onExit: func(_ scheduler.ProcessSpec) (int, error, string) {
		return 0, nil, ""
	}}
	sp, err := scheduler.NewSpawner(scheduler.SpawnerConfig{
		Binary:    "agent-center",
		MemoryDir: t.TempDir(),
		UsageDir:  t.TempDir(),
	}, scheduler.SpawnerDeps{
		DB: db, Repo: invRepo, Sink: sink, Clock: clk, IDGen: gen,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	_ = emitEvent(t, sink, "task.created", observability.EventRefs{TaskID: "T-1", ProjectID: "demo"})
	if n, _, err := coal.Tick(context.Background()); err != nil || n == 0 {
		t.Fatalf("tick1: n=%d err=%v", n, err)
	}
	if queue.Len() != 0 {
		t.Fatal("queue should be empty before window closes")
	}
	clk.Advance(31 * time.Second)
	if _, closed, err := coal.Tick(context.Background()); err != nil || closed != 1 {
		t.Fatalf("tick2: closed=%d err=%v", closed, err)
	}
	req, _ := queue.Dequeue()
	id, err := sp.Spawn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	runner.wg.Wait()
	deadline := time.Now().Add(2 * time.Second)
	var got *cognition.SupervisorInvocation
	for time.Now().Before(deadline) {
		got, _ = invRepo.FindByID(context.Background(), id)
		if got != nil && got.IsTerminal() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got == nil || got.Status() != cognition.StatusSucceeded {
		t.Errorf("status = %v", got)
	}
	until := clk.Now().Add(time.Hour)
	rows, _ := er.Find(context.Background(), observability.EventQueryFilter{Until: &until, Limit: 100})
	saw := map[observability.EventType]bool{}
	for _, r := range rows {
		saw[r.Type()] = true
	}
	for _, et := range []observability.EventType{
		"task.created",
		"supervisor.invocation_started",
		"supervisor.invocation_succeeded",
	} {
		if !saw[et] {
			t.Errorf("missing %s; saw=%+v", et, saw)
		}
	}
}

// TestPhase6_CrashRecoveryRecoversOrphans seeds a "running" invocation
// (simulating a center crash) and verifies recovery transitions it to
// failed(center_restart_orphan) and emits the alert.
func TestPhase6_CrashRecoveryRecoversOrphans(t *testing.T) {
	db := openCognitionDB(t)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(2))
	sink := observability.NewEventSink(er, er, gen, clk)
	repo := cognitiondb.NewInvocationRepo(db)
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-9")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{observability.EventID(gen.NewULID())})
	inv, _ := cognition.Spawn(cognition.SpawnInput{
		ID: cognition.InvocationID(gen.NewULID()), Scope: scope, TriggerEvents: tes,
		StartedAt: clk.Now(),
	})
	if err := repo.Save(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	cr, _ := scheduler.NewCrashRecovery(db, repo, er, sink, clk)
	n, _, err := cr.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("transitioned %d, want 1", n)
	}
	got, _ := repo.FindByID(context.Background(), inv.ID())
	if got.FailedReason() != cognition.FailedReasonCenterRestartOrphan {
		t.Errorf("reason = %s", got.FailedReason())
	}
	until := clk.Now().Add(time.Hour)
	rows, _ := er.Find(context.Background(), observability.EventQueryFilter{Until: &until, Limit: 100})
	saw := false
	for _, r := range rows {
		if r.Type() == "supervisor.invocation_failed_alert" {
			saw = true
		}
	}
	if !saw {
		t.Error("alert event not emitted")
	}
}

// TestPhase6_MemorySkeletonRealGit creates the memory tree under a fresh
// temp dir using real `git`. Exercises the full filesystem path.
func TestPhase6_MemorySkeletonRealGit(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	g := memory.NewGitOps(dir, memory.NewExecGitRunner(), home)
	f := memory.NewSkeletonFactory(dir, g)
	if err := f.EnsureRootInit(context.Background()); err != nil {
		t.Fatal(err)
	}
	scopes := []memory.MemoryScope{
		{Kind: memory.MemScopeProject, ProjectID: "demo"},
		{Kind: memory.MemScopeTask, ProjectID: "demo", Key: "T-1"},
		{Kind: memory.MemScopeConversation, Key: "C-1"},
		{Kind: memory.MemScopeWorker, Key: "W-1"},
	}
	for _, s := range scopes {
		if err := f.CreateSkeleton(context.Background(), s); err != nil {
			t.Errorf("skel %+v: %v", s, err)
		}
	}
	log, err := g.LogOneline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"global", "supervisor", "project:demo", "task:T-1", "conversation:C-1", "worker:W-1"} {
		if !strings.Contains(log, key) {
			t.Errorf("log missing %s; log=%s", key, log)
		}
	}
}

// TestPhase6_DecisionRecorderSameProcessFlow checks the
// supervisor-actor record path and the sentinel error paths.
// TestADR0014_SameTxRollback verifies the integration-level ADR-0014
// same-tx invariant: when DecisionRecorder.Record is invoked inside an
// outer tx that subsequently errors, the decision row is NOT persisted
// (rolled back with the outer tx).
func TestADR0014_SameTxRollback(t *testing.T) {
	db := openCognitionDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	clk := clock.NewFakeClock(time.Now().UTC())
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(7))
	r, err := decision.NewRecorder(repo, clk, gen)
	if err != nil {
		t.Fatal(err)
	}
	actor := decision.Actor{Kind: "supervisor", ID: "INV-RB", InvocationID: "INV-RB"}

	// Outer tx writes decision then forces an error → rollback.
	wantErr := os.ErrInvalid
	gotErr := persistence.RunInTx(context.Background(), db, func(txCtx context.Context) error {
		if _, err := r.Record(txCtx, actor, decision.RecordRequest{
			Kind:           cognition.DecisionDispatch,
			TargetRefsJSON: `{"task_id":"T-RB"}`,
			Rationale:      "to be rolled back",
			Outcome:        cognition.OutcomeSucceeded,
		}); err != nil {
			return err
		}
		return wantErr
	})
	if gotErr != wantErr {
		t.Fatalf("got %v, want %v", gotErr, wantErr)
	}
	// Verify the decision row never landed.
	rows, _ := repo.FindByInvocationID(context.Background(), "INV-RB")
	if len(rows) != 0 {
		t.Errorf("decision_records survived rollback: %d rows", len(rows))
	}
}

func TestPhase6_DecisionRecorderSameProcessFlow(t *testing.T) {
	db := openCognitionDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	clk := clock.NewFakeClock(time.Now())
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(5))
	r, err := decision.NewRecorder(repo, clk, gen)
	if err != nil {
		t.Fatal(err)
	}
	actor := decision.Actor{Kind: "supervisor", ID: "INV1", InvocationID: "INV1"}
	id, err := r.Record(context.Background(), actor, decision.RecordRequest{
		Kind: cognition.DecisionDispatch, TargetRefsJSON: `{"task_id":"T-1"}`,
		Rationale: "W-1 idle", Outcome: cognition.OutcomeSucceeded,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rationale() != "W-1 idle" {
		t.Errorf("rationale %q", got.Rationale())
	}
}

// emitEvent is a small helper to push a domain event via EventSink.
func emitEvent(t *testing.T, sink *observability.EventSink, etype observability.EventType, refs observability.EventRefs) observability.EventID {
	t.Helper()
	id, err := sink.Emit(context.Background(), observability.EmitCommand{
		EventType: etype,
		Refs:      refs,
		Actor:     observability.Actor("system"),
		Payload:   map[string]any{},
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return id
}

// phase6Runner is a minimal ProcessRunner used by Phase 6 integration tests.
type phase6Runner struct {
	onExit func(scheduler.ProcessSpec) (int, error, string)
	wg     sync.WaitGroup
}

func (f *phase6Runner) Start(_ context.Context, spec scheduler.ProcessSpec, cb func(int, error, string)) (scheduler.ProcessHandle, error) {
	h := &phase6Handle{done: make(chan struct{})}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		ec := 0
		var err error
		var stderr string
		if f.onExit != nil {
			ec, err, stderr = f.onExit(spec)
		}
		if cb != nil {
			cb(ec, err, stderr)
		}
		close(h.done)
	}()
	return h, nil
}

type phase6Handle struct {
	done chan struct{}
}

func (h *phase6Handle) PID() int                  { return 1 }
func (h *phase6Handle) Signal(_ os.Signal) error  { return nil }
func (h *phase6Handle) Kill() error               { return nil }
func (h *phase6Handle) Done() <-chan struct{}     { return h.done }
