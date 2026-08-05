package agentruntime

// executor_runtime.go — the LocalRuntime methods that DRIVE the per-agent
// ExecutorEngine (Phase 0c), moved verbatim off workerdaemon.AgentController:
// build / fork / drain / recover / watchdog, plus the SpawnExecutor
// (get_task→start_task→launch) and NotifyWork executor branch.
//
// Fork concurrency has two independent invariants. The executor Pool atomically
// reserves the ≤N process slots. LocalRuntime.forkStateMu only protects per-task
// idempotency plus the brief "worktree prepared, Pool not yet aware" liveness gap.
// It is never held across center RPC, git/worktree I/O, or process spawn, so distinct
// tasks can actually launch concurrently. SessionState remains guarded by r.mu.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/concurrency"
)

// defaultExecutorWatchdogInterval throttles the per-agent watchdog/orphan-poll sweep.
// Well below DefaultStallTimeout (5m) so a stalled executor is detected within one
// interval of crossing the threshold, but coarse enough not to hammer the filesystem.
const defaultExecutorWatchdogInterval = 30 * time.Second

// DefaultExecutorWatchdogInterval exposes the throttle to the daemon's OnTick so the
// 30s cadence stays daemon-driven (byte-identical to before the move).
const DefaultExecutorWatchdogInterval = defaultExecutorWatchdogInterval

// ExecutorConfig carries the concurrency-relevant reconcile fields BuildExecutorEngine
// reads. The daemon converts reconcilePayload → ExecutorConfig at the boundary so
// agentruntime stays free of the daemon's wire payload type.
type ExecutorConfig struct {
	AgentID string
	// AgentRef is the agent's identity-member ref (bare, e.g. "agent-20d5e05c") — the id
	// namespace task.assignee uses. Carried from ResumeState so the runtime seeds its
	// stable identity ref for the self-recovery should-continue check (T872). Empty ⇒ the
	// runtime falls back to the ULID AgentID.
	AgentRef             string
	DisplayName          string
	EnvVars              map[string]string
	MaxConcurrentTasks   int
	AllowedExecutors     []agent.ExecutorProfile
	OrchestratorModel    string
	DefaultExecutorModel string
	// SupervisorModel is the agent's own model (profile.model — what the supervisor
	// session runs under), threaded through as the router's last-resort executor-model
	// fallback so an executor always has a model even when neither default_executor_model
	// nor orchestrator_model is configured.
	SupervisorModel string
	CLI             string
	// JudgeEnabled (issue-93dd8daa ②, per-agent gated, DEFAULT OFF) turns on the LLM
	// difficulty judge: when true AND an orchestrator_model + claude binary are
	// available, the router consults a one-shot SubprocessJudge over the
	// catalog-annotated pool. OFF (the default) wires NewRouter(nil) → the existing
	// deterministic pool[0] fallback, byte-identical to today (zero cost, zero change).
	JudgeEnabled bool
}

// ConcurrencyEnabled reports whether this config opts the agent into the executor
// concurrency path (MaxConcurrentTasks>0 AND ≥1 allowed executor); otherwise the
// agent keeps the legacy single-active inject path. Delegates to
// agent.Profile.ConcurrencyEnabled so the daemon pool gate and the center's ≤N start
// cap share ONE predicate (issue-b8687f2a §2). Moved from the daemon's
// concurrent_exec.go into the runtime domain (issue-68ccb310 domain cleanup).
func (c ExecutorConfig) ConcurrencyEnabled() bool {
	return agent.Profile{
		MaxConcurrentTasks: c.MaxConcurrentTasks,
		AllowedExecutors:   c.AllowedExecutors,
	}.ConcurrencyEnabled()
}

// AttachExecutor installs the executor engine onto the runtime (under the shared
// mutex, exactly as ma.exec was set under c.mu). Called by the daemon's
// maybeAttachExecutorEngine / reattach paths.
func (r *LocalRuntime) AttachExecutor(ee *ExecutorEngine) {
	r.mu.Lock()
	r.exec = ee
	r.mu.Unlock()
}

// AttachExecutorEngine builds this agent's executor engine from pl and installs it,
// computing its OWN home (so a standalone agent-runtime process can attach without the
// daemon's agentPaths). It is the process-model equivalent of the daemon's
// maybeAttachExecutorEngine (T854 D6 fix): the agent-runtime process calls it BEFORE
// Boot — so selfReconcile has an engine to recover in-flight executors after a restart
// — and on the first concurrency-enabling reconcile. A no-op'd caller (empty/plain
// config) simply never calls it, leaving the single-active inject path.
func (r *LocalRuntime) AttachExecutorEngine(pl ExecutorConfig) error {
	home, _, _, err := r.agentPaths(r.cfg.AgentID)
	if err != nil {
		return err
	}
	ee, err := r.BuildExecutorEngine(home, pl)
	if err != nil {
		return err
	}
	r.AttachExecutor(ee)
	return nil
}

// routerConfigOf projects an ExecutorConfig onto the model-routing Config. The SINGLE
// source of truth for both the boot-time engine build (BuildExecutorEngine) and the
// live in-place refresh (UpdateExecutorConfig) so the two can never drift.
func routerConfigOf(pl ExecutorConfig) modelrouter.Config {
	return modelrouter.Config{
		OrchestratorModel:    pl.OrchestratorModel,
		AllowedExecutors:     routerCandidates(pl.AllowedExecutors),
		DefaultExecutorModel: pl.DefaultExecutorModel,
		SupervisorModel:      pl.SupervisorModel,
		DefaultCLI:           pl.CLI,
	}
}

// UpdateExecutorConfig refreshes the model-routing config of the ALREADY-ATTACHED
// engine in place — the counterpart to AttachExecutorEngine for the HasExecutor case.
// A live profile edit (config-triggered reconcile) must update the pool's model routing
// WITHOUT rebuilding the engine, because a rebuild would drop live executors/orphans
// (exactly the invariant the reconcile handler's !HasExecutor() guard protects). Only
// subsequent forks see the new model; in-flight executors keep running. Also refreshes
// the self-held exec config so a later boot self-reconcile / relaunch uses the new
// routing. No-op when no engine is attached.
func (r *LocalRuntime) UpdateExecutorConfig(pl ExecutorConfig) {
	ee := r.execEngine()
	if ee == nil {
		return
	}
	ee.engine.UpdateRouterConfig(routerConfigOf(pl))
	r.cacheExecConfig(pl)
}

// HasExecutor reports whether an executor engine is attached (the exec-vs-session
// branch predicate — exactly today's ma.exec != nil semantics).
func (r *LocalRuntime) HasExecutor() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.exec != nil
}

// execEngine reads the attached engine under the shared lock (nil when none).
func (r *LocalRuntime) execEngine() *ExecutorEngine {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.exec
}

