package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
)

// TestClassifyForkFailure locks the machine-readable fork-fail cause classification
// (issue-0186f85e): sentinel-based causes via errors.Is (wrapped, as they arrive from
// the fork chain), the rate-limit heuristic, and the generic/nil fallback.
func TestClassifyForkFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ForkFailureCause
	}{
		{"no model resolvable (wrapped)",
			fmt.Errorf("agent_controller: fork executor: orchestrator: resolve executor: %w", modelrouter.ErrNoExecutorModel),
			CauseNoModelResolvable},
		{"model not allowed (wrapped)",
			fmt.Errorf("fork executor: %w", modelrouter.ErrModelNotAllowed),
			CauseModelNotAllowed},
		{"rate limited 429", errors.New("anthropic API: 429 Too Many Requests"), CauseRateLimited},
		{"rate limited phrase", errors.New("provider returned rate limit exceeded"), CauseRateLimited},
		{"overloaded", errors.New("upstream overloaded_error: server busy"), CauseRateLimited},
		{"quota", errors.New("insufficient_quota for the model"), CauseRateLimited},
		{"generic fork error", errors.New("some spawn failure"), CauseForkError},
		{"nil", nil, CauseForkError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyForkFailure(tc.err); got != tc.want {
				t.Fatalf("classifyForkFailure(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestForkFailureReason_Structured verifies the blocked_reason carries the stable,
// greppable "[cause=<code>]" token plus the underlying detail (machine-readable +
// human-diagnosable).
func TestForkFailureReason_Structured(t *testing.T) {
	err := fmt.Errorf("resolve executor: %w", modelrouter.ErrNoExecutorModel)
	got := forkFailureReason(CauseNoModelResolvable, err)
	if !strings.Contains(got, "[cause=no_model_resolvable]") {
		t.Fatalf("reason %q missing machine-readable [cause=…] token", got)
	}
	if !strings.Contains(got, "no executor model resolvable") {
		t.Fatalf("reason %q dropped the underlying detail", got)
	}
}

// TestSpawnExecutor_NoModelResolvable_BlocksWithCause is the END-TO-END fail-loud lock
// for the no-model fallback path (issue-0186f85e): an engine with no resolvable executor
// model (empty allowed_executors, no default/supervisor) admits the task (start_task) then
// the fork returns modelrouter.ErrNoExecutorModel. Instead of leaving the task fake-running
// for the lease to reclaim, SpawnExecutor blocks it (block_task) with a machine-readable
// [cause=no_model_resolvable] reason.
func TestSpawnExecutor_NoModelResolvable_BlocksWithCause(t *testing.T) {
	trueBin := lookTrue(t)
	rt := newExecRuntime(t, t.TempDir(), "agent-nomodel", trueBin)
	home, _, _, err := rt.agentPaths("agent-nomodel")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	// NO DefaultExecutorModel / AllowedExecutors / Supervisor / Orchestrator → nothing resolvable.
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{AgentID: "agent-nomodel", MaxConcurrentTasks: 1})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-nm", "title": "t", "status": "open", // no "model" → falls through to fallback chain
	}}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-nm"})
	if res != nil || err != nil {
		t.Fatalf("SpawnExecutor (no model) = (%v, %v), want (nil, nil)", res, err)
	}
	seen := sc.toolsSeen()
	if len(seen) != 3 || seen[0] != "get_task" || seen[1] != "start_task" || seen[2] != "block_task" {
		t.Fatalf("tool calls = %v, want [get_task start_task block_task] (fork-fail must fail-loud → block, not silent)", seen)
	}
	body, ok := sc.callFor("block_task")
	if !ok {
		t.Fatal("block_task not called — fork-fail was silent (the P0 hole)")
	}
	if body["reason_type"] != "obstacle" {
		t.Errorf("reason_type = %v, want obstacle (retryable)", body["reason_type"])
	}
	reason, _ := body["reason"].(string)
	if !strings.Contains(reason, "[cause=no_model_resolvable]") {
		t.Fatalf("block_task reason = %q, want machine-readable [cause=no_model_resolvable]", reason)
	}
}

// TestBlockTaskOnForkFailure_RateLimited covers the rate-limit fallback path
// (issue-0186f85e): a rate-limited fork error blocks the task with a
// [cause=rate_limited] machine-readable reason (retryable). Driven through the block
// helper because a server-side rate-limit surfaces as an opaque wrapped error at fork,
// not a routable sentinel — the classification is the contract under test.
func TestBlockTaskOnForkFailure_RateLimited(t *testing.T) {
	rt := newExecRuntime(t, t.TempDir(), "agent-rl", lookTrue(t))
	sc := &scriptedToolCaller{}
	setToolCaller(rt, sc)

	rt.blockTaskOnForkFailure(context.Background(),
		"agent-rl", "task-rl", errors.New("executor fork: anthropic API error: 429 Too Many Requests (rate limit)"))

	body, ok := sc.callFor("block_task")
	if !ok {
		t.Fatal("block_task not called for a rate-limited fork failure")
	}
	if body["reason_type"] != "obstacle" {
		t.Errorf("reason_type = %v, want obstacle (retryable)", body["reason_type"])
	}
	reason, _ := body["reason"].(string)
	if !strings.Contains(reason, "[cause=rate_limited]") {
		t.Fatalf("block_task reason = %q, want machine-readable [cause=rate_limited]", reason)
	}
}
