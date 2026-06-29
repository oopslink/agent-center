package modelrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// fakeJudge records its calls and returns a canned {cli,model} / error. It is the
// LLM difficulty-judge port stand-in (conventions: zero LLM SDK in repo — the judge
// is a port the orchestrator wires to its reasoning model).
type fakeJudge struct {
	calls  int
	gotReq JudgeRequest
	cli    string
	model  string
	err    error
}

func (f *fakeJudge) Judge(_ context.Context, req JudgeRequest) (Judgment, error) {
	f.calls++
	f.gotReq = req
	if f.err != nil {
		return Judgment{}, f.err
	}
	return Judgment{CLI: f.cli, Model: f.model}, nil
}

var testGoal = executor.Goal{Title: "do a thing", Description: "with detail"}

func baseCfg() Config {
	return Config{
		OrchestratorModel: "haiku-cheap",
		AllowedExecutors: []ExecutorCandidate{
			{CLI: "claude-code", Model: "haiku-cheap"},
			{CLI: "claude-code", Model: "sonnet-mid"},
			{CLI: "claude-code", Model: "opus-hard"},
		},
		DefaultExecutorModel: "sonnet-mid",
	}
}

// Path 1 (highest priority): task.model set → used verbatim, judge NOT consulted.
// The CLI is paired from the matching allowed_executors entry.
func TestResolve_TaskModelHardOverride(t *testing.T) {
	j := &fakeJudge{cli: "claude-code", model: "opus-hard"} // would pick opus if asked
	r := NewRouter(j)

	dec, err := r.ResolveExecutor(context.Background(), "  opus-hard  ", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "opus-hard" {
		t.Errorf("model = %q, want trimmed %q", dec.Model, "opus-hard")
	}
	if dec.CLI != "claude-code" {
		t.Errorf("cli = %q, want claude-code (paired from candidates)", dec.CLI)
	}
	if dec.Source != SourceTaskOverride {
		t.Errorf("source = %q, want %q", dec.Source, SourceTaskOverride)
	}
	if j.calls != 0 {
		t.Errorf("judge consulted %d times; task.model must short-circuit before the LLM", j.calls)
	}
}

// task.model override of a model NOT in a single-CLI candidate set still pairs with
// that sole CLI (so an only-codex agent's pinned model runs under codex).
func TestResolve_TaskModelOverride_SoleCLI(t *testing.T) {
	r := NewRouter(nil)
	cfg := Config{AllowedExecutors: []ExecutorCandidate{{CLI: "codex", Model: "gpt-5.5"}}}
	dec, err := r.ResolveExecutor(context.Background(), "gpt-custom", testGoal, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.CLI != "codex" || dec.Model != "gpt-custom" || dec.Source != SourceTaskOverride {
		t.Errorf("got (%q,%q,%q), want (codex, gpt-custom, task_override)", dec.CLI, dec.Model, dec.Source)
	}
}

// Path 2: no task.model → judge picks a {cli,model} from allowed_executors → used.
func TestResolve_LLMJudged(t *testing.T) {
	j := &fakeJudge{cli: "claude-code", model: "opus-hard"}
	r := NewRouter(j)

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "opus-hard" || dec.CLI != "claude-code" || dec.Source != SourceJudged {
		t.Errorf("got (%q,%q,%q), want (claude-code, opus-hard, %q)", dec.CLI, dec.Model, dec.Source, SourceJudged)
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1", j.calls)
	}
	if j.gotReq.Goal != testGoal {
		t.Errorf("judge got goal %+v, want %+v", j.gotReq.Goal, testGoal)
	}
	if len(j.gotReq.AllowedExecutors) != 3 {
		t.Errorf("judge got %d allowed executors, want 3", len(j.gotReq.AllowedExecutors))
	}
}

// Path 2 cross-CLI: the judge may pick a codex executor when the agent allows it.
func TestResolve_LLMJudged_PicksCodex(t *testing.T) {
	cfg := Config{AllowedExecutors: []ExecutorCandidate{
		{CLI: "claude-code", Model: "opus-hard"},
		{CLI: "codex", Model: "gpt-5.5"},
	}}
	j := &fakeJudge{cli: "codex", model: "gpt-5.5"}
	r := NewRouter(j)

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.CLI != "codex" || dec.Model != "gpt-5.5" || dec.Source != SourceJudged {
		t.Errorf("got (%q,%q,%q), want (codex, gpt-5.5, llm_judged)", dec.CLI, dec.Model, dec.Source)
	}
}

// Symmetric BE-2 guard: an only-claude agent must NEVER produce a codex executor.
// Even if the judge hallucinates a codex pick, it is out-of-range → claude default.
func TestResolve_OnlyClaude_NeverCodex(t *testing.T) {
	j := &fakeJudge{cli: "codex", model: "gpt-5.5"} // hallucinated cross-CLI pick
	r := NewRouter(j)

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.CLI == "codex" {
		t.Fatalf("only-claude agent produced a codex executor: %+v", dec)
	}
	if dec.CLI != "claude-code" || dec.Model != "sonnet-mid" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q,%q), want (claude-code, sonnet-mid, default)", dec.CLI, dec.Model, dec.Source)
	}
	if !errors.Is(dec.JudgeError, ErrJudgeOutOfRange) {
		t.Errorf("JudgeError = %v, want ErrJudgeOutOfRange", dec.JudgeError)
	}
}

