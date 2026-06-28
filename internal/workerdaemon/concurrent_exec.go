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

	"github.com/oopslink/agent-center/internal/clock"
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
func concurrencyEnabled(pl reconcilePayload) bool {
	return pl.MaxConcurrentTasks > 0 && len(pl.AllowedModels) > 0
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
		// difficulty judge (consuming allowed_models) is a follow-up wiring.
		Router: modelrouter.NewRouter(nil),
		RouterConfig: modelrouter.Config{
			OrchestratorModel:    pl.OrchestratorModel,
			AllowedModels:        pl.AllowedModels,
			DefaultExecutorModel: pl.DefaultExecutorModel,
		},
		Runner: orchestrator.NewClaudeRunnerBuilder(c.cfg.ClaudeBinary),
		IDs:    orchestrator.NewULIDMinter(clk),
		Clock:  clk,
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
	// Monitor with NIL Writeback (W1/W3): reap/recover + free slot + cleanup, no
	// center write yet. W2 supplies a real Writeback so Report runs before teardown.
	// W3 wires the Watchdog + Reconciler so Sweep/Recover/CheckOrphan are live.
	mon, err := executor.NewMonitor(executor.MonitorConfig{
		Exchange:   fx,
		Pool:       pool,
		Watchdog:   watchdog,
		Reconciler: reconciler,
		Clock:      clk,
		// Liveness defaults to SignalLiveness (real pid probe).
	})
	if err != nil {
		return nil, err
	}
	return &executorEngine{engine: eng, monitor: mon}, nil
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
		// §5 chain falls back to default_executor_model. Plumbing task.model from the
		// center is a follow-up (does not block the W1 fork path).
	}
	launched, err := ee.engine.HandleWork(ctx, item)
	if err != nil {
		if errors.Is(err, executor.ErrAtCapacity) {
			return fmt.Errorf("agent_controller: agent=%s at executor capacity (queue, retry next tick): %w", pl.AgentID, err)
		}
		return fmt.Errorf("agent_controller: agent=%s fork executor: %w", pl.AgentID, err)
	}
	c.log("agent=%s task=%s forked executor=%s model=%s(%s) problem=%s",
		pl.AgentID, pl.TaskID, launched.ExecutorID, launched.Model, launched.ModelSource, launched.ProblemID)

	// Reap the executor when it exits, freeing its pool slot (W1). Runs detached so
	// the work command acks immediately and the next work can launch concurrently.
	go c.drainExecutor(ee, launched.Handle)

	// Mirror the inject path's work-state bookkeeping (L2 error surface / self-heal).
	c.mu.Lock()
	if cur := c.agents[pl.AgentID]; cur != nil {
		cur.hadWork = true
		if pl.TaskID != "" {
			cur.currentTaskID = pl.TaskID
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
