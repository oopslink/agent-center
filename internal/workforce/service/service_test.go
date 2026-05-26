package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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
	db           *sql.DB
	workerRepo   *wfsqlite.WorkerRepo
	mappingRepo  *wfsqlite.MappingRepo
	proposalRepo *wfsqlite.ProposalRepo
	projectRepo  *wfsqlite.ProjectRepo
	eventRepo    *obsqlite.EventRepo
	sink         *observability.EventSink
	idgen        idgen.Generator
	clock        *clock.FakeClock
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
		db:           db,
		workerRepo:   wfsqlite.NewWorkerRepo(db),
		mappingRepo:  wfsqlite.NewMappingRepo(db),
		proposalRepo: wfsqlite.NewProposalRepo(db),
		projectRepo:  wfsqlite.NewProjectRepo(db),
		eventRepo:    er,
		sink:         sink,
		idgen:        gen,
		clock:        fc,
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
// ProjectDiscoveryService
// =============================================================================

func TestEnsureProject_New(t *testing.T) {
	s := setupSuite(t)
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, s.clock)
	var res EnsureProjectResult
	err := persistence.RunInTx(context.Background(), s.db, func(ctx context.Context) error {
		var err error
		res, err = disc.EnsureProject(ctx, EnsureProjectInput{
			ID:        "x",
			Name:      "X",
			CreatedBy: "user:hayang",
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Fatal()
	}
	if res.Project.ID() != "x" {
		t.Fatal()
	}
}

func TestEnsureProject_AlreadyExists(t *testing.T) {
	s := setupSuite(t)
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, s.clock)
	// Pre-save a project.
	p, _ := workforce.NewProject(workforce.NewProjectInput{
		ID: "x", Name: "X", CreatedByIdentityID: "user:x", CreatedAt: s.clock.Now(),
	})
	_ = s.projectRepo.Save(context.Background(), p)
	var res EnsureProjectResult
	err := persistence.RunInTx(context.Background(), s.db, func(ctx context.Context) error {
		var err error
		res, err = disc.EnsureProject(ctx, EnsureProjectInput{
			ID:        "x",
			Name:      "X",
			CreatedBy: "user:hayang",
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created {
		t.Fatal("should not be created")
	}
}

func TestEnsureProject_RequiresTx(t *testing.T) {
	s := setupSuite(t)
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, s.clock)
	_, err := disc.EnsureProject(context.Background(), EnsureProjectInput{
		ID:        "x",
		Name:      "X",
		CreatedBy: "user:x",
	})
	if err == nil {
		t.Fatal("expected tx required error")
	}
}

func TestEnsureProject_BadActor(t *testing.T) {
	s := setupSuite(t)
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, s.clock)
	err := persistence.RunInTx(context.Background(), s.db, func(ctx context.Context) error {
		_, e := disc.EnsureProject(ctx, EnsureProjectInput{
			ID:        "x",
			Name:      "X",
			CreatedBy: "bogus:actor",
		})
		return e
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureProject_BadID(t *testing.T) {
	s := setupSuite(t)
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, s.clock)
	err := persistence.RunInTx(context.Background(), s.db, func(ctx context.Context) error {
		_, e := disc.EnsureProject(ctx, EnsureProjectInput{
			ID:        "BAD SLUG",
			Name:      "x",
			CreatedBy: "user:x",
		})
		return e
	})
	if err == nil {
		t.Fatal("expected validation error for malformed id")
	}
}

// =============================================================================
// ProposalAcceptanceService — Propose
// =============================================================================

func acceptanceService(s *suite) *ProposalAcceptanceService {
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, s.clock)
	return NewProposalAcceptanceService(s.db, s.proposalRepo, s.mappingRepo,
		s.projectRepo, disc, s.sink, s.idgen, s.clock)
}

func TestPropose_Happy(t *testing.T) {
	s := setupSuite(t)
	// Worker must exist for FK.
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	res, err := svc.Propose(context.Background(), ProposeCommand{
		WorkerID:           "W-1",
		CandidatePath:      "/x/y",
		SuggestedProjectID: "agent-center",
		Actor:              "worker:W-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProposalID == "" {
		t.Fatal()
	}
	if res.AlreadyExists {
		t.Fatal()
	}
}

func TestPropose_Dedup(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	cmd := ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "p", Actor: "worker:W-1",
	}
	res1, _ := svc.Propose(context.Background(), cmd)
	res2, _ := svc.Propose(context.Background(), cmd)
	if !res2.AlreadyExists {
		t.Fatal("expected AlreadyExists=true")
	}
	if res1.ProposalID != res2.ProposalID {
		t.Fatal("dedup should return same id")
	}
	// Only one event total.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	if len(events) != 1 {
		t.Fatalf("expected 1 event after dedup, got %d", len(events))
	}
}

func TestPropose_BadActor(t *testing.T) {
	s := setupSuite(t)
	svc := acceptanceService(s)
	_, err := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "p",
		Actor: "bogus:x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// ProposalAcceptanceService — Accept
// =============================================================================

func TestAccept_NewProject(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	proposeRes, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x/y", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	res, err := svc.Accept(context.Background(), AcceptCommand{
		ProposalID: proposeRes.ProposalID,
		Actor:      "user:hayang",
	})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if !res.ProjectCreated {
		t.Fatal()
	}
	if res.MappingID == "" {
		t.Fatal()
	}
	// 4 events: proposed + project.created + mapping.added + accepted
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	types := map[observability.EventType]int{}
	for _, e := range events {
		types[e.Type()]++
	}
	expected := []observability.EventType{
		"workforce.worker_project_proposal.proposed",
		"workforce.project.created",
		"workforce.worker_project_mapping.added",
		"workforce.worker_project_proposal.accepted",
	}
	for _, e := range expected {
		if types[e] != 1 {
			t.Fatalf("missing event %s (counts: %v)", e, types)
		}
	}
}

func TestAccept_ExistingProject(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	// Pre-create project.
	p, _ := workforce.NewProject(workforce.NewProjectInput{
		ID: "ac", Name: "AC",
		CreatedByIdentityID: "user:x", CreatedAt: s.clock.Now(),
	})
	_ = s.projectRepo.Save(context.Background(), p)
	svc := acceptanceService(s)
	proposeRes, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	res, err := svc.Accept(context.Background(), AcceptCommand{
		ProposalID: proposeRes.ProposalID,
		Actor:      "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectCreated {
		t.Fatal("project should already exist")
	}
	// 3 events: proposed + mapping.added + accepted (no project.created)
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d (types: %v)", len(events), eventTypes(events))
	}
	for _, e := range events {
		if strings.Contains(string(e.Type()), "project.created") {
			t.Fatal("project.created should NOT be emitted")
		}
	}
}

func TestAccept_AlreadyAccepted(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	pr, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	_, _ = svc.Accept(context.Background(), AcceptCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	_, err := svc.Accept(context.Background(), AcceptCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	if !errors.Is(err, workforce.ErrProposalAlreadyTerminated) {
		t.Fatalf("got %v", err)
	}
}

func TestAccept_RollsBackOnMappingFailure(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	pr, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	// Pre-populate active mapping → second Accept attempt collides on
	// (worker, project) pre-check, all writes roll back.
	existing, _ := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID:               workforce.MappingID(s.idgen.NewULID()),
		WorkerID:         "W-1",
		ProjectID:        "ac",
		BasePath:         "/already-here",
		SourceProposalID: "",
		AddedAt:          s.clock.Now(),
	})
	// Pre-create project so mapping FK passes.
	p, _ := workforce.NewProject(workforce.NewProjectInput{
		ID: "ac", Name: "AC",
		CreatedByIdentityID: "user:x", CreatedAt: s.clock.Now(),
	})
	_ = s.projectRepo.Save(context.Background(), p)
	_ = s.mappingRepo.Save(context.Background(), existing)

	beforeEvents, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	_, err := svc.Accept(context.Background(), AcceptCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	if !errors.Is(err, workforce.ErrMappingAlreadyActive) {
		t.Fatalf("got %v", err)
	}
	// No new events landed.
	afterEvents, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	if len(afterEvents) != len(beforeEvents) {
		t.Fatalf("expected no new events; got %d", len(afterEvents)-len(beforeEvents))
	}
	// Proposal status still pending.
	got, _ := s.proposalRepo.FindByID(context.Background(), pr.ProposalID)
	if got.Status() != workforce.ProposalPending {
		t.Fatalf("proposal status should be pending after rollback; got %s", got.Status())
	}
}

func TestAccept_OverrideProjectID(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	pr, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "suggested", Actor: "worker:W-1",
	})
	res, err := svc.Accept(context.Background(), AcceptCommand{
		ProposalID:        pr.ProposalID,
		OverrideProjectID: "overridden",
		Actor:             "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID != "overridden" {
		t.Fatalf("got %s", res.ProjectID)
	}
}

// =============================================================================
// ProposalAcceptanceService — Ignore / Unignore
// =============================================================================

func TestIgnore_Happy(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	pr, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	_, err := svc.Ignore(context.Background(), IgnoreCommand{
		ProposalID: pr.ProposalID,
		Actor:      "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.proposalRepo.FindByID(context.Background(), pr.ProposalID)
	if got.Status() != workforce.ProposalIgnored {
		t.Fatalf("status: %s", got.Status())
	}
}

func TestUnignore_Happy(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	pr, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	_, _ = svc.Ignore(context.Background(), IgnoreCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	_, err := svc.Unignore(context.Background(), IgnoreCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.proposalRepo.FindByID(context.Background(), pr.ProposalID)
	if got.Status() != workforce.ProposalPending {
		t.Fatalf("status: %s", got.Status())
	}
}

func TestIgnore_NotPending(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	pr, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	_, _ = svc.Accept(context.Background(), AcceptCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	_, err := svc.Ignore(context.Background(), IgnoreCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	if !errors.Is(err, workforce.ErrProposalAlreadyTerminated) {
		t.Fatalf("got %v", err)
	}
}

func TestUnignore_NotIgnored(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	pr, _ := svc.Propose(context.Background(), ProposeCommand{
		WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "ac", Actor: "worker:W-1",
	})
	_, err := svc.Unignore(context.Background(), IgnoreCommand{ProposalID: pr.ProposalID, Actor: "user:x"})
	if !errors.Is(err, workforce.ErrProposalInvalidTransition) {
		t.Fatalf("got %v", err)
	}
}

// =============================================================================
// ProjectCRUDService
// =============================================================================

func TestProjectCRUD_Add(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	res, err := svc.Add(context.Background(), AddCommand{
		ID: "p", Name: "P", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Project.ID() != "p" {
		t.Fatal()
	}
	if res.EventID == "" {
		t.Fatal()
	}
}

func TestProjectCRUD_Add_Duplicate(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, _ = svc.Add(context.Background(), AddCommand{ID: "p", Name: "P", Actor: "user:x"})
	_, err := svc.Add(context.Background(), AddCommand{ID: "p", Name: "P", Actor: "user:x"})
	if !errors.Is(err, workforce.ErrProjectAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectCRUD_Update(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, _ = svc.Add(context.Background(), AddCommand{ID: "p", Name: "P", Actor: "user:x"})
	newName := "Renamed"
	res, err := svc.Update(context.Background(), UpdateCommand{
		ID: "p", Version: 1, Fields: workforce.ProjectUpdateFields{Name: &newName}, Actor: "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Project.Name() != "Renamed" {
		t.Fatal()
	}
}

func TestProjectCRUD_Update_VersionConflict(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, _ = svc.Add(context.Background(), AddCommand{ID: "p", Name: "P", Actor: "user:x"})
	n := "x"
	_, err := svc.Update(context.Background(), UpdateCommand{
		ID: "p", Version: 99, Fields: workforce.ProjectUpdateFields{Name: &n}, Actor: "user:x",
	})
	if !errors.Is(err, workforce.ErrProjectVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectCRUD_Update_NoChanges(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, _ = svc.Add(context.Background(), AddCommand{ID: "p", Name: "P", Actor: "user:x"})
	_, err := svc.Update(context.Background(), UpdateCommand{
		ID: "p", Version: 1, Fields: workforce.ProjectUpdateFields{}, Actor: "user:x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectCRUD_Remove(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, _ = svc.Add(context.Background(), AddCommand{ID: "p", Name: "P", Actor: "user:x"})
	_, err := svc.Remove(context.Background(), RemoveCommand{ID: "p", Actor: "user:x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, e := s.projectRepo.FindByID(context.Background(), "p"); !errors.Is(e, workforce.ErrProjectNotFound) {
		t.Fatalf("expected project gone, got %v", e)
	}
}

func TestProjectCRUD_Remove_HasActiveMapping(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	_, _ = svc.Add(context.Background(), AddCommand{ID: "p", Name: "P", Actor: "user:x"})
	m, _ := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID: workforce.MappingID(s.idgen.NewULID()),
		WorkerID: "W-1", ProjectID: "p", BasePath: "/x", AddedAt: s.clock.Now(),
	})
	_ = s.mappingRepo.Save(context.Background(), m)
	_, err := svc.Remove(context.Background(), RemoveCommand{ID: "p", Actor: "user:x"})
	if !errors.Is(err, workforce.ErrProjectHasActiveDeps) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectCRUD_Remove_NotFound(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, err := svc.Remove(context.Background(), RemoveCommand{ID: "nope", Actor: "user:x"})
	if !errors.Is(err, workforce.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
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

// =============================================================================
// Helpers
// =============================================================================

func newW(t *testing.T, id workforce.WorkerID) *workforce.Worker {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:           id,
		Capabilities: []string{"claude-code"},
		EnrolledAt:   time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func eventTypes(es []*observability.Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Type())
	}
	return out
}

func TestServiceGuard(t *testing.T) {
	// Guard helper that confirms misconfigured service is detected.
	var s *ProposalAcceptanceService
	if err := s.guard(); err == nil {
		t.Fatal("nil receiver should fail guard")
	}
}
