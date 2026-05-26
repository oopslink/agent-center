package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
	"github.com/oopslink/agent-center/internal/workforce/service"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// fixedClock advances on demand for the reconciler edge-case tests.
type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time     { return c.now }
func (c *fixedClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func setupHB(t *testing.T) (workforce.WorkerRepository, *observability.EventSink, *fixedClock, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := persistence.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := &fixedClock{now: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)}
	gen := idgen.NewGenerator(clk)
	eventRepo, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(eventRepo, eventRepo, gen, clk)
	repo := wfsqlite.NewWorkerRepo(db)
	return repo, sink, clk, func() { _ = db.Close() }
}

// Worker just enrolled, no heartbeats yet, status=offline. Reconciler
// scans WorkerOnline only → no-op (worker not yet online).
func TestReconciler_FreshlyEnrolledNotInScope(t *testing.T) {
	repo, sink, clk, cleanup := setupHB(t)
	defer cleanup()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:         "w-fresh", Capabilities: []string{"fakeagent"}, EnrolledAt: clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	rec := service.NewHeartbeatReconciler(repo, sink, clk, 60*time.Second, 30*time.Second)
	clk.advance(5 * time.Minute) // way past stale threshold
	if err := rec.Tick(context.Background(), observability.Actor("user:test")); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "w-fresh")
	if got.Status() != workforce.WorkerOffline {
		t.Errorf("offline worker should stay offline; got %q", got.Status())
	}
}

// Worker went online via heartbeat, then stopped heartbeating > 60s
// → reconciler must flip offline + emit event.
func TestReconciler_OnlineWithStaleHeartbeatFlipsOffline(t *testing.T) {
	repo, sink, clk, cleanup := setupHB(t)
	defer cleanup()
	w, _ := workforce.NewWorker(workforce.NewWorkerInput{
		ID: "w-stale", Capabilities: []string{"x"}, EnrolledAt: clk.Now(),
	})
	_ = repo.Save(context.Background(), w)
	if err := repo.UpdateStatus(context.Background(), "w-stale", workforce.WorkerOffline, workforce.WorkerOnline, w.Version()); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateLastHeartbeatAt(context.Background(), "w-stale", clk.Now(), 0); err != nil {
		t.Fatal(err)
	}
	clk.advance(2 * time.Minute) // stale > 60s
	rec := service.NewHeartbeatReconciler(repo, sink, clk, 60*time.Second, 30*time.Second)
	if err := rec.Tick(context.Background(), observability.Actor("user:test")); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "w-stale")
	if got.Status() != workforce.WorkerOffline {
		t.Errorf("stale online worker should flip offline; got %q", got.Status())
	}
}

// Online worker with a recent heartbeat (< 60s old) must NOT be
// flipped to offline by the reconciler.
func TestReconciler_OnlineWithFreshHeartbeatStaysOnline(t *testing.T) {
	repo, sink, clk, cleanup := setupHB(t)
	defer cleanup()
	w, _ := workforce.NewWorker(workforce.NewWorkerInput{
		ID: "w-live", Capabilities: []string{"x"}, EnrolledAt: clk.Now(),
	})
	_ = repo.Save(context.Background(), w)
	_ = repo.UpdateStatus(context.Background(), "w-live", workforce.WorkerOffline, workforce.WorkerOnline, w.Version())
	_ = repo.UpdateLastHeartbeatAt(context.Background(), "w-live", clk.Now(), 0)
	clk.advance(30 * time.Second) // still well inside the 60s window
	rec := service.NewHeartbeatReconciler(repo, sink, clk, 60*time.Second, 30*time.Second)
	if err := rec.Tick(context.Background(), observability.Actor("user:test")); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "w-live")
	if got.Status() != workforce.WorkerOnline {
		t.Errorf("fresh-heartbeat worker should stay online; got %q", got.Status())
	}
}