// BuildExecutorEngine constructs the per-agent Engine + Monitor. Workspaces are
// plain isolated directories — PD ruling B: production agents have no per-agent
// source git repo, so the F1 worktree step is skipped. agentRoot is the per-agent home.
func (r *LocalRuntime) BuildExecutorEngine(agentRoot string, pl ExecutorConfig) (*ExecutorEngine, error) {
	// T848 §4.4: self-hold the config so boot self-reconcile can relaunch a recovered
	// executor with the same env / model routing WITHOUT a daemon-side cache.
	r.cacheExecConfig(pl)
	clk := funcClock{now: r.cfg.Now}
	layout, err := executor.NewLayout(agentRoot)
	if err != nil {
		return nil, err
	}
	fx, err := executor.NewFileExchange(layout, clk)
	if err != nil {
		return nil, err
	}
	// Tracker persists the orchestrator-private recovery Record (pid) per launch so a
	// restarted daemon can probe + re-adopt orphans (W3 / design §12).
	tracker, err := executor.NewTracker(layout)
	if err != nil {
		return nil, err
	}
	pool, err := executor.NewPool(executor.PoolConfig{
		Exchange:   fx,
		AgentRoot:  agentRoot,
		BinaryPath: r.cfg.BinaryPath,
		Tracker:    tracker,
		Max:        pl.MaxConcurrentTasks,
		Clock:      clk,
		AgentEnv:   runtimeAgentEnv(pl.AgentID, pl.DisplayName, pl.EnvVars),
		// No Worktrees/BaseRef → plain-dir workspaces (PD ruling B).
	})
	if err != nil {
		return nil, err
	}
	routing, err := executor.NewRoutingStore(agentRoot, clk)
	if err != nil {
		return nil, err
	}
	// issue-93dd8daa ②: per-agent gated (default OFF) LLM difficulty judge. OFF →
	// NewRouter(nil) = the existing deterministic pool[0] fallback, byte-identical to
	// today. ON → a one-shot SubprocessJudge on the cheap orchestrator model (an empty
	// ClaudeBinary defaults to PATH "claude", the deployment norm). It is nil ONLY when
	// no orchestrator_model is resolvable → NewRouter(nil) fallback.
	//
	// FAIL-LOUD (T950 re-Dev, tester3 P1): if judge_enabled=true but the judge ends up
	// nil, we MUST log a LOUD warning. The original bug was exactly this path failing
	// SILENTLY (empty binary → nil judge → judge_enabled inert in production with no
	// signal). A no-warning inert switch is more dangerous than a hard error.
	var judge modelrouter.DifficultyJudge
	if pl.JudgeEnabled {
		if sj := orchestrator.NewSubprocessJudge(orchestrator.JudgeConfig{
			OrchestratorModel: pl.OrchestratorModel,
			Binary:            r.cfg.ClaudeBinary,
			Log:               r.log,
		}); sj != nil {
			judge = sj
		} else {
			r.log("modelrouter judge: WARNING judge_enabled=true but judge DISABLED "+
				"(no orchestrator_model resolvable) — routing falls back to deterministic "+
				"pool[0]; set the agent's orchestrator_model to enable the difficulty judge (agent=%s)", pl.AgentID)
		}
	}
	eng, err := orchestrator.NewEngine(orchestrator.EngineConfig{
		Pool:         pool,
		Routing:      routing,
		Router:       modelrouter.NewRouter(judge),
		RouterConfig: routerConfigOf(pl),
		Runners: map[string]orchestrator.RunnerCmdBuilder{
			agent.DefaultExecutorCLI: orchestrator.NewClaudeRunnerBuilder(r.cfg.ClaudeBinary),
			CLICodex:                 orchestrator.NewCodexRunnerBuilder(r.cfg.CodexBinary),
		},
		IDs:   orchestrator.NewULIDMinter(clk),
		Clock: clk,
		// I109 ①: the orchestrator holds the center credentials, so it — not the frozen,
		// center-less executor — resolves the issue refs a task brief cites, and inlines
		// their bodies into the prompt before the fork.
		Issues: centerIssueResolver{r: r, agentID: pl.AgentID},
		Log:    r.log,
	})
	if err != nil {
		return nil, err
	}
	watchdog := executor.NewWatchdog(executor.WatchdogConfig{Clock: clk})
	reconciler, err := executor.NewReconciler(fx, tracker, nil) // nil probe → SignalLiveness
	if err != nil {
		return nil, err
	}
	var wb executor.Writeback
	caller := r.toolCaller()
	if cc := newCenterClient(caller); cc != nil {
		cwb, werr := orchestrator.NewCenterWriteback(cc, fx, pl.AgentID)
		if werr != nil {
			return nil, werr
		}
		cwb.WithUsageReporter(newUsageReporter(caller))
		cwb.WithDeliveryReporter(newDeliveryReporter(caller))
		// option b (issue-68ccb310): deliver a finished executor's result to THIS agent's
		// supervisor session for a judged completion, instead of auto-completing.
		cwb.WithSupervisorInjector(r.injectToSupervisor)
		wb = cwb
	}
	// P4/P5: when the repo-workspace flag is ON (materializer wired), give the Monitor
	// the Tracker (to read each executor's RepoKey/SourcePath teardown handle) + a
	// WorktreeCleaner adapter (executor→reporepo, defined here so the executor package
	// never imports reporepo — red line D). nil when off ⇒ no worktree cleanup, today's
	// behavior byte-for-byte.
	var cleaner executor.WorktreeCleaner
	var cleanupTracker *executor.Tracker
	if r.cfg.Materializer != nil {
		cleaner = materializerCleaner{m: r.cfg.Materializer}
		cleanupTracker = tracker
	}
	mon, err := executor.NewMonitor(executor.MonitorConfig{
		Exchange:        fx,
		Pool:            pool,
		Watchdog:        watchdog,
		Reconciler:      reconciler,
		Writeback:       wb,
		Tracker:         cleanupTracker,
		WorktreeCleaner: cleaner,
		Clock:           clk,
		Observer:        executorActivityObserver{r: r, agentID: pl.AgentID},
		Log:             r.log, // T969: fail-loud sink for codex thread_id capture
	})
	if err != nil {
		return nil, err
	}
	return &ExecutorEngine{engine: eng, monitor: mon, fx: fx}, nil
}

// workViaExecutor handles an agent.work brief by forking an executor (the executor
// concurrent path) instead of injecting into the resident claude. At capacity it
// returns a retryable error so the control loop re-pulls the command on the next tick.
func (r *LocalRuntime) workViaExecutor(ctx context.Context, req WorkRequest, ee *ExecutorEngine) error {
	title := firstNonEmptyLine(req.Brief)
	if title == "" {
		title = "task " + req.TaskID
	}
	item := orchestrator.WorkItem{
		TaskID:  req.TaskID,
		TaskRef: req.TaskRef,
		Goal:    executor.Goal{Title: title, Description: req.Brief},
		// I105 N2: carry the command's routing decision through to input.json. NotifyWork
		// only reaches here when the node is NOT supervisor_inline, so this is normally
		// "" / executor_fork; a stamped inline value would be an N4 case.
		DispatchMode: strings.TrimSpace(req.DispatchMode),
	}
	return r.launchExecutor(ctx, req.AgentID, req.TaskID, item, ee)
}

// beginTaskFork is the per-task single-flight gate. It deliberately does not wait:
// duplicate durable commands are already satisfied by the in-flight/live executor,
// while blocking here would spend the agent-control delivery deadline on work that
// must not run twice anyway. Different task ids never block one another.
func (r *LocalRuntime) beginTaskFork(taskID string) (bool, string) {
	r.forkStateMu.Lock()
	defer r.forkStateMu.Unlock()
	if execID := r.taskExecutors[taskID]; execID != "" {
		return false, execID
	}
	if _, exists := r.forkingTasks[taskID]; exists {
		return false, ""
	}
	if r.forkingTasks == nil {
		r.forkingTasks = make(map[string]struct{})
	}
	r.forkingTasks[taskID] = struct{}{}
	return true, ""
}

func (r *LocalRuntime) endTaskFork(taskID string) {
	r.forkStateMu.Lock()
	delete(r.forkingTasks, taskID)
	r.forkStateMu.Unlock()
}

