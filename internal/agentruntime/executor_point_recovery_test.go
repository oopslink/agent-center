package agentruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

// gettaskCaller scripts get_task with a fixed status/assignee (for should-continue vs
// terminal/reassigned) and records whether block_task (the recovery-exhausted escalate)
// was called.
type gettaskCaller struct {
	status   string
	assignee string
	blocked  bool
}

func (g *gettaskCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	switch tool {
	case "get_task":
		if out != nil {
			rb, _ := json.Marshal(map[string]any{"id": "task-1", "status": g.status, "assignee": g.assignee})
			*out = append((*out)[:0], rb...)
		}
	case "block_task":
		g.blocked = true
	}
	return nil
}

// seedExecutorWithTask plants an executor dir (input carrying a task ref + a durable
// Record) so reconcileOneExecutor can read its task ref + Record. No workspace dir → the
// RecoveryPlanner lands on tier3 (fresh), whose enactRecover just cleans residue (no real
// process spawn/kill), keeping the test hermetic.
func seedExecutorWithTask(t *testing.T, fx *executor.FileExchange, tr *executor.Tracker, id, taskRef string) {
	t.Helper()
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision %s: %v", id, err)
	}
	in := executor.Input{ExecutorID: id, Goal: executor.Goal{Title: "t"}, Model: "m", CreatedAt: time.Now()}
	in.Source.TaskRef = taskRef
	if err := fx.WriteInput(in); err != nil {
		t.Fatalf("WriteInput %s: %v", id, err)
	}
	// A nonzero pid is required by Tracker.Write; reconcileOneExecutor's recover branch
	// never signals it (decision.Alive is unset → no kill), so a fake gone-pid is fine.
	if err := tr.Write(executor.Record{ExecutorID: id, PID: 999999, SpawnedAt: time.Now()}); err != nil {
		t.Fatalf("Tracker.Write %s: %v", id, err)
	}
}

// remainingCrashBudget destructively counts how many crash recoveries task still has
// (used AFTER a reconcile to infer whether it consumed one).
func remainingCrashBudget(ee *ExecutorEngine, taskRef string) int {
	n := 0
	for ee.tryConsumeRecoverBudget(taskRef, false) {
		n++
	}
	return n
}

// A DEATH (Crashed) whose task is still should-continue consumes one crash-recovery
// budget (recovery attempted via the ladder).
func TestReconcileOneExecutor_DeathShouldContinue_ConsumesBudget(t *testing.T) {
	rt, ee, home := engineForAgent(t, "ag-pr1")
	attach(rt, ee)
	setToolCaller(rt, &gettaskCaller{status: "running", assignee: "agent:ag-pr1"})
	fx, tr := seedExchange(t, home)
	seedExecutorWithTask(t, fx, tr, "ex-1", "task-1")

	comp := executor.Completion{ExecutorID: "ex-1", Kind: executor.OutcomeCrashed}
	rt.reconcileOneExecutor(context.Background(), ee, "ex-1", comp, false /*wasStall*/, false /*orphan*/)

	if ee.isRecovering("ex-1") {
		t.Fatal("recovering claim must be cleared after reconcile returns")
	}
	if got := remainingCrashBudget(ee, "task-1"); got != maxCrashRecover-1 {
		t.Fatalf("should-continue death must consume ONE crash budget: remaining=%d, want %d", got, maxCrashRecover-1)
	}
}

// A definite failure the executor CONCLUDED (Failed WITH an output verdict) is terminal —
// NOT a death — so it finalizes and consumes NO recovery budget (§9 legit failure).
func TestReconcileOneExecutor_LegitFailure_NoRecovery(t *testing.T) {
	rt, ee, home := engineForAgent(t, "ag-pr2")
	attach(rt, ee)
	setToolCaller(rt, &gettaskCaller{status: "running", assignee: "agent:ag-pr2"})
	fx, tr := seedExchange(t, home)
	seedExecutorWithTask(t, fx, tr, "ex-2", "task-1")

	comp := executor.Completion{ExecutorID: "ex-2", Kind: executor.OutcomeFailed, Output: &executor.Output{}}
	rt.reconcileOneExecutor(context.Background(), ee, "ex-2", comp, false, false)

	if got := remainingCrashBudget(ee, "task-1"); got != maxCrashRecover {
		t.Fatalf("a legitimately-failed executor must NOT consume recovery budget: remaining=%d, want %d", got, maxCrashRecover)
	}
}

