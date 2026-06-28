// Package modelrouter implements F3 of the agent-concurrent-execution design
// (docs/design/features/agent-concurrent-execution.md §5 / §10): the orchestrator
// (监工) decides which model an executor runs under, by a fixed priority chain.
//
// Priority chain (design §5, authoritative):
//
//	task.model set   → use it verbatim (HARD override, highest priority; the LLM is
//	                   never consulted)
//	not set          → the orchestrator's LLM reads the goal, judges difficulty, and
//	                   picks one model from profile.allowed_models
//	can't judge      → profile.default_executor_model (fallback)
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
)

// Sentinel errors (errors.Is-matchable) for the unhappy paths.
var (
	// ErrInconclusive is what a DifficultyJudge returns when it cannot decide a
	// model for the goal — the router treats it as "can't judge" and falls back to
	// the default (design §5). Exported so a judge implementation can return it.
	ErrInconclusive = errors.New("modelrouter: difficulty judge inconclusive")
	// ErrJudgeOutOfRange is recorded when the judge picks a model that is not in
	// allowed_models (an LLM hallucination); the router refuses it and falls back
	// rather than spawn an unsanctioned model.
	ErrJudgeOutOfRange = errors.New("modelrouter: judged model not in allowed_models")
	// ErrNoExecutorModel is returned when the chain resolves to nothing: no
	// task.model, no usable judged model, and no default. The file-exchange
	// protocol requires a non-empty executor model before spawn, so this is fatal
	// for that task rather than a silent default.
	ErrNoExecutorModel = errors.New("modelrouter: no executor model resolvable")
	// ErrNoOrchestratorModel is returned by OrchestratorModel when the profile has
	// no orchestrator_model configured.
	ErrNoOrchestratorModel = errors.New("modelrouter: orchestrator_model not configured")
)

// Config is the per-agent model configuration the router reads (the routing fields
// of the agent profile, design §10). It is passed as primitives so this package
// stays decoupled from the agent bounded context (no import of the agent
// aggregate, no cycle).
type Config struct {
	OrchestratorModel    string   // profile.orchestrator_model — the orchestrator's own (cheap/fast) model
	AllowedModels        []string // profile.allowed_models — candidates the judge picks from
	DefaultExecutorModel string   // profile.default_executor_model — fallback when unjudgeable
}

// JudgeRequest is what the orchestrator hands the LLM port: the goal to size up
// and the menu of models it may choose from.
type JudgeRequest struct {
	Goal          executor.Goal
	AllowedModels []string
}

// Judgment is the judge's verdict: the chosen model (which MUST be one of the
// request's AllowedModels — the router rejects anything else).
type Judgment struct {
	Model string
}

// DifficultyJudge is the LLM port. The orchestrator implements it against its
// reasoning model (profile.orchestrator_model). Return ErrInconclusive (or any
// error) to signal "can't judge" → the router falls back to the default.
type DifficultyJudge interface {
	Judge(ctx context.Context, req JudgeRequest) (Judgment, error)
}

// Decision is the resolved executor model plus provenance. JudgeError is non-nil
// when the judge was attempted but did not yield a usable model and the router
// fell back to the default — the reason is carried here (not swallowed) for
// observability, while Model/Source still drive the spawn.
type Decision struct {
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

// ResolveExecutorModel applies the design §5 priority chain to choose the model
// the executor for this task will run under. taskModel is task.model ("" = unset).
//
//  1. taskModel set            → SourceTaskOverride (judge never consulted)
//  2. judge picks an allowed   → SourceJudged
//  3. otherwise default set    → SourceDefault (JudgeError carries why, if a judge ran)
//  4. nothing resolvable       → ErrNoExecutorModel (wrapping the judge reason)
func (r *Router) ResolveExecutorModel(ctx context.Context, taskModel string, goal executor.Goal, cfg Config) (Decision, error) {
	// 1. Hard override — highest priority; short-circuit before any LLM call.
	if m := strings.TrimSpace(taskModel); m != "" {
		return Decision{Model: m, Source: SourceTaskOverride}, nil
	}

	// 2. LLM difficulty judge — only when a judge is wired AND there are candidate
	// models to choose from. A failure / inconclusive / out-of-range pick does not
	// abort: it is remembered and we fall through to the default.
	var judgeErr error
	if r.judge != nil && len(cfg.AllowedModels) > 0 {
		j, err := r.judge.Judge(ctx, JudgeRequest{Goal: goal, AllowedModels: cfg.AllowedModels})
		switch {
		case err != nil:
			judgeErr = err
		case !containsModel(cfg.AllowedModels, j.Model):
			judgeErr = fmt.Errorf("%w: judge picked %q", ErrJudgeOutOfRange, j.Model)
		default:
			return Decision{Model: j.Model, Source: SourceJudged}, nil
		}
	}

	// 3. Default fallback.
	if d := strings.TrimSpace(cfg.DefaultExecutorModel); d != "" {
		return Decision{Model: d, Source: SourceDefault, JudgeError: judgeErr}, nil
	}

	// 4. Nothing resolvable — surface the judge reason (if any) alongside the
	// fatal sentinel (errors.Join keeps BOTH errors.Is-matchable) so callers can
	// see WHY the fallback was empty too.
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

func containsModel(models []string, m string) bool {
	for _, x := range models {
		if x == m {
			return true
		}
	}
	return false
}
