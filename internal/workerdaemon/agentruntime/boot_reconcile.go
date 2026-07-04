package agentruntime

// boot_reconcile.go — T848 D4 §4.4: the runtime SELF-TRIGGERS its own boot
// reconcile (self-recovery), instead of the daemon driving it from the outside.
//
// Boot(ctx) is the entry a durable, k8s-hosted runtime calls when it comes up. It
// ties together this plan's earlier tracks:
//   - D2 (list_my_inflight_tasks): the center's UNFILTERED active-task set for THIS
//     agent — the authority on "which of my on-disk executors are still mine".
//   - D3 (orchestrator.RecoveryPlanner): the three-rung degradation ladder that says
//     HOW to bring a should-continue-but-dead executor back (resume / rerun / fresh).
//
// The executor reconcile (selfReconcile) is a PURE decision (planExecutorReconcile)
// followed by a thin best-effort enactment, mirroring executor.Reconciler's
// no-loss/no-duplication discipline: every on-disk executor Record is classified
// exactly once against the inflight set + its liveness, then adopted / recovered /
// cancelled.
//
// The supervisor-SESSION recovery decision (decideBootAction) is ported here as the
// runtime-owned pure primitive (the "决策搬进 runtime"); its probe→reattach/relaunch
// enactment continues to run through the existing session primitives (Start /
// ReattachSupervisorSession), which the runtime already owns.

