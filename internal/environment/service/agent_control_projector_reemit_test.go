package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// reemitFixture wires the projector WITH the read-only workItems dep (PART ②) over
// one shared DB, so a test can seed work-items and assert the re-emit.
type reemitFixture struct {
	proj     *AgentControlProjector
	control  *environment.ControlLog
	wiRepo   *agentsql.WorkItemRepo
	taskRepo *pmsql.TaskRepo
	ctx      context.Context
}

func newReemitFixture(t *testing.T) *reemitFixture {
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
	control := environment.NewControlLog(envsql.NewControlEventRepo(db), gen, clk)
	applied := outboxsql.NewAppliedRepo(db)
	wiRepo := agentsql.NewWorkItemRepo(db)
	taskRepo := pmsql.NewTaskRepo(db)
	// #115 backfill: wire the tasks repo so the re-emit resolves the SAME brief
	// (title\n\ndesc) that pm enqueueWork captures.
	proj := NewAgentControlProjectorWithWork(db, control, applied, clk, wiRepo, taskRepo)
	return &reemitFixture{proj: proj, control: control, wiRepo: wiRepo, taskRepo: taskRepo, ctx: context.Background()}
}

// seedTask inserts a pm task so the re-emit can resolve its brief (title+desc).
// The taskRef "pm://tasks/<id>" on a seeded WorkItem must match this id.
func (f *reemitFixture) seedTask(t *testing.T, id, title, desc string) {
	t.Helper()
	at := time.Unix(1_700_000_000, 0).UTC()
	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: pm.TaskID(id), ProjectID: "P1", Title: title, Description: desc,
		CreatedBy: "user:a", CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.taskRepo.Save(f.ctx, tk); err != nil {
		t.Fatal(err)
	}
}

func (f *reemitFixture) commandsFor(t *testing.T, workerID string) []*environment.WorkerControlEvent {
	t.Helper()
	cmds, err := f.control.CommandsAfter(f.ctx, environment.WorkerID(workerID), 0)
	if err != nil {
		t.Fatalf("CommandsAfter: %v", err)
	}
	return cmds
}

// seedWorkItem inserts a work item for agentID in the given status (queued unless
// activated/parked). Active = create+Activate; waiting_input = active+WaitInput.
func (f *reemitFixture) seedWorkItem(t *testing.T, id, agentID, taskRef string, status agentpkg.WorkItemStatus) {
	t.Helper()
	at := time.Unix(1_700_000_000, 0).UTC()
	w, err := agentpkg.NewWorkItem(agentpkg.NewWorkItemInput{ID: id, AgentID: agentpkg.AgentID(agentID), TaskRef: taskRef, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	switch status {
	case agentpkg.WorkItemQueued:
		// leave queued
	case agentpkg.WorkItemActive:
		if err := w.Activate(at); err != nil {
			t.Fatal(err)
		}
	case agentpkg.WorkItemWaitingInput:
		if err := w.Activate(at); err != nil {
			t.Fatal(err)
		}
		if err := w.WaitInput(at); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("seedWorkItem: unsupported status %q", status)
	}
	if err := f.wiRepo.Save(f.ctx, w); err != nil {
		t.Fatal(err)
	}
}

// TestReemit_RunningEmitsReconcileThenWorkForReadyToDispatch is PART ②'s core
// acceptance: on lifecycle→running the projector appends the reconcile command
// FIRST, then an agent.work command for every READY-TO-DISPATCH (queued + active)
// WorkItem — in control-log order (session before work → no deadlock). waiting_input
// is skipped (wake path). QUEUED is the primary deliver-on-start case (the guard
// skipped its original enqueue); active-only would silently drop it (Tester #115
// outcome catch).
func TestReemit_RunningEmitsReconcileThenWorkForReadyToDispatch(t *testing.T) {
	f := newReemitFixture(t)
	// #115: seed the pm tasks so the re-emit backfills the SAME brief (title\n\ndesc)
	// that pm enqueueWork captures — t1 has a description (→ "title\n\ndesc"), t2 has
	// none (→ just "title"). An empty brief made claude greet generically = lost work.
	f.seedTask(t, "t1", "Fix login bug", "Users cannot log in after the v2.7 deploy.")
	f.seedTask(t, "t2", "Write release notes", "")
	f.seedWorkItem(t, "wi-active", "AG1", "pm://tasks/t1", agentpkg.WorkItemActive)
	f.seedWorkItem(t, "wi-queued", "AG1", "pm://tasks/t2", agentpkg.WorkItemQueued)
	f.seedWorkItem(t, "wi-waiting", "AG1", "pm://tasks/t3", agentpkg.WorkItemWaitingInput)

	e := lifecycleEvent("EV1", "AG1", "W1", "running", 2, "")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}

	cmds := f.commandsFor(t, "W1")
	// 1 reconcile + 2 work (queued + active); waiting_input skipped.
	if len(cmds) != 3 {
		t.Fatalf("want 3 commands (reconcile + queued + active work), got %d: %+v", len(cmds), cmds)
	}
	if cmds[0].CommandType() != "agent.reconcile" {
		t.Fatalf("cmds[0] type = %q, want agent.reconcile (must precede work)", cmds[0].CommandType())
	}
	reconcileOff := cmds[0].Offset()
	workKeys := map[string]bool{}
	briefByKey := map[string]string{}
	for _, c := range cmds[1:] {
		if c.CommandType() != "agent.work" {
			t.Fatalf("commands after reconcile must be agent.work, got %q", c.CommandType())
		}
		if c.Offset() <= reconcileOff {
			t.Fatalf("work offset %d must be > reconcile offset %d (session before work)", c.Offset(), reconcileOff)
		}
		workKeys[c.IdempotencyKey()] = true
		var pl workCommandPayload
		if err := json.Unmarshal([]byte(c.Payload()), &pl); err != nil {
			t.Fatalf("unmarshal work payload: %v", err)
		}
		briefByKey[c.IdempotencyKey()] = pl.Brief
	}
	if !workKeys["agent.work:wi-active"] || !workKeys["agent.work:wi-queued"] {
		t.Fatalf("re-emit must cover BOTH queued + active (ready-to-dispatch), got keys %v", workKeys)
	}
	if workKeys["agent.work:wi-waiting"] {
		t.Fatalf("waiting_input must NOT be re-emitted, got it in %v", workKeys)
	}
	// #115 CORE assertion: the re-emitted brief must be NON-EMPTY and equal the SAME
	// title\n\ndesc that pm enqueueWork.brief produces for the task (NOT "").
	const wantActiveBrief = "Fix login bug\n\nUsers cannot log in after the v2.7 deploy."
	if got := briefByKey["agent.work:wi-active"]; got != wantActiveBrief {
		t.Fatalf("re-emitted brief (with desc) = %q, want %q (title\\n\\ndesc, NOT empty — lost-work bug #115)", got, wantActiveBrief)
	}
	const wantQueuedBrief = "Write release notes" // desc empty → title only
	if got := briefByKey["agent.work:wi-queued"]; got != wantQueuedBrief {
		t.Fatalf("re-emitted brief (no desc) = %q, want %q (title only)", got, wantQueuedBrief)
	}
}

// TestReemit_NonRunningDoesNotEmitWork: a →stopped/→resetting/etc transition emits
// only the reconcile, never re-emits work.
func TestReemit_NonRunningDoesNotEmitWork(t *testing.T) {
	f := newReemitFixture(t)
	f.seedWorkItem(t, "wi-active", "AG1", "pm://tasks/t1", agentpkg.WorkItemActive)

	e := lifecycleEvent("EV1", "AG1", "W1", "stopping", 2, "")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.reconcile" {
		t.Fatalf("non-running transition must emit only reconcile, got %+v", cmds)
	}
}

