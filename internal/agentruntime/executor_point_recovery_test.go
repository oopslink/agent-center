package agentruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

// gettaskCaller scripts get_task with a fixed status/assignee/blocked_reason (for
// should-continue vs terminal/reassigned/blocked) and records whether block_task (the
// recovery-exhausted escalate) was called and whether get_task was queried at all.
type gettaskCaller struct {
	status        string
	assignee      string
	blockedReason string // issue-88e32d98: non-empty ⇒ task blocked (cancel evidence)
	blocked       bool
	queried       bool // set when get_task was actually called (the fused path skips it)
}

func (g *gettaskCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	switch tool {
	case "get_task":
		g.queried = true
		if out != nil {
			rb, _ := json.Marshal(map[string]any{"id": "task-1", "status": g.status, "assignee": g.assignee, "blocked_reason": g.blockedReason})
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

// TestReconcileOneExecutor_Fused_QuietFinalizeNoRecover locks issue-88e32d98 P0 (root
// causes ①/②/③): a FUSED death — one the runtime circuit-broke because the center revoked
// the task's lease (block mid-flight) — must QUIET-finalize, NEVER recover/relaunch. Even
// though get_task would report status="running" (ADR-0046: a block leaves running), the
// fused mark short-circuits the whole should-continue/budget logic: no get_task query, no
// recovery budget consumed, no relaunch (and thus no un-fusable orphan). This is the exact
// steady-state 60s+ symptom the fix closes.
func TestReconcileOneExecutor_Fused_QuietFinalizeNoRecover(t *testing.T) {
	rt, ee, home := engineForAgent(t, "ag-fuse")
	attach(rt, ee)
	// get_task would say should-continue (running+mine) — the PRE-FIX relaunch trigger.
	gc := &gettaskCaller{status: "running", assignee: "agent:ag-fuse"}
	setToolCaller(rt, gc)
	fx, tr := seedExchange(t, home)
	seedExecutorWithTask(t, fx, tr, "ex-fuse", "task-1")

	// The runtime fused this executor (center lease revoke) before it died.
	ee.monitor.MarkFused("ex-fuse")

	comp := executor.Completion{ExecutorID: "ex-fuse", Kind: executor.OutcomeCrashed, Retryable: true}
	rt.reconcileOneExecutor(context.Background(), ee, "ex-fuse", comp, false /*wasStall*/, true /*thisProcess*/)

	if ee.isRecovering("ex-fuse") {
		t.Fatal("recovering claim must be cleared after reconcile returns")
	}
	// A fused death must NOT consume recovery budget (it is finalized, not recovered).
	if got := remainingCrashBudget(ee, "task-1"); got != maxCrashRecover {
		t.Fatalf("fused death must NOT consume recovery budget (no relaunch): remaining=%d, want %d", got, maxCrashRecover)
	}
	// The fused path is a deliberate stop: it never even queries should-continue.
	if gc.queried {
		t.Fatal("fused death must short-circuit BEFORE get_task (no should-continue query)")
	}
	// The mark is consumed so it cannot linger onto a later same-id incarnation.
	if ee.monitor.TakeFused("ex-fuse") {
		t.Fatal("reconcile must consume the fused mark")
	}
}

// TestReconcileOneExecutor_BlockedTask_NoRecover is the durable net for root cause ②: even
// WITHOUT a fused mark (e.g. an executor that crashed on its own, or a mark lost across an
// orchestrator restart), a death whose task get_task reports as BLOCKED (status=running +
// a non-empty blocked_reason) must NOT relaunch. blocked-as-stop keeps the P67 relaunch
// shut on every path, not just the in-process fuse.
func TestReconcileOneExecutor_BlockedTask_NoRecover(t *testing.T) {
	rt, ee, home := engineForAgent(t, "ag-blk")
	attach(rt, ee)
	// The LEGACY (ADR-0046) parked shape: status="running" + a blocked_reason. ADR-0054
	// keeps this arm working for rows parked before the upgrade — see
	// TestReconcileOneExecutor_ParkedStatus_NoRecover for the new status-based shape.
	setToolCaller(rt, &gettaskCaller{status: "running", assignee: "agent:ag-blk", blockedReason: "obstacle: needs PD triage"})
	fx, tr := seedExchange(t, home)
	seedExecutorWithTask(t, fx, tr, "ex-blk", "task-1")

	comp := executor.Completion{ExecutorID: "ex-blk", Kind: executor.OutcomeCrashed}
	rt.reconcileOneExecutor(context.Background(), ee, "ex-blk", comp, false, false)

	if got := remainingCrashBudget(ee, "task-1"); got != maxCrashRecover {
		t.Fatalf("a crashed executor of a BLOCKED task must NOT consume recovery budget (no relaunch): remaining=%d, want %d", got, maxCrashRecover)
	}
}

// TestReconcileOneExecutor_ParkedStatus_NoRecover is the ADR-0054 命门-3 regression lock:
// "别只改停、不改别复活" — stopping dispatch is worthless if the recovery chain reads a
// parked task as still-mine-still-running and resurrects it.
//
// It pins the two ADR-0054 parked STATUSES as cancel evidence, INCLUDING the `blocked`
// case with an EMPTY blocked_reason (which the pre-ADR-0054 reason-only check would have
// missed entirely and relaunched) and `delivered`, where a relaunch is the worst outcome
// of all: a fresh empty-context executor forking onto work that is already delivered and
// pushed, whose frozen prompt tells it to implement everything from scratch.
//
// Crucially it reconciles REPEATEDLY: the historical failure was never "the first tick
// relaunched" — it was that a later tick / a relaunch path pulled the task back. A single
// no-op proves nothing, so every tick must stay a no-op.
func TestReconcileOneExecutor_ParkedStatus_NoRecover(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        string
		blockedReason string
	}{
		// The reason is EMPTY on purpose: the status alone must be sufficient evidence.
		{"blocked_status_no_reason", "blocked", ""},
		{"blocked_status_with_reason", "blocked", "obstacle: needs PD triage"},
		{"delivered_status", "delivered", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, ee, home := engineForAgent(t, "ag-park")
			attach(rt, ee)
			setToolCaller(rt, &gettaskCaller{status: tc.status, assignee: "agent:ag-park", blockedReason: tc.blockedReason})
			fx, tr := seedExchange(t, home)
			seedExecutorWithTask(t, fx, tr, "ex-park", "task-1")

			// Multiple recovery ticks — a park that only survives the first one is not a park.
			for i := 0; i < 3; i++ {
				comp := executor.Completion{ExecutorID: "ex-park", Kind: executor.OutcomeCrashed}
				rt.reconcileOneExecutor(context.Background(), ee, "ex-park", comp, false, false)
			}

			if got := remainingCrashBudget(ee, "task-1"); got != maxCrashRecover {
				t.Fatalf("a crashed executor of a %s task must NEVER relaunch across repeated ticks: remaining budget=%d, want %d (untouched)", tc.status, got, maxCrashRecover)
			}
		})
	}
}

// TestTaskCancelEvidence_ParkedAndLegacy pins the shared should-continue seam directly —
// point-recovery AND boot self-reconcile both route through taskCancelEvidence, so the
// parked vocabulary is asserted once, here, rather than per caller.
func TestTaskCancelEvidence_ParkedAndLegacy(t *testing.T) {
	const me = "agent:ag-1"
	cancel := []struct {
		name   string
		detail centerTaskDetail
	}{
		{"terminal completed", centerTaskDetail{Status: "completed", Assignee: me}},
		{"terminal discarded", centerTaskDetail{Status: "discarded", Assignee: me}},
		{"legacy canceled spelling", centerTaskDetail{Status: "canceled", Assignee: me}},
		// ADR-0054 parked statuses — status alone is proof, no reason needed.
		{"parked blocked", centerTaskDetail{Status: "blocked", Assignee: me}},
		{"parked delivered", centerTaskDetail{Status: "delivered", Assignee: me}},
		{"parked, case-insensitive", centerTaskDetail{Status: "Delivered", Assignee: me}},
		// The legacy ADR-0046 shape must STILL be caught by the reason arm (issue-88e32d98
		// P0 / the P67 Ship 事故): status reads healthy, only the reason betrays the park.
		{"legacy running+reason", centerTaskDetail{Status: "running", Assignee: me, BlockedReason: "stuck"}},
		{"reassigned away", centerTaskDetail{Status: "running", Assignee: "agent:someone-else"}},
	}
	for _, tc := range cancel {
		if !taskCancelEvidence(&tc.detail, "ag-1") {
			t.Errorf("%s: want cancel evidence (do NOT relaunch), got false", tc.name)
		}
	}

	keep := []struct {
		name   string
		detail centerTaskDetail
	}{
		// A healthy running task must still recover normally — parked-vs-stall stays
		// distinguished, so a genuine crash is not mistaken for a park.
		{"healthy running, mine", centerTaskDetail{Status: "running", Assignee: me}},
		{"open, mine", centerTaskDetail{Status: "open", Assignee: me}},
		// Absence of an answer is not proof of a park.
		{"unknown status, unknown assignee", centerTaskDetail{}},
	}
	for _, tc := range keep {
		if taskCancelEvidence(&tc.detail, "ag-1") {
			t.Errorf("%s: want NO cancel evidence (recover normally), got true", tc.name)
		}
	}
}

// TestReconcileOneExecutor_RunningNotBlocked_StillRecovers is the anti-regression guard:
// the blocked-as-stop / fused logic must NOT touch a healthy running task's normal
// crash-recover. A crash of a running task with an EMPTY blocked_reason and no fuse mark
// still recovers (consumes one budget) exactly as before.
func TestReconcileOneExecutor_RunningNotBlocked_StillRecovers(t *testing.T) {
	rt, ee, home := engineForAgent(t, "ag-run")
	attach(rt, ee)
	setToolCaller(rt, &gettaskCaller{status: "running", assignee: "agent:ag-run", blockedReason: ""})
	fx, tr := seedExchange(t, home)
	seedExecutorWithTask(t, fx, tr, "ex-run", "task-1")

	comp := executor.Completion{ExecutorID: "ex-run", Kind: executor.OutcomeCrashed}
	rt.reconcileOneExecutor(context.Background(), ee, "ex-run", comp, false, false)

	if got := remainingCrashBudget(ee, "task-1"); got != maxCrashRecover-1 {
		t.Fatalf("a healthy running (non-blocked, non-fused) crash must still recover: remaining=%d, want %d", got, maxCrashRecover-1)
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
