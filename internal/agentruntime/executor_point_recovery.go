package agentruntime

// executor_point_recovery.go — T851 D5 §4.6: MID-LIFE (runtime steady-state) point
// recovery for a SINGLE executor the runtime just observed die or stall.
//
// D4 (boot_reconcile.go) reconciles ALL executors ONCE at boot (markRecoveredOnce,
// full scan + full inflight query). D5 handles the RUNNING period: when the reactive
// drain (this-process handle) or the watchdog poll (adopted orphan) sees an executor
// go non-success, it hands that ONE executor here. Unlike boot reconcile this does NOT
// re-scan, does NOT pull the full inflight set, and does NOT run the boot-once guard —
// it is per-executor and re-entrant-safe via the engine's recovering-set.
//
// Policy (PD ruling, 2026-07-04):
//   - success                       → terminal finalize (free slot, report). No recover.
//   - not should-continue           → get_task says the task is terminal/reassigned (or
//     (or uncertain get_task)         we cannot confirm) → terminal finalize. Do NOT
//                                      resume a task that is gone / can't be confirmed.
//   - should-continue + budget left → RecoveryPlanner ladder → enactRecover (tier1/2
//                                      relaunch IN PLACE — worktree/session survive
//                                      because Monitor did NOT finalize/tear down; tier3
//                                      cleans residue for a fresh re-dispatch). stall
//                                      prefers tier2 rerun over tier1 resume.
//   - should-continue + budget out  → §9 bounded floor: terminal finalize (definite
//                                      failure) + ESCALATE to PD. Stops a stall→resume→
//                                      stall / crash-loop from running forever.
//
// Two guards, both required: the engine recovering-set stops CONCURRENT double-recovery
// (a death + a stall of the same id, or the drain + watchdog paths racing); the per-TASK
// recoverCount stops the SERIAL hang-loop across executor incarnations.

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
)

