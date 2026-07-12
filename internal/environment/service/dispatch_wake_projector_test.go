package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// dispatchHarness wires a DispatchWakeProjector over a REAL in-memory ControlLog (so the
// AppendCommand idempotency is exercised for real) with programmable target resolvers.
type dispatchHarness struct {
	proj    *DispatchWakeProjector
	control *environment.ControlLog
	ctx     context.Context

	assignFn func(ctx context.Context, assigneeRef, taskID string) (DispatchWakeTarget, bool, error)
	repushFn func(ctx context.Context, assigneeRef, finishedTaskID, status, prevStatus string) (DispatchWakeTarget, bool, error)
}

func newDispatchHarness(t *testing.T) *dispatchHarness {
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
	control := environment.NewControlLog(envsql.NewControlEventRepo(db), idgen.NewGenerator(clk), clk)
	h := &dispatchHarness{control: control, ctx: context.Background()}
	h.proj = NewDispatchWakeProjector(DispatchWakeProjectorDeps{
		ControlLog: control,
		AssignTarget: func(ctx context.Context, a, tk string) (DispatchWakeTarget, bool, error) {
			return h.assignFn(ctx, a, tk)
		},
		RepushTarget: func(ctx context.Context, a, tk, st, prev string) (DispatchWakeTarget, bool, error) {
			return h.repushFn(ctx, a, tk, st, prev)
		},
	})
	return h
}

func (h *dispatchHarness) commands(t *testing.T, worker string) []*environment.WorkerControlEvent {
	t.Helper()
	cmds, err := h.control.CommandsAfter(h.ctx, environment.WorkerID(worker), 0)
	if err != nil {
		t.Fatalf("CommandsAfter: %v", err)
	}
	return cmds
}

func taskEvent(id, evtType, assignee, taskID, status, prev string) outbox.Event {
	pl, _ := json.Marshal(map[string]string{
		"task_id": taskID, "assignee": assignee, "status": status, "prev_status": prev,
	})
	return outbox.Event{ID: id, EventType: evtType, Payload: string(pl)}
}

