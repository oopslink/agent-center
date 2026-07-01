package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"strconv"

	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// projectorFixture wires a real in-memory DB + ControlLog + the projector over
// one shared DB.
type projectorFixture struct {
	proj    *AgentControlProjector
	control *environment.ControlLog
	eventsR *envsql.ControlEventRepo
	ctx     context.Context
}

func newProjectorFixture(t *testing.T) *projectorFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	eventsR := envsql.NewControlEventRepo(db)
	control := environment.NewControlLog(eventsR, gen, clk)
	applied := outboxsql.NewAppliedRepo(db)
	proj := NewAgentControlProjector(db, control, applied, clk)
	return &projectorFixture{proj: proj, control: control, eventsR: eventsR, ctx: context.Background()}
}

// commandsFor returns the full control stream for a worker (offset 0 cursor).
func (f *projectorFixture) commandsFor(t *testing.T, workerID string) []*environment.WorkerControlEvent {
	t.Helper()
	cmds, err := f.control.CommandsAfter(f.ctx, environment.WorkerID(workerID), 0)
	if err != nil {
		t.Fatalf("CommandsAfter: %v", err)
	}
	return cmds
}

func lifecycleEvent(id, agentID, workerID, lifecycle string, version int, resetScope string) outbox.Event {
	scopeJSON := ""
	if resetScope != "" {
		scopeJSON = `,"reset_scope":"` + resetScope + `"`
	}
	payload := `{"agent_id":"` + agentID + `","worker_id":"` + workerID +
		`","lifecycle":"` + lifecycle + `","version":` + strconv.Itoa(version) + scopeJSON + `}`
	return outbox.Event{
		ID:        id,
		EventType: agentsvc.EvtAgentLifecycleChanged,
		Payload:   payload,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

func TestAgentControlProjector_EnqueuesReconcile(t *testing.T) {
	f := newProjectorFixture(t)
	e := lifecycleEvent("EV1", "AG1", "W1", "running", 2, "")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d", len(cmds))
	}
	c := cmds[0]
	if c.CommandType() != "agent.reconcile" {
		t.Fatalf("command_type = %q, want agent.reconcile", c.CommandType())
	}
	if c.IdempotencyKey() != "agent.lifecycle:AG1:2" {
		t.Fatalf("idempotency_key = %q", c.IdempotencyKey())
	}
	if !strings.Contains(c.Payload(), `"desired_lifecycle":"running"`) {
		t.Fatalf("payload missing desired_lifecycle: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"version":2`) {
		t.Fatalf("payload missing version: %s", c.Payload())
	}
	if !strings.Contains(c.Payload(), `"agent_id":"AG1"`) {
		t.Fatalf("payload missing agent_id: %s", c.Payload())
	}
}

// TestAgentControlProjector_PassesModel pins the v2.7 Model-plumbing slice-A
// passthrough: a lifecycle event carrying the agent's model is forwarded into the
// reconcile command (pure event-driven — no Agent-repo read). A model-less event
// omits it (additive/backward-compatible → daemon → claude default).
func TestAgentControlProjector_PassesModel(t *testing.T) {
	f := newProjectorFixture(t)
	withModel := outbox.Event{
		ID:        "EVM",
		EventType: agentsvc.EvtAgentLifecycleChanged,
		Payload:   `{"agent_id":"AG1","worker_id":"W1","lifecycle":"running","version":2,"model":"claude-proj-model"}`,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := f.proj.Project(f.ctx, withModel); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || !strings.Contains(cmds[0].Payload(), `"model":"claude-proj-model"`) {
		t.Fatalf("reconcile command must carry the model, got: %s", cmds[0].Payload())
	}

	// Model-less event → no model key in the command (omitempty, backward-compat).
	f2 := newProjectorFixture(t)
	if err := f2.proj.Project(f2.ctx, lifecycleEvent("EV1", "AG2", "W2", "running", 1, "")); err != nil {
		t.Fatalf("Project no-model: %v", err)
	}
	if c := f2.commandsFor(t, "W2"); len(c) != 1 || strings.Contains(c[0].Payload(), `"model"`) {
		t.Fatalf("model-less event must omit model, got: %s", c[0].Payload())
	}
}

// TestAgentControlProjector_PassesDisplayName (T469) pins the display_name passthrough:
// a lifecycle event carrying the agent's display_name is forwarded into the reconcile
// command so the worker→supervisor injects it as git author NAME (② AgentEnv seam). A
// display_name-less event omits it (additive → supervisor falls back to the ULID).
func TestAgentControlProjector_PassesDisplayName(t *testing.T) {
	f := newProjectorFixture(t)
	withName := outbox.Event{
		ID:        "EVDN",
		EventType: agentsvc.EvtAgentLifecycleChanged,
		Payload:   `{"agent_id":"AG1","worker_id":"W1","lifecycle":"running","version":2,"display_name":"agent-center-dev4"}`,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := f.proj.Project(f.ctx, withName); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || !strings.Contains(cmds[0].Payload(), `"display_name":"agent-center-dev4"`) {
		t.Fatalf("reconcile command must carry display_name, got: %s", cmds[0].Payload())
	}

	// display_name-less event → no display_name key (omitempty, backward-compat).
	f2 := newProjectorFixture(t)
	if err := f2.proj.Project(f2.ctx, lifecycleEvent("EV1", "AG2", "W2", "running", 1, "")); err != nil {
		t.Fatalf("Project no-display-name: %v", err)
	}
	if c := f2.commandsFor(t, "W2"); len(c) != 1 || strings.Contains(c[0].Payload(), `"display_name"`) {
		t.Fatalf("display_name-less event must omit display_name, got: %s", c[0].Payload())
	}
}

// TestAgentControlProjector_PassesEnvVars pins profile env passthrough: lifecycle
// events are the worker's start/reconcile source, so the projector must carry
// env_vars without reading the agent repository.
func TestAgentControlProjector_PassesEnvVars(t *testing.T) {
	f := newProjectorFixture(t)
	withEnv := outbox.Event{
		ID:        "EVE",
		EventType: agentsvc.EvtAgentLifecycleChanged,
		Payload:   `{"agent_id":"AG1","worker_id":"W1","lifecycle":"running","version":2,"env_vars":{"FOO":"bar","EMPTY":""}}`,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := f.proj.Project(f.ctx, withEnv); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || !strings.Contains(cmds[0].Payload(), `"env_vars":{"EMPTY":"","FOO":"bar"}`) {
		t.Fatalf("reconcile command must carry env_vars, got: %s", cmds[0].Payload())
	}

	f2 := newProjectorFixture(t)
	if err := f2.proj.Project(f2.ctx, lifecycleEvent("EV1", "AG2", "W2", "running", 1, "")); err != nil {
		t.Fatalf("Project no-env: %v", err)
	}
	if c := f2.commandsFor(t, "W2"); len(c) != 1 || strings.Contains(c[0].Payload(), `"env_vars"`) {
		t.Fatalf("env-less event must omit env_vars, got: %s", c[0].Payload())
	}
}

func TestAgentControlProjector_ResetScope(t *testing.T) {
	f := newProjectorFixture(t)
	e := lifecycleEvent("EV1", "AG1", "W1", "resetting", 5, "all")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0].Payload(), `"reset_scope":"all"`) {
		t.Fatalf("payload missing reset_scope: %s", cmds[0].Payload())
	}
	if !strings.Contains(cmds[0].Payload(), `"desired_lifecycle":"resetting"`) {
		t.Fatalf("payload missing desired_lifecycle: %s", cmds[0].Payload())
	}
}

// Restart (version bump) yields a NEW reconcile command.
func TestAgentControlProjector_RestartBumpsVersion(t *testing.T) {
	f := newProjectorFixture(t)
	if err := f.proj.Project(f.ctx, lifecycleEvent("EV1", "AG1", "W1", "running", 2, "")); err != nil {
		t.Fatalf("Project v2: %v", err)
	}
	if err := f.proj.Project(f.ctx, lifecycleEvent("EV2", "AG1", "W1", "running", 3, "")); err != nil {
		t.Fatalf("Project v3: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 2 {
		t.Fatalf("want 2 commands, got %d", len(cmds))
	}
	if cmds[0].Offset() != 1 || cmds[1].Offset() != 2 {
		t.Fatalf("offsets = %d,%d want 1,2", cmds[0].Offset(), cmds[1].Offset())
	}
	if cmds[0].IdempotencyKey() == cmds[1].IdempotencyKey() {
		t.Fatalf("idempotency keys must differ by version: %q", cmds[0].IdempotencyKey())
	}
	if cmds[0].IdempotencyKey() != "agent.lifecycle:AG1:2" || cmds[1].IdempotencyKey() != "agent.lifecycle:AG1:3" {
		t.Fatalf("unexpected keys: %q, %q", cmds[0].IdempotencyKey(), cmds[1].IdempotencyKey())
	}
}

// Re-delivery of the SAME outbox event ID enqueues only one command.
func TestAgentControlProjector_IdempotentRedelivery(t *testing.T) {
	f := newProjectorFixture(t)
	e := lifecycleEvent("EV1", "AG1", "W1", "running", 2, "")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("re-delivery must not duplicate: want 1 command, got %d", len(cmds))
	}
}

func TestAgentControlProjector_AgentCreatedSkipped(t *testing.T) {
	f := newProjectorFixture(t)
	e := outbox.Event{
		ID:        "EV1",
		EventType: agentsvc.EvtAgentCreated,
		Payload:   `{"agent_id":"AG1","worker_id":"W1","lifecycle":"stopped","version":1}`,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("agent.created must enqueue nothing, got %d", len(cmds))
	}
}

func TestAgentControlProjector_UnknownEventSkipped(t *testing.T) {
	f := newProjectorFixture(t)
	e := outbox.Event{ID: "EV1", EventType: "some.other.event", Payload: `{}`}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("unknown event must enqueue nothing, got %d", len(cmds))
	}
}

func TestAgentControlProjector_MalformedPayload(t *testing.T) {
	f := newProjectorFixture(t)
	e := outbox.Event{ID: "EV1", EventType: agentsvc.EvtAgentLifecycleChanged, Payload: `{not json`}
	if err := f.proj.Project(f.ctx, e); err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("malformed payload must enqueue nothing, got %d", len(cmds))
	}
}

func TestAgentControlProjector_Name(t *testing.T) {
	f := newProjectorFixture(t)
	if f.proj.Name() != "env-agent-control" {
		t.Fatalf("Name = %q", f.proj.Name())
	}
}
