package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
)

// IDMinter mints the identity keys the orchestrator assigns when it forks an
// executor / opens a problem. A PORT so tests inject deterministic ids; production
// wraps idgen (ULID-based, collision-free). Executor ids must be path-safe
// (executor.validateExecutorID rejects separators) — idgen's "<prefix>-<ULID>"
// satisfies that.
type IDMinter interface {
	NewExecutorID() string
	NewProblemID() string
}

// WorkItem is one unit of incoming work the orchestrator turns into an executor
// (distilled by the daemon from an agent.work / agent.converse event). The refs
// drive F4 consistency routing; Goal + TaskModel + Context drive F3 model routing
// and the F2 input.
type WorkItem struct {
	TaskID    string
	TaskRef   string
	IssueRef  string
	ChatID    string
	Goal      executor.Goal
	TaskModel string // task.model hard override ("" = unset → F3 judges/falls back)
	Context   string // aggregated context the orchestrator assembled (design §6.E)
	// ExecutorID, when non-empty, is a pre-minted executor id the caller already used
	// to materialize a worktree BEFORE HandleWork (P3): HandleWork uses it verbatim so
	// the launched executor's workspace path matches the prepared worktree. Empty ⇒
	// HandleWork mints one (today's behavior, byte-for-byte).
	ExecutorID string
	// Prepared, when non-nil, is a git worktree already materialized at the executor's
	// workspace (P4): it is threaded into the LaunchSpec so the pool uses it as-is and
	// its teardown handle is persisted. Nil ⇒ today's provisioning path.
	Prepared *executor.PreparedWorkspace
}

// Launched is the result of a successful fork: the executor handle plus the
// routing/model provenance (for logging + the daemon's completion tracking).
type Launched struct {
	ExecutorID  string
	ProblemID   string
	CLI         string // v2.18.1 BE-2: which CLI runner forked this executor (claude-code|codex)
	Model       string
	ModelSource modelrouter.Source
	RouteReason executor.MatchReason
	Handle      *executor.Handle
}

// EngineConfig wires an Engine's per-agent collaborators.
type EngineConfig struct {
	// Pool is the F1 concurrency-gated launcher (required).
	Pool *executor.Pool
	// Routing is the F4 consistency-routing store over <agent_root>/routing.json
	// (required).
	Routing *executor.RoutingStore
	// Router is the F3 model-routing priority chain (required).
	Router *modelrouter.Router
	// RouterConfig is this agent's profile model config (orchestrator/allowed/default).
	RouterConfig modelrouter.Config
	// Runners builds the executor's model-routed runner argv, keyed by CLI (v2.18.1
	// BE-2): the F3 decision's CLI selects the builder (e.g. "claude-code" →
	// ClaudeRunnerBuilder, "codex" → CodexRunnerBuilder). Must be non-empty with
	// non-nil values; HandleWork errors if the routed CLI has no registered builder.
	Runners map[string]RunnerCmdBuilder
	// IDs mints executor / problem ids (required).
	IDs IDMinter
	// Clock stamps Input.CreatedAt. Nil → SystemClock.
	Clock clock.Clock
}

// Engine is the per-agent orchestration brain: it chains F4 routing → F3 model
// selection → F2 input → F1 fork for each incoming WorkItem. One Engine per agent;
// its methods are driven by the daemon's single per-agent work path, so they need
// no internal locking beyond what the Pool / RoutingStore already provide.
type Engine struct {
	pool    *executor.Pool
	routing *executor.RoutingStore
	router  *modelrouter.Router
	// rcfgMu guards rcfg: HandleWork reads it per-fork while a live profile change
	// (UpdateRouterConfig, driven by a config-edit reconcile) may replace it. A plain
	// field would race under concurrent forks + a reconcile.
	rcfgMu  sync.RWMutex
	rcfg    modelrouter.Config
	runners map[string]RunnerCmdBuilder
	ids     IDMinter
	clk     clock.Clock
}

