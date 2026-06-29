package workerdaemon

// concurrent_exec.go — W1 (agent-concurrent-execution phase 2) production wiring:
// the seam where the daemon's per-agent work path forks executors instead of
// injecting the brief into the single resident claude. It chains the v2.17.0
// foundations via internal/workerdaemon/orchestrator (F4 routing → F3 model → F2
// input → F1 Pool fork) and reaps finished executors to free their slot.
//
// OPT-IN + REVERSIBLE (PD ruling, decision 2): the executor path activates ONLY
// for an agent whose profile sets MaxConcurrentTasks>0 AND lists ≥1 allowed model.
// Every other agent keeps the legacy single-claude inject path byte-for-byte, so
// this is additive and fully reversible (disable by clearing the profile fields).
//
// SCOPE (W1): fork + concurrency cap + reap-to-free-slot. The center writeback
// (reporting the executor's result to the source chat / task) is W2 — wired here as
// a Monitor with a NIL Writeback (reap + free slot + cleanup, no center write);
// W2 swaps in a real Writeback. The LLM difficulty judge (F3 allowed_models →
// per-difficulty model) is wired as a nil judge for now: the §5 chain still resolves
// task.model → default_executor_model; the judge port wiring is a follow-up.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// executorEngine bundles the per-agent W1 wiring: the orchestration Engine (the
// F4→F3→F2→F1 chain) plus the Monitor that reaps a finished executor and frees its
// pool slot. Attached to a managedAgent only when concurrency is enabled.
//
// W3 adds crash-recovery state: orphans is the set of ADOPTED orphan executors
// (executor_id → pid) rebuilt by Recover after a restart. Unlike this-process
// spawns (reaped by a drainExecutor goroutine), an orphan has no reapable handle,
// so the watchdog tick polls each via Monitor.CheckOrphan until it terminates.
type executorEngine struct {
	engine  *orchestrator.Engine
	monitor *executor.Monitor
	// fx reads each executor's input.json / status.json for the real-time
	// concurrency snapshot (v2.19.0).
	fx *executor.FileExchange

	mu      sync.Mutex
	orphans map[string]int // adopted orphan executor_id → pid; watchdog-polled until terminal
}

// addOrphan registers a recovered, still-alive executor for watchdog polling.
func (ee *executorEngine) addOrphan(id string, pid int) {
	ee.mu.Lock()
	defer ee.mu.Unlock()
	if ee.orphans == nil {
		ee.orphans = make(map[string]int)
	}
	ee.orphans[id] = pid
}

// snapshotOrphans returns a copy of the orphan set for lock-free iteration by the
// watchdog tick.
func (ee *executorEngine) snapshotOrphans() map[string]int {
	ee.mu.Lock()
	defer ee.mu.Unlock()
	out := make(map[string]int, len(ee.orphans))
	for id, pid := range ee.orphans {
		out[id] = pid
	}
	return out
}

// dropOrphan removes an orphan that reached a terminal completion (stop polling it).
func (ee *executorEngine) dropOrphan(id string) {
	ee.mu.Lock()
	defer ee.mu.Unlock()
	delete(ee.orphans, id)
}

// SnapshotConcurrency builds the real-time per-executor view for this agent
// (v2.19.0, #并发讨论2): it enumerates the live this-process executors from the
// Pool (μs lock) AND merges the adopted orphans (deduped by id) — otherwise active
// would under-report after a daemon restart. Each executor's task/cli/model come
// from its input.json and its state/started_at/last_progress_at from status.json
// (best-effort: a mid-write/absent file degrades to a sparse entry, never an error).
func (ee *executorEngine) SnapshotConcurrency() []concurrency.ExecutorSnapshot {
	var out []concurrency.ExecutorSnapshot
	seen := make(map[string]struct{})

	for _, h := range ee.engine.Pool().Handles() {
		seen[h.ExecutorID] = struct{}{}
		snap := concurrency.ExecutorSnapshot{
			ExecutorID: h.ExecutorID,
			PID:        h.PID,
			StartedAt:  h.StartedAt(),
		}
		ee.enrichFromFiles(&snap, false)
		out = append(out, snap)
	}

	for id, pid := range ee.snapshotOrphans() {
		if _, dup := seen[id]; dup {
			continue
		}
		snap := concurrency.ExecutorSnapshot{ExecutorID: id, PID: pid}
		ee.enrichFromFiles(&snap, true)
		out = append(out, snap)
	}
	return out
}