// reconcileOneExecutor enacts the §4.6 point-recovery policy for one executor. comp is
// the NoFinalize completion the caller harvested (Monitor killed/classified but did NOT
// finalize), wasStall is whether the cause was a watchdog stall-kill (vs a crash/exit) —
// it selects the tighter stall budget. isThisProcess distinguishes a reactive drain
// (this-process handle, slot already accounted by the caller) from a watchdog orphan.
// thisProcess=true means the caller is the reactive drain (a this-process handle whose
// pool slot must be freed via ReleaseSlot on the recover branch — Finalize would free it
// on the terminal branch); false is the watchdog orphan path (no pool Launch-handle, so
// no ReleaseSlot).
func (r *LocalRuntime) reconcileOneExecutor(ctx context.Context, ee *ExecutorEngine, execID string, comp executor.Completion, wasStall, thisProcess bool) {
	if ee == nil {
		return
	}
	// Concurrency guard: exactly one in-flight recovery per executor id. A duplicate
	// trigger (death+stall of the same id, or drain vs watchdog) gets false and returns.
	if !ee.beginRecovery(execID) {
		return
	}
	defer ee.endRecovery(execID)

	// fail-loud (T872 dogfood): make the whole recover-vs-terminal decision visible in
	// the log — the §6.2 "kill→tier-1 resume" path was silent, so a production divergence
	// (real claude crash NOT recovering) could not be diagnosed from logs.
	r.log("agent=%s point-recovery executor=%s: kind=%v output_present=%v wasStall=%v thisProcess=%v — deciding recover-vs-terminal",
		r.cfg.AgentID, execID, comp.Kind, comp.Output != nil, wasStall, thisProcess)

	// Only a DEATH or STALL is a recovery candidate. A SUCCESS, or a definite failure the
	// executor itself CONCLUDED — OutcomeFailed WITH a harvested output.json verdict — is
	// terminal: the executor ran and reported an outcome, so we finalize + writeback (this
	// preserves §9's "don't re-queue a job that legitimately failed"). A DEATH is a process
	// that died/was killed WITHOUT a valid verdict: OutcomeCrashed, OR OutcomeFailed with no
	// Output (nonzero exit / SIGKILL, no output.json), OR a watchdog stall-kill (wasStall).
	// Recover only those — a legitimately-failed executor must not be auto-re-run.
	deathOrStall := wasStall || comp.Kind == executor.OutcomeCrashed ||
		(comp.Kind == executor.OutcomeFailed && comp.Output == nil)
	if !deathOrStall {
		r.log("agent=%s point-recovery executor=%s → TERMINAL: not death/stall (kind=%v output_present=%v = executor concluded a verdict) — finalizing, no recover",
			r.cfg.AgentID, execID, comp.Kind, comp.Output != nil)
		r.finalizeTerminal(ctx, ee, execID, comp)
		return
	}

	taskRef := r.executorTaskRef(ee, execID)
	if taskRef == "" {
		// Untracked/ownerless executor — no task to reconcile against. Finalize.
		r.log("agent=%s point-recovery executor=%s → TERMINAL: no task ref on executor input — finalizing", r.cfg.AgentID, execID)
		r.finalizeTerminal(ctx, ee, execID, comp)
		return
	}

	// should-continue? get_task the ONE task (not the full inflight set). Recover only
	// when the center confirms it is still ours and non-terminal; a terminal/reassigned
	// task — OR an uncertain query — falls to terminal finalize (never resume a gone task).
	detail, err := r.fetchCenterTask(ctx, r.cfg.AgentID, taskRef)
	// Identity compare uses the center agent-ref (assignee namespace), NOT the ULID
	// AgentID (get_task auth id) — the ULID never matches "agent:<member>" so every
	// crash was misjudged "reassigned" → finalize (T872). fetchCenterTask above keeps
	// the ULID (that IS the auth id).
	if err != nil || detail == nil || taskCancelEvidence(detail, r.identityRef()) {
		// fail-loud: log EXACTLY why we cannot confirm should-continue — this is the
		// prime suspect for "clean crash didn't tier-1 resume" (get_task erroring in
		// production while writeback works = different center path/permission).
		switch {
		case err != nil:
			r.log("agent=%s point-recovery executor=%s task=%s → TERMINAL: get_task ERROR (cannot confirm should-continue): %v — finalizing, NO tier-1 resume",
				r.cfg.AgentID, execID, taskRef, err)
		case detail == nil:
			r.log("agent=%s point-recovery executor=%s task=%s → TERMINAL: get_task returned nil detail — finalizing, NO tier-1 resume",
				r.cfg.AgentID, execID, taskRef)
		default:
			r.log("agent=%s point-recovery executor=%s task=%s → TERMINAL: cancel-evidence (task terminal/reassigned, status=%q assignee=%q) — finalizing",
				r.cfg.AgentID, execID, taskRef, detail.Status, detail.Assignee)
		}
		r.finalizeTerminal(ctx, ee, execID, comp)
		ee.clearRecoverBudget(taskRef)
		return
	}
	r.log("agent=%s point-recovery executor=%s task=%s → should-continue CONFIRMED (status=%q) — proceeding to recover",
		r.cfg.AgentID, execID, taskRef, detail.Status)

	// should-continue. Within this task's recovery budget (stall stricter than crash)?
	if !ee.tryConsumeRecoverBudget(taskRef, wasStall) {
		// §9 bounded floor exhausted: finalize as a definite failure + escalate to PD.
		r.log("agent=%s point-recovery executor=%s task=%s → TERMINAL: recovery budget exhausted (wasStall=%v) — finalizing + escalating",
			r.cfg.AgentID, execID, taskRef, wasStall)
		r.finalizeTerminal(ctx, ee, execID, comp)
		r.escalateRecoveryExhausted(ctx, taskRef, execID, wasStall)
		ee.clearRecoverBudget(taskRef)
		return
	}

	// Plan the ladder rung from the durable Record and enact it IN PLACE.
	rec := r.readExecutorRecord(ee, execID)
	planner, perr := orchestrator.NewRecoveryPlanner(ee.fx.Layout(), nil)
	if perr != nil {
		r.log("agent=%s point-recovery executor=%s planner: %v — finalizing", r.cfg.AgentID, execID, perr)
		r.finalizeTerminal(ctx, ee, execID, comp)
		return
	}
	plan := planner.Plan(execID, rec)
	// PD §4.6: a stalled executor's resumed conversation ≈ re-stalls; for the single
	// stall retry prefer tier2 rerun (fresh LLM state; committed worktree progress
	// survives) over tier1 --resume. Same workspace, persisted argv (not resume-rewritten).
	if wasStall && plan.Action == orchestrator.RecoverResume && rec != nil {
		plan.Action = orchestrator.RecoverRerun
		plan.RunnerCmd = rec.RunnerCmd
	}

	// Free the dead this-process handle's pool slot before relaunching as an orphan
	// (dev2's ReleaseSlot — the recover branch has NO writeback, so it does not touch the
	// Finalize "retain slot on writeback error" invariant) and clear any stall mark so the
	// relaunched executor is not mislabeled. Orphans have no pool Launch-handle → skip.
	if thisProcess {
		ee.monitor.ReleaseSlot(execID)
		ee.monitor.ClearStalled(execID)
	}

	// Observability continuity (§4.6 "事件流不断"): emit executor.recover BEFORE the
	// relaunch so the activity stream shows the recovery and keeps flowing across it.
	r.emitExecutorRecover(r.cfg.AgentID, taskRef, execID, plan, wasStall)
	r.log("agent=%s point-recovery executor=%s task=%s → RECOVER tier=%v (wasStall=%v) — relaunching in place",
		r.cfg.AgentID, execID, taskRef, plan.Action, wasStall)

	// enactRecover: tier1/2 relaunch into the surviving workspace + re-adopt as an
	// orphan (the watchdog then polls it); tier3 cleans residue so normal dispatch
	// re-forks fresh (the task stays in-flight — we never told the center it is done).
	r.enactRecover(ctx, ee, execReconcileDecision{
		ExecutorID: execID,
		TaskRef:    taskRef,
		Action:     reconcileRecover,
		Record:     rec,
		Plan:       plan,
	})
}