// TestReemit_IdempotentOnReplay: re-delivering the SAME →running event ID is a
// no-op (AppliedStore dedups the source event) — no duplicate reconcile or work.
func TestReemit_IdempotentOnReplay(t *testing.T) {
	f := newReemitFixture(t)
	f.seedWorkItem(t, "wi-active", "AG1", "pm://tasks/t1", agentpkg.WorkItemActive)

	e := lifecycleEvent("EV1", "AG1", "W1", "running", 2, "")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2 (replay): %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 2 {
		t.Fatalf("replay of same event must not duplicate, got %d commands", len(cmds))
	}
}

// TestReemit_FlapDoesNotDoubleDeliverWork is CAVEAT 1: a lifecycle FLAP
// (running v2 → stopped v3 → running v4) yields DISTINCT →running outbox events
// the AppliedStore does NOT dedup. The re-emit must still NOT create a second
// agent.work for the same WI — the stable idempotency key "agent.work:<wi>"
// collapses the second append into the existing stream entry (same offset), so the
// daemon never re-pulls and never double-injects. We assert exactly ONE agent.work
// remains across the flap (the reconcile commands DO multiply — version bumps —
// which is correct; only work must not duplicate).
func TestReemit_FlapDoesNotDoubleDeliverWork(t *testing.T) {
	f := newReemitFixture(t)
	f.seedWorkItem(t, "wi-active", "AG1", "pm://tasks/t1", agentpkg.WorkItemActive)

	// First →running (v2): reconcile + work.
	if err := f.proj.Project(f.ctx, lifecycleEvent("EV1", "AG1", "W1", "running", 2, "")); err != nil {
		t.Fatalf("Project running v2: %v", err)
	}
	// Stop (v3): reconcile only.
	if err := f.proj.Project(f.ctx, lifecycleEvent("EV2", "AG1", "W1", "stopping", 3, "")); err != nil {
		t.Fatalf("Project stopping v3: %v", err)
	}
	// Flap back to →running (v4): reconcile (new version) + work re-emit.
	if err := f.proj.Project(f.ctx, lifecycleEvent("EV3", "AG1", "W1", "running", 4, "")); err != nil {
		t.Fatalf("Project running v4: %v", err)
	}

	cmds := f.commandsFor(t, "W1")
	workCount := 0
	for _, c := range cmds {
		if c.CommandType() == "agent.work" {
			workCount++
			if c.IdempotencyKey() != "agent.work:wi-active" {
				t.Fatalf("unexpected work key %q", c.IdempotencyKey())
			}
		}
	}
	if workCount != 1 {
		t.Fatalf("flap must NOT double-deliver work: got %d agent.work commands, want 1", workCount)
	}
}

// TestReemit_NilWorkItemsDepSkips: the legacy constructor (nil workItems) keeps
// reconcile-only behavior — no re-emit, no panic.
func TestReemit_NilWorkItemsDepSkips(t *testing.T) {
	f := newProjectorFixture(t)
	e := lifecycleEvent("EV1", "AG1", "W1", "running", 2, "")
	if err := f.proj.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("nil workItems dep must emit only reconcile, got %d", len(cmds))
	}
}
