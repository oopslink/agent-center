package agentruntime

// executor_runtime.go — the LocalRuntime methods that DRIVE the per-agent
// ExecutorEngine (Phase 0c), moved verbatim off workerdaemon.AgentController:
// build / fork / drain / recover / watchdog, plus the SpawnExecutor
// (get_task→start_task→launch) and NotifyWork executor branch.
//
// 🔴 FORK MUTEX (防双 fork / 爆 max_concurrent): forkMu (a field on LocalRuntime,
// SEPARATE from r.mu which guards SessionState) serializes the WHOLE
// get_task→start_task→launch sequence in SpawnExecutor AND the shared launch tail
// (launchExecutor), so two concurrent forks for one agent can never both pass the
// pool cap. forkMu is held only for the fork/launch bookkeeping — the drain goroutine
// runs detached as before, and the SessionState write (HadWork/CurrentTaskID) takes
// r.mu separately (never nested under forkMu guarding the same field).

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
	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
	"github.com/oopslink/agent-center/internal/workerdaemon/reporepo"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
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
	AgentID              string
	DisplayName          string
	EnvVars              map[string]string
	MaxConcurrentTasks   int
	AllowedExecutors     []agent.ExecutorProfile
	OrchestratorModel    string
	DefaultExecutorModel string
	CLI                  string
}

