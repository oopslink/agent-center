package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsql "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

const (
	testOrg      = "org-1"
	otherOrg     = "org-2"
	testWorker   = "worker-1"
	onlineWorker = "worker-online"
)

var tNow = time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

type fixture struct {
	svc       *Service
	db        *sql.DB
	outbox    *outboxsql.OutboxRepo
	workers   *wfsql.WorkerRepo
	workItems *agentsql.WorkItemRepo
	clk       *clock.FakeClock
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(tNow)
	ob := outboxsql.NewOutboxRepo(db)
	workers := wfsql.NewWorkerRepo(db)
	workItems := agentsql.NewWorkItemRepo(db)
	svc := New(Deps{
		DB:        db,
		Agents:    agentsql.NewAgentRepo(db),
		WorkItems: workItems,
		Activity:  agentsql.NewActivityEventRepo(db),
		Workers:   workers,
		Outbox:    ob,
		IDGen:     idgen.NewGenerator(clk),
		Clock:     clk,
	})
	return &fixture{svc: svc, db: db, outbox: ob, workers: workers, workItems: workItems, clk: clk}
}

// seedWorker saves an OFFLINE worker in orgID.
func (f *fixture) seedWorker(t *testing.T, id, orgID string) {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             workforce.WorkerID(id),
		Capabilities:   []string{"claude-code"},
		EnrolledAt:     tNow,
		OrganizationID: orgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.workers.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

// seedOnlineWorker saves a worker in orgID and flips it online.
func (f *fixture) seedOnlineWorker(t *testing.T, id, orgID string) {
	t.Helper()
	f.seedWorker(t, id, orgID)
	if err := f.workers.UpdateStatus(context.Background(), workforce.WorkerID(id),
		workforce.WorkerOffline, workforce.WorkerOnline, 1); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) createAgent(t *testing.T, workerID string) agent.AgentID {
	t.Helper()
	id, err := f.svc.CreateAgent(context.Background(), CreateAgentCommand{
		OrganizationID: testOrg, Name: "coder", Description: "d", Model: "claude",
		CLI: "claude-code", WorkerID: workerID, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	return id
}

// outboxEvents returns all unprocessed outbox events.
func (f *fixture) outboxEvents(t *testing.T) []outbox.Event {
	t.Helper()
	evs, err := f.outbox.FetchUnprocessed(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func (f *fixture) outboxCount(t *testing.T) int { return len(f.outboxEvents(t)) }

// ---------------------------------------------------------------------------

func TestCreateAgent_Happy(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)

	id := f.createAgent(t, testWorker)

	a, err := f.svc.GetAgent(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != agent.LifecycleStopped {
		t.Fatalf("lifecycle = %s, want stopped", a.Lifecycle())
	}
	if a.WorkerID() != testWorker || a.OrganizationID() != testOrg {
		t.Fatalf("agent fields: %+v", a)
	}
	evs := f.outboxEvents(t)
	if len(evs) != 1 || evs[0].EventType != EvtAgentCreated {
		t.Fatalf("outbox = %+v, want one agent.created", evs)
	}
}

// v2.7 #181 / FINDING-F: agent creation rejects a cli the runtime can't
// execute. v2.7 allowlist = {claude-code}; empty / codex / opencode / unknown
// are 400 invalid_cli (mapped at the handler). Tx must not run (no outbox).
func TestCreateAgent_RejectsUnsupportedCLI(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	for _, cli := range []string{"", "codex", "opencode", "foobar", "claudecode"} {
		_, err := f.svc.CreateAgent(context.Background(), CreateAgentCommand{
			OrganizationID: testOrg, Name: "coder", Model: "claude", CLI: cli,
			WorkerID: testWorker, CreatedBy: "user:hayang",
		})
		if !errors.Is(err, agent.ErrUnsupportedCLI) {
			t.Fatalf("cli=%q: want ErrUnsupportedCLI, got %v", cli, err)
		}
	}
	if c := f.outboxCount(t); c != 0 {
		t.Fatalf("outbox count = %d, want 0 (no agent created)", c)
	}
}

func TestCreateAgent_AcceptsClaudeCode(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	if _, err := f.svc.CreateAgent(context.Background(), CreateAgentCommand{
		OrganizationID: testOrg, Name: "coder", Model: "claude", CLI: "claude-code",
		WorkerID: testWorker, CreatedBy: "user:hayang",
	}); err != nil {
		t.Fatalf("claude-code should be accepted, got %v", err)
	}
}

func TestCreateAgent_WorkerUnknown(t *testing.T) {
	f := newFixture(t)
	// no worker seeded
	_, err := f.svc.CreateAgent(context.Background(), CreateAgentCommand{
		OrganizationID: testOrg, Name: "coder", Model: "claude", CLI: "claude-code",
		WorkerID: "nope", CreatedBy: "user:hayang",
	})
	if !errors.Is(err, ErrWorkerNotInOrg) {
		t.Fatalf("want ErrWorkerNotInOrg, got %v", err)
	}
	if c := f.outboxCount(t); c != 0 {
		t.Fatalf("outbox count = %d, want 0 (tx rolled back)", c)
	}
}

func TestCreateAgent_WorkerCrossOrg(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, otherOrg) // worker exists but in a different org
	_, err := f.svc.CreateAgent(context.Background(), CreateAgentCommand{
		OrganizationID: testOrg, Name: "coder", Model: "claude", CLI: "claude-code",
		WorkerID: testWorker, CreatedBy: "user:hayang",
	})
	if !errors.Is(err, ErrWorkerNotInOrg) {
		t.Fatalf("want ErrWorkerNotInOrg, got %v", err)
	}
	if c := f.outboxCount(t); c != 0 {
		t.Fatalf("outbox count = %d, want 0", c)
	}
}

func TestLifecycle_StartStopRestart(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	base := f.outboxCount(t) // 1 (agent.created)

	// Start: stopped → running, emits one lifecycle event.
	if err := f.svc.StartAgent(context.Background(), id); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	a, _ := f.svc.GetAgent(context.Background(), id)
	if a.Lifecycle() != agent.LifecycleRunning {
		t.Fatalf("after start lifecycle = %s", a.Lifecycle())
	}
	if c := f.outboxCount(t); c != base+1 {
		t.Fatalf("outbox after start = %d, want %d", c, base+1)
	}
	if last := f.outboxEvents(t)[base]; last.EventType != EvtAgentLifecycleChanged {
		t.Fatalf("last event = %s, want lifecycle_changed", last.EventType)
	}

	// Restart: running → running, version bumps.
	verBefore := a.Version()
	if err := f.svc.RestartAgent(context.Background(), id); err != nil {
		t.Fatalf("RestartAgent: %v", err)
	}
	a, _ = f.svc.GetAgent(context.Background(), id)
	if a.Version() <= verBefore {
		t.Fatalf("version did not bump on restart: %d <= %d", a.Version(), verBefore)
	}
	if a.Lifecycle() != agent.LifecycleRunning {
		t.Fatalf("after restart lifecycle = %s", a.Lifecycle())
	}

	// Stop: running → stopping.
	if err := f.svc.StopAgent(context.Background(), id); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}
	a, _ = f.svc.GetAgent(context.Background(), id)
	if a.Lifecycle() != agent.LifecycleStopping {
		t.Fatalf("after stop lifecycle = %s", a.Lifecycle())
	}
	// created + start + restart + stop = base+3
	if c := f.outboxCount(t); c != base+3 {
		t.Fatalf("outbox after stop = %d, want %d", c, base+3)
	}
}

func TestLifecycle_IllegalTransition_NoEvent(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	if err := f.svc.StartAgent(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	countBefore := f.outboxCount(t)

	// Start an already-running agent → illegal, rejected by the AR.
	err := f.svc.StartAgent(context.Background(), id)
	if !errors.Is(err, agent.ErrIllegalLifecycle) {
		t.Fatalf("want ErrIllegalLifecycle, got %v", err)
	}
	// Same-tx purity: a failed op appends NO event.
	if c := f.outboxCount(t); c != countBefore {
		t.Fatalf("outbox changed on failed op: %d != %d", c, countBefore)
	}
	// Lifecycle unchanged.
	a, _ := f.svc.GetAgent(context.Background(), id)
	if a.Lifecycle() != agent.LifecycleRunning {
		t.Fatalf("lifecycle mutated on failed op: %s", a.Lifecycle())
	}
}

func TestResetAgent(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	base := f.outboxCount(t)

	// confirm=false → ErrResetNotConfirmed, no event, no mutation.
	if err := f.svc.ResetAgent(context.Background(), id, agent.ResetMemory, false); !errors.Is(err, ErrResetNotConfirmed) {
		t.Fatalf("want ErrResetNotConfirmed, got %v", err)
	}
	if c := f.outboxCount(t); c != base {
		t.Fatalf("outbox changed on unconfirmed reset: %d != %d", c, base)
	}

	// confirm=true → lifecycle resetting + event carries scope.
	if err := f.svc.ResetAgent(context.Background(), id, agent.ResetAll, true); err != nil {
		t.Fatalf("ResetAgent confirm: %v", err)
	}
	a, _ := f.svc.GetAgent(context.Background(), id)
	if a.Lifecycle() != agent.LifecycleResetting {
		t.Fatalf("lifecycle = %s, want resetting", a.Lifecycle())
	}
	evs := f.outboxEvents(t)
	last := evs[len(evs)-1]
	if last.EventType != EvtAgentLifecycleChanged {
		t.Fatalf("last event = %s", last.EventType)
	}
	if !strings.Contains(last.Payload, `"reset_scope":"all"`) {
		t.Fatalf("event payload missing reset scope: %s", last.Payload)
	}
}

func TestAvailability(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Agent on an ONLINE worker.
	f.seedOnlineWorker(t, onlineWorker, testOrg)
	id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "a", Model: "claude", CLI: "claude-code",
		WorkerID: onlineWorker, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}

	// stopped → unavailable (regardless of worker).
	a, _ := f.svc.GetAgent(ctx, id)
	if av, _ := f.svc.Availability(ctx, a); av != agent.Unavailable {
		t.Fatalf("stopped availability = %s, want unavailable", av)
	}

	// running + no active work + online worker → available.
	if err := f.svc.StartAgent(ctx, id); err != nil {
		t.Fatal(err)
	}
	a, _ = f.svc.GetAgent(ctx, id)
	if av, _ := f.svc.Availability(ctx, a); av != agent.Available {
		t.Fatalf("running/no-work availability = %s, want available", av)
	}

	// running + active work item → busy.
	wi, err := agent.NewWorkItem(agent.NewWorkItemInput{
		ID: "wi-1", AgentID: id, TaskRef: "task:t-1", CreatedAt: tNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.workItems.Save(ctx, wi); err != nil {
		t.Fatal(err)
	}
	if err := wi.Activate(tNow); err != nil {
		t.Fatal(err)
	}
	if err := f.workItems.Update(ctx, wi); err != nil {
		t.Fatal(err)
	}
	a, _ = f.svc.GetAgent(ctx, id)
	if av, _ := f.svc.Availability(ctx, a); av != agent.Busy {
		t.Fatalf("running/active-work availability = %s, want busy", av)
	}

	// offline worker → unavailable even when running.
	offID, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "b", Model: "claude", CLI: "claude-code",
		WorkerID: onlineWorker, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartAgent(ctx, offID); err != nil {
		t.Fatal(err)
	}
	// Take the worker offline.
	w, _ := f.workers.FindByID(ctx, workforce.WorkerID(onlineWorker))
	if err := f.workers.UpdateStatus(ctx, workforce.WorkerID(onlineWorker),
		workforce.WorkerOnline, workforce.WorkerOffline, w.Version()); err != nil {
		t.Fatal(err)
	}
	off, _ := f.svc.GetAgent(ctx, offID)
	if av, _ := f.svc.Availability(ctx, off); av != agent.Unavailable {
		t.Fatalf("offline-worker availability = %s, want unavailable", av)
	}
}

// TestMarkAgentFailed_CascadesInflightWorkItems pins the v2.7 GATE-7 Mode-B B3
// cascade: agent terminal-failed → its in-flight WorkItems (active + waiting_input)
// → failed atomically, terminal ones untouched. Also asserts the STRUCTURAL guard:
// the general feedback path (MarkWorkItemState "failed") on a waiting_input WI is
// STILL rejected — only the agent-death cascade may move waiting_input→failed.
func TestMarkAgentFailed_CascadesInflightWorkItems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	if err := f.svc.StartAgent(ctx, id); err != nil { // → running (MarkFailed needs running/error)
		t.Fatal(err)
	}

	mkWI := func(wiID string, prep func(*agent.AgentWorkItem)) {
		wi, err := agent.NewWorkItem(agent.NewWorkItemInput{ID: wiID, AgentID: id, TaskRef: "task:" + wiID, CreatedAt: tNow})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.workItems.Save(ctx, wi); err != nil {
			t.Fatal(err)
		}
		prep(wi)
		if err := f.workItems.Update(ctx, wi); err != nil {
			t.Fatal(err)
		}
	}
	mkWI("wi-active", func(w *agent.AgentWorkItem) { _ = w.Activate(tNow) })
	mkWI("wi-wait", func(w *agent.AgentWorkItem) { _ = w.Activate(tNow); _ = w.WaitInput(tNow) })
	mkWI("wi-done", func(w *agent.AgentWorkItem) { _ = w.Activate(tNow); _ = w.Done(tNow) })

	// Structural guard: the GENERAL feedback path must STILL reject
	// waiting_input→failed (the edge is not globally open).
	if err := f.svc.MarkWorkItemState(ctx, id, "wi-wait", WorkItemFeedbackFailed, tNow); err != agent.ErrWorkItemIllegalMove {
		t.Fatalf("general MarkWorkItemState(failed) on waiting_input must stay illegal, got %v", err)
	}

	// Terminal: agent → failed cascades ALL in-flight WIs → failed.
	if err := f.svc.MarkAgentFailed(ctx, id, "crash-loop", tNow); err != nil {
		t.Fatal(err)
	}
	a, _ := f.svc.GetAgent(ctx, id)
	if a.Lifecycle() != agent.LifecycleFailed || a.LifecycleError() != "crash-loop" {
		t.Fatalf("agent want failed+cause, got %s / %q", a.Lifecycle(), a.LifecycleError())
	}
	items, _ := f.svc.ListWorkItems(ctx, id)
	got := map[string]agent.WorkItemStatus{}
	for _, wi := range items {
		got[wi.ID()] = wi.Status()
	}
	if got["wi-active"] != agent.WorkItemFailed {
		t.Fatalf("active WI must cascade → failed, got %s", got["wi-active"])
	}
	if got["wi-wait"] != agent.WorkItemFailed {
		t.Fatalf("waiting_input WI must cascade → failed, got %s", got["wi-wait"])
	}
	if got["wi-done"] != agent.WorkItemDone {
		t.Fatalf("terminal (done) WI must be untouched, got %s", got["wi-done"])
	}
}

func TestReads(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)

	// ListAgents scopes by org.
	in, err := f.svc.ListAgents(ctx, testOrg)
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 1 || in[0].ID() != id {
		t.Fatalf("ListAgents(org) = %+v", in)
	}
	if other, _ := f.svc.ListAgents(ctx, otherOrg); len(other) != 0 {
		t.Fatalf("ListAgents(otherOrg) = %d, want 0", len(other))
	}

	// GetAgent.
	if a, err := f.svc.GetAgent(ctx, id); err != nil || a.ID() != id {
		t.Fatalf("GetAgent = %v, %v", a, err)
	}

	// Seed a work item + activity event, then list.
	wi, _ := agent.NewWorkItem(agent.NewWorkItemInput{ID: "wi-1", AgentID: id, TaskRef: "task:t-1", CreatedAt: tNow})
	if err := f.workItems.Save(ctx, wi); err != nil {
		t.Fatal(err)
	}
	items, err := f.svc.ListWorkItems(ctx, id)
	if err != nil || len(items) != 1 || items[0].ID() != "wi-1" {
		t.Fatalf("ListWorkItems = %+v, %v", items, err)
	}

	ev, _ := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID: "ev-1", AgentID: id, EventType: "status", Payload: `{"x":1}`, OccurredAt: tNow,
	})
	if err := agentsql.NewActivityEventRepo(f.db).Append(ctx, ev); err != nil {
		t.Fatal(err)
	}
	acts, err := f.svc.ListActivity(ctx, id, 0) // limit<=0 defaults to 100
	if err != nil || len(acts) != 1 || acts[0].ID() != "ev-1" {
		t.Fatalf("ListActivity = %+v, %v", acts, err)
	}
}

func TestNew_DefaultsClock(t *testing.T) {
	// New with nil Clock falls back to SystemClock (no panic on Now()).
	s := New(Deps{})
	if s.clock == nil {
		t.Fatal("clock not defaulted")
	}
	_ = s.clock.Now()
}