// enrichFromFiles fills task/cli/model from input.json and state/started_at/
// last_progress_at from status.json. orphan forces State=orphan regardless of
// status. The state mapping for live executors: no status yet → starting; running
// → running; terminal (done/failed) but slot not yet freed → finishing.
func (ee *executorEngine) enrichFromFiles(snap *concurrency.ExecutorSnapshot, orphan bool) {
	if ee.fx == nil {
		if orphan {
			snap.State = concurrency.StateOrphan
		}
		return
	}
	if in, err := ee.fx.ReadInput(snap.ExecutorID); err == nil {
		snap.TaskID = in.Source.TaskRef
		snap.CLI = in.CLI
		snap.Model = in.Model
	}
	st, stErr := ee.fx.ReadStatus(snap.ExecutorID)
	switch {
	case orphan:
		snap.State = concurrency.StateOrphan
	case stErr != nil:
		snap.State = concurrency.StateStarting // spawned, no running status yet
	case st.State == executor.StateRunning:
		snap.State = concurrency.StateRunning
	default: // StateDone / StateFailed — terminal, slot not yet freed
		snap.State = concurrency.StateFinishing
	}
	if stErr == nil {
		if snap.StartedAt.IsZero() {
			snap.StartedAt = st.StartedAt
		}
		if !st.LastProgressAt.IsZero() {
			lp := st.LastProgressAt
			snap.LastProgressAt = &lp
		}
	}
}

// SnapshotConcurrency returns the per-agent live executor view for every agent on
// this worker running the concurrent path (v2.19.0). It is what the heartbeat ships
// to the center (agent_id → snapshot) and is read by GET .../agents/{id}/concurrency.
// Agents with a concurrency engine but zero live executors are still emitted (active
// 0) so the center's last-known state reflects "idle now", not a stale set.
func (c *AgentController) SnapshotConcurrency() map[string]concurrency.AgentSnapshot {
	c.mu.Lock()
	type agentEng struct {
		id string
		ee *executorEngine
	}
	var engines []agentEng
	for id, ma := range c.agents {
		if ma != nil && ma.exec != nil {
			engines = append(engines, agentEng{id: id, ee: ma.exec})
		}
	}
	c.mu.Unlock()

	if len(engines) == 0 {
		return nil
	}
	out := make(map[string]concurrency.AgentSnapshot, len(engines))
	for _, ae := range engines {
		execs := ae.ee.SnapshotConcurrency()
		out[ae.id] = concurrency.AgentSnapshot{Active: len(execs), Executors: execs}
	}
	return out
}

// funcClock adapts the controller's func() time.Time test-seam to clock.Clock (the
// interface the executor/orchestrator packages take), so the wiring shares the
// controller's clock and stays deterministic under the daemon's test clock.
type funcClock struct{ now func() time.Time }

func (f funcClock) Now() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// concurrencyEnabled is the W1 opt-in gate (PD ruling, decision 2): the executor
// concurrency path activates only when the profile sets MaxConcurrentTasks>0 AND
// lists at least one allowed model. Otherwise the agent keeps the legacy inject path.
//
// v2.18.0 W4c: delegates to agent.Profile.ConcurrencyEnabled so the daemon's
// executor-pool gate and the center's ≤N start cap share ONE predicate and cannot
// drift (issue-b8687f2a §2). The reconcile payload carries the same fields the
// profile predicate reads.
//
// v2.18.1 BE-1: the authoritative opt-in list is now AllowedExecutors [{cli,model}]
// (issue-8746a5b9) — the predicate reads it, not the legacy model-only AllowedModels.
func concurrencyEnabled(pl reconcilePayload) bool {
	return agent.Profile{
		MaxConcurrentTasks: pl.MaxConcurrentTasks,
		AllowedExecutors:   pl.AllowedExecutors,
	}.ConcurrencyEnabled()
}

// routerCandidates maps the authoritative agent.ExecutorProfile list onto the
// modelrouter's decoupled {cli,model} candidate type (v2.18.1 BE-2) — the seam that
// keeps modelrouter free of the agent bounded context.
func routerCandidates(execs []agent.ExecutorProfile) []modelrouter.ExecutorCandidate {
	if len(execs) == 0 {
		return nil
	}
	out := make([]modelrouter.ExecutorCandidate, 0, len(execs))
	for _, e := range execs {
		out = append(out, modelrouter.ExecutorCandidate{CLI: e.CLI, Model: e.Model})
	}
	return out
}