// Reverse guard: an only-codex agent must NEVER produce a claude executor — the
// default model pairs with codex (the sole CLI), not the fallback claude-code.
func TestResolve_OnlyCodex_NeverClaude(t *testing.T) {
	cfg := Config{
		AllowedExecutors:     []ExecutorCandidate{{CLI: "codex", Model: "gpt-5.5"}},
		DefaultExecutorModel: "gpt-5.4", // not in the list → sole-CLI pairing applies
	}
	r := NewRouter(nil)
	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.CLI != "codex" {
		t.Errorf("only-codex agent produced cli %q, want codex", dec.CLI)
	}
}

// Path 3a: judge returns ErrInconclusive ("can't judge") → default fallback.
func TestResolve_JudgeInconclusive_FallsBackToDefault(t *testing.T) {
	j := &fakeJudge{err: ErrInconclusive}
	r := NewRouter(j)

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "sonnet-mid" || dec.CLI != "claude-code" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q,%q), want (claude-code, sonnet-mid, %q)", dec.CLI, dec.Model, dec.Source, SourceDefault)
	}
	if !errors.Is(dec.JudgeError, ErrInconclusive) {
		t.Errorf("JudgeError = %v, want it to wrap ErrInconclusive (surfaced, not swallowed)", dec.JudgeError)
	}
}

// Path 3b: judge returns an arbitrary error → still fall back to default, but the
// error is surfaced on the Decision (conventions §17: don't silently swallow).
func TestResolve_JudgeError_FallsBackToDefault(t *testing.T) {
	boom := errors.New("llm timeout")
	j := &fakeJudge{err: boom}
	r := NewRouter(j)

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "sonnet-mid" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q), want default fallback", dec.Model, dec.Source)
	}
	if !errors.Is(dec.JudgeError, boom) {
		t.Errorf("JudgeError = %v, want it to wrap the judge error", dec.JudgeError)
	}
}

// Guard: judge picks an executor NOT in allowed_executors (hallucination) → reject
// it and fall back to default rather than spawn an unsanctioned executor.
func TestResolve_JudgeOutOfRange_FallsBackToDefault(t *testing.T) {
	j := &fakeJudge{cli: "claude-code", model: "gpt-rogue"}
	r := NewRouter(j)

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "sonnet-mid" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q), want default fallback", dec.Model, dec.Source)
	}
	if !errors.Is(dec.JudgeError, ErrJudgeOutOfRange) {
		t.Errorf("JudgeError = %v, want ErrJudgeOutOfRange", dec.JudgeError)
	}
}

