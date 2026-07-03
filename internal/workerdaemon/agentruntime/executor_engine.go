package agentruntime

// executor_engine.go — the per-agent EXECUTOR面, moved (Phase 0c) off
// workerdaemon.AgentController into agentruntime so the runtime owns both the
// SESSION面 (0b) and the EXECUTOR面. This file holds the pure ExecutorEngine value
// (the orchestration Engine + Monitor + FileExchange + adopted-orphan set) and its
// methods; the LocalRuntime methods that DRIVE it (build/fork/drain/recover/
// watchdog/spawn) live in executor_runtime.go.
//
// Import direction (unchanged): agentruntime must NOT import workerdaemon. This
// file imports only the pure lower packages (agent / concurrency / executor /
// modelrouter / orchestrator / clock), none of which import workerdaemon.

import (
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

// ExecutorEngine bundles the per-agent W1 wiring: the orchestration Engine (the
// F4→F3→F2→F1 chain) plus the Monitor that reaps a finished executor and frees its
// pool slot. Installed on a LocalRuntime (r.exec) only when concurrency is enabled.
//
// W3 adds crash-recovery state: orphans is the set of ADOPTED orphan executors
// (executor_id → pid) rebuilt by Recover after a restart. Unlike this-process
// spawns (reaped by a drainExecutor goroutine), an orphan has no reapable handle,
// so the watchdog tick polls each via Monitor.CheckOrphan until it terminates.
type ExecutorEngine struct {
	engine  *orchestrator.Engine
	monitor *executor.Monitor
	// fx reads each executor's input.json / status.json for the real-time
	// concurrency snapshot (v2.19.0).
	fx *executor.FileExchange

	mu      sync.Mutex
	orphans map[string]int // adopted orphan executor_id → pid; watchdog-polled until terminal
}

// addOrphan registers a recovered, still-alive executor for watchdog polling.
func (ee *ExecutorEngine) addOrphan(id string, pid int) {
	ee.mu.Lock()
	defer ee.mu.Unlock()
	if ee.orphans == nil {
		ee.orphans = make(map[string]int)
	}
	ee.orphans[id] = pid
}

// snapshotOrphans returns a copy of the orphan set for lock-free iteration by the
// watchdog tick.
func (ee *ExecutorEngine) snapshotOrphans() map[string]int {
	ee.mu.Lock()
	defer ee.mu.Unlock()
	out := make(map[string]int, len(ee.orphans))
	for id, pid := range ee.orphans {
		out[id] = pid
	}
	return out
}

// dropOrphan removes an orphan that reached a terminal completion (stop polling it).
func (ee *ExecutorEngine) dropOrphan(id string) {
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
func (ee *ExecutorEngine) SnapshotConcurrency() []concurrency.ExecutorSnapshot {
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
func (ee *ExecutorEngine) enrichFromFiles(snap *concurrency.ExecutorSnapshot, orphan bool) {
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

// funcClock adapts a func() time.Time test-seam to clock.Clock (the interface the
// executor/orchestrator packages take), so the executor wiring shares the runtime's
// clock and stays deterministic under the daemon's test clock.
type funcClock struct{ now func() time.Time }

func (f funcClock) Now() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
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
