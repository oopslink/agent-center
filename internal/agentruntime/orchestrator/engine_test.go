package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
	"github.com/oopslink/agent-center/internal/clock"
)

// fakeIDMinter hands out deterministic, path-safe ids.
type fakeIDMinter struct {
	mu   sync.Mutex
	e, p int
}

func (m *fakeIDMinter) NewExecutorID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.e++
	return fmt.Sprintf("executor-%03d", m.e)
}
func (m *fakeIDMinter) NewProblemID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.p++
	return fmt.Sprintf("problem-%03d", m.p)
}

// fakeRunner returns a harmless argv (the engine test forks `true`, so the runner
// content is irrelevant to process behaviour — we assert it reached input via the
// model instead). cli labels which builder ran (so cli-dispatch tests can tell which
// runner the engine selected).
type fakeRunner struct {
	cli                             string
	lastModel, lastPrompt, lastSess string
}

func (r *fakeRunner) Build(model, prompt, sessionID string) ([]string, error) {
	r.lastModel, r.lastPrompt, r.lastSess = model, prompt, sessionID
	return []string{"true"}, nil
}

func newTestEngine(t *testing.T, max int, rcfg modelrouter.Config, runner RunnerCmdBuilder) (*Engine, *executor.FileExchange, string) {
	t.Helper()
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("`true` not available: %v", err)
	}
	root := t.TempDir()
	layout, err := executor.NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	clk := clock.NewFakeClock(time.Unix(1700000000, 0))
	fx, err := executor.NewFileExchange(layout, clk)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	// Plain-dir pool (no worktrees) with a REAL spawner pointed at `true` — exercises
	// the genuine fork path harmlessly.
	pool, err := executor.NewPool(executor.PoolConfig{
		Exchange: fx, Spawner: executor.NewSpawner(), AgentRoot: root, BinaryPath: trueBin, Max: max,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	routing, err := executor.NewRoutingStore(root, clk)
	if err != nil {
		t.Fatalf("NewRoutingStore: %v", err)
	}
	if runner == nil {
		runner = &fakeRunner{}
	}
	// Default tests route to claude-code (resolveCLI's fallback), so register the
	// injected runner under that key.
	eng, err := NewEngine(EngineConfig{
		Pool: pool, Routing: routing, Router: modelrouter.NewRouter(nil),
		RouterConfig: rcfg,
		Runners:      map[string]RunnerCmdBuilder{"claude-code": runner},
		IDs:          &fakeIDMinter{}, Clock: clk,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng, fx, root
}

func reap(t *testing.T, h *executor.Handle) {
	t.Helper()
	if h != nil {
		_ = h.Wait() // reap the harmless `true` process
	}
}

func TestEngine_HandleWork_ChainsAndForks(t *testing.T) {
	fr := &fakeRunner{}
	eng, fx, _ := newTestEngine(t, 3, modelrouter.Config{DefaultExecutorModel: "claude-default"}, fr)

	got, err := eng.HandleWork(context.Background(), WorkItem{
		TaskID:  "task-1",
		TaskRef: "task-1",
		ChatID:  "channel-A",
		Goal:    executor.Goal{Title: "fix bug", Description: "the widget breaks"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer reap(t, got.Handle)

	// F3: no task.model, nil judge → default fallback.
	if got.Model != "claude-default" || got.ModelSource != modelrouter.SourceDefault {
		t.Errorf("model = %q/%v, want claude-default/default_fallback", got.Model, got.ModelSource)
	}
	if fr.lastModel != "claude-default" || !strings.Contains(fr.lastPrompt, "fix bug") {
		t.Errorf("runner saw model=%q prompt=%q", fr.lastModel, fr.lastPrompt)
	}
	// F2: input.json written with resolved model + goal + problem.
	in, err := fx.ReadInput(got.ExecutorID)
	if err != nil {
		t.Fatalf("ReadInput: %v", err)
	}
	if in.Model != "claude-default" || in.Goal.Title != "fix bug" || in.ProblemID != got.ProblemID {
		t.Errorf("input = %+v", in)
	}
	// F4: a new problem was registered with the chat + task ref + the executor merged.
	tbl, err := executor.NewRoutingStore(fxRoot(t, fx), clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("routing store: %v", err)
	}
	prob, ok := mustLoadProblem(t, tbl, got.ProblemID)
	if !ok {
		t.Fatal("problem not registered")
	}
	if !contains(prob.ChatIDs, "channel-A") || !contains(prob.TaskRefs, "task-1") || !contains(prob.ExecutorIDs, got.ExecutorID) {
		t.Errorf("problem routing incomplete: %+v", prob)
	}
}

func TestEngine_HandleWork_TaskModelHardOverride(t *testing.T) {
	eng, _, _ := newTestEngine(t, 2, modelrouter.Config{DefaultExecutorModel: "claude-default"}, nil)
	got, err := eng.HandleWork(context.Background(), WorkItem{
		Goal: executor.Goal{Title: "t"}, TaskModel: "claude-opus-4-8", ChatID: "c1",
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer reap(t, got.Handle)
	if got.Model != "claude-opus-4-8" || got.ModelSource != modelrouter.SourceTaskOverride {
		t.Errorf("expected task.model hard override, got %q/%v", got.Model, got.ModelSource)
	}
}

// BE-2: the engine forks the runner that matches the F3-decided CLI. With a codex
// candidate whose model is the default, the codex builder (not claude) is selected.
func TestEngine_HandleWork_DispatchesByCLI(t *testing.T) {
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("`true` not available: %v", err)
	}
	root := t.TempDir()
	layout, _ := executor.NewLayout(root)
	clk := clock.NewFakeClock(time.Unix(1700000000, 0))
	fx, _ := executor.NewFileExchange(layout, clk)
	pool, _ := executor.NewPool(executor.PoolConfig{Exchange: fx, Spawner: executor.NewSpawner(), AgentRoot: root, BinaryPath: trueBin, Max: 3})
	routing, _ := executor.NewRoutingStore(root, clk)

	claudeR := &fakeRunner{cli: "claude-code"}
	codexR := &fakeRunner{cli: "codex"}
	eng, err := NewEngine(EngineConfig{
		Pool: pool, Routing: routing, Router: modelrouter.NewRouter(nil),
		RouterConfig: modelrouter.Config{
			AllowedExecutors:     []modelrouter.ExecutorCandidate{{CLI: "codex", Model: "gpt-5.5"}},
			DefaultExecutorModel: "gpt-5.5",
		},
		Runners: map[string]RunnerCmdBuilder{"claude-code": claudeR, "codex": codexR},
		IDs:     &fakeIDMinter{}, Clock: clk,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	got, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "t"}, ChatID: "c1"})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer reap(t, got.Handle)
	if got.CLI != "codex" {
		t.Errorf("Launched.CLI = %q, want codex", got.CLI)
	}
	if codexR.lastModel != "gpt-5.5" {
		t.Errorf("codex runner saw model %q, want gpt-5.5", codexR.lastModel)
	}
	if codexR.lastSess != "" {
		t.Errorf("fresh codex runner session = %q, want empty", codexR.lastSess)
	}
	if claudeR.lastModel != "" {
		t.Errorf("claude runner must NOT have been built (model=%q)", claudeR.lastModel)
	}
}

// BE-2: a routed CLI with no registered runner builder is a clear error, not a panic.
func TestEngine_HandleWork_UnknownCLIErrors(t *testing.T) {
	// Only a claude runner is registered, but routing decides codex → no builder.
	eng, _, _ := newTestEngine(t, 2, modelrouter.Config{
		AllowedExecutors:     []modelrouter.ExecutorCandidate{{CLI: "codex", Model: "gpt-5.5"}},
		DefaultExecutorModel: "gpt-5.5",
	}, nil)
	_, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "t"}, ChatID: "c1"})
	if err == nil || !strings.Contains(err.Error(), "no runner builder for cli") {
		t.Fatalf("err = %v, want a no-runner-builder error", err)
	}
}

func TestEngine_HandleWork_SameChatMergesProblem(t *testing.T) {
	eng, _, _ := newTestEngine(t, 3, modelrouter.Config{DefaultExecutorModel: "m"}, nil)
	a, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g1"}, ChatID: "chan-X"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer reap(t, a.Handle)
	b, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g2"}, ChatID: "chan-X"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	defer reap(t, b.Handle)
	if b.ProblemID != a.ProblemID {
		t.Errorf("same chat should merge to one problem: %s vs %s", a.ProblemID, b.ProblemID)
	}
	if b.RouteReason != executor.MatchChatID {
		t.Errorf("second route reason = %v, want chat_id", b.RouteReason)
	}
}

func TestEngine_HandleWork_AtCapacityBubbles(t *testing.T) {
	eng, _, _ := newTestEngine(t, 1, modelrouter.Config{DefaultExecutorModel: "m"}, nil)
	first, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g1"}, ChatID: "c1"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer reap(t, first.Handle)
	_, err = eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g2"}, ChatID: "c2"})
	if !errors.Is(err, executor.ErrAtCapacity) {
		t.Fatalf("second at cap err = %v, want ErrAtCapacity", err)
	}
}

func TestEngine_HandleWork_RejectsEmptyGoal(t *testing.T) {
	eng, _, _ := newTestEngine(t, 2, modelrouter.Config{DefaultExecutorModel: "m"}, nil)
	if _, err := eng.HandleWork(context.Background(), WorkItem{ChatID: "c"}); err == nil {
		t.Error("empty goal.title must error")
	}
}

func TestEngine_HandleWork_NoModelResolvableSurfaces(t *testing.T) {
	// No task.model, nil judge, no default → ErrNoExecutorModel.
	eng, _, _ := newTestEngine(t, 2, modelrouter.Config{}, nil)
	if _, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g"}, ChatID: "c"}); err == nil {
		t.Error("unresolvable model must surface as an error")
	}
}

// UpdateRouterConfig live-refresh: a config edit to a RUNNING agent (config-triggered
// reconcile) must make SUBSEQUENT forks resolve the NEW model without rebuilding the
// engine. The pool_fallback source is the exact path oopslink hit (allowed_executors
// pool, no task.model / default) — refreshing the pool's model must take effect.
func TestEngine_UpdateRouterConfig_TakesEffectOnNextFork(t *testing.T) {
	fr := &fakeRunner{}
	eng, _, _ := newTestEngine(t, 3, modelrouter.Config{
		AllowedExecutors: []modelrouter.ExecutorCandidate{{CLI: "claude-code", Model: "opus-4-8"}},
	}, fr)

	// Before: the stale pool model (mirrors the boot-cached bug value).
	got, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g1"}, ChatID: "c1"})
	if err != nil {
		t.Fatalf("HandleWork before: %v", err)
	}
	defer reap(t, got.Handle)
	if got.Model != "opus-4-8" || got.ModelSource != modelrouter.SourcePoolFallback {
		t.Fatalf("before: model=%q/%v, want opus-4-8/pool_fallback", got.Model, got.ModelSource)
	}

	// Live edit: the corrected pool model arrives via a reconcile.
	eng.UpdateRouterConfig(modelrouter.Config{
		AllowedExecutors: []modelrouter.ExecutorCandidate{{CLI: "claude-code", Model: "claude-opus-4-8"}},
	})

	// After: the very next fork resolves the NEW model — no engine rebuild, no restart.
	got2, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g2"}, ChatID: "c2"})
	if err != nil {
		t.Fatalf("HandleWork after: %v", err)
	}
	defer reap(t, got2.Handle)
	if got2.Model != "claude-opus-4-8" || got2.ModelSource != modelrouter.SourcePoolFallback {
		t.Errorf("after: model=%q/%v, want claude-opus-4-8/pool_fallback", got2.Model, got2.ModelSource)
	}
	if fr.lastModel != "claude-opus-4-8" {
		t.Errorf("runner saw stale model=%q, want claude-opus-4-8", fr.lastModel)
	}
}

func TestNewEngine_Validation(t *testing.T) {
	if _, err := NewEngine(EngineConfig{}); err == nil {
		t.Error("empty config must error")
	}
}

// --- small helpers ---

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func fxRoot(t *testing.T, fx *executor.FileExchange) string {
	t.Helper()
	// Layout's executors dir is <root>/executors; the routing.json sits at <root>.
	return strings.TrimSuffix(fx.Layout().ExecutorsDir(), "/executors")
}

func mustLoadProblem(t *testing.T, s *executor.RoutingStore, id string) (executor.Problem, bool) {
	t.Helper()
	tbl, err := s.Load()
	if err != nil {
		t.Fatalf("routing Load: %v", err)
	}
	return tbl.Find(id)
}