// AttachExecutor installs the executor engine onto the runtime (under the shared
// mutex, exactly as ma.exec was set under c.mu). Called by the daemon's
// maybeAttachExecutorEngine / reattach paths.
func (r *LocalRuntime) AttachExecutor(ee *ExecutorEngine) {
	r.mu.Lock()
	r.exec = ee
	r.mu.Unlock()
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
	eng, err := orchestrator.NewEngine(orchestrator.EngineConfig{
		Pool:    pool,
		Routing: routing,
		Router:  modelrouter.NewRouter(nil),
		RouterConfig: modelrouter.Config{
			OrchestratorModel:    pl.OrchestratorModel,
			AllowedExecutors:     routerCandidates(pl.AllowedExecutors),
			DefaultExecutorModel: pl.DefaultExecutorModel,
			DefaultCLI:           pl.CLI,
		},
		Runners: map[string]orchestrator.RunnerCmdBuilder{
			agent.DefaultExecutorCLI: orchestrator.NewClaudeRunnerBuilder(r.cfg.ClaudeBinary),
			CLICodex:                 orchestrator.NewCodexRunnerBuilder(r.cfg.CodexBinary),
		},
		IDs:   orchestrator.NewULIDMinter(clk),
		Clock: clk,
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
	}
	return r.launchExecutor(ctx, req.AgentID, req.TaskID, item, ee)
}

// launchExecutor is the shared fork tail: it serializes the whole HandleWork + drain
// + bookkeeping under forkMu (red line #1) so two concurrent forks for one agent
// cannot both pass the pool cap. Used by BOTH the agent.work executor branch
// (workViaExecutor) and callers that already hold the built WorkItem.
func (r *LocalRuntime) launchExecutor(ctx context.Context, agentID, taskID string, item orchestrator.WorkItem, ee *ExecutorEngine) error {
	r.forkMu.Lock()
	defer r.forkMu.Unlock()
	_, err := r.launchExecutorLocked(ctx, agentID, taskID, item, ee)
	return err
}

// launchExecutorLocked forks an executor for a fully-built WorkItem and wires the
// reap + work-state bookkeeping. The CALLER MUST hold forkMu (SpawnExecutor holds it
// across the whole get_task→start_task→launch sequence; launchExecutor holds it for
// the fork tail alone). At capacity it returns a retryable ErrAtCapacity; any other
// fork error is wrapped. On success it detaches a drain goroutine (freeing the pool
// slot when the executor exits) and mirrors the inject path's hadWork/currentTaskID.
func (r *LocalRuntime) launchExecutorLocked(ctx context.Context, agentID, taskID string, item orchestrator.WorkItem, ee *ExecutorEngine) (*orchestrator.Launched, error) {
	launched, err := ee.engine.HandleWork(ctx, item)
	if err != nil {
		if errors.Is(err, executor.ErrAtCapacity) {
			return nil, fmt.Errorf("agent_controller: agent=%s at executor capacity (queue, retry next tick): %w", agentID, err)
		}
		return nil, fmt.Errorf("agent_controller: agent=%s fork executor: %w", agentID, err)
	}
	r.log("agent=%s task=%s forked executor=%s cli=%s model=%s(%s) problem=%s",
		agentID, taskID, launched.ExecutorID, launched.CLI, launched.Model, launched.ModelSource, launched.ProblemID)

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

	// Reap the executor when it exits, freeing its pool slot. Runs detached.
	go r.drainExecutor(ee, launched.Handle)

	// Mirror the inject path's work-state bookkeeping (takes r.mu, NOT forkMu — two
	// different concerns; matches today's launchExecutor which took c.mu only here).
	r.mu.Lock()
	if r.state != nil {
		r.state.HadWork = true
		if taskID != "" {
			r.state.CurrentTaskID = taskID
			r.state.CurrentConversationID = ""
		}
	}
	r.mu.Unlock()
	return launched, nil
}

// drainExecutor blocks until the forked executor exits, then finalizes it via the
// F5 Monitor (dual-signal classification → free pool slot → cleanup dir).
func (r *LocalRuntime) drainExecutor(ee *ExecutorEngine, h *executor.Handle) {
	if ee == nil || h == nil {
		return
	}
	if _, err := ee.monitor.AwaitCompletion(context.Background(), h); err != nil {
		r.log("agent executor=%s drain: %v", h.ExecutorID, err)
	}
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

// liveExecIDs snapshots the currently live executor id set (pool-active + adopted
// orphans) for the orphan-reap fail-safe: only an executor id ABSENT here is reaped.
func (r *LocalRuntime) liveExecIDs(ee *ExecutorEngine) map[string]bool {
	live := map[string]bool{}
	for _, s := range ee.SnapshotConcurrency() {
		if s.ExecutorID != "" {
			live[s.ExecutorID] = true
		}
	}
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
	// 2. Poll the adopted orphans (no reapable handle): stall + completion.
	for id, pid := range ee.snapshotOrphans() {
		done, kind, err := r.checkOrphanOnce(ctx, ee, id, pid)
		if err != nil {
			r.log("agent=%s orphan watchdog executor=%s: %v", agentID, id, err)
			continue
		}
		if done {
			ee.dropOrphan(id)
			r.log("agent=%s recovered orphan executor=%s finalized: %s", agentID, id, kind)
		}
	}
}

// checkOrphanOnce runs one orphan watchdog+completion tick and reports whether it
// reached a terminal outcome (so the caller stops polling it).
func (r *LocalRuntime) checkOrphanOnce(ctx context.Context, ee *ExecutorEngine, id string, pid int) (bool, executor.OutcomeKind, error) {
	comp, done, err := ee.monitor.CheckOrphan(ctx, id, pid)
	if err != nil {
		return false, "", err
	}
	return done, comp.Kind, nil
}

// SnapshotConcurrency returns this agent's executor snapshots (nil when no engine).
func (r *LocalRuntime) SnapshotConcurrency() []concurrency.ExecutorSnapshot {
	ee := r.execEngine()
	if ee == nil {
		return nil
	}
	return ee.SnapshotConcurrency()
}

// ---------------------------------------------------------------------------
// SpawnExecutor — the live get_task→start_task→launch fork sequence (was
// forkOnWorkAvailable), serialized end-to-end under forkMu (red line #1).
// ---------------------------------------------------------------------------

// SpawnExecutor admits a queued task through the center (start_task, ≤N cap) and
// forks an executor for it. It is best-effort and NON-WEDGING — every non-success
// outcome (empty id / no transport / get_task error / already-running / declined /
// at-cap reap-skew) is logged and returns (nil, nil), leaving the task queued for a
// later re-emit. A model-not-allowed fork blocks the task (already admitted). On a
// successful fork it returns the SpawnResult.
//
// forkMu is held across the WHOLE sequence so two concurrent SpawnExecutor calls for
// one agent can't both pass the pool cap (red line #1 — the single-ControlLoop
// serialization guarantee is gone now that SpawnExecutor is callable from multiple
// entry points).
func (r *LocalRuntime) SpawnExecutor(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	agentID := r.cfg.AgentID
	taskID := req.TaskID
	ee := r.execEngine()
	if ee == nil {
		r.log("work_available agent=%s task=%s SpawnExecutor: no executor engine — left queued", agentID, taskID)
		return nil, nil
	}

	r.forkMu.Lock()
	defer r.forkMu.Unlock()

	if strings.TrimSpace(taskID) == "" {
		r.log("work_available agent=%s concurrency fork: empty task_id — skipping", agentID)
		return nil, nil
	}
	caller := r.toolCaller()
	if caller == nil {
		r.log("work_available agent=%s task=%s concurrency fork: no ToolCaller — left queued", agentID, taskID)
		return nil, nil
	}

	// 1. Pull the task detail to build the WorkItem (title/description/model).
	task, err := r.fetchCenterTask(ctx, agentID, taskID)
	if err != nil {
		r.log("work_available agent=%s task=%s get_task: %v — left queued", agentID, taskID, err)
		return nil, nil
	}

	// Cheap idempotency precheck: only an OPEN/REOPENED task is startable.
	if st := strings.TrimSpace(task.Status); st != "" && st != "open" && st != "reopened" {
		r.log("work_available agent=%s task=%s already %s — not forking", agentID, taskID, st)
		return nil, nil
	}

	// 2. Repo workspace (P3, red line A): materialize the canonical source + a
	// per-executor worktree BEFORE start_task/reserve, so an EnsureSource/PrepareWorktree
	// failure means the task is NEVER admitted (best-effort, non-wedging: log + leave
	// queued). Only when the flag is ON (materializer wired) AND the task carries a
	// primary repo; otherwise execID stays empty + prepared nil ⇒ the plain-dir path is
	// byte-for-byte unchanged.
	var execID string
	var prepared *executor.PreparedWorkspace
	if r.cfg.Materializer != nil && task.Repo != nil {
		execID = ee.engine.NewExecutorID() // must be known BEFORE PrepareWorktree (path+branch embed it)
		wsPath, wsErr := ee.fx.Layout().WorkspaceDir(execID)
		if wsErr != nil {
			r.log("work_available agent=%s task=%s resolve workspace: %v — left queued", agentID, taskID, wsErr)
			return nil, nil
		}
		source, srcErr := r.cfg.Materializer.EnsureSource(ctx, resolveRepoTarget(task))
		if srcErr != nil {
			r.log("work_available agent=%s task=%s ensure repo source: %v — left queued (start_task NOT called)",
				agentID, taskID, srcErr)
			return nil, nil
		}
		// v2.31.1 orphan-reap (spawn hook): before adding THIS executor's worktree, reap
		// any orphaned worktrees under this source (a prior retryable-crash's kept worktree
		// whose task was re-dispatched fresh-id — never reused, nothing else cleans it).
		// Safe here: we hold forkMu (no concurrent same-agent spawn) and the new execID's
		// worktree does not exist yet (PrepareWorktree is below), so it can't be reaped.
		// isLive = the current pool+orphan snapshot (fail-safe KEEP for anything live).
		live := r.liveExecIDs(ee)
		if n, perr := r.cfg.Materializer.PruneOrphanWorktrees(ctx, source, func(id string) bool { return live[id] }); perr != nil {
			r.log("work_available agent=%s repo_key=%s prune orphan worktrees: %v (non-fatal)", agentID, source.RepoKey, perr)
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
			r.log("work_available agent=%s task=%s prepare worktree: %v — left queued (start_task NOT called)",
				agentID, taskID, wtErr)
			return nil, nil
		}
		prepared = &executor.PreparedWorkspace{
			Path:       wt.WorkspacePath,
			RepoKey:    wt.RepoKey,
			SourcePath: wt.SourcePath,
			Branch:     wt.Branch,
		}
	}

	// 3. Admission gate: 代 start_task (open→running). The center enforces the ≤N cap.
	if err := r.startCenterTask(ctx, agentID, taskID); err != nil {
		// start_task declined AFTER the worktree was prepared → tear it down (red line B:
		// no worktree leak). The task was never admitted, so nothing else to roll back.
		r.removePreparedWorktree(ctx, agentID, taskID, prepared)
		r.log("work_available agent=%s task=%s start_task declined (cap/again/not-runnable): %v — left queued",
			agentID, taskID, err)
		return nil, nil
	}

	// 4. Fork the executor (W1 HandleWork chain) under the SAME forkMu.
	launched, err := r.launchExecutorLocked(ctx, agentID, taskID, buildWorkItem(taskID, task, execID, prepared), ee)
	if err != nil {
		if errors.Is(err, modelrouter.ErrModelNotAllowed) {
			// Task already admitted → block it; the prepared worktree is now orphaned, tear
			// it down (red line B).
			r.removePreparedWorktree(ctx, agentID, taskID, prepared)
			r.log("work_available agent=%s task=%s model not allowed: %v — blocking task", agentID, taskID, err)
			r.blockTaskOnModelNotAllowed(ctx, agentID, taskID, err)
			return nil, nil
		}
		// Fork failed but the task is already running (start_task succeeded). Tear down
		// the worktree (red line B); the task status is left to lease reclaim → re-dispatch
		// (matches today's no-rollback-of-start_task behavior).
		r.removePreparedWorktree(ctx, agentID, taskID, prepared)
		r.log("work_available agent=%s task=%s started but fork failed: %v (lease will reclaim → re-dispatch)",
			agentID, taskID, err)
		return nil, nil
	}
	return &SpawnResult{ExecutorID: launched.ExecutorID, Model: launched.Model, CLI: launched.CLI}, nil
}

// removePreparedWorktree tears down a worktree materialized in SpawnExecutor before
// the launch succeeded (every failure path after PrepareWorktree — red line B). No-op
// when nothing was prepared (flag off / no repo). Best-effort: a teardown failure is
// logged, not surfaced (the canonical source is untouched — design §10).
func (r *LocalRuntime) removePreparedWorktree(ctx context.Context, agentID, taskID string, prepared *executor.PreparedWorkspace) {
	if prepared == nil || r.cfg.Materializer == nil {
		return
	}
	if err := r.cfg.Materializer.RemoveWorktree(ctx, reporepo.PreparedWorktree{
		RepoKey:       prepared.RepoKey,
		SourcePath:    prepared.SourcePath,
		WorkspacePath: prepared.Path,
		Branch:        prepared.Branch,
	}); err != nil {
		r.log("work_available agent=%s task=%s remove prepared worktree: %v", agentID, taskID, err)
	}
}

// blockTaskOnModelNotAllowed blocks a task whose task.model is not in the agent's
// allowed_executors. The task was already started (open→running) by startCenterTask,
// so we block it (running→blocked) with an obstacle reason.
func (r *LocalRuntime) blockTaskOnModelNotAllowed(ctx context.Context, agentID, taskID string, err error) {
	caller := r.toolCaller()
	if caller == nil {
		return
	}
	body := map[string]any{
		"agent_id":    agentID,
		"task_id":     taskID,
		"reason":      err.Error(),
		"reason_type": "obstacle",
	}
	if bErr := caller.CallAgentTool(ctx, "block_task", body, nil); bErr != nil {
		r.log("work_available agent=%s task=%s block_task failed: %v", agentID, taskID, bErr)
	}
}

// fetchCenterTask reads one task's detail via the get_task agent-tool.
func (r *LocalRuntime) fetchCenterTask(ctx context.Context, agentID, taskID string) (*centerTaskDetail, error) {
	var raw json.RawMessage
	body := map[string]any{"agent_id": agentID, "task_id": taskID}
	if err := r.toolCaller().CallAgentTool(ctx, "get_task", body, &raw); err != nil {
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
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	// Assignee is the task's current owner ("agent:<id>" or a bare id). Used by boot
	// self-reconcile (T848 §4.4) to detect a reassigned task as positive cancel evidence.
	Assignee string `json:"assignee"`
	Model    string `json:"model"`
	OrgRef   string `json:"org_ref"`
	// v2.31.0 (issue-9f749a19 Phase 1): repo hint from the get_task projection —
	// the project's PRIMARY repo reference (credential-free) plus base_ref (its
	// default branch). Emitted only when the project has a primary repo; absent on
	// older centers (the zero value is a nil Repo + empty BaseRef). Consumed by the
	// per-executor repo-workspace track; decoded here so the fields survive.
	Repo    *centerTaskRepo `json:"repo,omitempty"`
	BaseRef string          `json:"base_ref"`
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
func buildWorkItem(taskID string, task *centerTaskDetail, execID string, prepared *executor.PreparedWorkspace) orchestrator.WorkItem {
	return orchestrator.WorkItem{
		TaskID:  taskID,
		TaskRef: taskID,
		Goal: executor.Goal{
			Title:       task.goalTitle(taskID),
			Description: task.Description,
		},
		TaskModel:  task.Model,
		ExecutorID: execID,
		Prepared:   prepared,
	}
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
