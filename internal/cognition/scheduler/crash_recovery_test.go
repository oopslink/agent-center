package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
)

func TestCrashRecovery_NewValidation(t *testing.T) {
	if _, err := scheduler.NewCrashRecovery(nil, nil, nil, nil, nil); err == nil {
		t.Fatal("missing db")
	}
	db := openSchedulerTestDB(t)
	if _, err := scheduler.NewCrashRecovery(db, nil, nil, nil, nil); err == nil {
		t.Fatal("missing repo")
	}
}

func TestCrashRecovery_NoOrphans(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	cr, err := scheduler.NewCrashRecovery(db, repo, er, sink, clk)
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := cr.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 orphans, got %d", n)
	}
}

func TestCrashRecovery_OrphansTransition(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"01HEZX"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INV1", Scope: scope, TriggerEvents: tes, StartedAt: clk.Now()})
	if err := repo.Save(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	cr, _ := scheduler.NewCrashRecovery(db, repo, er, sink, clk)
	n, cursor, err := cr.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("transitioned = %d", n)
	}
	if cursor == "" {
		t.Error("expected cursor")
	}
	got, _ := repo.FindByID(context.Background(), inv.ID())
	if got.Status() != cognition.StatusFailed {
		t.Errorf("status = %s", got.Status())
	}
	if got.FailedReason() != cognition.FailedReasonCenterRestartOrphan {
		t.Errorf("reason = %s", got.FailedReason())
	}
	// emit reached
	until := clk.Now().Add(time.Hour)
	rows, _ := er.Find(context.Background(), observability.EventQueryFilter{Until: &until, Limit: 50})
	saw := false
	for _, r := range rows {
		if r.Type() == "supervisor.invocation_failed_alert" {
			saw = true
		}
	}
	if !saw {
		t.Error("failed_alert not emitted")
	}
}

func TestCrashRecovery_RepeatedIsIdempotent(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	scope := cognition.MustNewInvocationScope(cognition.ScopeIssue, "I-9")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"01HE"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INVQ", Scope: scope, TriggerEvents: tes, StartedAt: clk.Now()})
	_ = repo.Save(context.Background(), inv)
	cr, _ := scheduler.NewCrashRecovery(db, repo, er, sink, clk)
	_, _, _ = cr.Recover(context.Background())
	// second recover should find no orphans
	n, _, err := cr.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second recover transitioned = %d", n)
	}
}