// When the per-task recovery budget is already exhausted, a further should-continue death
// finalizes AND escalates to PD (block_task).
func TestReconcileOneExecutor_BudgetExhausted_Escalates(t *testing.T) {
	rt, ee, home := engineForAgent(t, "ag-pr3")
	attach(rt, ee)
	gc := &gettaskCaller{status: "running", assignee: "agent:ag-pr3"}
	setToolCaller(rt, gc)
	fx, tr := seedExchange(t, home)
	seedExecutorWithTask(t, fx, tr, "ex-3", "task-1")

	// Drain the crash budget first.
	for i := 0; i < maxCrashRecover; i++ {
		if !ee.tryConsumeRecoverBudget("task-1", false) {
			t.Fatalf("pre-consume %d", i)
		}
	}
	comp := executor.Completion{ExecutorID: "ex-3", Kind: executor.OutcomeCrashed}
	rt.reconcileOneExecutor(context.Background(), ee, "ex-3", comp, false, false)

	if !gc.blocked {
		t.Fatal("exhausted budget must escalate to PD via block_task")
	}
}

// The recovering-set is the CONCURRENT-dup guard: exactly one in-flight recovery per
// executor id (a death + a stall of the same id, or the drain vs watchdog paths, must
// not both start a recovery → double relaunch / orphan-pid leak).
func TestExecutorEngine_RecoveringGuard(t *testing.T) {
	ee := &ExecutorEngine{}
	if !ee.beginRecovery("x") {
		t.Fatal("first beginRecovery must claim")
	}
	if ee.beginRecovery("x") {
		t.Fatal("second beginRecovery must be denied while the first is in flight")
	}
	if !ee.isRecovering("x") {
		t.Fatal("isRecovering must be true while claimed")
	}
	// A different id is independent.
	if !ee.beginRecovery("y") {
		t.Fatal("a different executor id must be independently claimable")
	}
	ee.endRecovery("x")
	if ee.isRecovering("x") {
		t.Fatal("isRecovering must be false after endRecovery")
	}
	if !ee.beginRecovery("x") {
		t.Fatal("id must be re-claimable after endRecovery")
	}
}

// The per-TASK recoverCount is the SERIAL hang-loop guard: crash ≤3, stall ≤1, counted
// per task (across executor incarnations), stall and crash budgets independent.
func TestExecutorEngine_RecoverBudget_TieredPerTask(t *testing.T) {
	ee := &ExecutorEngine{}

	// crash budget = maxCrashRecover, consumed then denied.
	for i := 0; i < maxCrashRecover; i++ {
		if !ee.tryConsumeRecoverBudget("task-1", false) {
			t.Fatalf("crash recovery %d must be within budget (max=%d)", i+1, maxCrashRecover)
		}
	}
	if ee.tryConsumeRecoverBudget("task-1", false) {
		t.Fatal("crash recovery over maxCrashRecover must be denied")
	}
	// stall budget for the SAME task is independent (and stricter).
	for i := 0; i < maxStallRecover; i++ {
		if !ee.tryConsumeRecoverBudget("task-1", true) {
			t.Fatalf("stall recovery %d must be within budget (max=%d) even after crash budget spent", i+1, maxStallRecover)
		}
	}
	if ee.tryConsumeRecoverBudget("task-1", true) {
		t.Fatal("stall recovery over maxStallRecover must be denied")
	}

	// A DIFFERENT task has its own fresh budget (per-task isolation).
	if !ee.tryConsumeRecoverBudget("task-2", true) {
		t.Fatal("a different task must have an independent budget")
	}

	// clearRecoverBudget resets a task (terminal cleanup).
	ee.clearRecoverBudget("task-1")
	if !ee.tryConsumeRecoverBudget("task-1", false) {
		t.Fatal("crash budget must reset after clearRecoverBudget")
	}
}

// maxStallRecover must be strictly tighter than maxCrashRecover (PD ruling: a resumed
// stuck conversation ≈ re-stalls, so give it fewer tries than a transient crash).
func TestExecutorEngine_StallBudgetStricterThanCrash(t *testing.T) {
	if !(maxStallRecover < maxCrashRecover) {
		t.Fatalf("maxStallRecover (%d) must be < maxCrashRecover (%d)", maxStallRecover, maxCrashRecover)
	}
	if maxStallRecover < 1 {
		t.Fatalf("maxStallRecover must be >= 1 (one retry for a transient hang), got %d", maxStallRecover)
	}
}