import (
	"context"
	"os"
	"strings"
	"syscall"

	"github.com/oopslink/agent-center/internal/supervisormanager"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// Boot runs the runtime's self-triggered boot recovery (§4.4). It is safe to call
// once per runtime bring-up: the executor reconcile is guarded so it scans the
// durable executor dirs exactly once per runtime (recovered-once), and a runtime
// with no executor engine (single-claude agent) is a no-op.
func (r *LocalRuntime) Boot(ctx context.Context) error {
	return r.selfReconcile(ctx)
}

// selfReconcile reconciles this agent's on-disk executors against its center
// inflight set (§4.4). Guarded by the per-runtime recovered-once flag so a later
// in-process engine rebuild does not re-scan and double-finalize an orphan already
// classified this process (the semantics of the migrated daemon guard).
func (r *LocalRuntime) selfReconcile(ctx context.Context) error {
	ee := r.execEngine()
	if ee == nil {
		return nil // single-claude agent: no executors to reconcile
	}
	if !r.markRecoveredOnce() {
		return nil // already reconciled this runtime
	}
	if err := r.reconcileExecutors(ctx, ee); err != nil {
		return err
	}
	// Non-regression: after reconcile has re-adopted the still-live orphans, reap
	// worktrees left by a prior process whose executor is gone (the v2.31.1 boot-hook
	// behavior the daemon's Recover did; isLive = the post-reconcile snapshot).
	r.reapOrphanWorktrees(ctx, ee)
	return nil
}

// reapOrphanWorktrees tears down worktrees whose executor is no longer live (extracted
// from Recover so both the legacy daemon path and Boot share it). Fail-safe KEEP: an
// executor id present in the live snapshot is never reaped. No-op without a materializer.
func (r *LocalRuntime) reapOrphanWorktrees(ctx context.Context, ee *ExecutorEngine) {
	if r.cfg.Materializer == nil {
		return
	}
	live := r.liveExecIDs(ee)
	if n, err := r.cfg.Materializer.ReapOrphanWorktrees(ctx, func(id string) bool { return live[id] }); err != nil {
		r.log("agent=%s boot orphan-worktree reap: %v (non-fatal)", r.cfg.AgentID, err)
	} else if n > 0 {
		r.log("agent=%s boot-reaped %d orphan worktree(s)", r.cfg.AgentID, n)
	}
}

// reconcileAction is the verdict for one on-disk executor.
type reconcileAction int

const (
	// reconcileLeave — do nothing: don't adopt, don't finalize, don't cancel. Used for an
	// ALIVE ownerless executor (no task ref) — there is no positive evidence to act, so
	// leave it running.
	reconcileLeave reconcileAction = iota
	// reconcileAdopt — the task is still in-flight AND the process is alive: re-adopt it
	// into the pool + watchdog (no re-spawn), the survivor-restart case.
	reconcileAdopt
	// reconcileRecover — the task is still in-flight, the process is DEAD, and it died
	// WITHOUT a valid result verdict (a crash/kill, not a legit failure): bring it back
	// via the D3 RecoveryPlanner ladder (resume / rerun / fresh). This is the §4.4 core —
	// it must run on the SURVIVING worktree/session, so the scan must NOT have finalized
	// (torn down) the executor first (T854 D6 P0-2).
	reconcileRecover
	// reconcileFinalize — report the terminal result (writeback) + tear down: a succeeded
	// executor, a LEGITIMATELY-failed one (dead WITH a valid output verdict — §9: don't
	// re-run a real failure), or the degraded/ownerless dead case. Best-effort per
	// executor so one writeback failure does not wedge the whole reconcile.
	reconcileFinalize
	// reconcileVerifyCancel — the task is ABSENT from this agent's in-flight set: a
	// CANDIDATE for cancel, NOT an immediate one. Absence ≠ cancel evidence (the set may
	// be incomplete), so the enactment first get_task-verifies and cancels ONLY on
	// positive proof (terminal, or reassigned to another agent). PD ruling (T848 review):
	// losing an in-flight-but-live executor is worse than leaving a zombie.
	reconcileVerifyCancel
)

// execReconcileFacts are the PURE inputs to the §4.4 boot decision for one executor.
type execReconcileFacts struct {
	Kind            executor.OutcomeKind // Succeeded / Failed / Crashed / Running
	HasValidVerdict bool                 // a valid output.json result exists (⇒ a legit conclusion, not a death)
	TaskRef         string
	Alive           bool
	Inflight        map[string]bool
	HaveInflight    bool
}

// classifyExecutor is the PURE §4.4 boot decision. It distinguishes a DEATH (recover
// via the ladder) from a legitimate terminal result (finalize), and only cancels an
// absent task with positive evidence.
func classifyExecutor(f execReconcileFacts) reconcileAction {
	// A succeeded executor's work is DONE regardless of the inflight set → report + teardown.
	if f.Kind == executor.OutcomeSucceeded {
		return reconcileFinalize
	}
	if f.Alive {
		if !f.HaveInflight {
			return reconcileAdopt // degraded: adopt the alive (fail-safe, never cancel)
		}
		if f.TaskRef == "" {
			return reconcileLeave // ownerless but alive: leave it running
		}
		if !f.Inflight[f.TaskRef] {
			return reconcileVerifyCancel
		}
		return reconcileAdopt
	}
	// Dead (Crashed / Failed).
	if !f.HaveInflight {
		// Degraded (inflight query failed): we can't confirm should-continue, so preserve
		// the pre-D6 behavior — report the dead executor's result (finalize), don't strand it.
		return reconcileFinalize
	}
	if f.TaskRef == "" {
		return reconcileFinalize // ownerless dead: report + teardown
	}
	if !f.Inflight[f.TaskRef] {
		return reconcileVerifyCancel
	}
	// In-flight (should-continue) + dead: DEATH (no valid verdict) → tier recover; a
	// legitimate failure (valid verdict) → finalize (§9: don't auto-re-run a real failure).
	if isDeath(f.Kind, f.HasValidVerdict) {
		return reconcileRecover
	}
	return reconcileFinalize
}

// isDeath reports whether a terminal executor DIED without concluding (crash / kill /
// no-valid-output) — the recoverable case — vs reached a legitimate failure verdict.
// Mirrors the D5 mid-life discriminator (comp.Output!=nil ⟺ a valid verdict).
func isDeath(kind executor.OutcomeKind, hasValidVerdict bool) bool {
	return kind == executor.OutcomeCrashed || (kind == executor.OutcomeFailed && !hasValidVerdict)
}

// execReconcileDecision is one executor's classified outcome plus the durable facts the
// enactment needs. Plan is set only for reconcileRecover; Completion carries the result
// for reconcileFinalize.
type execReconcileDecision struct {
	ExecutorID string
	TaskRef    string
	Action     reconcileAction
	Alive      bool
	PID        int
	Record     *executor.Record
	Completion executor.Completion
	Plan       orchestrator.RecoveryPlan
}

// planExecutorReconcile is the PURE core: it maps each SCANNED executor × the in-flight
// set → a decision (+ the D3 ladder plan for the recover branch), with NO side effects.
// It operates on a NON-FINALIZING scan, so a dead-but-should-continue executor is routed
// to reconcileRecover with its worktree/session INTACT (the T854 D6 P0-2 fix); the
// enactment finalizes only the executors classifyExecutor marks reconcileFinalize.
func planExecutorReconcile(items []executor.Reconciled, inflight map[string]bool, haveInflight bool, planner *orchestrator.RecoveryPlanner) []execReconcileDecision {
	out := make([]execReconcileDecision, 0, len(items))
	for _, it := range items {
		d := execReconcileDecision{ExecutorID: it.ExecutorID, Record: it.Record, Completion: it.Completion}
		d.TaskRef = taskRefOf(it.Snapshot)
		d.Alive = it.Completion.Kind == executor.OutcomeRunning && it.Record != nil && it.Record.PID > 0
		if d.Alive {
			d.PID = it.Record.PID
		}
		d.Action = classifyExecutor(execReconcileFacts{
			Kind:            it.Completion.Kind,
			HasValidVerdict: it.Completion.Output != nil,
			TaskRef:         d.TaskRef,
			Alive:           d.Alive,
			Inflight:        inflight,
			HaveInflight:    haveInflight,
		})
		if d.Action == reconcileRecover && planner != nil {
			d.Plan = planner.Plan(it.ExecutorID, it.Record)
		}
		out = append(out, d)
	}
	return out
}

// taskRefOf extracts the executor's task ref from its input.json snapshot (the same
// id list_my_inflight_tasks returns; SpawnExecutor sets TaskRef == task_id). Empty
// when the input is unreadable — such an executor is untracked and gets cancelled.
func taskRefOf(snap executor.Snapshot) string {
	if snap.Input == nil {
		return ""
	}
	return snap.Input.Source.TaskRef
}

// reconcileExecutors scans the durable executor Records, plans each against the
// inflight set, and enacts. Enactment is best-effort per executor: one failure logs
// and moves on (a stuck reconcile must not wedge boot).
func (r *LocalRuntime) reconcileExecutors(ctx context.Context, ee *ExecutorEngine) error {
	// NON-FINALIZING scan (T854 D6 P0-2): unlike Recover, Scan does NOT finalize the dead
	// ones — so a dead-but-should-continue executor still has its worktree/session for the
	// tier ladder. The driver below decides, per executor, adopt / recover / finalize.
	items, err := ee.monitor.Scan()
	if err != nil {
		r.log("agent=%s self-reconcile scan: %v", r.cfg.AgentID, err)
		return err
	}
	inflight, haveInflight := r.inflightTaskSet(ctx)
	planner, perr := orchestrator.NewRecoveryPlanner(ee.fx.Layout(), nil)
	if perr != nil {
		return perr
	}

	decisions := planExecutorReconcile(items, inflight, haveInflight, planner)
	var adopted, recovered, finalized, cancelled, kept int
	for _, d := range decisions {
		switch d.Action {
		case reconcileAdopt:
			ee.adoptAlive(d.ExecutorID, d.PID) // pool slot + watchdog
			adopted++
		case reconcileRecover:
			r.enactRecover(ctx, ee, d)
			recovered++
		case reconcileFinalize:
			// Report the terminal result + teardown. Best-effort: a writeback failure
			// (e.g. block_task 404) logs and moves on — it must NOT abort the whole
			// reconcile (the pre-fix monitor.Recover aborted on the first such failure).
			if ferr := ee.monitor.FinalizeRecovered(ctx, d.Completion); ferr != nil {
				r.log("agent=%s self-reconcile finalize executor=%s: %v (continuing)", r.cfg.AgentID, d.ExecutorID, ferr)
			}
			finalized++
		case reconcileVerifyCancel:
			// Absence is not cancel evidence: get_task-verify, cancel only on proof.
			if r.verifyThenCancel(ctx, ee, d) {
				cancelled++
			} else {
				kept++
			}
		case reconcileLeave:
			kept++
		}
	}
	if adopted+recovered+finalized+cancelled+kept > 0 {
		r.log("agent=%s self-reconcile: scanned=%d adopted=%d recovered=%d finalized=%d cancelled=%d kept=%d inflight_known=%t",
			r.cfg.AgentID, len(items), adopted, recovered, finalized, cancelled, kept, haveInflight)
	}
	return nil
}

// inflightTaskSet fetches this agent's UNFILTERED in-flight task set from the center
// (D2). Returns (set, true) on success, (nil, false) on any failure/absent lister —
// the fail-safe signal that reconcile must not cancel.
func (r *LocalRuntime) inflightTaskSet(ctx context.Context) (map[string]bool, bool) {
	lister, err := r.CenterInflightLister()
	if err != nil || lister == nil {
		if err != nil {
			r.log("agent=%s self-reconcile inflight lister: %v", r.cfg.AgentID, err)
		}
		return nil, false
	}
	tasks, err := lister.ListMyInflightTasks(ctx, r.cfg.AgentID)
	if err != nil {
		r.log("agent=%s self-reconcile list_my_inflight_tasks: %v", r.cfg.AgentID, err)
		return nil, false
	}
	set := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if t.TaskID != "" {
			set[t.TaskID] = true
		}
	}
	return set, true
}

