package modelrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// fakeJudge records its calls and returns a canned model / error. It is the LLM
// difficulty-judge port stand-in (conventions: zero LLM SDK in repo — the judge
// is a port the orchestrator wires to its reasoning model).
type fakeJudge struct {
	calls  int
	gotReq JudgeRequest
	model  string
	err    error
}

func (f *fakeJudge) Judge(_ context.Context, req JudgeRequest) (Judgment, error) {
	f.calls++
	f.gotReq = req
	if f.err != nil {
		return Judgment{}, f.err
	}
	return Judgment{Model: f.model}, nil
}

var testGoal = executor.Goal{Title: "do a thing", Description: "with detail"}

func baseCfg() Config {
	return Config{
		OrchestratorModel:    "haiku-cheap",
		AllowedModels:        []string{"haiku-cheap", "sonnet-mid", "opus-hard"},
		DefaultExecutorModel: "sonnet-mid",
	}
}

// Path 1 (highest priority): task.model set → used verbatim, judge NOT consulted.
func TestResolve_TaskModelHardOverride(t *testing.T) {
	j := &fakeJudge{model: "opus-hard"} // would pick opus if asked
	r := NewRouter(j)

	dec, err := r.ResolveExecutorModel(context.Background(), "  custom-pinned  ", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "custom-pinned" {
		t.Errorf("model = %q, want trimmed %q", dec.Model, "custom-pinned")
	}
	if dec.Source != SourceTaskOverride {
		t.Errorf("source = %q, want %q", dec.Source, SourceTaskOverride)
	}
	if j.calls != 0 {
		t.Errorf("judge consulted %d times; task.model must short-circuit before the LLM", j.calls)
	}
}

// Path 2: no task.model → judge picks a model from allowed_models → used.
func TestResolve_LLMJudged(t *testing.T) {
	j := &fakeJudge{model: "opus-hard"}
	r := NewRouter(j)

	dec, err := r.ResolveExecutorModel(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "opus-hard" || dec.Source != SourceJudged {
		t.Errorf("got (%q,%q), want (opus-hard, %q)", dec.Model, dec.Source, SourceJudged)
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1", j.calls)
	}
	if j.gotReq.Goal != testGoal {
		t.Errorf("judge got goal %+v, want %+v", j.gotReq.Goal, testGoal)
	}
	if len(j.gotReq.AllowedModels) != 3 {
		t.Errorf("judge got %d allowed models, want 3", len(j.gotReq.AllowedModels))
	}
}

// Path 3a: judge returns ErrInconclusive ("can't judge") → default fallback.
func TestResolve_JudgeInconclusive_FallsBackToDefault(t *testing.T) {
	j := &fakeJudge{err: ErrInconclusive}
	r := NewRouter(j)

	dec, err := r.ResolveExecutorModel(context.Background(), "", testGoal, baseCfg())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "sonnet-mid" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q), want (sonnet-mid, %q)", dec.Model, dec.Source, SourceDefault)
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

	dec, err := r.ResolveExecutorModel(context.Background(), "", testGoal, baseCfg())
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

// Guard: judge picks a model NOT in allowed_models (hallucination) → reject it and
// fall back to default rather than spawn an unsanctioned model.
func TestResolve_JudgeOutOfRange_FallsBackToDefault(t *testing.T) {
	j := &fakeJudge{model: "gpt-rogue"}
	r := NewRouter(j)

	dec, err := r.ResolveExecutorModel(context.Background(), "", testGoal, baseCfg())
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

// No allowed_models configured → the judge is skipped entirely → default fallback.
func TestResolve_NoAllowedModels_SkipsJudge(t *testing.T) {
	j := &fakeJudge{model: "opus-hard"}
	r := NewRouter(j)
	cfg := baseCfg()
	cfg.AllowedModels = nil

	dec, err := r.ResolveExecutorModel(context.Background(), "", testGoal, cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "sonnet-mid" || dec.Source != SourceDefault {
		t.Errorf("got (%q,%q), want default fallback", dec.Model, dec.Source)
	}
	if j.calls != 0 {
		t.Errorf("judge consulted %d times with no allowed_models; should skip", j.calls)
	}
}

// No judge wired (nil) → judge step skipped → default fallback.
func TestResolve_NilJudge_FallsBackToDefault(t *testing.T) {
	r := NewRouter(nil)

	dec, err := r.ResolveExecutorModel(context.Background(), "", testGoal, baseCfg())
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

	_, err := r.ResolveExecutorModel(context.Background(), "", testGoal, cfg)
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
// allowed list, no default).
func TestResolve_TaskModelWorksWithEmptyConfig(t *testing.T) {
	r := NewRouter(nil)
	dec, err := r.ResolveExecutorModel(context.Background(), "pin", testGoal, Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Model != "pin" || dec.Source != SourceTaskOverride {
		t.Errorf("got (%q,%q), want (pin, task_override)", dec.Model, dec.Source)
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