// trackPreparingExecutor closes the orphan-prune visibility gap between creating a
// worktree and Pool.Launch reserving/tracking the executor. The returned release is
// idempotent by construction (called once via defer by SpawnExecutor).
func (r *LocalRuntime) trackPreparingExecutor(execID string) func() {
	if execID == "" {
		return func() {}
	}
	r.forkStateMu.Lock()
	if r.preparingExecutors == nil {
		r.preparingExecutors = make(map[string]struct{})
	}
	r.preparingExecutors[execID] = struct{}{}
	r.forkStateMu.Unlock()
	return func() {
		r.forkStateMu.Lock()
		delete(r.preparingExecutors, execID)
		r.forkStateMu.Unlock()
	}
}

func (r *LocalRuntime) trackTaskExecutor(taskID, execID string) {
	if taskID == "" || execID == "" {
		return
	}
	r.forkStateMu.Lock()
	if r.taskExecutors == nil {
		r.taskExecutors = make(map[string]string)
	}
	r.taskExecutors[taskID] = execID
	r.forkStateMu.Unlock()
}

func (r *LocalRuntime) untrackTaskExecutor(taskID, execID string) {
	r.forkStateMu.Lock()
	if r.taskExecutors[taskID] == execID {
		delete(r.taskExecutors, taskID)
	}
	r.forkStateMu.Unlock()
}

// executorForTask checks both the explicit live-task registry and recovered/adopted
// executor snapshots. Snapshot enrichment reads input.json, the durable task binding.
func (r *LocalRuntime) executorForTask(ee *ExecutorEngine, taskID string) (string, bool) {
	r.forkStateMu.Lock()
	execID := r.taskExecutors[taskID]
	r.forkStateMu.Unlock()
	if execID != "" {
		return execID, true
	}
	for _, snap := range ee.SnapshotConcurrency() {
		if snap.TaskID == taskID {
			return snap.ExecutorID, true
		}
	}
	return "", false
}

// launchExecutor is the shared fork tail used by the legacy agent.work branch.
// Per-task single-flight prevents duplicate work; Pool.Launch independently enforces
// the atomic ≤N cap while allowing distinct tasks to provision and spawn in parallel.
func (r *LocalRuntime) launchExecutor(ctx context.Context, agentID, taskID string, item orchestrator.WorkItem, ee *ExecutorEngine) error {
	if existing, ok := r.executorForTask(ee, taskID); ok {
		r.log("FORK-COALESCED agent_namespace=%s task_id=%s executor_id=%s reason=already_active", agentID, taskID, existing)
		return nil
	}
	started, existing := r.beginTaskFork(taskID)
	if !started {
		r.log("FORK-COALESCED agent_namespace=%s task_id=%s executor_id=%s reason=already_in_flight", agentID, taskID, existing)
		return nil
	}
	defer r.endTaskFork(taskID)
	_, err := r.launchExecutorNow(ctx, agentID, taskID, item, ee)
	return err
}

// codexAuthPreflight returns a fail-loud warning (and true) when a cli=codex executor
// is about to fork but its $CODEX_HOME/auth.json login state is not resolvable — either
// CODEX_HOME is unset (so the executor won't inherit it via the allowlist) or auth.json
// is missing. Returns ("", false) for healthy codex auth or any non-codex cli. Pure over
// (cli, codexHome, statFn) so the T962 hard-acceptance fail-loud is unit-locked (mirrors
// the T950 judge fail-loud regression lock — a silent config-source-missing death is the
// worst mode).
func codexAuthPreflight(cli, codexHome string, statFn func(string) error) (string, bool) {
	if cli != CLICodex {
		return "", false
	}
	home := strings.TrimSpace(codexHome)
	if home == "" {
		return "CODEX_HOME is unset in the worker env — cannot inherit $CODEX_HOME/auth.json, " +
			"codex will FAIL authentication; run `codex login` and set CODEX_HOME", true
	}
	if err := statFn(filepath.Join(home, "auth.json")); err != nil {
		return "CODEX_HOME=" + home + " but auth.json is missing — codex will FAIL authentication; run `codex login`", true
	}
	return "", false
}

// statErr adapts os.Stat to the codexAuthPreflight statFn signature.
func statErr(path string) error { _, err := os.Stat(path); return err }

// launchExecutorNow forks an executor for a WorkItem after the caller has acquired
// the task single-flight gate. At capacity it returns ErrAtCapacity; on success it
// registers task→executor before starting the drain goroutine, closing the duplicate
// request race even for a very short-lived child process.
func (r *LocalRuntime) launchExecutorNow(ctx context.Context, agentID, taskID string, item orchestrator.WorkItem, ee *ExecutorEngine) (*orchestrator.Launched, error) {
	// issue-d118b5dc instrument: the single commit point where an executor fork is actually
	// launched (reached from BOTH SpawnExecutor(fork_executor) and workViaExecutor(work)).
	// Fail-loud entry log with the forking runtime's namespace so a repro can correlate WHICH
	// runtime committed the fork for a given task_id (pairs with the DISPATCH-DECISION logs to
	// prove a ① dual fan-out / ② cross-namespace fork). Instrument-only, no behavior change.
	r.log("DISPATCH-FORK-ENTRY agent_namespace=%s task_id=%s — launchExecutorNow committing an executor fork", agentID, taskID)
	launched, err := ee.engine.HandleWork(ctx, item)
	if err != nil {
		if errors.Is(err, executor.ErrAtCapacity) {
			return nil, fmt.Errorf("agent_controller: agent=%s at executor capacity (queue, retry next tick): %w", agentID, err)
		}
		return nil, fmt.Errorf("agent_controller: agent=%s fork executor: %w", agentID, err)
	}
	r.log("agent=%s task=%s forked executor=%s cli=%s model=%s(%s) problem=%s",
		agentID, taskID, launched.ExecutorID, launched.CLI, launched.Model, launched.ModelSource, launched.ProblemID)

	// T962 BE-2 HARD fail-loud (pd acceptance point): a cli=codex executor authenticates
	// via $CODEX_HOME/auth.json, which reaches the executor ONLY if CODEX_HOME is set in
	// the worker env (the allowlist then passes it through, executorenv.go). If CODEX_HOME
	// is unset OR auth.json is missing, the codex exec will fail auth — warn LOUDLY at fork
	// rather than let it die opaquely or silently fork-fail (the codex analogue of the T950
	// judge fail-loud; a silent config-source-missing death is the worst mode).
	if msg, warn := codexAuthPreflight(launched.CLI, os.Getenv("CODEX_HOME"), statErr); warn {
		r.log("agent=%s executor=%s WARNING codex executor: %s", agentID, launched.ExecutorID, msg)
	}

	// T758: the fork just succeeded — emit executor.start into the activity stream.
	r.emitExecutorStart(agentID, item.TaskRef, item.Goal.Title, launched)

	// Persist the executor system prompt to executors/<executor_id>/SYSTEM.md.
	if ee.fx != nil {
		if execDir, dirErr := ee.fx.Layout().Dir(launched.ExecutorID); dirErr == nil {
			sysPromptPath := filepath.Join(execDir, "SYSTEM.md")
			if wErr := os.WriteFile(sysPromptPath, []byte(orchestrator.ExecutorSystemPrompt()), 0o600); wErr != nil {
				r.log("agent=%s executor=%s warn: write SYSTEM.md: %v", agentID, launched.ExecutorID, wErr)
			}
		}
	}

	// Register before the drain goroutine can observe an instant child exit; the drain
	// keeps this binding until recovery/finalization completes.
	r.trackTaskExecutor(taskID, launched.ExecutorID)

	// Mirror the inject path's work-state bookkeeping under the SessionState lock.
	r.mu.Lock()
	if r.state != nil {
		r.state.HadWork = true
		if taskID != "" {
			r.state.CurrentTaskID = taskID
			r.state.CurrentConversationID = ""
		}
	}
	r.mu.Unlock()

	// Reap the executor when it exits, freeing its pool slot. Runs detached.
	go r.drainExecutor(ee, taskID, launched.Handle)
	return launched, nil
}