// enactRecover brings a dead-but-should-continue executor back per the D3 ladder:
// tier 1/2 relaunch the executor process in its surviving workspace (--resume argv
// for tier 1, the persisted argv for tier 2) and re-adopt it; tier 3 (workspace
// gone) cleans the residue so the agent's normal dispatch re-forks fresh.
func (r *LocalRuntime) enactRecover(ctx context.Context, ee *ExecutorEngine, d execReconcileDecision) {
	switch d.Plan.Action {
	case orchestrator.RecoverResume, orchestrator.RecoverRerun:
		r.relaunchExecutor(ee, d.ExecutorID, d.Plan.RunnerCmd)
	default: // RecoverFresh (or no plan): clean up; normal dispatch re-forks fresh.
		r.enactCancel(ctx, ee, d)
	}
}

// relaunchExecutor re-forks the executor process for id with runnerCmd into its
// EXISTING workspace and re-adopts it into the watchdog. Best-effort: a missing
// cached config or spawn error logs and returns (the task stays in-flight, so the
// normal dispatch loop can still re-fork it fresh).
func (r *LocalRuntime) relaunchExecutor(ee *ExecutorEngine, id string, runnerCmd []string) {
	cfg, ok := r.cachedExecConfig()
	if !ok || len(runnerCmd) == 0 {
		r.log("agent=%s self-reconcile relaunch executor=%s skipped (config_cached=%t cmd_len=%d)",
			r.cfg.AgentID, id, ok, len(runnerCmd))
		return
	}
	home, _, _, err := r.agentPaths(r.cfg.AgentID)
	if err != nil {
		r.log("agent=%s self-reconcile relaunch executor=%s paths: %v", r.cfg.AgentID, id, err)
		return
	}
	h, err := executor.NewSpawner().Spawn(executor.SpawnSpec{
		BinaryPath: r.cfg.BinaryPath,
		ExecutorID: id,
		AgentRoot:  home,
		RunnerCmd:  runnerCmd,
		AgentEnv:   runtimeAgentEnv(cfg.AgentID, cfg.DisplayName, cfg.EnvVars),
	})
	if err != nil {
		r.log("agent=%s self-reconcile relaunch executor=%s spawn: %v", r.cfg.AgentID, id, err)
		return
	}
	ee.addOrphan(id, h.PID)
	r.log("agent=%s self-reconcile relaunched executor=%s pid=%d", r.cfg.AgentID, id, h.PID)
}

