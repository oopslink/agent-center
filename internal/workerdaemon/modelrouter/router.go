// Package modelrouter implements F3 of the agent-concurrent-execution design
// (docs/design/features/agent-concurrent-execution.md §5 / §10): the orchestrator
// (监工) decides which model an executor runs under, by a fixed priority chain.
//
// Priority chain (design §5, authoritative):
//
//	task.model set   → use it verbatim (HARD override, highest priority; the LLM is
//	                   never consulted); CLI paired from profile.allowed_executors
//	not set          → the orchestrator's LLM reads the goal, judges difficulty, and
//	                   picks one {cli, model} from profile.allowed_executors (v2.18.1 BE-2)
//	can't judge      → profile.default_executor_model (fallback), or when that is unset
//	                   profile.orchestrator_model as a last-resort default (T743) so an
//	                   orchestrator-only profile still spawns instead of erroring
//
// The orchestrator's OWN model (used for routing / judging / aggregation — the
// cheap-fast tier) is profile.orchestrator_model.
//
// Difficulty is judged by LLM REASONING, never hardcoded heuristics (design §5):
// this package therefore depends only on a DifficultyJudge PORT and contains no
// model-tiering rules of its own. The orchestrator wires the port to its reasoning
// model; tests inject a fake (conventions §4: zero LLM SDK in this repo).
package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// Source records which rung of the §5 priority chain produced the model, so the
// orchestrator can log / inspect why a given executor got its model.
type Source string

const (
	// SourceTaskOverride — task.model was set; used verbatim (highest priority).
	SourceTaskOverride Source = "task_override"
	// SourceJudged — the LLM judged difficulty and picked from allowed_models.
	SourceJudged Source = "llm_judged"
	// SourceDefault — difficulty was unjudgeable; default_executor_model fallback.
	SourceDefault Source = "default_fallback"
	// SourcePoolFallback — allowed_executors is configured but no judge is wired (or the
	// judge was inconclusive), so a candidate is picked deterministically from the pool
	// rather than ignoring the configured pool. Distinct provenance so logs show the
	// pool WAS used (not silently bypassed) pending a real difficulty judge.
	SourcePoolFallback Source = "pool_fallback"
)

// Sentinel errors (errors.Is-matchable) for the unhappy paths.
var (
	// ErrInconclusive is what a DifficultyJudge returns when it cannot decide a
	// model for the goal — the router treats it as "can't judge" and falls back to
	// the default (design §5). Exported so a judge implementation can return it.
	ErrInconclusive = errors.New("modelrouter: difficulty judge inconclusive")
	// ErrJudgeOutOfRange is recorded when the judge picks an executor {cli,model}
	// that is not in allowed_executors (an LLM hallucination); the router refuses it
	// and falls back rather than spawn an unsanctioned executor.
	ErrJudgeOutOfRange = errors.New("modelrouter: judged executor not in allowed_executors")
	// ErrNoExecutorModel is returned when the chain resolves to nothing: no
	// task.model, no usable judged model, and no default. The file-exchange
	// protocol requires a non-empty executor model before spawn, so this is fatal
	// for that task rather than a silent default.
	ErrNoExecutorModel = errors.New("modelrouter: no executor model resolvable")
	// ErrNoOrchestratorModel is returned by OrchestratorModel when the profile has
	// no orchestrator_model configured.
	ErrNoOrchestratorModel = errors.New("modelrouter: orchestrator_model not configured")
	// ErrModelNotAllowed is returned when task.model is set but not found in
	// the agent's allowed_executors list. The caller should block the task
	// with an obstacle rather than silently falling back.
	ErrModelNotAllowed = errors.New("modelrouter: task model not in allowed_executors")
)

// ExecutorCandidate is one {cli, model} executor candidate the router chooses among
// (v2.18.1 BE-2). It is a decoupled mirror of agent.ExecutorProfile: this package
// stays free of the agent bounded context (passed as primitives, no import, no
// cycle), so the daemon maps agent.ExecutorProfile → ExecutorCandidate at the seam.
type ExecutorCandidate struct {
	CLI   string
	Model string
}

// fallbackCLI is the CLI paired with a model-only override / default that does not
// match any allowed_executors entry (and the candidate set is not single-CLI). It
// mirrors agent.DefaultExecutorCLI ("claude-code", the historical default) without
// importing the agent aggregate.
const fallbackCLI = "claude-code"