// (a) assign: an agent assignee whose target task resolves → exactly one work_available on
// its worker stream, carrying the ENTITY agent id + task id, keyed under "assign".
func TestDispatchWake_Assign_EmitsWorkAvailable(t *testing.T) {
	h := newDispatchHarness(t)
	h.assignFn = func(_ context.Context, a, tk string) (DispatchWakeTarget, bool, error) {
		return DispatchWakeTarget{WorkerID: "W1", AgentID: "entity-A", TaskID: tk}, true, nil
	}
	ev := taskEvent("e1", pmservice.EvtTaskAssigned, "agent:mem-A", "T1", "open", "")
	if err := h.proj.Project(h.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := h.commands(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 work_available, got %d", len(cmds))
	}
	if cmds[0].CommandType() != commandTypeWorkAvailable {
		t.Fatalf("command type = %q, want %q", cmds[0].CommandType(), commandTypeWorkAvailable)
	}
	var pl sweepWakePayload
	if err := json.Unmarshal([]byte(cmds[0].Payload()), &pl); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if pl.AgentID != "entity-A" || pl.TaskID != "T1" {
		t.Fatalf("payload = %+v, want entity-A/T1", pl)
	}
}

// (b) reassign: the projector wakes the assignee carried on the event (which the producer
// sets to the NEW assignee) — the previous assignee is never passed to the resolver.
func TestDispatchWake_Reassign_WakesEventAssigneeOnly(t *testing.T) {
	h := newDispatchHarness(t)
	var sawAssignee string
	h.assignFn = func(_ context.Context, a, tk string) (DispatchWakeTarget, bool, error) {
		sawAssignee = a
		return DispatchWakeTarget{WorkerID: "W-new", AgentID: "entity-new", TaskID: tk}, true, nil
	}
	ev := taskEvent("e2", pmservice.EvtTaskReassigned, "agent:new", "T9", "open", "")
	if err := h.proj.Project(h.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if sawAssignee != "agent:new" {
		t.Fatalf("resolver saw assignee %q, want the NEW assignee agent:new", sawAssignee)
	}
	if got := h.commands(t, "W-new"); len(got) != 1 {
		t.Fatalf("new assignee must get 1 wake, got %d", len(got))
	}
}

// (c) re-push: a freed agent with a resolved next task → one wake keyed under "repush".
func TestDispatchWake_StateChanged_RepushesNext(t *testing.T) {
	h := newDispatchHarness(t)
	h.repushFn = func(_ context.Context, a, tk, st, prev string) (DispatchWakeTarget, bool, error) {
		return DispatchWakeTarget{WorkerID: "W2", AgentID: "entity-B", TaskID: "T-next"}, true, nil
	}
	ev := taskEvent("e3", pmservice.EvtTaskStateChanged, "agent:B", "T-done", "completed", "running")
	if err := h.proj.Project(h.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := h.commands(t, "W2")
	if len(cmds) != 1 {
		t.Fatalf("want 1 re-push wake, got %d", len(cmds))
	}
	var pl sweepWakePayload
	_ = json.Unmarshal([]byte(cmds[0].Payload()), &pl)
	if pl.TaskID != "T-next" {
		t.Fatalf("re-push must anchor on the NEXT task, got %q", pl.TaskID)
	}
}

// re-push resolver says "not freed / nothing next" → no wake.
func TestDispatchWake_StateChanged_NoNext_NoWake(t *testing.T) {
	h := newDispatchHarness(t)
	h.repushFn = func(_ context.Context, a, tk, st, prev string) (DispatchWakeTarget, bool, error) {
		return DispatchWakeTarget{}, false, nil
	}
	ev := taskEvent("e4", pmservice.EvtTaskStateChanged, "agent:B", "T-done", "running", "open")
	if err := h.proj.Project(h.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	// No worker should have received anything.
	for _, w := range []string{"W1", "W2", "W-new"} {
		if got := h.commands(t, w); len(got) != 0 {
			t.Fatalf("worker %s must get no wake, got %d", w, len(got))
		}
	}
}

// A non-agent (user:) assignee is short-circuited before the resolver — no wake, resolver
// never consulted.
func TestDispatchWake_NonAgentAssignee_Skipped(t *testing.T) {
	h := newDispatchHarness(t)
	called := false
	h.assignFn = func(_ context.Context, a, tk string) (DispatchWakeTarget, bool, error) {
		called = true
		return DispatchWakeTarget{}, false, nil
	}
	ev := taskEvent("e5", pmservice.EvtTaskAssigned, "user:human-1", "T1", "open", "")
	if err := h.proj.Project(h.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if called {
		t.Fatal("resolver must not be called for a user: assignee")
	}
}

// Idempotency 1: re-projecting the SAME assign event folds to one control row (UNIQUE key).
func TestDispatchWake_Assign_Idempotent_OnReplay(t *testing.T) {
	h := newDispatchHarness(t)
	h.assignFn = func(_ context.Context, a, tk string) (DispatchWakeTarget, bool, error) {
		return DispatchWakeTarget{WorkerID: "W1", AgentID: "entity-A", TaskID: tk}, true, nil
	}
	ev := taskEvent("e6", pmservice.EvtTaskAssigned, "agent:A", "T1", "open", "")
	for i := 0; i < 3; i++ {
		if err := h.proj.Project(h.ctx, ev); err != nil {
			t.Fatalf("Project #%d: %v", i, err)
		}
	}
	if got := h.commands(t, "W1"); len(got) != 1 {
		t.Fatalf("replayed assign must fold to 1 wake, got %d", len(got))
	}
}

// Idempotency 2: two DIFFERENT done-events that resolve to the SAME next task fold to one
// wake (the re-push key omits the event id and anchors on agent+next-task).
func TestDispatchWake_Repush_FoldsConcurrentDoneEvents(t *testing.T) {
	h := newDispatchHarness(t)
	h.repushFn = func(_ context.Context, a, tk, st, prev string) (DispatchWakeTarget, bool, error) {
		return DispatchWakeTarget{WorkerID: "W2", AgentID: "entity-B", TaskID: "T-next"}, true, nil
	}
	// task X done and task Y done, both for agent B, both resolving next == T-next.
	_ = h.proj.Project(h.ctx, taskEvent("ex", pmservice.EvtTaskStateChanged, "agent:B", "T-X", "completed", "running"))
	_ = h.proj.Project(h.ctx, taskEvent("ey", pmservice.EvtTaskStateChanged, "agent:B", "T-Y", "completed", "running"))
	if got := h.commands(t, "W2"); len(got) != 1 {
		t.Fatalf("concurrent done-events to the same next task must fold to 1 wake, got %d", len(got))
	}
}

// issue-d118b5dc ① REPRO — plan-node-ready double fan-out on ONE task.
//
// When a plan node becomes ready, the SAME newly-ready task can be woken by TWO of this
// projector's triggers at once:
//   - (a) pm.task.assigned    — the ready-set dispatch assigns the ready node to the agent.
//   - (c) pm.task.state_changed — the PREDECESSOR node completing frees the agent's single
//     slot, and the re-push resolver picks the very same ready node as the agent's "next".
// Both resolve to the SAME entity/worker/task, but the two emit paths key the ControlLog
// AppendCommand under DIFFERENT idempotency schemes — assign includes the event id
// ("dispatch.wake:assign:<eid>:…"), repush omits it ("dispatch.wake:repush:…"). They do
// NOT collide, so the worker receives TWO agent.work_available commands for one task_id,
// each of which drives an unconditional SpawnExecutor fork downstream (executor_runtime.go
// DISPATCH-DECISION/DISPATCH-FORK-ENTRY). This is the structural fan-out the runtime logs
// are meant to catch — the mode/dedup XOR is missing at the SOURCE, not the sink.
//
// This test LOCKS the current (buggy) 2-command behavior so the eventual fix (unify the
// dedup anchor to agent+task regardless of trigger) has a failing assertion to flip.
func TestDispatchWake_Issue_d118b5dc_AssignAndRepush_DoubleFanout(t *testing.T) {
	h := newDispatchHarness(t)
	const worker, entity, readyTask = "W2", "entity-B", "T-ready"
	// (a) the ready node is assigned to agent B → wakes B for T-ready.
	h.assignFn = func(_ context.Context, a, tk string) (DispatchWakeTarget, bool, error) {
		return DispatchWakeTarget{WorkerID: worker, AgentID: entity, TaskID: tk}, true, nil
	}
	// (c) the predecessor completing frees B's slot; the re-push "next task" resolves to the
	// SAME ready node.
	h.repushFn = func(_ context.Context, a, tk, st, prev string) (DispatchWakeTarget, bool, error) {
		return DispatchWakeTarget{WorkerID: worker, AgentID: entity, TaskID: readyTask}, true, nil
	}
	// One ready event, both arms fire for the same agent+task.
	if err := h.proj.Project(h.ctx, taskEvent("assign-evt", pmservice.EvtTaskAssigned, "agent:B", readyTask, "open", "")); err != nil {
		t.Fatalf("assign Project: %v", err)
	}
	if err := h.proj.Project(h.ctx, taskEvent("done-evt", pmservice.EvtTaskStateChanged, "agent:B", "T-pred", "completed", "running")); err != nil {
		t.Fatalf("state_changed Project: %v", err)
	}

	cmds := h.commands(t, worker)
	// ROOT-CAUSE ASSERTION: two work_available for ONE ready task — the double fan-out.
	if len(cmds) != 2 {
		t.Fatalf("issue-d118b5dc ①: assign+repush for the SAME agent+task should currently "+
			"emit 2 work_available (non-colliding idempotency keys) — got %d; if this is now "+
			"1 the source-side dedup fix landed, retarget this lock", len(cmds))
	}
	for _, c := range cmds {
		var pl sweepWakePayload
		_ = json.Unmarshal([]byte(c.Payload()), &pl)
		if pl.AgentID != entity || pl.TaskID != readyTask {
			t.Fatalf("both wakes should target %s/%s (one ready task), got %s/%s",
				entity, readyTask, pl.AgentID, pl.TaskID)
		}
	}
}

// Dormant: nil resolvers make every trigger a graceful no-op (the relay can register the
// projector unconditionally).
func TestDispatchWake_NilResolvers_NoOp(t *testing.T) {
	p := NewDispatchWakeProjector(DispatchWakeProjectorDeps{})
	for _, ev := range []outbox.Event{
		taskEvent("e7", pmservice.EvtTaskAssigned, "agent:A", "T1", "open", ""),
		taskEvent("e8", pmservice.EvtTaskReassigned, "agent:A", "T1", "open", ""),
		taskEvent("e9", pmservice.EvtTaskStateChanged, "agent:A", "T1", "completed", "running"),
		{ID: "e10", EventType: "pm.task.created", Payload: "{}"},
	} {
		if err := p.Project(context.Background(), ev); err != nil {
			t.Fatalf("dormant Project(%s) must be a no-op, got %v", ev.EventType, err)
		}
	}
}