// enactCancel stops a no-longer-ours executor and cleans its residue: kill the live
// process, tear down its repo worktree (if repo-backed + a materializer is wired —
// never the canonical source, design §10), and remove the executor dir so it is not
// re-reconciled. Best-effort throughout.
func (r *LocalRuntime) enactCancel(ctx context.Context, ee *ExecutorEngine, d execReconcileDecision) {
	if d.Alive && d.PID > 0 {
		// SIGKILL the whole process group (executors run in their own group, spawn.go).
		_ = syscall.Kill(-d.PID, syscall.SIGKILL)
	}
	if d.Record != nil && d.Record.RepoKey != "" && r.cfg.Materializer != nil {
		if ws, err := ee.fx.Layout().WorkspaceDir(d.ExecutorID); err == nil {
			_ = materializerCleaner{m: r.cfg.Materializer}.RemoveWorktree(ctx, d.Record.RepoKey, d.Record.SourcePath, ws)
		}
	}
	if dir, err := ee.fx.Layout().Dir(d.ExecutorID); err == nil {
		_ = os.RemoveAll(dir)
	}
	ee.dropOrphan(d.ExecutorID)
}

// verifyThenCancel resolves a verify-cancel CANDIDATE with POSITIVE evidence (PD
// ruling, T848 review): an executor whose task is merely ABSENT from the in-flight
// set is get_task-verified, and cancelled ONLY when the center confirms the task is
// terminal (discarded/completed) or reassigned to another agent. Any other outcome —
// still mine/running (⇒ the in-flight set was incomplete), or an uncertain/failed
// query — is treated as "keep": adopt the live process, leave the dead one to the
// monitor's terminal finalize. Never SIGKILL a live executor without proof. Returns
// true iff it cancelled.
func (r *LocalRuntime) verifyThenCancel(ctx context.Context, ee *ExecutorEngine, d execReconcileDecision) bool {
	detail, err := r.fetchCenterTask(ctx, r.cfg.AgentID, d.TaskRef)
	if err != nil || detail == nil {
		// Uncertain: the inflight set said absent but get_task couldn't confirm why.
		// Do NOT cancel — keep the executor (adopt if live).
		if err != nil {
			r.log("agent=%s self-reconcile verify task=%s get_task: %v — keeping executor=%s",
				r.cfg.AgentID, d.TaskRef, err, d.ExecutorID)
		}
		if d.Alive {
			ee.addOrphan(d.ExecutorID, d.PID)
		}
		return false
	}
	if taskCancelEvidence(detail, r.cfg.AgentID) {
		r.enactCancel(ctx, ee, d)
		return true
	}
	// Still mine/running ⇒ the in-flight set was incomplete: keep it.
	if d.Alive {
		ee.addOrphan(d.ExecutorID, d.PID)
	}
	return false
}

