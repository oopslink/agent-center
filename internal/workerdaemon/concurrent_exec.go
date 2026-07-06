package workerdaemon

// concurrent_exec.go — DAEMON-side residue of the per-agent executor面 after Phase
// 0c moved the engine + fork/drain/recover/watchdog into agentruntime.LocalRuntime.
// What stays here is (1) the opt-in predicate, (2) the reconcile→ExecutorConfig
// boundary converter, and (3) the per-worker concurrency snapshot aggregate, all of
// which are daemon concerns (reconcile payload / cross-agent iteration).

import (
	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/agentruntime"
)

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