// Config is the per-agent model configuration the router reads (the routing fields
// of the agent profile, design §10). It is passed as primitives so this package
// stays decoupled from the agent bounded context (no import of the agent
// aggregate, no cycle).
type Config struct {
	OrchestratorModel string // profile.orchestrator_model — the orchestrator's own (cheap/fast) model
	// AllowedExecutors is the authoritative {cli,model} candidate set (v2.18.1 BE-2,
	// from profile.allowed_executors): the judge picks one, and a model-only override
	// / default is paired with the CLI of its matching entry.
	AllowedExecutors     []ExecutorCandidate
	DefaultExecutorModel string // profile.default_executor_model — fallback model when unjudgeable
	// SupervisorModel is the agent's OWN model (profile.model — what the supervisor
	// session runs under). It is the ABSOLUTE last-resort fallback so an executor always
	// has a model to run — "use whatever the supervisor uses" — instead of failing to
	// spawn when neither default_executor_model nor orchestrator_model is configured.
	SupervisorModel string
	// DefaultCLI is the CLI paired with the task.model override / default_executor_model
	// when that model is not found in AllowedExecutors and the candidates are not
	// single-CLI (typically the agent's own cli). Empty → fallbackCLI ("claude-code").
	DefaultCLI string
}

// JudgeRequest is what the orchestrator hands the LLM port: the goal to size up
// and the menu of {cli,model} executors it may choose from.
type JudgeRequest struct {
	Goal             executor.Goal
	AllowedExecutors []ExecutorCandidate
}

// Judgment is the judge's verdict: the chosen executor (which MUST be one of the
// request's AllowedExecutors — the router rejects anything else).
type Judgment struct {
	CLI   string
	Model string
}

// DifficultyJudge is the LLM port. The orchestrator implements it against its
// reasoning model (profile.orchestrator_model). Return ErrInconclusive (or any
// error) to signal "can't judge" → the router falls back to the default.
type DifficultyJudge interface {
	Judge(ctx context.Context, req JudgeRequest) (Judgment, error)
}

// Decision is the resolved executor {cli, model} plus provenance (v2.18.1 BE-2:
// the decision now carries the CLI too, so the engine forks the right runner).
// JudgeError is non-nil when the judge was attempted but did not yield a usable
// executor and the router fell back to the default — the reason is carried here (not
// swallowed) for observability, while CLI/Model/Source still drive the spawn.
type Decision struct {
	CLI        string
	Model      string
	Source     Source
	JudgeError error
}

// Router applies the §5 priority chain. It is stateless beyond the judge port and
// safe to share across concurrent executor launches.
type Router struct {
	judge DifficultyJudge
}

// NewRouter builds a Router over the given judge. A nil judge is allowed: the
// judge step is then skipped and resolution goes straight task.model → default
// (useful before the orchestrator's LLM port is wired, and in tests).
func NewRouter(judge DifficultyJudge) *Router {
	return &Router{judge: judge}
}