// buildExecutorEngine constructs the per-agent Engine + Monitor. Workspaces are
// plain isolated directories — PD ruling B: production agents have no per-agent
// source git repo, so the F1 worktree step is skipped (process-group + env +
// path containment still isolate executors). agentRoot is the per-agent home.
func (c *AgentController) buildExecutorEngine(agentRoot string, pl reconcilePayload) (*executorEngine, error) {
	clk := funcClock{now: c.now}
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
		BinaryPath: c.cfg.BinaryPath,
		Tracker:    tracker,
		Max:        pl.MaxConcurrentTasks,
		Clock:      clk,
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
		// nil judge: §5 chain resolves task.model → default_executor_model. The LLM
		// difficulty judge (consuming allowed_executors) is a follow-up wiring.
		Router: modelrouter.NewRouter(nil),
		RouterConfig: modelrouter.Config{
			OrchestratorModel: pl.OrchestratorModel,
			// v2.18.1 BE-2: route over the authoritative {cli,model} candidates, not the
			// legacy model-only list. DefaultCLI = the agent's own cli, so a model-only
			// task.model/default not found among the candidates pairs with the supervisor's
			// CLI (matching BE-1's legacy-models backfill rule).
			AllowedExecutors:     routerCandidates(pl.AllowedExecutors),
			DefaultExecutorModel: pl.DefaultExecutorModel,
			DefaultCLI:           pl.CLI,
		},
		// v2.18.1 BE-2: per-CLI runner builders — the F3 decision's CLI selects which
		// one forks the executor (a claude-code supervisor may dispatch a codex executor).
		Runners: map[string]orchestrator.RunnerCmdBuilder{
			agent.DefaultExecutorCLI: orchestrator.NewClaudeRunnerBuilder(c.cfg.ClaudeBinary),
			cliCodex:                 orchestrator.NewCodexRunnerBuilder(c.cfg.CodexBinary),
		},
		IDs:   orchestrator.NewULIDMinter(clk),
		Clock: clk,
	})
	if err != nil {
		return nil, err
	}
	// Watchdog (F5 §9): stall detection + graceful kill, default timeouts.
	watchdog := executor.NewWatchdog(executor.WatchdogConfig{Clock: clk})
	// Reconciler (F5 §12): rebuilds in-flight state from durable files at restart.
	reconciler, err := executor.NewReconciler(fx, tracker, nil) // nil probe → SignalLiveness
	if err != nil {
		return nil, err
	}
	// W2: a real center Writeback so the orchestrator (sole writer) reports each
	// finished executor back to the center — complete_task on success / block_task on
	// failure (both atomically relay to the task conversation). Wired only when the
	// daemon has an agent-tool caller; without one the Monitor degrades to no center
	// write (reap + free slot + cleanup). Orthogonal to W3's Watchdog/Reconciler:
	// stacked together, W3's stalled-kill + recovered-orphan finalize now flow
	// through this Writeback too (Monitor.Finalize is the single report point).
	var wb executor.Writeback
	if cc := newCenterClient(c.cfg.ToolCaller); cc != nil {
		cwb, werr := orchestrator.NewCenterWriteback(cc, fx, pl.AgentID)
		if werr != nil {
			return nil, werr
		}
		// T613: relay each executor run's token usage to the center's report_usage,
		// tagged with the fork-time Source.TaskRef. Same authed transport as the
		// writeback; the executor stays credential-free (it only records usage in
		// output.json). Best-effort inside the writeback — never blocks completion.
		cwb.WithUsageReporter(newUsageReporter(c.cfg.ToolCaller))
		wb = cwb
	}
	// Monitor = W1 reap + W3 Watchdog/Reconciler (Sweep/Recover/CheckOrphan) + W2
	// Writeback: every finalize (this-process exit, stalled kill, recovered orphan)
	// reports to the center before teardown, frees the slot, then tears down terminal dirs.
	mon, err := executor.NewMonitor(executor.MonitorConfig{
		Exchange:   fx,
		Pool:       pool,
		Watchdog:   watchdog,
		Reconciler: reconciler,
		Writeback:  wb,
		Clock:      clk,
		// Liveness defaults to SignalLiveness (real pid probe).
	})
	if err != nil {
		return nil, err
	}
	return &executorEngine{engine: eng, monitor: mon, fx: fx}, nil
}