// taskCancelEvidence is the PURE cancel-proof test: a task is safe to cancel its
// executor for ONLY when the center says it is terminal (completed/discarded/
// cancelled) OR it is now assigned to a DIFFERENT agent (reassigned). Anything else —
// running/open/blocked and still mine, or an empty/unknown status — is NOT proof, so
// the executor is kept. Absence-from-inflight alone never reaches here.
func taskCancelEvidence(detail *centerTaskDetail, agentID string) bool {
	if detail == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(detail.Status)) {
	case "completed", "discarded", "cancelled", "canceled":
		return true
	}
	return taskReassigned(detail.Assignee, agentID)
}

// taskReassigned reports whether a non-empty assignee names an agent OTHER than this
// one (the get_task projection renders the assignee as "agent:<id>" or a bare id).
func taskReassigned(assignee, agentID string) bool {
	assignee = strings.TrimSpace(assignee)
	if assignee == "" || strings.TrimSpace(agentID) == "" {
		return false // unknown assignee is not proof of reassignment
	}
	return assignee != agentID && assignee != "agent:"+agentID
}

// ---------------------------------------------------------------------------
// Supervisor-session boot decision (ported from daemon boot_reconcile.go, §4.4:
// "把 daemon 的探测→reattach/relaunch 决策搬进 runtime"). This is the PURE decision
// over (local probe state × center desired record); the reattach/relaunch enactment
// runs through the runtime's existing session primitives (Start / ReattachSupervisor
// Session).
// ---------------------------------------------------------------------------

// bootActionKind is the supervisor-session boot action chosen by decideBootAction.
type bootActionKind int

