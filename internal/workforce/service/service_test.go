package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// setupSuite wires the full stack used by service tests.
type suite struct {
	db         *sql.DB
	workerRepo *wfsqlite.WorkerRepo
	eventRepo  *obsqlite.EventRepo
	sink       *observability.EventSink
	idgen      idgen.Generator
	clock      *clock.FakeClock
}

func setupSuite(t *testing.T) *suite {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, fc)
	return &suite{
		db:         db,
		workerRepo: wfsqlite.NewWorkerRepo(db),
		eventRepo:  er,
		sink:       sink,
		idgen:      gen,
		clock:      fc,
	}
}

// =============================================================================
// WorkerEnrollService
// =============================================================================

func TestEnroll_Happy(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	res, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID:      "W-1",
		Capabilities:  []string{"claude-code"},
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if res.WorkerID != "W-1" {
		t.Fatal()
	}
	if res.EventID == "" {
		t.Fatal()
	}
	w, _ := s.workerRepo.FindByID(context.Background(), "W-1")
	if w == nil {
		t.Fatal("worker not saved")
	}
	// Verify event landed in events table.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "W-1"},
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type() != "workforce.worker.enrolled" {
		t.Fatalf("event type: %s", events[0].Type())
	}
}

// v2.5-B1: Enroll is now idempotent for offline workers (claim path
// after Add() pre-creates the row at mint time). Online workers stay
// rejected so a second daemon can't shadow a live one — operator
// must Remove the row first.
func TestEnroll_RejectsOnlineWorker(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	cmd := EnrollCommand{
		WorkerID: "W-1", Capabilities: []string{"x"}, ActorIdentity: "user:hayang",
	}
	if _, err := enroll.Enroll(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	// Flip status to online (simulating first heartbeat) so the next
	// enroll falls into the "already live" branch instead of claim.
	if err := s.workerRepo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOffline, workforce.WorkerOnline, 1); err != nil {
		t.Fatal(err)
	}
	_, err := enroll.Enroll(context.Background(), cmd)
	if !errors.Is(err, workforce.ErrWorkerAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

// v2.5-B1: Enroll on a worker pre-created by Add() (status=offline,
// no capabilities) takes the claim path: capabilities updated +
// workforce.worker.enrolled emitted. The pre-create event
// (workforce.worker.added) and the claim event coexist in the audit
// log.
func TestEnroll_ClaimsPreCreated(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	if _, err := enroll.AddWorker(context.Background(), AddWorkerCommand{
		WorkerID: "W-1", Name: "alice-box", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatalf("AddWorker: %v", err)
	}
	res, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID: "W-1", Capabilities: []string{"claude-code"}, ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if res.WorkerID != "W-1" {
		t.Fatal()
	}
	// Capabilities should now reflect the claim payload.
	w, _ := s.workerRepo.FindByID(context.Background(), "W-1")
	caps := w.Capabilities()
	if len(caps) != 1 || caps[0] != "claude-code" {
		t.Fatalf("capabilities not updated: %v", caps)
	}
	// Two events: added + enrolled.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "W-1"},
	})
	if len(events) != 2 {
		t.Fatalf("expected 2 events (added+enrolled), got %d", len(events))
	}
	gotAdded, gotEnrolled := false, false
	for _, e := range events {
		switch e.Type() {
		case "workforce.worker.added":
			gotAdded = true
		case "workforce.worker.enrolled":
			gotEnrolled = true
		}
	}
	if !gotAdded || !gotEnrolled {
		t.Fatalf("missing event: added=%v enrolled=%v", gotAdded, gotEnrolled)
	}
}

// v2.5-B1: AddWorker creates a Worker AR at mint-enroll time with
// status=offline. Emits workforce.worker.added so SSE can refresh
// Fleet immediately.
func TestAddWorker_Happy(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	res, err := enroll.AddWorker(context.Background(), AddWorkerCommand{
		WorkerID: "worker-abc123", Name: "tenant-foo", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("AddWorker: %v", err)
	}
	if res.WorkerID != "worker-abc123" {
		t.Fatal()
	}
	w, err := s.workerRepo.FindByID(context.Background(), "worker-abc123")
	if err != nil {
		t.Fatalf("worker missing: %v", err)
	}
	if w.Status() != workforce.WorkerOffline {
		t.Fatalf("expected offline status, got %v", w.Status())
	}
	if w.Name() != "tenant-foo" {
		t.Fatalf("name: %q", w.Name())
	}
	if w.LastHeartbeatAt() != nil {
		t.Fatalf("expected nil last_heartbeat_at, got %v", w.LastHeartbeatAt())
	}
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "worker-abc123"},
	})
	if len(events) != 1 || events[0].Type() != "workforce.worker.added" {
		t.Fatalf("expected one workforce.worker.added event, got %v", events)
	}
}

