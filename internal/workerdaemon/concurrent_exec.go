package workerdaemon

// concurrent_exec.go — DAEMON-side residue of the per-agent executor面 after Phase
// 0c moved the engine + fork/drain/recover/watchdog into agentruntime.LocalRuntime.
// What stays here is (1) the opt-in predicate, (2) the reconcile→ExecutorConfig
// boundary converter, and (3) the per-worker concurrency snapshot aggregate, all of
// which are daemon concerns (reconcile payload / cross-agent iteration).

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
)

// defaultExecutorWatchdogInterval is the daemon-side throttle for the executor
// watchdog/orphan-poll sweep (kept daemon-driven so the 30s cadence is byte-identical
// to before Phase 0c; the per-agent sweep body now lives in LocalRuntime.RunWatchdog).
const defaultExecutorWatchdogInterval = agentruntime.DefaultExecutorWatchdogInterval

// maybeRunExecutorWatchdog is the per-tick executor watchdog sweep (W3), called from
// OnTick and throttled to defaultExecutorWatchdogInterval. When due it snapshots the
// runtimes under the lock, then drives each runtime's RunWatchdog OUTSIDE the lock
// (RunWatchdog no-ops when no engine is attached). Preserves maybeRunGC's tick-driven,
// throttled, lock-snapshotted shape (no background goroutines).
func (c *AgentController) maybeRunExecutorWatchdog(ctx context.Context, now time.Time) {
	c.mu.Lock()
	if !c.lastExecWatchdogAt.IsZero() && now.Sub(c.lastExecWatchdogAt) < defaultExecutorWatchdogInterval {
		c.mu.Unlock()
		return
	}
	rts := make([]*agentruntime.LocalRuntime, 0, len(c.agents))
	for _, ma := range c.agents {
		if ma != nil && ma.runtime != nil {
			rts = append(rts, ma.runtime)
		}
	}
	c.lastExecWatchdogAt = now
	c.mu.Unlock()

	for _, rt := range rts {
		rt.RunWatchdog(ctx)
	}
}

// concurrencyEnabled is the opt-in gate (PD ruling, decision 2): the executor
// concurrency path activates only when the profile sets MaxConcurrentTasks>0 AND
// lists at least one allowed executor. Otherwise the agent keeps the legacy inject
// path. Delegates to agent.Profile.ConcurrencyEnabled so the daemon's pool gate and
// the center's ≤N start cap share ONE predicate (issue-b8687f2a §2 / v2.18.1 BE-1).
func concurrencyEnabled(pl reconcilePayload) bool {
	return agent.Profile{
		MaxConcurrentTasks: pl.MaxConcurrentTasks,
		AllowedExecutors:   pl.AllowedExecutors,
	}.ConcurrencyEnabled()
}

// execConfigOf converts a reconcile payload into the neutral ExecutorConfig the
// runtime's BuildExecutorEngine consumes (the daemon→runtime boundary — agentruntime
// never sees the daemon's wire payload type).
func execConfigOf(pl reconcilePayload) agentruntime.ExecutorConfig {
	return agentruntime.ExecutorConfig{
		AgentID:              pl.AgentID,
		DisplayName:          pl.DisplayName,
		EnvVars:              pl.EnvVars,
		MaxConcurrentTasks:   pl.MaxConcurrentTasks,
		AllowedExecutors:     pl.AllowedExecutors,
		OrchestratorModel:    pl.OrchestratorModel,
		DefaultExecutorModel: pl.DefaultExecutorModel,
		CLI:                  pl.CLI,
	}
}

// SnapshotConcurrency returns the per-agent live executor view for every agent on
// this worker running the concurrent path (v2.19.0). It is what the heartbeat ships
// to the center (agent_id → snapshot) and is read by GET .../agents/{id}/concurrency.
// Agents with a concurrency engine but zero live executors are still emitted (active
// 0) so the center's last-known state reflects "idle now", not a stale set.
func (c *AgentController) SnapshotConcurrency() map[string]concurrency.AgentSnapshot {
	// Snapshot the runtimes under c.mu, then query each OUTSIDE it — HasExecutor /
	// SnapshotConcurrency take the runtime's own StateMu (T839 §4.1 去共享状态); keeping
	// the snapshot-then-query split preserves the lock-order discipline (never hold c.mu
	// while taking a runtime's StateMu) with no behavior change.
	c.mu.Lock()
	type agentRT struct {
		id string
		rt *agentruntime.LocalRuntime
	}
	var rts []agentRT
	for id, ma := range c.agents {
		if ma != nil && ma.runtime != nil {
			rts = append(rts, agentRT{id: id, rt: ma.runtime})
		}
	}
	c.mu.Unlock()

	var out map[string]concurrency.AgentSnapshot
	for _, ae := range rts {
		if !ae.rt.HasExecutor() {
			continue
		}
		if out == nil {
			out = make(map[string]concurrency.AgentSnapshot, len(rts))
		}
		execs := ae.rt.SnapshotConcurrency()
		out[ae.id] = concurrency.AgentSnapshot{Active: len(execs), Executors: execs}
	}
	return out
}
