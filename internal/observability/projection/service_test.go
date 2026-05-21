package projection_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/projection"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

type testEnv struct {
	repo *obsqlite.ProjectionRepo
	sink *observability.EventSink
	er   *obsqlite.EventRepo
	clk  clock.Clock
}

func setup(t *testing.T) *testEnv {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	eRepo, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatalf("NewEventRepo: %v", err)
	}
	sink := observability.NewEventSink(eRepo, eRepo, gen, clk)
	pRepo := obsqlite.NewProjectionRepo(db)
	return &testEnv{repo: pRepo, sink: sink, er: eRepo, clk: clk}
}

func (e *testEnv) svc(checker projection.TaskExecutionExistenceChecker) *projection.TaskExecutionProjectionService {
	return projection.NewTaskExecutionProjectionService(e.repo, e.sink, checker, e.clk)
}

func TestService_Update_HappyPath(t *testing.T) {
	env := setup(t)
	svc := env.svc(nil)
	id := taskruntime.TaskExecutionID("E-1")
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	upd := projection.ProjectionUpdate{
		LastPushAt:        now,
		CurrentActivity:   "work",
		CurrentActivityAt: now,
		TotalToolCalls:    1,
	}
	if err := svc.UpdateProjection(context.Background(), id, upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := env.repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.CurrentActivity != "work" {
		t.Fatalf("activity off: %+v", got)
	}
}

func TestService_Update_Stale_EmitsEvent_NoOverwrite(t *testing.T) {
	env := setup(t)
	svc := env.svc(nil)
	id := taskruntime.TaskExecutionID("E-1")
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	if err := svc.UpdateProjection(context.Background(), id, projection.ProjectionUpdate{LastPushAt: t0, TotalToolCalls: 5}); err != nil {
		t.Fatalf("first: %v", err)
	}
	// stale push
	if err := svc.UpdateProjection(context.Background(), id, projection.ProjectionUpdate{LastPushAt: t0.Add(-time.Minute), TotalToolCalls: 1}); err != nil {
		t.Fatalf("stale push must not surface as error, got %v", err)
	}
	// state preserved
	got, err := env.repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 5 {
		t.Fatalf("expected 5, got %d", got.TotalToolCalls)
	}
	// event emitted
	staleType := observability.EventType("observability.projection_stale_drop")
	events, err := env.er.Find(context.Background(), observability.EventQueryFilter{EventType: &staleType})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 stale_drop event, got %d", len(events))
	}
	if events[0].Refs().ExecutionID != string(id) {
		t.Fatalf("event refs.execution_id = %q", events[0].Refs().ExecutionID)
	}
	payload := events[0].Payload()
	if payload["reason"] != "out_of_order_push" {
		t.Fatalf("reason = %v", payload["reason"])
	}
}

func TestService_Update_NilCheckerSkipsExistence(t *testing.T) {
	env := setup(t)
	svc := env.svc(nil)
	if err := svc.UpdateProjection(context.Background(), "E-missing", projection.ProjectionUpdate{LastPushAt: time.Now()}); err != nil {
		t.Fatalf("nil checker should write: %v", err)
	}
}

type stubChecker struct {
	exists map[taskruntime.TaskExecutionID]bool
	err    error
}

func (s stubChecker) TaskExecutionExists(_ context.Context, id taskruntime.TaskExecutionID) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.exists[id], nil
}

func TestService_Update_NotFound_FromChecker(t *testing.T) {
	env := setup(t)
	svc := env.svc(stubChecker{exists: map[taskruntime.TaskExecutionID]bool{"E-1": false}})
	err := svc.UpdateProjection(context.Background(), "E-1", projection.ProjectionUpdate{LastPushAt: time.Now()})
	if !errors.Is(err, projection.ErrTaskExecutionNotFound) {
		t.Fatalf("expected ErrTaskExecutionNotFound, got %v", err)
	}
}

func TestService_Update_CheckerError_Surfaces(t *testing.T) {
	env := setup(t)
	boom := errors.New("boom")
	svc := env.svc(stubChecker{err: boom})
	err := svc.UpdateProjection(context.Background(), "E-1", projection.ProjectionUpdate{LastPushAt: time.Now()})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}

func TestService_Update_Validation(t *testing.T) {
	env := setup(t)
	svc := env.svc(nil)
	if err := svc.UpdateProjection(context.Background(), "", projection.ProjectionUpdate{}); err == nil {
		t.Fatal("expected empty id error")
	}
	if err := svc.UpdateProjection(context.Background(), "E-1", projection.ProjectionUpdate{}); err == nil {
		t.Fatal("expected validation error for zero last_push_at")
	}
}

func TestService_NilReceiver_Guard(t *testing.T) {
	var svc *projection.TaskExecutionProjectionService
	if err := svc.UpdateProjection(context.Background(), "E-1", projection.ProjectionUpdate{LastPushAt: time.Now()}); err == nil {
		t.Fatal("expected nil-receiver guard")
	}
}