// workViaExecutor handles an agent.work brief by forking an executor (the W1
// concurrent path) instead of injecting into the resident claude. At capacity it
// returns a retryable error so the control loop re-pulls the command on the next
// tick (queue, don't hard-start — design §3 / PD decision 2).
func (c *AgentController) workViaExecutor(ctx context.Context, pl workPayload, ee *executorEngine) error {
	title := firstNonEmptyLine(pl.Brief)
	if title == "" {
		title = "task " + pl.TaskID
	}
	item := orchestrator.WorkItem{
		TaskID:  pl.TaskID,
		TaskRef: pl.TaskRef,
		Goal:    executor.Goal{Title: title, Description: pl.Brief},
		// task.model and a structured goal/chat ref are not in workPayload yet; the
		// §5 chain falls back to default_executor_model. The live work_available fork
		// path (forkOnWorkAvailable) builds a WorkItem WITH task.model from get_task.
	}
	return c.launchExecutor(ctx, pl.AgentID, pl.TaskID, item, ee)
}

// launchExecutor forks an executor for a fully-built WorkItem and wires the reap +
// work-state bookkeeping. It is the shared fork tail of both the legacy agent.work
// path (workViaExecutor) and the W4a live work_available path (forkOnWorkAvailable):
// at capacity it returns a retryable ErrAtCapacity (the caller queues), any other
// fork error is wrapped. On success it detaches a drain goroutine that frees the
// pool slot when the executor exits and mirrors the inject path's hadWork/currentTaskID.
func (c *AgentController) launchExecutor(ctx context.Context, agentID, taskID string, item orchestrator.WorkItem, ee *executorEngine) error {
	launched, err := ee.engine.HandleWork(ctx, item)
	if err != nil {
		if errors.Is(err, executor.ErrAtCapacity) {
			return fmt.Errorf("agent_controller: agent=%s at executor capacity (queue, retry next tick): %w", agentID, err)
		}
		return fmt.Errorf("agent_controller: agent=%s fork executor: %w", agentID, err)
	}
	c.log("agent=%s task=%s forked executor=%s cli=%s model=%s(%s) problem=%s",
		agentID, taskID, launched.ExecutorID, launched.CLI, launched.Model, launched.ModelSource, launched.ProblemID)

	// Reap the executor when it exits, freeing its pool slot (W1). Runs detached so
	// the work command acks immediately and the next work can launch concurrently.
	go c.drainExecutor(ee, launched.Handle)

	// Mirror the inject path's work-state bookkeeping (L2 error surface / self-heal).
	c.mu.Lock()
	if cur := c.agents[agentID]; cur != nil {
		cur.hadWork = true
		if taskID != "" {
			cur.currentTaskID = taskID
			cur.currentConversationID = ""
		}
	}
	c.mu.Unlock()
	return nil
}

// drainExecutor blocks until the forked executor exits, then finalizes it via the
// F5 Monitor (dual-signal classification → free pool slot → cleanup dir). With a
// nil Writeback (W1) the result is not yet relayed to the center — that is W2 — but
// the slot is freed so concurrency is sustained.
func (c *AgentController) drainExecutor(ee *executorEngine, h *executor.Handle) {
	if ee == nil || h == nil {
		return
	}
	if _, err := ee.monitor.AwaitCompletion(context.Background(), h); err != nil {
		c.log("agent executor=%s drain: %v", h.ExecutorID, err)
	}
}

// defaultExecutorWatchdogInterval throttles the per-agent watchdog/orphan-poll
// sweep on OnTick. Well below DefaultStallTimeout (5m) so a stalled executor is
// detected within one interval of crossing the threshold, but coarse enough not to
// hammer the filesystem each sub-second tick.
const defaultExecutorWatchdogInterval = 30 * time.Second