// v2.5-B4: RemoveWorker drops the row and emits
// workforce.worker.removed so SSE consumers retire the Fleet row.
func TestRemoveWorker_Happy(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	if _, err := enroll.AddWorker(context.Background(), AddWorkerCommand{
		WorkerID: "W-rm", Name: "doomed", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := enroll.RemoveWorker(context.Background(), RemoveWorkerCommand{
		WorkerID: "W-rm", ActorIdentity: "user:hayang", Reason: "test cleanup",
	}); err != nil {
		t.Fatalf("RemoveWorker: %v", err)
	}
	if _, err := s.workerRepo.FindByID(context.Background(), "W-rm"); !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("worker still present after Remove: %v", err)
	}
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "W-rm"},
	})
	var sawRemoved bool
	for _, e := range events {
		if e.Type() == "workforce.worker.removed" {
			sawRemoved = true
		}
	}
	if !sawRemoved {
		t.Fatalf("expected workforce.worker.removed event, got %d total", len(events))
	}
}

// RemoveWorker on a missing id surfaces ErrWorkerNotFound (handler
// turns this into a 404).
func TestRemoveWorker_NotFound(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	_, err := enroll.RemoveWorker(context.Background(), RemoveWorkerCommand{
		WorkerID: "W-ghost", ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected ErrWorkerNotFound, got %v", err)
	}
}

// AddWorker is single-shot per worker_id: a second AddWorker call
// with the same id surfaces ErrWorkerAlreadyExists (caller can then
// either remove + re-add, or use the future re-mint flow in B3).
func TestAddWorker_DuplicateID(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	cmd := AddWorkerCommand{WorkerID: "W-2", Name: "x", ActorIdentity: "user:hayang"}
	if _, err := enroll.AddWorker(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	_, err := enroll.AddWorker(context.Background(), cmd)
	if !errors.Is(err, workforce.ErrWorkerAlreadyExists) {
		t.Fatalf("expected ErrWorkerAlreadyExists, got %v", err)
	}
}

func TestEnroll_RejectsBadActor(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	_, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID:      "W-1",
		ActorIdentity: "bad:foo:bar",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnroll_RejectsBadID(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	_, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID:      "",
		ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnroll_RollbackOnSinkFailure(t *testing.T) {
	s := setupSuite(t)
	// Replace sink with one that fails on Emit.
	failingSink := observability.NewEventSink(
		&failingEventRepo{},
		s.eventRepo,
		s.idgen,
		s.clock,
	)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, failingSink, s.clock)
	_, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// Worker row should NOT be present (tx rolled back).
	if _, err := s.workerRepo.FindByID(context.Background(), "W-1"); !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected rollback, got %v", err)
	}
}

type failingEventRepo struct{}

func (failingEventRepo) Append(ctx context.Context, e *observability.Event) error {
	return errors.New("simulated sink failure")
}
func (failingEventRepo) FindByID(ctx context.Context, id observability.EventID) (*observability.Event, error) {
	return nil, errors.New("nope")
}
func (failingEventRepo) Find(ctx context.Context, _ observability.EventQueryFilter) ([]*observability.Event, error) {
	return nil, nil
}

// =============================================================================
// Heartbeat (v2.3-1 task #24)
// =============================================================================

func TestHeartbeat_Happy(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	if _, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID: "W-HB", Capabilities: []string{"claude-code"}, ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(30 * time.Second)
	if err := enroll.Heartbeat(context.Background(), HeartbeatCommand{
		WorkerID: "W-HB", AdditionalWorkingSeconds: 30,
	}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	w, err := s.workerRepo.FindByID(context.Background(), "W-HB")
	if err != nil {
		t.Fatal(err)
	}
	if w.LastHeartbeatAt() == nil {
		t.Fatal("last_heartbeat_at not updated")
	}
	if w.WorkingSeconds() != 30 {
		t.Fatalf("working_seconds=%d want 30", w.WorkingSeconds())
	}
}

func TestHeartbeat_Idempotent(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	if _, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID: "W-HB2", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	// Repeat heartbeat should not error (this is the whole point of
	// the new endpoint vs v2.2 re-enroll which returned 409).
	for i := 0; i < 3; i++ {
		s.clock.Advance(time.Second)
		if err := enroll.Heartbeat(context.Background(), HeartbeatCommand{WorkerID: "W-HB2"}); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
}

func TestHeartbeat_UnknownWorker(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	err := enroll.Heartbeat(context.Background(), HeartbeatCommand{WorkerID: "W-MISSING"})
	if err == nil {
		t.Fatal("expected error on unknown worker")
	}
}

func TestHeartbeat_ValidatesArgs(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	if err := enroll.Heartbeat(context.Background(), HeartbeatCommand{}); err == nil {
		t.Fatal("expected error on empty worker_id")
	}
	if err := enroll.Heartbeat(context.Background(), HeartbeatCommand{WorkerID: "W-X", AdditionalWorkingSeconds: -1}); err == nil {
		t.Fatal("expected error on negative working seconds")
	}
}
