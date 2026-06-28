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
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// executorEngine bundles the per-agent W1 wiring: the orchestration Engine (the
// F4→F3→F2→F1 chain) plus the Monitor that reaps a finished executor and frees its
// pool slot. Attached to a managedAgent only when concurrency is enabled.
type executorEngine struct {
	engine  *orchestrator.Engine
	monitor *executor.Monitor
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
	pool, err := executor.NewPool(executor.PoolConfig{
		Exchange:   fx,
		AgentRoot:  agentRoot,
		BinaryPath: c.cfg.BinaryPath,
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
	// Monitor with NIL Writeback (W1): reap + free slot + cleanup, no center write.
	// W2 supplies a real Writeback so Report runs before teardown.
	mon, err := executor.NewMonitor(executor.MonitorConfig{
		Exchange: fx,
		Pool:     pool,
		Clock:    clk,
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