// NewEngine validates cfg and builds an Engine.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	switch {
	case cfg.Pool == nil:
		return nil, errors.New("orchestrator: engine pool required")
	case cfg.Routing == nil:
		return nil, errors.New("orchestrator: engine routing required")
	case cfg.Router == nil:
		return nil, errors.New("orchestrator: engine router required")
	case len(cfg.Runners) == 0:
		return nil, errors.New("orchestrator: engine runners required")
	case cfg.IDs == nil:
		return nil, errors.New("orchestrator: engine id minter required")
	}
	for cli, rb := range cfg.Runners {
		if rb == nil {
			return nil, fmt.Errorf("orchestrator: engine runner for cli %q is nil", cli)
		}
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	runners := make(map[string]RunnerCmdBuilder, len(cfg.Runners))
	for cli, rb := range cfg.Runners {
		runners[cli] = rb
	}
	return &Engine{
		pool:    cfg.Pool,
		routing: cfg.Routing,
		router:  cfg.Router,
		rcfg:    cfg.RouterConfig,
		runners: runners,
		ids:     cfg.IDs,
		clk:     clk,
	}, nil
}

// UpdateRouterConfig replaces the agent's model-routing config in place so a live
// profile change (web-console edit → config-triggered reconcile) propagates to
// SUBSEQUENT forks WITHOUT rebuilding the engine. The pool, monitor, tracker, and any
// in-flight/orphan executors are untouched — an in-flight executor keeps the model it
// was forked with; only new HandleWork forks see the updated config. This is what lets
// an agent's config change reach a RUNNING agent without a restart (k8s: each
// agent-runtime is its own process; the reconcile command is the only channel).
func (e *Engine) UpdateRouterConfig(cfg modelrouter.Config) {
	e.rcfgMu.Lock()
	e.rcfg = cfg
	e.rcfgMu.Unlock()
}

// Pool exposes the underlying Pool (the daemon checks Available()/drives completion).
func (e *Engine) Pool() *executor.Pool { return e.pool }

// NewExecutorID mints a fresh executor id via the engine's minter. It lets the
// SpawnExecutor caller pre-mint the id BEFORE HandleWork (P3), so the repo-materializer
// worktree path/branch embed the SAME id the pool ultimately launches.
func (e *Engine) NewExecutorID() string { return e.ids.NewExecutorID() }

