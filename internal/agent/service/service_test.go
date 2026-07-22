package service

import (
	"context"
	"database/sql"
	"encoding/json"
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
	svc     *Service
	db      *sql.DB
	outbox  *outboxsql.OutboxRepo
	workers *wfsql.WorkerRepo
	clk     *clock.FakeClock
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
	svc := New(Deps{
		DB:       db,
		Agents:   agentsql.NewAgentRepo(db),
		Activity: agentsql.NewActivityEventRepo(db),
		Skills:   agentsql.NewInstalledSkillRepo(db),
		Workers:  workers,
		Outbox:   ob,
		IDGen:    idgen.NewGenerator(clk),
		Clock:    clk,
	})
	return &fixture{svc: svc, db: db, outbox: ob, workers: workers, clk: clk}
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

// #181 / FINDING-F: agent creation rejects a cli the runtime can't execute.
// Allowlist = {claude-code, codex}; empty / opencode / unknown are 400
// invalid_cli (mapped at the handler). Tx must not run (no outbox).
func TestCreateAgent_RejectsUnsupportedCLI(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	for _, cli := range []string{"", "opencode", "foobar", "claudecode"} {
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

func TestCreateAgent_AcceptsCodex(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	if _, err := f.svc.CreateAgent(context.Background(), CreateAgentCommand{
		OrganizationID: testOrg, Name: "coder", Model: "gpt-5", CLI: "codex",
		WorkerID: testWorker, CreatedBy: "user:hayang",
	}); err != nil {
		t.Fatalf("codex should be accepted, got %v", err)
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

// ADR-0049 §5 update (k8s live-config): a config edit to a RUNNING agent emits a
// lifecycle event so the reconcile projector live-propagates the new config to the
// isolated agent-runtime (no restart). A config edit to a STOPPED agent stays
// persist-only (config applies on next spawn) — emitting would spawn a stopped agent.
func TestUpdateAgentConfig_LivePropagatesOnlyWhenRunning(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)

	cfg := func(model string) UpdateAgentConfigCommand {
		return UpdateAgentConfigCommand{
			Model: model, CLI: "claude-code", Reasoning: "medium",
			AllowedExecutors: []agent.ExecutorProfile{{CLI: "claude-code", Model: model}},
		}
	}

	// STOPPED: config edit is persist-only — no new outbox event.
	base := f.outboxCount(t)
	if err := f.svc.UpdateAgentConfig(context.Background(), id, cfg("opus-4-8")); err != nil {
		t.Fatalf("UpdateAgentConfig (stopped): %v", err)
	}
	if c := f.outboxCount(t); c != base {
		t.Fatalf("stopped config edit emitted %d events, want 0 (persist-only)", c-base)
	}

	// Start → running.
	if err := f.svc.StartAgent(context.Background(), id); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	base = f.outboxCount(t)
	a, _ := f.svc.GetAgent(context.Background(), id)
	verBefore := a.Version()

	// RUNNING: config edit emits exactly one lifecycle_changed carrying the new config.
	if err := f.svc.UpdateAgentConfig(context.Background(), id, cfg("claude-opus-4-8")); err != nil {
		t.Fatalf("UpdateAgentConfig (running): %v", err)
	}
	if c := f.outboxCount(t); c != base+1 {
		t.Fatalf("running config edit emitted %d events, want 1", c-base)
	}
	last := f.outboxEvents(t)[base]
	if last.EventType != EvtAgentLifecycleChanged {
		t.Fatalf("event = %s, want lifecycle_changed", last.EventType)
	}
	// The event must carry the NEW model so the reconcile pushes fresh config downstream.
	if !strings.Contains(last.Payload, "claude-opus-4-8") {
		t.Errorf("event payload missing new model claude-opus-4-8: %s", last.Payload)
	}
	// Version bumped → the projector's version-keyed idempotency won't dedupe the push.
	a, _ = f.svc.GetAgent(context.Background(), id)
	if a.Version() <= verBefore {
		t.Errorf("version did not bump on config edit: %d <= %d", a.Version(), verBefore)
	}
	if a.Lifecycle() != agent.LifecycleRunning {
		t.Errorf("config edit changed lifecycle to %s, want running", a.Lifecycle())
	}
}

// T338: each user-triggered lifecycle action records a "lifecycle" activity event
// (event=started/restarted/stopped/reset) so it shows in the AgentDetail timeline.
func TestLifecycle_RecordsActivity(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	id := f.createAgent(t, testWorker)
	ctx := context.Background()
	if err := f.svc.StartAgent(ctx, id); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if err := f.svc.RestartAgent(ctx, id); err != nil {
		t.Fatalf("RestartAgent: %v", err)
	}
	if err := f.svc.StopAgent(ctx, id); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}

	events, err := agentsql.NewActivityEventRepo(f.db).ListByAgent(ctx, id, 100, "")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	got := map[string]bool{}
	for _, e := range events {
		if e.EventType() != agent.EventTypeLifecycle {
			continue
		}
		var p struct {
			Event string `json:"event"`
		}
		_ = json.Unmarshal([]byte(e.Payload()), &p)
		got[p.Event] = true
	}
	for _, want := range []string{"started", "restarted", "stopped"} {
		if !got[want] {
			t.Errorf("missing lifecycle activity event %q (got %v)", want, got)
		}
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

	// v2.14.0 F7 (issue I14): the "running + active work item → busy" assertion was
	// removed — AgentWorkItem retired, so Availability no longer reflects an in-flight
	// work item (busy is now an observable of the pm Task model, not lifecycle-derived).

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

// v2.14.0 F7 (issue I14): TestMarkAgentFailed_CascadesInflightWorkItems was removed —
// AgentWorkItem retired, so MarkAgentFailed no longer cascades in-flight work items
// to failed. A dead agent's stuck tasks are now recovered by the F3 execution-lease
// checker (Task.Block on lease expiry / reassign), not an inline WorkItem cascade.

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

	// v2.14.0 F7 (issue I14): the seed-WorkItem + ListWorkItems assertions were
	// removed — AgentWorkItem retired (the agent's work queue is now the pm Task model).

	// Seed an activity event, then list.
	ev, _ := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID: "ev-1", AgentID: id, EventType: "status", Payload: `{"x":1}`, OccurredAt: tNow,
	})
	if err := agentsql.NewActivityEventRepo(f.db).Append(ctx, ev); err != nil {
		t.Fatal(err)
	}
	acts, err := f.svc.ListActivity(ctx, id, 0, "") // limit<=0 = unlimited (#274), before="" = newest
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

// TestAllowedExecutors_ServiceRoundTrip covers v2.18.1 BE-1 at the service layer:
// create-with-executors persists the authoritative list + derived model mirror; a
// legacy allowed_models-only update is converted to {cli, model} via the agent's
// cli; and an invalid executor is rejected with ErrInvalidExecutorProfile.
func TestAllowedExecutors_ServiceRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	ctx := context.Background()

	// Create with an authoritative {cli,model} list.
	id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "coder", CLI: "claude-code", WorkerID: testWorker,
		CreatedBy: "user:a", MaxConcurrentTasks: 2,
		AllowedExecutors: []agent.ExecutorProfile{
			{CLI: "claude-code", Model: "opus"},
			{CLI: "codex", Model: "gpt-5-codex"},
		},
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	a, err := f.svc.GetAgent(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Profile().AllowedExecutors) != 2 || !a.Profile().ConcurrencyEnabled() {
		t.Fatalf("create: executors=%v enabled=%v", a.Profile().AllowedExecutors, a.Profile().ConcurrencyEnabled())
	}
	if got := a.Profile().AllowedModels; len(got) != 2 || got[0] != "opus" || got[1] != "gpt-5-codex" {
		t.Fatalf("create: derived allowed_models = %v, want [opus gpt-5-codex]", got)
	}

	// Legacy update: only allowed_models given, cli=codex → lifted to codex executors.
	if err := f.svc.UpdateAgentConfig(ctx, id, UpdateAgentConfigCommand{
		CLI: "codex", MaxConcurrentTasks: 1, AllowedModels: []string{"gpt-5", "gpt-5"}, // dup collapses
	}); err != nil {
		t.Fatalf("UpdateAgentConfig (legacy models): %v", err)
	}
	a, _ = f.svc.GetAgent(ctx, id)
	xe := a.Profile().AllowedExecutors
	if len(xe) != 1 || xe[0] != (agent.ExecutorProfile{CLI: "codex", Model: "gpt-5"}) {
		t.Fatalf("legacy convert: executors = %v, want one {codex, gpt-5}", xe)
	}

	// Invalid executor → ErrInvalidExecutorProfile (maps to 400 at the API).
	err = f.svc.UpdateAgentConfig(ctx, id, UpdateAgentConfigCommand{
		CLI: "claude-code", MaxConcurrentTasks: 1,
		AllowedExecutors: []agent.ExecutorProfile{{CLI: "opencode", Model: "x"}},
	})
	if !errors.Is(err, agent.ErrInvalidExecutorProfile) {
		t.Fatalf("invalid executor err = %v, want ErrInvalidExecutorProfile", err)
	}
}

// TestAutoAssignable_ServiceDefaults covers v2.18.3 BE-1: CreateAgent defaults
// auto_assignable to true (nil → assignable); an explicit false opts out;
// UpdateAgentConfig preserves the value when the field is omitted (nil) and flips it
// when present.
func TestAutoAssignable_ServiceDefaults(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	ctx := context.Background()

	// Create without the field → default true.
	id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "a", CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id); !a.Profile().AutoAssignable {
		t.Fatal("create without field → want auto_assignable=true (default)")
	}

	// Create with explicit false → opted out.
	no := false
	id2, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "b", CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:a",
		AutoAssignable: &no,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id2); a.Profile().AutoAssignable {
		t.Fatal("create with false → want auto_assignable=false")
	}

	// Update omitting the field (nil) → preserve the existing false.
	if err := f.svc.UpdateAgentConfig(ctx, id2, UpdateAgentConfigCommand{CLI: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id2); a.Profile().AutoAssignable {
		t.Fatal("update nil → want preserved false")
	}

	// Update with explicit true → flips back on.
	yes := true
	if err := f.svc.UpdateAgentConfig(ctx, id2, UpdateAgentConfigCommand{CLI: "claude-code", AutoAssignable: &yes}); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id2); !a.Profile().AutoAssignable {
		t.Fatal("update true → want auto_assignable=true")
	}
}

