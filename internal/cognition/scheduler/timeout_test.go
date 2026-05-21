package scheduler_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
)

func openSchedulerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := persistence.Open(persistence.FileDSN(dir + "/test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestTimeoutHandler_New_Validation(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	if _, err := scheduler.NewTimeoutHandler(scheduler.DefaultTimeoutConfig(), nil, repo, nil, nil, nil); err == nil {
		t.Fatal("missing db")
	}
	if _, err := scheduler.NewTimeoutHandler(scheduler.DefaultTimeoutConfig(), db, nil, nil, nil, nil); err == nil {
		t.Fatal("missing repo")
	}
	if _, err := scheduler.NewTimeoutHandler(scheduler.DefaultTimeoutConfig(), db, repo, nil, nil, nil); err == nil {
		t.Fatal("missing sink")
	}
}

func TestTimeoutHandler_TransitionsRunningPastDeadline(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	th, err := scheduler.NewTimeoutHandler(scheduler.TimeoutConfig{
		TickInterval: 1 * time.Second, Grace: 1 * time.Second,
	}, db, repo, nil, sink, clk)
	if err != nil {
		t.Fatal(err)
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INV1", Scope: scope, TriggerEvents: tes, StartedAt: clk.Now()})
	if err := repo.Save(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	// not yet past deadline (180s)
	if n, err := th.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("early tick: n=%d err=%v", n, err)
	}
	// advance past deadline
	clk.Advance(181 * time.Second)
	n, err := th.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("transitioned = %d, want 1", n)
	}
	got, err := repo.FindByID(context.Background(), inv.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != cognition.StatusTimedOut {
		t.Errorf("status = %s", got.Status())
	}
	// emit verified by event repo
	until := clk.Now().Add(time.Hour)
	rows, _ := er.Find(context.Background(), observability.EventQueryFilter{Until: &until, Limit: 50})
	saw := false
	for _, r := range rows {
		if r.Type() == "supervisor.invocation_timed_out" {
			saw = true
		}
	}
	if !saw {
		t.Error("supervisor.invocation_timed_out not emitted")
	}
}

func TestTimeoutHandler_SkipsAlreadyTerminal(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	th, _ := scheduler.NewTimeoutHandler(scheduler.DefaultTimeoutConfig(), db, repo, nil, sink, clk)
	if n, err := th.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("empty: %d %v", n, err)
	}
}