// HandleWork turns one WorkItem into a forked executor, chaining the foundations
// (design §11.1 step a–d):
//
//  1. F4 — route the signal to an existing problem or register a new one;
//  2. F3 — resolve the executor model via the §5 priority chain;
//  3. build the model-routed runner argv + the F2 Input;
//  4. F1 — Pool.Launch (real fork into an isolated process group, ≤ max);
//  5. F4 — merge the launched executor + source refs back onto the problem.
//
// Returns executor.ErrAtCapacity (unwrapped) when the pool is full so the caller
// QUEUES the work rather than treating it as a failure (design §3 "超额排队，不硬起").
func (e *Engine) HandleWork(ctx context.Context, item WorkItem) (*Launched, error) {
	if strings.TrimSpace(item.Goal.Title) == "" {
		return nil, errors.New("orchestrator: work item goal.title required")
	}

	// 1. F4 consistency routing.
	sig := executor.Signal{ChatID: item.ChatID, IssueRef: item.IssueRef, TaskRef: item.TaskRef}
	dec, err := e.routing.Route(sig)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: route: %w", err)
	}
	problemID := dec.ProblemID
	if dec.IsNew {
		problemID = e.ids.NewProblemID()
		if err := e.routing.Register(executor.Problem{
			ProblemID: problemID,
			IssueRef:  item.IssueRef,
			TaskRefs:  refsOf(item.TaskRef),
			ChatIDs:   refsOf(item.ChatID),
		}); err != nil {
			return nil, fmt.Errorf("orchestrator: register problem: %w", err)
		}
	}

	// 2. F3 routing (priority chain §5) — resolves the executor {cli, model}. Snapshot
	// rcfg under the read lock so a concurrent UpdateRouterConfig (live config edit)
	// cannot tear the config out from under this fork.
	e.rcfgMu.RLock()
	rcfg := e.rcfg
	e.rcfgMu.RUnlock()
	modelDec, err := e.router.ResolveExecutor(ctx, item.TaskModel, item.Goal, rcfg)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: resolve executor: %w", err)
	}

	// 3. build the runner argv (per-CLI builder, BE-2) + the F2 input.
	runner := e.runners[modelDec.CLI]
	if runner == nil {
		return nil, fmt.Errorf("orchestrator: no runner builder for cli %q", modelDec.CLI)
	}
	// Use the caller's pre-minted id (P3: the worktree path/branch already embed it) or
	// mint a fresh one — the plain-dir path, byte-for-byte as before. Resolved BEFORE
	// the runner argv so the executor's session id can be derived from it (§4.3).
	execID := strings.TrimSpace(item.ExecutorID)
	if execID == "" {
		execID = e.ids.NewExecutorID()
	}
	// §4.3: allocate a durable, resumable session id for this executor's LLM
	// conversation. Derived deterministically from execID (a valid v5 UUID claude
	// accepts as --session-id) so it is stable and reconstructible; the builder binds
	// it into the argv (claude) or ignores it (codex mints its own thread), and it is
	// persisted into Record.SessionID for tier-1 --resume crash recovery.
	sessionID := claudestream.SessionUUID(execID, 0)
	runnerCmd, err := runner.Build(modelDec.Model, buildPrompt(item), sessionID)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: build runner: %w", err)
	}
	input := executor.Input{
		ExecutorID: execID,
		ProblemID:  problemID,
		Goal:       item.Goal,
		Model:      modelDec.Model,
		CLI:        modelDec.CLI, // v2.19.0: persisted for the real-time concurrency snapshot
		Context:    item.Context,
		Source: executor.SourceRefs{
			ChatIDs:  refsOf(item.ChatID),
			IssueRef: item.IssueRef,
			TaskRef:  item.TaskRef,
		},
		CreatedAt: e.clk.Now(),
	}

	// 4. F1 fork (≤ max; ErrAtCapacity bubbles up unwrapped for the caller to queue).
	h, err := e.pool.Launch(ctx, executor.LaunchSpec{Input: input, RunnerCmd: runnerCmd, SessionID: sessionID, Prepared: item.Prepared})
	if err != nil {
		if errors.Is(err, executor.ErrAtCapacity) {
			return nil, err
		}
		return nil, fmt.Errorf("orchestrator: launch executor: %w", err)
	}

	// 5. F4 merge — bind the launched executor + source refs onto the problem so a
	// later message about the same problem routes here (best-effort: a merge failure
	// must not orphan a successfully-forked executor; surface it without unforking).
	if mErr := e.routing.Merge(problemID, sig, execID); mErr != nil {
		return &Launched{
			ExecutorID:  execID,
			ProblemID:   problemID,
			CLI:         modelDec.CLI,
			Model:       modelDec.Model,
			ModelSource: modelDec.Source,
			RouteReason: dec.Reason,
			Handle:      h,
		}, fmt.Errorf("orchestrator: merge routing (executor %s launched): %w", execID, mErr)
	}

	return &Launched{
		ExecutorID:  execID,
		ProblemID:   problemID,
		CLI:         modelDec.CLI,
		Model:       modelDec.Model,
		ModelSource: modelDec.Source,
		RouteReason: dec.Reason,
		Handle:      h,
	}, nil
}

// buildPrompt assembles the executor's prompt from the goal + aggregated context.
// Title is mandatory; the rest are appended when present.
func buildPrompt(item WorkItem) string {
	var b strings.Builder
	b.WriteString(item.Goal.Title)
	if d := strings.TrimSpace(item.Goal.Description); d != "" {
		b.WriteString("\n\n")
		b.WriteString(d)
	}
	if s := strings.TrimSpace(item.Goal.IssueSpec); s != "" {
		b.WriteString("\n\n## Spec\n")
		b.WriteString(s)
	}
	if c := strings.TrimSpace(item.Context); c != "" {
		b.WriteString("\n\n## Context\n")
		b.WriteString(c)
	}
	return b.String()
}

// refsOf returns a single-element slice for a non-empty ref, else nil (so omitempty
// drops it and routing set-fields stay clean).
func refsOf(ref string) []string {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	return []string{ref}
}