// drainExecutor blocks until the forked (this-process) executor exits, then hands it to
// the §4.6 point-recovery policy. It harvests + classifies WITHOUT finalizing
// (AwaitCompletionNoFinalize) so the worktree/session survive if the runtime decides to
// recover in place; reconcileOneExecutor then either recovers (should-continue + budget)
// or terminally finalizes (success / gone task / budget exhausted). This is the reactive,
// immediate death detection for a this-process child (it holds a reapable handle — no
// lease/poll). wasStall carries the watchdog stall-kill cause for the tiered budget.
func (r *LocalRuntime) drainExecutor(ee *ExecutorEngine, taskID string, h *executor.Handle) {
	if ee == nil || h == nil {
		return
	}
	defer r.untrackTaskExecutor(taskID, h.ExecutorID)
	comp, wasStall, err := ee.monitor.AwaitCompletionNoFinalize(h)
	if err != nil {
		// Rare (harvest/classify error): fall back to the plain finalize so the slot is
		// never leaked, matching the pre-§4.6 drain behavior.
		r.log("agent executor=%s drain classify: %v — finalizing", h.ExecutorID, err)
		if fErr := ee.monitor.Finalize(context.Background(), comp); fErr != nil {
			r.log("agent executor=%s drain finalize: %v", h.ExecutorID, fErr)
		}
		return
	}
	r.reconcileOneExecutor(context.Background(), ee, h.ExecutorID, comp, wasStall, true /*thisProcess*/)
}

// Recover rebuilds in-flight executor state the FIRST time this daemon process
// manages the agent's concurrency (design §12 "监工重启扫 executors/"). It drives the
// F5 Monitor.Recover, which — from durable files alone, never re-spawning — finalizes
// terminal/crashed orphans and re-adopts still-alive ones into the pool. The
// still-alive orphans are registered here so the watchdog tick polls them to
// completion, since they carry no reapable handle.
//
// The recovery-once-per-agent-per-process guard lives on the DAEMON (recoveredExec):
// a later in-process engine rebuild must NOT re-scan (its running executors are THIS
// process's children, reaped by drainExecutor — re-adopting would double-finalize).
func (r *LocalRuntime) Recover(ctx context.Context) error {
	agentID := r.cfg.AgentID
	ee := r.execEngine()
	if ee == nil {
		return nil
	}
	if err := r.recoverExecutors(ctx, agentID, ee); err != nil {
		return err
	}
	// v2.31.1 orphan-reap (boot hook): after recovery has re-adopted the still-live
	// orphans, reap worktrees left by a prior process whose executor is gone (full-crash:
	// the retryable-crash kept its worktree, the task re-dispatched fresh-id, nothing
	// cleaned it). isLive = the post-recovery snapshot (adopted-running); fail-safe KEEP.
	if r.cfg.Materializer != nil {
		live := r.liveExecIDs(ee)
		if n, err := r.cfg.Materializer.ReapOrphanWorktrees(ctx, func(id string) bool { return live[id] }); err != nil {
			r.log("agent=%s boot orphan-worktree reap: %v (non-fatal)", agentID, err)
		} else if n > 0 {
			r.log("agent=%s boot-reaped %d orphan worktree(s)", agentID, n)
		}
	}
	return nil
}

// executorIDLive dynamically answers the spawn-time prune callback. It must not be
// replaced with a captured snapshot: another task can register and prepare a sibling
// worktree while this prune waits on the materializer's per-repo lock.
func (r *LocalRuntime) executorIDLive(ee *ExecutorEngine, execID string) bool {
	r.forkStateMu.Lock()
	_, preparing := r.preparingExecutors[execID]
	r.forkStateMu.Unlock()
	if preparing {
		return true
	}
	for _, s := range ee.SnapshotConcurrency() {
		if s.ExecutorID == execID {
			return true
		}
	}
	return false
}

// liveExecIDs snapshots the currently live executor id set (pool-active + adopted
// orphans + worktree-prepared/in-flight). The last set closes the interval between
// PrepareWorktree and Pool.Launch: a concurrent spawn must not prune a sibling's
// newly prepared worktree merely because the Pool has not registered it yet.
func (r *LocalRuntime) liveExecIDs(ee *ExecutorEngine) map[string]bool {
	live := map[string]bool{}
	for _, s := range ee.SnapshotConcurrency() {
		if s.ExecutorID != "" {
			live[s.ExecutorID] = true
		}
	}
	r.forkStateMu.Lock()
	for id := range r.preparingExecutors {
		live[id] = true
	}
	r.forkStateMu.Unlock()
	return live
}

func (r *LocalRuntime) recoverExecutors(ctx context.Context, agentID string, ee *ExecutorEngine) error {
	items, err := ee.monitor.Recover(ctx)
	if err != nil {
		r.log("agent=%s executor recover: %v", agentID, err)
		return err
	}
	var adopted, finalized, lost int
	for _, it := range items {
		if it.Completion.Kind != executor.OutcomeRunning {
			finalized++
			continue
		}
		if it.Record != nil && it.Record.PID > 0 {
			ee.addOrphan(it.ExecutorID, it.Record.PID)
			adopted++
		} else {
			lost++
			r.log("agent=%s recovered running executor=%s lacks a pid record — not watchdog-tracked", agentID, it.ExecutorID)
		}
	}
	if adopted > 0 || finalized > 0 || lost > 0 {
		r.log("agent=%s executor crash-recovery: scanned=%d adopted=%d finalized=%d untracked=%d",
			agentID, len(items), adopted, finalized, lost)
	}
	return nil
}