// No allowed_executors configured → the judge is skipped entirely → default fallback.
func TestResolve_NoAllowedExecutors_SkipsJudge(t *testing.T) {
	j := &fakeJudge{cli: "claude-code", model: "opus-hard"}
	r := NewRouter(j)
	cfg := baseCfg()
	cfg.AllowedExecutors = nil

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "sonnet-mid" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q), want default fallback", dec.Model, dec.Source)
	}
	// No candidates to pair the default model with → fallbackCLI.
	if dec.CLI != "claude-code" {
		t.Errorf("cli = %q, want fallback claude-code", dec.CLI)
	}
	if j.calls != 0 {
		t.Errorf("judge consulted %d times with no allowed_executors; should skip", j.calls)
	}
}

// DefaultCLI is honored when the default model isn't among (mixed-CLI) candidates.
func TestResolve_DefaultCLI_WhenModelUnmatchedAndMixed(t *testing.T) {
	r := NewRouter(nil)
	cfg := Config{
		AllowedExecutors: []ExecutorCandidate{
			{CLI: "claude-code", Model: "opus-hard"},
			{CLI: "codex", Model: "gpt-5.5"},
		},
		DefaultExecutorModel: "mystery-model", // not in list; candidates are mixed-CLI
		DefaultCLI:           "codex",
	}
	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.CLI != "codex" || dec.Model != "mystery-model" {
		t.Errorf("got (%q,%q), want (codex, mystery-model) via DefaultCLI", dec.CLI, dec.Model)
	}
}

// No judge wired (nil) → judge step skipped → default fallback.
func TestResolve_NilJudge_FallsBackToDefault(t *testing.T) {
	r := NewRouter(nil)

	dec, err := r.ResolveExecutor(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "sonnet-mid" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q), want default fallback", dec.Model, dec.Source)
	}
}

// Nothing resolvable: no task.model, judge inconclusive, no default → error
// (protocol requires a non-empty executor model before spawn).
func TestResolve_Unresolvable_Errors(t *testing.T) {
	j := &fakeJudge{err: ErrInconclusive}
	r := NewRouter(j)
	cfg := baseCfg()
	cfg.DefaultExecutorModel = ""

	_, err := r.ResolveExecutor(context.Background(), "", testGoal, cfg)
	if err == nil {
		t.Fatal("expected error when no model is resolvable")
	}
	if !errors.Is(err, ErrNoExecutorModel) {
		t.Errorf("err = %v, want ErrNoExecutorModel", err)
	}
	// The underlying judge reason must remain inspectable on the returned error.
	if !errors.Is(err, ErrInconclusive) {
		t.Errorf("err = %v, want it to also wrap the judge reason", err)
	}
}

// task.model override works even when nothing else is configured (no judge, no
// allowed list, no default) → CLI is the fallback.
func TestResolve_TaskModelWorksWithEmptyConfig(t *testing.T) {
	r := NewRouter(nil)
	dec, err := r.ResolveExecutor(context.Background(), "pin", testGoal, Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "pin" || dec.Source != SourceTaskOverride || dec.CLI != "claude-code" {
		t.Errorf("got (%q,%q,%q), want (claude-code, pin, task_override)", dec.CLI, dec.Model, dec.Source)
	}
}

func TestOrchestratorModel(t *testing.T) {
	r := NewRouter(nil)

	got, err := r.OrchestratorModel(baseCfg())
	if err != nil {
		t.Fatalf("OrchestratorModel: %v", err)
	}
	if got != "haiku-cheap" {
		t.Errorf("orchestrator model = %q, want haiku-cheap", got)
	}

	if _, err := r.OrchestratorModel(Config{OrchestratorModel: "   "}); !errors.Is(err, ErrNoOrchestratorModel) {
		t.Errorf("empty orchestrator_model err = %v, want ErrNoOrchestratorModel", err)
	}
}
