package cli

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// fakeDispatchReads implements dispatchPMReads for the resolver tests (reuses the
// fakeAgentReads / mustAgent / openTask helpers from sweep_candidates_test.go).
type fakeDispatchReads struct {
	runnableErr error // EnsureTaskRunnable result (nil = runnable; pm.ErrTaskNotRunnable = not)
	freed       map[string]bool
	freedCalled *bool
	runnable    map[pm.IdentityRef][]*pm.Task
}

func (f fakeDispatchReads) EnsureTaskRunnable(_ context.Context, _ pm.TaskID) error {
	return f.runnableErr
}
func (f fakeDispatchReads) AgentFreedFromTask(_ context.Context, tk pm.TaskID) (bool, error) {
	if f.freedCalled != nil {
		*f.freedCalled = true
	}
	return f.freed[string(tk)], nil
}
func (f fakeDispatchReads) ListRunnableAgentTasks(_ context.Context, a pm.IdentityRef) ([]*pm.Task, error) {
	return f.runnable[a], nil
}

// --- assign target ---------------------------------------------------------

// A runnable task assigned to a desired-running agent → target carries the ENTITY id +
// worker (not the identity-member ref the assignee uses).
func TestAssignTarget_Runnable_ResolvesEntityTarget(t *testing.T) {
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakeDispatchReads{runnableErr: nil}
	tgt, ok, err := buildAssignTarget(pmr, ars)(context.Background(), "agent:member-1", "T1")
	if err != nil || !ok {
		t.Fatalf("want ok target, got ok=%v err=%v", ok, err)
	}
	if tgt.AgentID != "entity-1" || tgt.WorkerID != "W1" || tgt.TaskID != "T1" {
		t.Fatalf("target = %+v, want entity-1/W1/T1", tgt)
	}
}

// A not-yet-runnable assignment (deps pending) is skipped (it re-emits when dispatched ready).
func TestAssignTarget_NotRunnable_Skipped(t *testing.T) {
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakeDispatchReads{runnableErr: pm.ErrTaskNotRunnable}
	if _, ok, err := buildAssignTarget(pmr, ars)(context.Background(), "agent:member-1", "T1"); ok || err != nil {
		t.Fatalf("not-runnable must skip, got ok=%v err=%v", ok, err)
	}
}

// A non-agent assignee never resolves.
func TestAssignTarget_NonAgent_Skipped(t *testing.T) {
	pmr := fakeDispatchReads{}
	if _, ok, _ := buildAssignTarget(pmr, newAgentReads())(context.Background(), "user:h1", "T1"); ok {
		t.Fatal("user: assignee must not resolve")
	}
}

// A stopped (not desired-running) agent is not woken even with a runnable task.
func TestAssignTarget_StoppedAgent_Skipped(t *testing.T) {
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleStopped)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakeDispatchReads{runnableErr: nil}
	if _, ok, _ := buildAssignTarget(pmr, ars)(context.Background(), "agent:member-1", "T1"); ok {
		t.Fatal("stopped agent must not be woken")
	}
}

// --- repush target ---------------------------------------------------------

// A plain open→running start is pre-filtered: it never frees the slot, so the freed read
// is not even consulted and no next task is woken.
func TestRepushTarget_StartTransition_PreFiltered(t *testing.T) {
	freedCalled := false
	pmr := fakeDispatchReads{freedCalled: &freedCalled}
	tgt, ok, err := buildRepushTarget(pmr, newAgentReads())(
		context.Background(), "agent:member-1", "T1", string(pm.TaskRunning), string(pm.TaskOpen))
	if ok || err != nil {
		t.Fatalf("start transition must skip, got ok=%v err=%v tgt=%+v", ok, err, tgt)
	}
	if freedCalled {
		t.Fatal("open→running must be pre-filtered WITHOUT an AgentFreedFromTask read")
	}
}

// Completed task + a next OPEN runnable task → re-push target anchored on the NEXT task.
func TestRepushTarget_FreedWithNext_ResolvesNext(t *testing.T) {
	ref := pm.IdentityRef("agent:member-1")
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakeDispatchReads{
		freed:    map[string]bool{"T-done": true},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: {openTask(t, "T-next")}},
	}
	tgt, ok, err := buildRepushTarget(pmr, ars)(
		context.Background(), "agent:member-1", "T-done", string(pm.TaskCompleted), string(pm.TaskRunning))
	if err != nil || !ok {
		t.Fatalf("want ok, got ok=%v err=%v", ok, err)
	}
	if tgt.TaskID != "T-next" || tgt.AgentID != "entity-1" {
		t.Fatalf("target = %+v, want next=T-next/entity-1", tgt)
	}
}

// Freed but NO further open task → no re-push.
func TestRepushTarget_FreedNoNext_Skipped(t *testing.T) {
	ref := pm.IdentityRef("agent:member-1")
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakeDispatchReads{
		freed:    map[string]bool{"T-done": true},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: {}}, // nothing else
	}
	if _, ok, _ := buildRepushTarget(pmr, ars)(
		context.Background(), "agent:member-1", "T-done", string(pm.TaskCompleted), string(pm.TaskRunning)); ok {
		t.Fatal("no next open task → no re-push")
	}
}

// Not freed (e.g. a non-block running re-emit) → no re-push.
func TestRepushTarget_NotFreed_Skipped(t *testing.T) {
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakeDispatchReads{freed: map[string]bool{"T-done": false}}
	if _, ok, _ := buildRepushTarget(pmr, ars)(
		context.Background(), "agent:member-1", "T-done", string(pm.TaskRunning), string(pm.TaskRunning)); ok {
		t.Fatal("not-freed must skip")
	}
}