const (
	bootNoop bootActionKind = iota
	// bootReattach — a live, reattachable supervisor the center still wants running:
	// re-attach to it (never a fresh spawn, so no lost context).
	bootReattach
	// bootReapRelaunch — the supervisor is gone but the center wants it running: reap
	// residue and relaunch (Mode-B self-heal; resume the prior session iff it had a
	// completed turn).
	bootReapRelaunch
	// bootStopReap — a live supervisor the center no longer wants running (or an
	// orphan): stop + reap.
	bootStopReap
	// bootReapOnly — the supervisor is gone and the center does not want it running:
	// reap residue only.
	bootReapOnly
)

// bootAction pairs the kind with whether a relaunch should nudge (drive the resumed
// turn). Nudge is meaningful only for bootReapRelaunch.
type bootAction struct {
	Kind  bootActionKind
	Nudge bool
}

// centerRecord is the center's desired view of this agent (nil ⇒ the center has no
// record — an orphan). wantsRunning drives the decision.
type centerRecord struct {
	DesiredLifecycle string
	HasActive        bool
	ActiveTaskID     string
}

func (r *centerRecord) wantsRunning() bool {
	return r != nil && r.DesiredLifecycle == "running"
}

// BootSessionAction is the exported supervisor-session boot action (T860 fold-in) — the
// wired counterpart of the pure decideBootAction. The agent-runtime process boot
// orchestration (which owns the reattach/relaunch primitives, living in the parent
// workerdaemon package) consumes this so the dead-coded decision is finally enacted.
type BootSessionAction int

const (
	// BootSessionNoop — leave as-is (unknown probe / conservative).
	BootSessionNoop BootSessionAction = iota
	// BootSessionReattach — a live survivor the center still wants running: reattach,
	// never interrupt it.
	BootSessionReattach
	// BootSessionReapRelaunch — the supervisor is gone but desired running: reap +
	// relaunch (resume-gated on the prior completed turn).
	BootSessionReapRelaunch
	// BootSessionStopReap — a live supervisor no longer wanted (or orphan): stop + reap.
	BootSessionStopReap
	// BootSessionReapOnly — gone and not wanted: reap residue only.
	BootSessionReapOnly
)

// DecideBootSession is the exported pure boot-session decision over (local supervisor
// probe × desired state), delegating to decideBootAction. The agent-runtime process
// probes locally, calls this, and enacts the returned action with its session
// primitives — the wiring the migration left dead-coded.
func DecideBootSession(probe supervisormanager.ProbeState, desiredRunning, hasActive bool) BootSessionAction {
	rec := &centerRecord{HasActive: hasActive}
	if desiredRunning {
		rec.DesiredLifecycle = "running"
	}
	switch decideBootAction(probe, rec).Kind {
	case bootReattach:
		return BootSessionReattach
	case bootReapRelaunch:
		return BootSessionReapRelaunch
	case bootStopReap:
		return BootSessionStopReap
	case bootReapOnly:
		return BootSessionReapOnly
	default:
		return BootSessionNoop
	}
}

// decideBootAction is the PURE supervisor-recovery decision (§4.4). It maps the
// local supervisor probe state × the center's desired record to one action, with no
// I/O — fully unit-testable over the probe × record grid.
//
//   - Reattachable: wantsRunning ⇒ reattach (never nudge); else stop+reap (desired-
//     stopped wins, and an orphan reattachable session is stopped).
//   - Unavailable: wantsRunning ⇒ reap+relaunch (Mode-B self-heal, nudge iff a task
//     is active); else reap only.
//   - anything else (unknown probe) ⇒ conservative noop.
func decideBootAction(probe supervisormanager.ProbeState, rec *centerRecord) bootAction {
	switch probe {
	case supervisormanager.Reattachable:
		if rec.wantsRunning() {
			return bootAction{Kind: bootReattach}
		}
		return bootAction{Kind: bootStopReap}
	case supervisormanager.Unavailable:
		if rec.wantsRunning() {
			return bootAction{Kind: bootReapRelaunch, Nudge: rec.HasActive}
		}
		return bootAction{Kind: bootReapOnly}
	default:
		return bootAction{Kind: bootNoop}
	}
}