// RunWatchdog is the per-agent executor watchdog sweep (W3), driven by the daemon's
// OnTick (throttled daemon-side to DefaultExecutorWatchdogInterval so the 30s cadence
// stays byte-identical). No-op when no engine is attached. It (1) Sweeps the live
// this-process handles, graceful-killing any that stalled, and (2) polls each ADOPTED
// ORPHAN via CheckOrphan — the orphans have no reapable handle, so this poll is the
// only thing that detects their stall or completion and finalizes them.
func (r *LocalRuntime) RunWatchdog(ctx context.Context) {
	agentID := r.cfg.AgentID
	ee := r.execEngine()
	if ee == nil {
		return
	}
	// 0. T758: sample each live executor's progress into the activity stream.
	ee.monitor.SampleProgress()
	// 1. Stall-sweep the live (this-process) executors.
	if killed, err := ee.monitor.Sweep(ctx); err != nil {
		r.log("agent=%s executor watchdog sweep: %v", agentID, err)
	} else if len(killed) > 0 {
		r.log("agent=%s watchdog killed stalled executors: %v", agentID, killed)
	}
	// 2. Poll the adopted orphans (no reapable handle): stall + completion. §4.6: a
	//    terminal orphan goes to point recovery (recover in place vs finalize) — NOT the
	//    old auto-finalize+teardown. CheckOrphanNoFinalize kills a stalled orphan + classifies
	//    but does NOT finalize (so the worktree survives for a tier1/2 relaunch); the runtime
	//    then decides. Skip one already being recovered (the recovering-set also guards, this
	//    just avoids a wasted CheckOrphan syscall/kill on the same tick).
	for id, pid := range ee.snapshotOrphans() {
		if ee.isRecovering(id) {
			continue
		}
		comp, terminal, _, wasStall, err := ee.monitor.CheckOrphanNoFinalize(ctx, id, pid)
		if err != nil {
			r.log("agent=%s orphan watchdog executor=%s: %v", agentID, id, err)
			continue
		}
		if terminal {
			r.reconcileOneExecutor(ctx, ee, id, comp, wasStall, false /*orphan*/)
		}
	}
	// 3. Delayed-teardown reap (issue-f30b7e7b): remove terminal executors retained by
	//    the delayed teardown once they pass the TTL / exceed the count cap. This is the
	//    k8s TTL-after-finished + terminated-pod-GC analog, run agent-runtime-local (the
	//    center only holds the writeback). Best-effort; a reap error never fails the tick.
	if n, err := ee.monitor.ReapFinalized(ctx, finalizedRetainTTL, finalizedRetainCap); err != nil {
		r.log("agent=%s finalized-executor reap: %v (non-fatal)", agentID, err)
	} else if n > 0 {
		r.log("agent=%s reaped %d finalized executor(s)", agentID, n)
	}
}

// finalizedRetainTTL / finalizedRetainCap bound the delayed teardown of TERMINAL
// executors (issue-f30b7e7b). A finished executor's dir + worktree are RETAINED for
// up to the TTL (long enough for the supervisor to push its branch + audit its work,
// short enough to bound disk), and at most Cap are kept regardless of TTL (the oldest
// beyond that are reaped early). Consts for now — promote to worker config if
// per-deployment tuning is needed.
const (
	finalizedRetainTTL = 30 * time.Minute
	finalizedRetainCap = 20
)

// SnapshotConcurrency returns this agent's executor snapshots (nil when no engine).
func (r *LocalRuntime) SnapshotConcurrency() []concurrency.ExecutorSnapshot {
	ee := r.execEngine()
	if ee == nil {
		return nil
	}
	return ee.SnapshotConcurrency()
}

// ---------------------------------------------------------------------------
// SpawnExecutor — the explicit supervisor-driven fork_executor sequence.
// ---------------------------------------------------------------------------

