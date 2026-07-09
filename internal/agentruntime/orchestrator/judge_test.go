package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
)

func judgeReq() modelrouter.JudgeRequest {
	return modelrouter.JudgeRequest{
		Goal: executor.Goal{Title: "fix a typo in the README", Description: "one-line change"},
		AllowedExecutors: []modelrouter.ExecutorCandidate{
			{CLI: "claude-code", Model: "haiku", InputCost: 1, OutputCost: 5, ContextWindow: 200000, Tier: "cheap, mechanical edits"},
			{CLI: "claude-code", Model: "opus", InputCost: 15, OutputCost: 75, ContextWindow: 200000, Tier: "hardest reasoning"},
		},
	}
}

// A valid JSON verdict → the judge returns the chosen {cli, model}. Tolerates the
// stream-json wrapper + surrounding prose (the parser scans for the JSON object).
func TestSubprocessJudge_HappyPath(t *testing.T) {
	var gotArgv []string
	j := NewSubprocessJudge(JudgeConfig{
		OrchestratorModel: "haiku", Binary: "claude",
		Run: func(_ context.Context, argv []string) (string, error) {
			gotArgv = argv
			// Simulate claude stream-json: a result line carrying the model's text.
			return `{"type":"result","result":"Here you go: {\"difficulty\":\"low\",\"cli\":\"claude-code\",\"model\":\"haiku\",\"rationale\":\"trivial edit\"}"}`, nil
		},
	})
	if j == nil {
		t.Fatal("judge nil")
	}
	v, err := j.Judge(context.Background(), judgeReq())
	if err != nil {
		t.Fatalf("Judge err: %v", err)
	}
	if v.CLI != "claude-code" || v.Model != "haiku" {
		t.Fatalf("verdict = %+v, want claude-code/haiku", v)
	}
	// Guardrail 2: reuses the claude CLI primitive (model + stream-json + auth flags),
	// on the cheap orchestrator model, NO --session-id (stateless one-shot).
	joined := strings.Join(gotArgv, " ")
	if !strings.Contains(joined, "--model haiku") || !strings.Contains(joined, "stream-json") {
		t.Errorf("argv missing model/stream-json: %v", gotArgv)
	}
	if strings.Contains(joined, "--session-id") {
		t.Errorf("judge argv must be a stateless one-shot (no --session-id): %v", gotArgv)
	}
	// The prompt annotates the pool with tier/cost so the judge can pick cheapest-sufficient.
	if !strings.Contains(joined, "cheap, mechanical edits") || !strings.Contains(joined, "hardest reasoning") {
		t.Errorf("prompt missing catalog tier annotations: %v", gotArgv)
	}
}

// Guardrail 1: a subprocess error (timeout / nonzero exit) → ErrInconclusive so the
// router falls back to pool[0]; the judge never hard-fails.
func TestSubprocessJudge_SubprocessError_FallsBack(t *testing.T) {
	j := NewSubprocessJudge(JudgeConfig{
		OrchestratorModel: "haiku", Binary: "claude",
		Run: func(_ context.Context, _ []string) (string, error) { return "", errors.New("signal: killed") },
	})
	_, err := j.Judge(context.Background(), judgeReq())
	if !errors.Is(err, modelrouter.ErrInconclusive) {
		t.Fatalf("err = %v, want ErrInconclusive (fallback)", err)
	}
}

// Guardrail 3: unparseable output → ErrInconclusive fallback (never a hard fail or a
// bogus pick).
func TestSubprocessJudge_Unparseable_FallsBack(t *testing.T) {
	for _, out := range []string{"", "I cannot decide", `{"difficulty":"low"}` /* no cli/model */} {
		j := NewSubprocessJudge(JudgeConfig{
			OrchestratorModel: "haiku", Binary: "claude",
			Run: func(_ context.Context, _ []string) (string, error) { return out, nil },
		})
		if _, err := j.Judge(context.Background(), judgeReq()); !errors.Is(err, modelrouter.ErrInconclusive) {
			t.Fatalf("out=%q → err=%v, want ErrInconclusive", out, err)
		}
	}
}

// The judge is nil ONLY when there is no orchestrator model (nothing to run it on) →
// the caller wires NewRouter(nil) = pure pool[0] fallback. A missing BINARY is NOT
// unconfigured — it defaults to PATH "claude" (see
// TestNewSubprocessJudge_EmptyBinary_DefaultsToClaude below).
func TestNewSubprocessJudge_NilWhenUnconfigured(t *testing.T) {
	if NewSubprocessJudge(JudgeConfig{Binary: "claude"}) != nil {
		t.Error("no orchestrator model → want nil judge")
	}
	if NewSubprocessJudge(JudgeConfig{}) != nil {
		t.Error("empty config (no orchestrator model) → want nil judge")
	}
}

// TestNewSubprocessJudge_EmptyBinary_DefaultsToClaude is the REGRESSION LOCK for the
// tester3 P1 deployment bug: the worker's ClaudeBinary comes from the normally-UNSET
// AGENT_CENTER_CLAUDE_BINARY env, so an EMPTY binary is the COMMON production case —
// not a misconfiguration. The judge MUST still build (non-nil) and invoke PATH
// "claude". The old code returned nil here, making judge_enabled=true 100% inert in
// production with no signal. Every OTHER judge test injects Binary, so this real
// deployment-default path was the exact blind spot that let the bug ship past unit
// tests + Gate + three code-review rounds.
func TestNewSubprocessJudge_EmptyBinary_DefaultsToClaude(t *testing.T) {
	var gotArgv []string
	j := NewSubprocessJudge(JudgeConfig{
		OrchestratorModel: "haiku", // Binary intentionally left "" — the deployment default
		Run: func(_ context.Context, argv []string) (string, error) {
			gotArgv = argv
			return `{"type":"result","result":"{\"difficulty\":\"low\",\"cli\":\"claude-code\",\"model\":\"haiku\",\"rationale\":\"x\"}"}`, nil
		},
	})
	if j == nil {
		t.Fatal("empty binary must NOT disable the judge — want non-nil (PATH claude default)")
	}
	if _, err := j.Judge(context.Background(), judgeReq()); err != nil {
		t.Fatalf("Judge err: %v", err)
	}
	if len(gotArgv) == 0 || gotArgv[0] != "claude" {
		t.Errorf("empty binary must invoke PATH \"claude\", got argv[0]=%v", gotArgv)
	}
}