// recoverExecutors rebuilds in-flight executor state the FIRST time this daemon
// process manages an agent's concurrency (i.e. right after a daemon restart, the
// design §12 "监工重启扫 executors/" path). It drives the F5 Monitor.Recover, which
// — from durable files alone, never re-spawning — finalizes terminal/crashed
// orphans (their result is not lost across the restart) and re-adopts still-alive
// ones into the pool. The still-alive orphans are then registered here so the
// watchdog tick (maybeRunExecutorWatchdog) polls them to completion, since they
// carry no reapable handle.
//
// It runs ONCE per agent per process (guarded by the caller): a later in-process
// engine rebuild on a routine reconcile must NOT re-scan, because by then the
// agent's running executors are THIS process's children (reaped by drainExecutor),
// not orphans — re-adopting them would double-finalize.
func (c *AgentController) recoverExecutors(ctx context.Context, agentID string, ee *executorEngine) {
	items, err := ee.monitor.Recover(ctx)
	if err != nil {
		c.log("agent=%s executor recover: %v", agentID, err)
		return
	}
	var adopted, finalized, lost int
	for _, it := range items {
		if it.Completion.Kind != executor.OutcomeRunning {
			finalized++
			continue
		}
		// Still alive → Recover already re-adopted it into the pool; register it for
		// watchdog polling so its eventual completion/stall is observed.
		if it.Record != nil && it.Record.PID > 0 {
			ee.addOrphan(it.ExecutorID, it.Record.PID)
			adopted++
		} else {
			// Alive but no pid record (e.g. record write lost): we cannot probe/kill it.
			// Leave it running and surface it — the next process boot re-scans.
			lost++
			c.log("agent=%s recovered running executor=%s lacks a pid record — not watchdog-tracked", agentID, it.ExecutorID)
		}
	}
	if adopted > 0 || finalized > 0 || lost > 0 {
		c.log("agent=%s executor crash-recovery: scanned=%d adopted=%d finalized=%d untracked=%d",
			agentID, len(items), adopted, finalized, lost)
	}
}

// maybeRunExecutorWatchdog is the per-tick executor watchdog sweep (W3), called
// from OnTick and throttled to defaultExecutorWatchdogInterval. For every agent
// running the concurrent path it (1) Sweeps the live this-process handles, graceful
// -killing any that stalled (the kill makes them exit → drainExecutor classifies
// them as failed), and (2) polls each ADOPTED ORPHAN via CheckOrphan — the orphans
// have no reapable handle, so this poll is the only thing that detects their stall
// or completion and finalizes them. Mirrors maybeRunGC's tick-driven, throttled,
// lock-snapshotted shape (no background goroutines).
func (c *AgentController) maybeRunExecutorWatchdog(ctx context.Context, now time.Time) {
	c.mu.Lock()
	if !c.lastExecWatchdogAt.IsZero() && now.Sub(c.lastExecWatchdogAt) < defaultExecutorWatchdogInterval {
		c.mu.Unlock()
		return
	}
	type agentEng struct {
		id string
		ee *executorEngine
	}
	var engines []agentEng
	for id, ma := range c.agents {
		if ma != nil && ma.exec != nil {
			engines = append(engines, agentEng{id: id, ee: ma.exec})
		}
	}
	c.lastExecWatchdogAt = now
	c.mu.Unlock()

	for _, ae := range engines {
		// 1. Stall-sweep the live (this-process) executors.
		if killed, err := ae.ee.monitor.Sweep(ctx); err != nil {
			c.log("agent=%s executor watchdog sweep: %v", ae.id, err)
		} else if len(killed) > 0 {
			c.log("agent=%s watchdog killed stalled executors: %v", ae.id, killed)
		}
		// 2. Poll the adopted orphans (no reapable handle): stall + completion.
		for id, pid := range ae.ee.snapshotOrphans() {
			done, kind, err := c.checkOrphanOnce(ctx, ae.ee, id, pid)
			if err != nil {
				c.log("agent=%s orphan watchdog executor=%s: %v", ae.id, id, err)
				continue
			}
			if done {
				ae.ee.dropOrphan(id)
				c.log("agent=%s recovered orphan executor=%s finalized: %s", ae.id, id, kind)
			}
		}
	}
}

// checkOrphanOnce runs one orphan watchdog+completion tick and reports whether it
// reached a terminal outcome (so the caller stops polling it).
func (c *AgentController) checkOrphanOnce(ctx context.Context, ee *executorEngine, id string, pid int) (bool, executor.OutcomeKind, error) {
	comp, done, err := ee.monitor.CheckOrphan(ctx, id, pid)
	if err != nil {
		return false, "", err
	}
	return done, comp.Kind, nil
}

// firstNonEmptyLine returns the first non-blank, trimmed line of s, capped to a
// reasonable title length (used to derive a goal title from the work brief).
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 120 {
			return line[:120]
		}
		return line
	}
	return ""
}

// clock import retained for funcClock's interface conformance.
var _ clock.Clock = funcClock{}