// SpawnExecutor either admits an open/reopened task through the center or consumes a
// Supervisor's already-admitted running task, then forks an executor. Duplicate calls
// for the same task coalesce; distinct tasks proceed concurrently and Pool.Launch owns
// the atomic ≤N cap. It is best-effort and NON-WEDGING — bounded control-path failures
// are surfaced/logged and do not hold the worker's shared control cursor indefinitely.
//
// NON-WEDGING IS A DEADLINE CLAIM, NOT JUST A RETURN-VALUE CLAIM. A caller may be a
// control-command handler whose transport deadline is 5s (see source_prewarm.go), so
// every step below MUST be bounded and fast: local-disk work, or a center RPC. It must
// NOT perform "possibly minutes" network I/O — that is what issue-13e7bfe8 was: an
// inline `git clone` here could not finish inside the deadline, so the command was never
// acked and the worker's shared control cursor re-delivered it forever, starving every
// other agent on that worker. Repo-source materialization is therefore deferred to a
// background prewarm; if you add a slow step here, it belongs there instead.
//
// "Left queued" is likewise NOT self-healing on its own: the center's wake-sweep
// re-drives a queued task only while the agent has NO running task. Any new path that
// leaves a task queued must either be re-driven by its own follow-up (as the prewarm
// gate does) or be surfaced loudly — never silently dropped.
func (r *LocalRuntime) SpawnExecutor(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	agentID := r.cfg.AgentID
	taskID := strings.TrimSpace(req.TaskID)
	ee := r.execEngine()
	if ee == nil {
		r.log("fork_executor agent=%s task=%s SpawnExecutor: no executor engine — left queued", agentID, taskID)
		return nil, nil
	}

	if strings.TrimSpace(taskID) == "" {
		r.log("fork_executor agent=%s concurrency fork: empty task_id — skipping", agentID)
		return nil, nil
	}
	r.log("DISPATCH-FORK-REQUEST route=fork_executor agent_namespace=%s task_id=%s — SpawnExecutor entry",
		agentID, taskID)
	started, existing := r.beginTaskFork(taskID)
	if !started {
		r.log("FORK-COALESCED agent_namespace=%s task_id=%s executor_id=%s reason=already_in_flight", agentID, taskID, existing)
		return nil, nil
	}
	defer r.endTaskFork(taskID)

	caller := r.toolCaller()
	if caller == nil {
		r.log("fork_executor agent=%s task=%s concurrency fork: no ToolCaller — left queued", agentID, taskID)
		return nil, nil
	}

	// 1. Pull the task detail to build the WorkItem (title/description/model).
	task, err := r.fetchCenterTask(ctx, agentID, taskID)
	if err != nil {
		r.log("fork_executor agent=%s task=%s get_task: %v — left queued", agentID, taskID, err)
		return nil, nil
	}

	// issue-d118b5dc ② foreign-assignee guard: SpawnExecutor forks for whatever task_id the
	// fork_executor command carried, so a fork request for a task assigned to agent B
	// that reaches agent A's runtime would have A fork an executor for B's task = a cross-
	// namespace fork (A also force-starts it as itself). Guard: if the fetched task's assignee
	// names an agent OTHER than this runtime, SKIP — do not start_task, do not fork, leave the
	// task queued for its real assignee. The decision is logged loud (keeper) so a stray
	// cross-namespace signal is still visible.
	//
	// The compare MUST key on the center agent-ref (identityRef — the "agent:<ref>" namespace
	// task.Assignee is rendered in), NOT the ULID r.cfg.AgentID: assignee is never a ULID, so
	// taskReassigned(assignee, cfg.AgentID) would report "reassigned" for EVERY normal
	// same-agent fork (false positive → skipping every dispatch). This mirrors the
	// boot-reconcile / point-recovery identity compare (taskCancelEvidence uses identityRef).
	if ref := r.identityRef(); taskReassigned(task.Assignee, ref) {
		r.log("DISPATCH-CROSS-NAMESPACE agent_namespace=%s agent_ref=%s task_id=%s task_assignee=%q — fork_executor on a task NOT assigned to this runtime (issue-d118b5dc ②); SKIPPING fork (foreign-assignee guard), left queued for the real assignee",
			agentID, ref, taskID, task.Assignee)
		return nil, nil
	}

	// A task explicitly marked supervisor_inline must be handled by the supervisor,
	// not by this fork primitive. work_available already nudges the supervisor; if it
	// calls fork_executor anyway, refuse loudly and leave the task queued.
	if strings.TrimSpace(task.DispatchMode) == executor.DispatchModeSupervisorInline {
		r.log("FORK-REJECTED route=fork_executor dispatch_mode=supervisor_inline agent_namespace=%s task_id=%s — supervisor_inline tasks must be handled inline by the supervisor",
			agentID, taskID)
		return nil, nil
	}

	// The supervisor-driven protocol has TWO valid entry states:
	//   - open/reopened (or legacy empty): fork_executor owns start_task admission;
	//   - running: the Supervisor already called start_task, so skip admission and fork.
	// The previous auto-dispatch-era guard rejected the second case with nil error. The
	// worker then acked the durable command despite creating no executor — the dominant
	// production failure mode after dispatch became supervisor-driven.
	//
	// Parked/terminal/unknown states remain fail-closed. A legacy running task carrying
	// blocked_reason is parked even though its status still says running (ADR-0046).
	alreadyAdmitted := false
	if reason := strings.TrimSpace(task.BlockedReason); reason != "" {
		r.log("FORK-REJECTED agent_namespace=%s task_id=%s status=%s reason=task_blocked — blocked_reason is non-empty", agentID, taskID, strings.TrimSpace(task.Status))
		return nil, nil
	}
	switch st := strings.TrimSpace(task.Status); st {
	case "running":
		alreadyAdmitted = true
	case "", "open", "reopened":
		// fork_executor performs admission below.
	default:
		r.log("FORK-REJECTED agent_namespace=%s task_id=%s status=%s reason=task_not_dispatchable", agentID, taskID, st)
		return nil, nil
	}
	if activeID, active := r.executorForTask(ee, taskID); active {
		r.log("FORK-COALESCED agent_namespace=%s task_id=%s executor_id=%s reason=already_active", agentID, taskID, activeID)
		return nil, nil
	}
	if alreadyAdmitted {
		r.log("FORK-ADMISSION-REUSED agent_namespace=%s task_id=%s status=running — Supervisor already admitted task; continuing to executor launch", agentID, taskID)
	}

	// A forked node is a code task. It must have a center-resolved repository and a
	// runtime materializer; otherwise fail before any model process starts. Starting
	// then blocking records the durable non-delivery instead of leaving an open task
	// to be re-emitted forever.
	codeDispatch := strings.TrimSpace(task.DispatchMode) == executor.DispatchModeExecutorFork
	if codeDispatch && (task.Repo == nil || strings.TrimSpace(task.Repo.URL) == "") {
		if alreadyAdmitted {
			r.blockTaskOnForkFailure(ctx, agentID, taskID, errors.New("code task has no resolved repo_ref"))
		} else if err := r.startCenterTask(ctx, agentID, taskID); err == nil {
			r.blockTaskOnForkFailure(ctx, agentID, taskID, errors.New("code task has no resolved repo_ref"))
		}
		r.log("fork_executor agent=%s task=%s repository preflight failed: missing repo_ref — executor NOT forked", agentID, taskID)
		return nil, nil
	}
	if task.Repo != nil && r.cfg.Materializer == nil && r.cfg.CloneMaterializer == nil {
		if alreadyAdmitted {
			r.blockTaskOnForkFailure(ctx, agentID, taskID, errors.New("code task repo workspace materializer unavailable"))
		} else if err := r.startCenterTask(ctx, agentID, taskID); err == nil {
			r.blockTaskOnForkFailure(ctx, agentID, taskID, errors.New("code task repo workspace materializer unavailable"))
		}
		r.log("fork_executor agent=%s task=%s repository preflight failed: workspace materializer unavailable — executor NOT forked", agentID, taskID)
		return nil, nil
	}

	// 2. Repo workspace (P3, red line A): materialize the canonical source + a
	// per-executor worktree BEFORE start_task/reserve, so a source/worktree failure
	// means the task is NEVER admitted. Production always wires the materializer for
	// code tasks; the legacy plain-dir fallback is reachable only for an unstamped task
	// from an older center.
	//
	// The source is NOT materialized here (issue-13e7bfe8 layer 1). SpawnExecutor can be
	// reached from a control command whose transport deadline is 5s, and a `git clone` is
	// minutes — running it inline blew that deadline, wedged the worker's SHARED control
	// cursor behind an un-ackable command (420 retries in prod, every agent on the worker
	// starved), and SIGKILLed git mid-clone into the canonical path. So this path only
	// ever reads an already-materialized source from the prewarm gate; a miss defers the
	// task to a BACKGROUND clone (source_prewarm.go) which re-drives it on completion.
	// Everything below the gate is local-disk or center-RPC, i.e. control-path-safe.
	var execID string
	var prepared *executor.PreparedWorkspace
	if task.Repo != nil && r.cfg.Materializer != nil {
		target := resolveRepoTarget(task)
		repoKey := reporepo.RepoKey(target.URL)
		source, ready := r.freshSource(repoKey)
		if !ready && req.redrive {
			// This spawn IS the prewarm's re-drive: take the source it just materialized
			// even if the freshness window lapsed while cloning. Re-deferring here would
			// bounce back into another prewarm and livelock (see SpawnRequest.redrive).
			source, ready = r.anySource(repoKey)
		}
		if !ready {
			// Return NOW so the control command acks; the background materialize calls
			// SpawnExecutor again for this task once the source lands.
			r.deferForSource(agentID, taskID, repoKey, target)
			return nil, nil
		}
		execID = ee.engine.NewExecutorID() // must be known BEFORE PrepareWorktree (path+branch embed it)
		releasePreparing := r.trackPreparingExecutor(execID)
		defer releasePreparing()
		wsPath, wsErr := ee.fx.Layout().WorkspaceDir(execID)
		if wsErr != nil {
			r.log("fork_executor agent=%s task=%s resolve workspace: %v — left queued", agentID, taskID, wsErr)
			return nil, nil
		}
		// v2.31.1 orphan-reap (spawn hook): before adding THIS executor's worktree, reap
		// any orphaned worktrees under this source (a prior retryable-crash's kept worktree
		// whose task was re-dispatched fresh-id — never reused, nothing else cleans it).
		// Concurrent same-agent spawns are valid. The callback dynamically checks the
		// preparing registry (not a stale snapshot), so one spawn cannot reap a sibling
		// between that sibling's PrepareWorktree and Pool.Launch.
		if n, perr := r.cfg.Materializer.PruneOrphanWorktrees(ctx, source, func(id string) bool { return r.executorIDLive(ee, id) }); perr != nil {
			r.log("fork_executor agent=%s repo_key=%s prune orphan worktrees: %v (non-fatal)", agentID, source.RepoKey, perr)
		} else if n > 0 {
			r.log("agent=%s repo_key=%s reaped %d orphan worktree(s) at spawn", agentID, source.RepoKey, n)
		}
		wt, wtErr := r.cfg.Materializer.PrepareWorktree(ctx, source, reporepo.WorktreeRequest{
			ExecutorID:    execID,
			TaskID:        taskID,
			BranchName:    "ac-exec/" + taskID + "/" + execID,
			WorkspacePath: wsPath,
			BaseRef:       source.BaseRef,
		})
		if wtErr != nil {
			r.log("fork_executor agent=%s task=%s prepare worktree: %v — left queued (start_task NOT called)",
				agentID, taskID, wtErr)
			return nil, nil
		}
		prepared = &executor.PreparedWorkspace{
			Path:       wt.WorkspacePath,
			RepoKey:    wt.RepoKey,
			SourcePath: wt.SourcePath,
			Branch:     wt.Branch,
			// Carry the spawn-time base into the Record so finalize can measure HEAD-ahead-of
			// -base (issue-f30b7e7b P0: this producer was missing → Record.BaseRef always "").
			// It MUST be wt.BaseRef (the SHA the branch was really cut from), never
			// source.BaseRef (the requested NAME, e.g. "main"): the name resolves in the
			// worktree against the source's stale local branch, so ahead counted everyone's
			// commits since the first clone, not this executor's ("ahead 5" for 1 real commit).
			BaseRef: wt.BaseRef,
		}
	} else if task.Repo != nil {
		target := resolveRepoTarget(task)
		clone, ready := r.takePreparedClone(taskID)
		if !ready {
			execID = ee.engine.NewExecutorID()
			wsPath, wsErr := ee.fx.Layout().WorkspaceDir(execID)
			if wsErr != nil {
				r.log("fork_executor agent=%s task=%s resolve workspace: %v — left queued", agentID, taskID, wsErr)
				return nil, nil
			}
			r.deferForClone(agentID, taskID, target, reporepo.CloneRequest{
				ExecutorID:    execID,
				TaskID:        taskID,
				BranchName:    "ac-exec/" + taskID + "/" + execID,
				WorkspacePath: wsPath,
				BaseRef:       target.BaseRef,
			})
			return nil, nil
		}
		execID = clone.ExecutorID
		prepared = &executor.PreparedWorkspace{
			Path:    clone.WorkspacePath,
			Branch:  clone.Branch,
			BaseRef: clone.BaseRef,
		}
	}

	// 3. Admission gate. Open/reopened tasks transition through start_task here. A
	// Supervisor may already have admitted the task before calling fork_executor; in
	// that valid running state a second start would be rejected and must be skipped.
	if !alreadyAdmitted {
		if err := r.startCenterTask(ctx, agentID, taskID); err != nil {
			// start_task declined AFTER the worktree was prepared → tear it down (red line B:
			// no worktree leak). The task was never admitted, so nothing else to roll back.
			r.removePreparedWorkspace(ctx, agentID, taskID, prepared)
			r.log("fork_executor agent=%s task=%s start_task declined (cap/again/not-runnable): %v — left queued",
				agentID, taskID, err)
			return nil, nil
		}
	}

	// 4. Fork the executor (W1 HandleWork chain). Pool.Launch reserves the slot
	// atomically; no global runtime mutex is held across the process launch.
	launched, err := r.launchExecutorNow(ctx, agentID, taskID, buildWorkItem(taskID, task, execID, prepared, req.Model, req.Context), ee)
	if err != nil {
		// Tear down the now-orphaned prepared worktree on every fork-fail path (red line B).
		r.removePreparedWorkspace(ctx, agentID, taskID, prepared)
		if errors.Is(err, executor.ErrAtCapacity) {
			// The task is already running and the explicit command will be acked; there is
			// no queued-task wake that can safely redrive it. Surface the center/local cap
			// skew as retryable blocked work instead of silently leaving it fake-running.
			r.blockTaskOnForkFailure(ctx, agentID, taskID,
				fmt.Errorf("center admitted task but local executor pool is at capacity: %w", err))
			return nil, nil
		}
		// NON-TRANSIENT fork failure (no model resolvable / model not allowed / rate-limited
		// / other): the task was admitted (start_task ok → running) but no executor will ever
		// run it. FAIL-LOUD (issue-0186f85e P0): block the task (running→blocked, retryable)
		// with a machine-readable [cause=…] blocked_reason, instead of leaving it fake-running
		// for the lease to reclaim (the silent-failure hole that stalled tasks for hours and
		// got misdiagnosed as a hook/code bug).
		r.blockTaskOnForkFailure(ctx, agentID, taskID, err)
		return nil, nil
	}
	return &SpawnResult{ExecutorID: launched.ExecutorID, Model: launched.Model, CLI: launched.CLI}, nil
}