// ResolveExecutor applies the design §5 priority chain to choose the {cli, model}
// the executor for this task will run under (v2.18.1 BE-2). taskModel is task.model
// ("" = unset).
//
//  1. taskModel set             → SourceTaskOverride (judge never consulted); CLI paired via resolveCLI
//  2. judge picks an allowed    → SourceJudged (CLI is the chosen candidate's)
//     2b. allowed_executors set,   → SourcePoolFallback: pick the first candidate deterministically —
//     judge unwired/inconclusive  a configured pool must be used, not bypassed for lack of a judge
//  3. default/orchestrator/     → SourceDefault (default_executor_model, else orchestrator_model,
//     supervisor model            else the agent's own supervisor model — always resolvable);
//     JudgeError carries why, if a judge ran; CLI paired via resolveCLI
//  4. nothing resolvable        → ErrNoExecutorModel (wrapping the judge reason)
//
// The CLI for the model-only override / default paths is paired by resolveCLI:
// the matching candidate's CLI if the model is in allowed_executors, else the sole
// CLI when the candidates are single-CLI (so an only-codex agent never produces a
// claude executor and vice versa), else cfg.DefaultCLI / fallbackCLI.
func (r *Router) ResolveExecutor(ctx context.Context, taskModel string, goal executor.Goal, cfg Config) (Decision, error) {
	// 1. Hard override — highest priority; short-circuit before any LLM call.
	if m := strings.TrimSpace(taskModel); m != "" {
		// When allowed_executors is configured (concurrent mode), task.model
		// MUST be among them — a model the agent cannot run should block the
		// task, not silently fall back. When allowed_executors is empty
		// (single-task mode), the agent runs the model directly via its CLI,
		// so no validation is needed here.
		if len(cfg.AllowedExecutors) > 0 && !containsModel(cfg.AllowedExecutors, m) {
			return Decision{}, fmt.Errorf("%w: %q", ErrModelNotAllowed, m)
		}
		return Decision{CLI: resolveCLI(cfg, m), Model: m, Source: SourceTaskOverride}, nil
	}

	// 2. LLM difficulty judge — only when a judge is wired AND there are candidate
	// executors to choose from. A failure / inconclusive / out-of-range pick does not
	// abort: it is remembered and we fall through to the default.
	var judgeErr error
	if r.judge != nil && len(cfg.AllowedExecutors) > 0 {
		j, err := r.judge.Judge(ctx, JudgeRequest{Goal: goal, AllowedExecutors: cfg.AllowedExecutors})
		switch {
		case err != nil:
			judgeErr = err
		case !containsExecutor(cfg.AllowedExecutors, j.CLI, j.Model):
			judgeErr = fmt.Errorf("%w: judge picked %q/%q", ErrJudgeOutOfRange, j.CLI, j.Model)
		default:
			return Decision{CLI: j.CLI, Model: j.Model, Source: SourceJudged}, nil
		}
	}

	// 3. Explicit default — profile.default_executor_model, or (when unset)
	// profile.orchestrator_model as a last-resort default (T743). An operator's explicit
	// default beats the deterministic pool/supervisor fallbacks below.
	d := strings.TrimSpace(cfg.DefaultExecutorModel)
	if d == "" {
		d = strings.TrimSpace(cfg.OrchestratorModel)
	}
	if d != "" {
		return Decision{CLI: resolveCLI(cfg, d), Model: d, Source: SourceDefault, JudgeError: judgeErr}, nil
	}

	// 3b. Pool fallback — no judge/default/orchestrator resolved a model, but an
	// allowed_executors pool IS configured. A configured pool must be USED, not silently
	// bypassed because the LLM judge port is unwired (or the judge was inconclusive): pick
	// the first candidate deterministically so the agent's configured pool actually drives
	// the executor model. A real difficulty judge over the pool is the follow-up.
	if len(cfg.AllowedExecutors) > 0 {
		c := cfg.AllowedExecutors[0]
		return Decision{CLI: c.CLI, Model: c.Model, Source: SourcePoolFallback, JudgeError: judgeErr}, nil
	}

	// 3c. Supervisor-model fallback — nothing above resolved, but the agent's OWN model
	// (what the supervisor session runs under) is known. Spawn the executor under it ("use
	// whatever the supervisor uses") instead of failing with ErrNoExecutorModel and
	// stranding no-task.model tasks in a silent fork-fail loop.
	if s := strings.TrimSpace(cfg.SupervisorModel); s != "" {
		return Decision{CLI: resolveCLI(cfg, s), Model: s, Source: SourceDefault, JudgeError: judgeErr}, nil
	}

	// 4. Nothing resolvable — no task.model, no judged/pool model, no default/orchestrator/
	// supervisor model. Fatal for this task (the file-exchange protocol needs a non-empty
	// model before spawn), rather than a silent guess. Surface the judge reason if any.
	if judgeErr != nil {
		return Decision{}, errors.Join(ErrNoExecutorModel, judgeErr)
	}
	return Decision{}, ErrNoExecutorModel
}

// OrchestratorModel returns the orchestrator's own model (design §5: the cheap/fast
// tier used for routing / judging / aggregation). It errors when unconfigured
// rather than guessing — the orchestrator cannot run without one.
func (r *Router) OrchestratorModel(cfg Config) (string, error) {
	if m := strings.TrimSpace(cfg.OrchestratorModel); m != "" {
		return m, nil
	}
	return "", ErrNoOrchestratorModel
}

// containsExecutor reports whether {cli, model} is an exact entry of execs.
func containsExecutor(execs []ExecutorCandidate, cli, model string) bool {
	for _, e := range execs {
		if e.CLI == cli && e.Model == model {
			return true
		}
	}
	return false
}

// containsModel reports whether ANY executor candidate has the given model
// (regardless of CLI). Used to validate task.model against allowed_executors.
func containsModel(execs []ExecutorCandidate, model string) bool {
	for _, e := range execs {
		if e.Model == model {
			return true
		}
	}
	return false
}

// resolveCLI pairs a model-only override / default with a CLI (v2.18.1 BE-2):
//  1. the CLI of the first allowed_executors entry with that model (exact pin); else
//  2. the sole CLI when every candidate shares one (so an only-codex agent never
//     produces a claude executor and vice versa — the symmetric BE-2 guard); else
//  3. cfg.DefaultCLI, or fallbackCLI when that too is empty.
func resolveCLI(cfg Config, model string) string {
	for _, e := range cfg.AllowedExecutors {
		if e.Model == model {
			return e.CLI
		}
	}
	if cli, ok := soleCLI(cfg.AllowedExecutors); ok {
		return cli
	}
	if d := strings.TrimSpace(cfg.DefaultCLI); d != "" {
		return d
	}
	return fallbackCLI
}

// soleCLI returns the single CLI shared by every candidate (ok=false for an empty
// set or a mix of CLIs).
func soleCLI(execs []ExecutorCandidate) (string, bool) {
	if len(execs) == 0 {
		return "", false
	}
	cli := execs[0].CLI
	for _, e := range execs[1:] {
		if e.CLI != cli {
			return "", false
		}
	}
	return cli, true
}