// finalizeTerminal drives the Monitor's terminal finalize (writeback + worktree
// teardown + slot free) for an executor the runtime decided is DONE (success, gone
// task, or budget exhausted), then drops any orphan tracking. This is the ONLY place
// D5 tears down — worktree/session survive every recover branch above.
func (r *LocalRuntime) finalizeTerminal(ctx context.Context, ee *ExecutorEngine, execID string, comp executor.Completion) {
	if err := ee.monitor.Finalize(ctx, comp); err != nil {
		r.log("agent=%s point-recovery finalize executor=%s: %v", r.cfg.AgentID, execID, err)
	}
	ee.dropOrphan(execID)
}

// executorTaskRef reads the executor's task ref from its input.json (the id
// get_task/list_my_inflight_tasks key on). Empty when the input is unreadable.
func (r *LocalRuntime) executorTaskRef(ee *ExecutorEngine, execID string) string {
	if ee.fx == nil {
		return ""
	}
	in, err := ee.fx.ReadInput(execID)
	if err != nil {
		return ""
	}
	return in.Source.TaskRef
}

// readExecutorRecord single-reads the executor's durable orchestrator Record (pid /
// session-id / runner argv / worktree handle) for the ladder. Nil (→ tier3 fresh) when
// there is no tracker or the record is unreadable.
func (r *LocalRuntime) readExecutorRecord(ee *ExecutorEngine, execID string) *executor.Record {
	tr, err := executor.NewTracker(ee.fx.Layout())
	if err != nil {
		return nil
	}
	rec, err := tr.Read(execID)
	if err != nil {
		return nil
	}
	return &rec
}

// escalateRecoveryExhausted surfaces a task whose recovery budget is spent to PD: it
// blocks the task with a reason (an obstacle needing owner/PM intervention — PD decides
// whether the task itself is bad (discard) or the environment is (fix + redispatch)).
// Best-effort: a missing transport just logs.
func (r *LocalRuntime) escalateRecoveryExhausted(ctx context.Context, taskRef, execID string, wasStall bool) {
	// Stop renewing the exhausted task's execution lease FIRST, unconditionally. Recovery
	// gave up and the executor is finalized, but the task stays running + blocked_reason (a
	// PD-triage annotation, not terminal). Leaving state.CurrentTaskID set makes lease_gc keep
	// 续租'ing it forever → the lease never lapses → a later MANUAL PD reset (reset_task with
	// confirmedDead=false) hits ErrLeaseStillLive and can never reset. Same root as the tier-3
	// path — mirror resetRecoveredTask's guarded clear (guarded by task-id so a concurrent
	// supervisor task is untouched).
	r.mu.Lock()
	if r.state != nil && r.state.CurrentTaskID == taskRef {
		r.state.CurrentTaskID = ""
	}
	r.mu.Unlock()

	cause := "crashes"
	if wasStall {
		cause = "stalls"
	}
	reason := "executor recovery budget exhausted (repeated " + cause + "): auto-recovery gave up after the per-task limit — needs PD triage (task bad → discard, or environment → fix + redispatch)"
	client := newCenterClient(r.toolCaller())
	if client == nil {
		r.log("agent=%s recovery-exhausted task=%s executor=%s: no transport to escalate", r.cfg.AgentID, taskRef, execID)
		return
	}
	if err := client.BlockTask(ctx, r.cfg.AgentID, taskRef, reason, "obstacle"); err != nil {
		r.log("agent=%s recovery-exhausted escalate task=%s: %v", r.cfg.AgentID, taskRef, err)
	} else {
		r.log("agent=%s recovery-exhausted task=%s executor=%s escalated to PD (blocked)", r.cfg.AgentID, taskRef, execID)
	}
}

// emitExecutorRecover emits an executor.recover lifecycle event into the activity
// stream so observation does NOT break across a self-recovery (§4.6 "事件流不断"): the
// UI/LLM sees recover(tier) between the executor's prior progress and its resumed
// progress, instead of a silent gap.
func (r *LocalRuntime) emitExecutorRecover(agentID, taskRef, execID string, plan orchestrator.RecoveryPlan, wasStall bool) {
	cause := "crash"
	if wasStall {
		cause = "stall"
	}
	r.emitExecutorLifecycle(agentID, execID, taskRef, map[string]any{
		"event":       "executor.recover",
		"executor_id": execID,
		"task_ref":    taskRef,
		"tier":        plan.Action.String(),
		"cause":       cause,
	}, time.Now())
}