// removePreparedWorkspace tears down a repo workspace materialized in SpawnExecutor
// before the launch succeeded. ON removes only the linked git worktree; OFF removes the
// independent clone directory. Best-effort: a teardown failure is logged, not surfaced.
func (r *LocalRuntime) removePreparedWorkspace(ctx context.Context, agentID, taskID string, prepared *executor.PreparedWorkspace) {
	if prepared == nil {
		return
	}
	if r.cfg.Materializer == nil || strings.TrimSpace(prepared.SourcePath) == "" {
		if err := os.RemoveAll(prepared.Path); err != nil {
			r.log("fork_executor agent=%s task=%s remove prepared clone workspace: %v", agentID, taskID, err)
		}
		return
	}
	if err := r.cfg.Materializer.RemoveWorktree(ctx, reporepo.PreparedWorktree{
		RepoKey:       prepared.RepoKey,
		SourcePath:    prepared.SourcePath,
		WorkspacePath: prepared.Path,
		Branch:        prepared.Branch,
	}); err != nil {
		r.log("fork_executor agent=%s task=%s remove prepared worktree: %v", agentID, taskID, err)
	}
}

// blockTaskOnForkFailure fails a fork-failed task LOUDLY (issue-0186f85e P0). The task
// was admitted (start_task ok → running) but the fork failed for a non-transient reason,
// so no executor will run it. Rather than leave it fake-running for the lease to reclaim
// (the silent hole), it classifies the error into a machine-readable cause and blocks the
// task (running→blocked, retryable=obstacle) with a structured "[cause=<code>]"
// blocked_reason. It ALSO logs loudly (uppercase FORK FAILED + cause) so a worker-log
// watcher — not just the center — can see "should have run, didn't, because X". Reused by
// the model-not-allowed, no-model-resolvable and rate-limited paths (classifyForkFailure
// distinguishes them), replacing the prior model-not-allowed-only blocker.
func (r *LocalRuntime) blockTaskOnForkFailure(ctx context.Context, agentID, taskID string, err error) {
	cause := classifyForkFailure(err)
	reason := forkFailureReason(cause, err)
	r.log("fork_executor agent=%s task=%s FORK FAILED [cause=%s]: %v — blocking task (running→blocked, retryable)",
		agentID, taskID, cause, err)
	caller := r.toolCaller()
	if caller == nil {
		return
	}
	body := map[string]any{
		"agent_id":    agentID,
		"task_id":     taskID,
		"reason":      reason,
		"reason_type": "obstacle",
	}
	if bErr := caller.CallAgentTool(ctx, "block_task", body, nil); bErr != nil {
		r.log("fork_executor agent=%s task=%s block_task failed: %v", agentID, taskID, bErr)
	}
}

// fetchCenterTask reads one task's detail via the get_task agent-tool. Returns an error
// (never panics) when no center transport is wired — §4.6 point recovery treats that as
// "cannot confirm should-continue" and finalizes rather than resuming a task it can't see.
func (r *LocalRuntime) fetchCenterTask(ctx context.Context, agentID, taskID string) (*centerTaskDetail, error) {
	caller := r.toolCaller()
	if caller == nil {
		return nil, fmt.Errorf("agent_controller: get_task agent=%s: no center transport", agentID)
	}
	var raw json.RawMessage
	body := map[string]any{"agent_id": agentID, "task_id": taskID}
	if err := caller.CallAgentTool(ctx, "get_task", body, &raw); err != nil {
		return nil, err
	}
	var t centerTaskDetail
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("decode get_task response: %w", err)
	}
	return &t, nil
}

// startCenterTask admits a task by calling start_task on the agent's behalf.
func (r *LocalRuntime) startCenterTask(ctx context.Context, agentID, taskID string) error {
	body := map[string]any{"agent_id": agentID, "task_id": taskID}
	return r.toolCaller().CallAgentTool(ctx, "start_task", body, nil)
}