// TestJudgeEnabled_ServiceRoundTrip covers T950 ②: the per-agent LLM-judge opt-in is a
// real producer — CreateAgent defaults it OFF, an explicit true opts in, and
// UpdateAgentConfig preserves it when the field is omitted (nil) and flips it when
// present. This proves the config-surface → command → Profile → persistence round-trip
// (the source half of the producer chain that feeds ExecutorConfig.JudgeEnabled).
func TestJudgeEnabled_ServiceRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	ctx := context.Background()

	// Create without the field → default OFF (byte-identical routing).
	id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "a", CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id); a.Profile().JudgeEnabled {
		t.Fatal("create without field → want judge_enabled=false (default OFF)")
	}

	// Create with explicit true → opted in.
	id2, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "b", CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:a",
		JudgeEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id2); !a.Profile().JudgeEnabled {
		t.Fatal("create with true → want judge_enabled=true")
	}

	// Update omitting the field (nil) → preserve the existing true.
	if err := f.svc.UpdateAgentConfig(ctx, id2, UpdateAgentConfigCommand{CLI: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id2); !a.Profile().JudgeEnabled {
		t.Fatal("update nil → want preserved true")
	}

	// Update with explicit false → flips OFF.
	no := false
	if err := f.svc.UpdateAgentConfig(ctx, id2, UpdateAgentConfigCommand{CLI: "claude-code", JudgeEnabled: &no}); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.svc.GetAgent(ctx, id2); a.Profile().JudgeEnabled {
		t.Fatal("update false → want judge_enabled=false")
	}
}

func TestExecutorGitWorktree_ServicePreserveAndOverride(t *testing.T) {
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	ctx := context.Background()
	id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "worktree", CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if err := f.svc.UpdateAgentConfig(ctx, id, UpdateAgentConfigCommand{CLI: "claude-code", ExecutorGitWorktree: &yes}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.GetAgent(ctx, id); !got.Profile().ExecutorGitWorktree {
		t.Fatal("explicit true was not persisted")
	}
	if err := f.svc.UpdateAgentConfig(ctx, id, UpdateAgentConfigCommand{CLI: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.GetAgent(ctx, id); !got.Profile().ExecutorGitWorktree {
		t.Fatal("omitted field did not preserve true")
	}
}