// createTaskDir creates the pending task-execution directory (the executor branch of
// the old work()).
func (r *LocalRuntime) createTaskDir(agentID, taskID string) {
	if r.cfg.TaskDirManager == nil {
		return
	}
	_, tasksDir, _, pathErr := r.agentPaths(agentID)
	if pathErr != nil {
		r.log("agent=%s task=%s resolve paths: %v", agentID, taskID, pathErr)
		return
	}
	now := r.now()
	meta := taskexec.TaskExecutionMeta{TaskID: taskID, Status: taskexec.StatusPending, CreatedAt: now, UpdatedAt: now}
	if createErr := r.cfg.TaskDirManager.Create(tasksDir, meta, taskexec.ExecutionContext{}); createErr != nil {
		r.log("agent=%s task=%s create task dir: %v", agentID, taskID, createErr)
	}
}

// centerTaskDetail is the subset of the get_task agent-tool projection the fork path
// needs to build a WorkItem.
type centerTaskDetail struct {
	ID string `json:"id"`
	// ProjectID scopes an I<n> issue-ref lookup during spawn-time inline expansion
	// (I109 ①) — org refs are display numbers, resolvable only within a project.
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	// Assignee is the task's current owner ("agent:<id>" or a bare id). Used by boot
	// self-reconcile (T848 §4.4) to detect a reassigned task as positive cancel evidence.
	Assignee string `json:"assignee"`
	// BlockedReason is the task's block annotation. ADR-0046: a blocked task stays
	// status=running with a non-empty blocked_reason (not terminal), so status alone can't
	// distinguish a healthy running task from a blocked one. issue-88e32d98 P0: a non-empty
	// blocked_reason is positive cancel evidence — point-recovery must NOT relaunch a dead
	// executor whose task the center blocked (that produced the P67 Ship 事故).
	BlockedReason string `json:"blocked_reason"`
	Model         string `json:"model"`
	OrgRef        string `json:"org_ref"`
	// v2.31.0 (issue-9f749a19 Phase 1): repo hint from the get_task projection —
	// the project's PRIMARY repo reference (credential-free) plus base_ref (its
	// default branch). Emitted only when the project has a primary repo; absent on
	// older centers (the zero value is a nil Repo + empty BaseRef). Consumed by the
	// per-executor repo-workspace track; decoded here so the fields survive.
	Repo    *centerTaskRepo `json:"repo,omitempty"`
	BaseRef string          `json:"base_ref"`
	// DispatchMode is the per-node fork override (I105 Phase 1). The center emits the
	// key ONLY for supervisor_inline nodes, so on every ordinary task this decodes to
	// "" = executor_fork = today's routing. An older center never sends it (same zero
	// value); a newer center sending an unknown value also falls through to the fork
	// (DispatchMode.RoutesInline is a strict equality test).
	DispatchMode     string `json:"dispatch_mode"`
	DeliveryContract string `json:"delivery_contract"`
}

// centerTaskRepo mirrors the agentRepoRefMap projection (internal/admin/api): a
// credential-free view of a project↔repo reference. Only the fields the fork path
// may need to build a repo workspace are decoded; unknown keys are ignored.
type centerTaskRepo struct {
	RefID         string `json:"ref_id"`
	RepoID        string `json:"repo_id"`
	Label         string `json:"label"`
	URL           string `json:"url"`
	Provider      string `json:"provider"`
	DefaultBranch string `json:"default_branch"`
	IsPrimary     bool   `json:"is_primary"`
}

// goalTitle derives the executor goal title: the task title, else the first
// non-blank line of the description, else a stable task-id fallback.
func (d *centerTaskDetail) goalTitle(taskID string) string {
	if t := strings.TrimSpace(d.Title); t != "" {
		return t
	}
	if t := firstNonEmptyLine(d.Description); t != "" {
		return t
	}
	return "task " + taskID
}

// buildWorkItem maps a center task detail onto the orchestrator WorkItem the W1 fork
// chain consumes. TaskRef carries the CANONICAL task_id; TaskModel carries task.model
// so the §5 model chain can hard-override. execID (P3) is the pre-minted executor id
// whose worktree was materialized before the launch (empty ⇒ HandleWork mints one);
// prepared (P4) is the worktree threaded to the pool (nil ⇒ today's provisioning path).
func buildWorkItem(taskID string, task *centerTaskDetail, execID string, prepared *executor.PreparedWorkspace, modelOverride, contextOverride string) orchestrator.WorkItem {
	var repo *executor.RepoRef
	if task.Repo != nil {
		repo = &executor.RepoRef{
			RefID:    task.Repo.RefID,
			RepoID:   task.Repo.RepoID,
			URL:      task.Repo.URL,
			Provider: task.Repo.Provider,
			BaseRef:  task.BaseRef,
		}
		if prepared != nil {
			repo.BaseSHA = prepared.BaseRef
		}
	}
	return orchestrator.WorkItem{
		TaskID:    taskID,
		TaskRef:   taskID,
		ProjectID: task.ProjectID, // I109 ①: scopes an I<n> issue-ref inline expansion
		Goal: executor.Goal{
			Title:       task.goalTitle(taskID),
			Description: task.Description,
		},
		TaskModel:  firstNonEmpty(modelOverride, task.Model),
		Context:    strings.TrimSpace(contextOverride),
		ExecutorID: execID,
		Prepared:   prepared,
		Repo:       repo,
		// I105 N2: stamp the center's routing decision onto input.json. On this path it
		// is normally "" / executor_fork (the gate in SpawnExecutor already diverted a
		// supervisor_inline node); a supervisor_inline value arriving here means the gate
		// was bypassed, which is precisely what the N4 writeback net exists to catch.
		DispatchMode:     strings.TrimSpace(task.DispatchMode),
		DeliveryContract: strings.TrimSpace(task.DeliveryContract),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// materializerCleaner adapts a reporepo.RepoMaterializer to the narrow
// executor.WorktreeCleaner port the Monitor consumes. It lives HERE (agentruntime),
// which imports BOTH executor and reporepo, so the executor package stays free of any
// reporepo import (red line D — no cycle). It rebuilds the minimal
// reporepo.PreparedWorktree the materializer needs to tear down ONLY the worktree
// (never the canonical source — design §10).
type materializerCleaner struct{ m reporepo.RepoMaterializer }

var _ executor.WorktreeCleaner = materializerCleaner{}

func (c materializerCleaner) RemoveWorktree(ctx context.Context, repoKey, sourcePath, workspacePath string) error {
	return c.m.RemoveWorktree(ctx, reporepo.PreparedWorktree{
		RepoKey:       repoKey,
		SourcePath:    sourcePath,
		WorkspacePath: workspacePath,
	})
}

// resolveRepoTarget maps a task's credential-free repo hint (get_task projection, P1)
// onto the RepoTarget the materializer clones/fetches. BaseRef is the task-resolved
// base ref; the materializer falls back to DefaultBranch when it is empty.
func resolveRepoTarget(task *centerTaskDetail) reporepo.RepoTarget {
	return reporepo.RepoTarget{
		RepoID:        task.Repo.RepoID,
		URL:           task.Repo.URL,
		Provider:      task.Repo.Provider,
		DefaultBranch: task.Repo.DefaultBranch,
		BaseRef:       task.BaseRef,
	}
}

// runtimeAgentEnv builds the per-agent CLI process env overlay (git identity +
// display name + profile env). Kept in lockstep with the daemon's runtimeAgentEnv.
func runtimeAgentEnv(agentID, displayName string, profileEnv map[string]string) map[string]string {
	out := agentsupervisor.GitIdentityEnv(agentID)
	if out == nil {
		out = map[string]string{}
	}
	for k, v := range agentsupervisor.DisplayNameEnv(displayName) {
		out[k] = v
	}
	for k, v := range profileEnv {
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
