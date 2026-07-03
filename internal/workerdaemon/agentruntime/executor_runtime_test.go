package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
)

func TestBuildExecutorEngine_ErrorOnBadRoot(t *testing.T) {
	rt := newExecRuntime(t, t.TempDir(), "agent-bad", "true")
	if _, err := rt.BuildExecutorEngine("", ExecutorConfig{MaxConcurrentTasks: 1, AllowedExecutors: testExecs}); err == nil {
		t.Error("empty agent root must surface an error")
	}
}

func TestDrainExecutor_NilGuards(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-nilguard")
	rt.drainExecutor(nil, nil) // must not panic
	rt.drainExecutor(ee, nil)  // nil handle → no-op
}

func TestBuildExecutorEngine_ForksAndDrainFreesSlot(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-x")
	if ee.engine.Pool().Max() != 2 {
		t.Fatalf("pool max = %d, want 2", ee.engine.Pool().Max())
	}
	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskRef: "task-1", Goal: executor.Goal{Title: "do it"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	if launched.Model != "claude-default" {
		t.Errorf("model = %q, want claude-default (chain fallback)", launched.Model)
	}
	if ee.engine.Pool().Active() != 1 {
		t.Fatalf("active = %d, want 1 after fork", ee.engine.Pool().Active())
	}
	rt.drainExecutor(ee, launched.Handle)
	if ee.engine.Pool().Active() != 0 {
		t.Errorf("active = %d, want 0 after drain", ee.engine.Pool().Active())
	}
}

func TestWorkViaExecutor_ForksAndRegistersRouting(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-y")
	attach(rt, ee)

	err := rt.workViaExecutor(context.Background(), WorkRequest{
		AgentID: "agent-y", TaskID: "t-1", TaskRef: "task-1", Brief: "fix the thing\nmore detail",
	}, ee)
	if err != nil {
		t.Fatalf("workViaExecutor: %v", err)
	}
	// Routing.json persists past the (async) drain.
	if probs := loadRouting(t, home); len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-1" {
		t.Errorf("expected one problem bound to task-1, got %+v", probs)
	}
	if got := rt.State().CurrentTaskID; got != "t-1" {
		t.Errorf("currentTaskID = %q, want t-1", got)
	}
}

// errRunnerWD makes the engine's runner build fail, exercising the non-capacity
// error branch.
type errRunnerWD struct{}

func (errRunnerWD) Build(string, string) ([]string, error) { return nil, errors.New("runner boom") }

func TestWorkViaExecutor_NonCapacityErrorWraps(t *testing.T) {
	trueBin := lookTrue(t)
	home := t.TempDir()
	rt := newExecRuntime(t, t.TempDir(), "a", trueBin)
	layout, _ := executor.NewLayout(home)
	fx, _ := executor.NewFileExchange(layout, nil)
	pool, _ := executor.NewPool(executor.PoolConfig{Exchange: fx, Spawner: executor.NewSpawner(), AgentRoot: home, BinaryPath: trueBin, Max: 2})
	routing, _ := executor.NewRoutingStore(home, nil)
	eng, _ := orchestrator.NewEngine(orchestrator.EngineConfig{
		Pool: pool, Routing: routing, Router: modelrouter.NewRouter(nil),
		RouterConfig: modelrouter.Config{DefaultExecutorModel: "m"},
		Runners:      map[string]orchestrator.RunnerCmdBuilder{"claude-code": errRunnerWD{}},
		IDs:          orchestrator.NewULIDMinter(nil),
	})
	mon, _ := executor.NewMonitor(executor.MonitorConfig{Exchange: fx, Pool: pool})
	ee := &ExecutorEngine{engine: eng, monitor: mon}

	err := rt.workViaExecutor(context.Background(), WorkRequest{AgentID: "a", TaskID: "t", Brief: "do"}, ee)
	if err == nil || errors.Is(err, executor.ErrAtCapacity) {
		t.Fatalf("expected a non-capacity fork error, got %v", err)
	}
	if !strings.Contains(err.Error(), "fork executor") {
		t.Errorf("expected wrapped fork-executor error, got %v", err)
	}
}

func TestWorkViaExecutor_AtCapacityRetryable(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-z") // max 2
	for i := 0; i < 2; i++ {
		if _, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
			TaskRef: "task-" + string(rune('a'+i)), Goal: executor.Goal{Title: "g"},
		}); err != nil {
			t.Fatalf("saturating launch %d: %v", i, err)
		}
	}
	err := rt.workViaExecutor(context.Background(), WorkRequest{AgentID: "agent-z", TaskID: "t3", TaskRef: "task-c", Brief: "x"}, ee)
	if err == nil {
		t.Fatal("expected a retryable at-capacity error")
	}
	if !errors.Is(err, executor.ErrAtCapacity) {
		t.Errorf("expected at-capacity error, got %v", err)
	}
}

func TestBuildWorkItem(t *testing.T) {
	t.Run("full detail", func(t *testing.T) {
		got := buildWorkItem("task-9", &centerTaskDetail{
			ID: "task-9", Title: "Fix the bug", Description: "do the fix", Model: "claude-haiku",
		})
		want := orchestrator.WorkItem{
			TaskID: "task-9", TaskRef: "task-9", TaskModel: "claude-haiku",
			Goal: executor.Goal{Title: "Fix the bug", Description: "do the fix"},
		}
		if got != want {
			t.Errorf("buildWorkItem = %+v, want %+v", got, want)
		}
	})
	t.Run("title falls back to first description line", func(t *testing.T) {
		got := buildWorkItem("task-1", &centerTaskDetail{Description: "  \nfirst line\nrest"})
		if got.Goal.Title != "first line" {
			t.Errorf("goal title = %q, want 'first line'", got.Goal.Title)
		}
	})
	t.Run("title falls back to task id", func(t *testing.T) {
		got := buildWorkItem("task-7", &centerTaskDetail{})
		if got.Goal.Title != "task task-7" {
			t.Errorf("goal title = %q, want 'task task-7'", got.Goal.Title)
		}
	})
}

// TestBuildExecutorEngine_WiresWriteback covers the W2 branch that builds the center
// writeback when the runtime has an agent-tool caller.
func TestBuildExecutorEngine_WiresWriteback(t *testing.T) {
	trueBin := lookTrue(t)
	rt := newExecRuntime(t, t.TempDir(), "a-wb", trueBin)
	setToolCaller(rt, &fakeToolCaller{}) // W2: enables the real center Writeback path
	home, _, _, err := rt.agentPaths("a-wb")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID: "a-wb", MaxConcurrentTasks: 1, DefaultExecutorModel: "d",
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine with ToolCaller: %v", err)
	}
	if ee == nil || ee.monitor == nil || ee.engine == nil {
		t.Fatal("engine/monitor should be built")
	}
}

// ---- SpawnExecutor (was forkOnWorkAvailable) ----

// spawn drives SpawnExecutor with the given scripted caller attached.
func spawn(t *testing.T, agentID, taskID string, sc ToolCaller) (*LocalRuntime, *ExecutorEngine, string) {
	t.Helper()
	rt, ee, home := engineForAgent(t, agentID)
	attach(rt, ee)
	if sc != nil {
		setToolCaller(rt, sc)
	}
	_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: taskID})
	return rt, ee, home
}

func TestSpawnExecutor_AdmitsThenForks(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-9", "title": "Fix the bug", "description": "do the fix", "status": "open", "model": "claude-haiku",
	}}
	rt, _, home := spawn(t, "agent-fork", "task-9", sc)

	if seen := sc.toolsSeen(); len(seen) != 2 || seen[0] != "get_task" || seen[1] != "start_task" {
		t.Fatalf("tool call order = %v, want [get_task start_task]", seen)
	}
	if body, ok := sc.callFor("start_task"); !ok || body["task_id"] != "task-9" || body["agent_id"] != "agent-fork" {
		t.Errorf("start_task body = %v", body)
	}
	if probs := loadRouting(t, home); len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-9" {
		t.Fatalf("expected one problem bound to task-9, got %+v", probs)
	}
	if got := rt.State().CurrentTaskID; got != "task-9" {
		t.Errorf("currentTaskID = %q, want task-9", got)
	}
}

func TestSpawnExecutor_StartTaskDeclinedSkipsFork(t *testing.T) {
	sc := &scriptedToolCaller{
		getTaskBody: map[string]any{"id": "task-2", "title": "t", "status": "open"},
		startErr:    errors.New("409 agent_busy"),
	}
	rt, _, home := spawn(t, "agent-cap", "task-2", sc)

	if seen := sc.toolsSeen(); len(seen) != 2 || seen[1] != "start_task" {
		t.Fatalf("tool calls = %v, want get_task then start_task", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("declined admission must NOT fork, got problems %+v", probs)
	}
	if got := rt.State().CurrentTaskID; got != "" {
		t.Errorf("currentTaskID = %q, want empty (no fork)", got)
	}
}

func TestSpawnExecutor_AlreadyRunningSkips(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{"id": "task-3", "title": "t", "status": "running"}}
	_, _, home := spawn(t, "agent-again", "task-3", sc)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only (no start on a running task)", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("running task must NOT fork, got %+v", probs)
	}
}

func TestSpawnExecutor_GetTaskErrorSkips(t *testing.T) {
	sc := &scriptedToolCaller{getTaskErr: errors.New("403 not_agents_task")}
	_, _, home := spawn(t, "agent-gterr", "task-4", sc)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("get_task failure must NOT fork, got %+v", probs)
	}
}

func TestSpawnExecutor_MalformedGetTaskSkips(t *testing.T) {
	sc := &scriptedToolCaller{getTaskRaw: []byte("{not json")}
	_, _, home := spawn(t, "agent-bad", "task-8", sc)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("malformed get_task must NOT fork, got %+v", probs)
	}
}

func TestSpawnExecutor_NoToolCallerLeavesQueued(t *testing.T) {
	_, _, home := spawn(t, "agent-noc", "task-5", nil) // no ToolCaller wired
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("no ToolCaller must NOT fork, got %+v", probs)
	}
}

func TestSpawnExecutor_EmptyTaskID(t *testing.T) {
	sc := &scriptedToolCaller{}
	spawn(t, "agent-empty", "  ", sc)
	if seen := sc.toolsSeen(); len(seen) != 0 {
		t.Errorf("empty task_id must short-circuit before any center call, got %v", seen)
	}
}

// TestSpawnExecutor_ForkFailsAfterAdmission covers the reap-skew branch: the center
// admits (start_task ok) but the local pool is saturated, so the fork returns
// ErrAtCapacity. It must be logged, not panic; no extra slot is taken.
func TestSpawnExecutor_ForkFailsAfterAdmission(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-skew") // pool max 2
	attach(rt, ee)
	for i := 0; i < 2; i++ {
		if _, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
			TaskRef: "sat-" + string(rune('a'+i)), Goal: executor.Goal{Title: "g"},
		}); err != nil {
			t.Fatalf("saturate %d: %v", i, err)
		}
	}
	sc := &scriptedToolCaller{getTaskBody: map[string]any{"id": "task-6", "title": "t", "status": "open"}}
	setToolCaller(rt, sc)

	_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-6"}) // must not panic

	if seen := sc.toolsSeen(); len(seen) != 2 || seen[1] != "start_task" {
		t.Fatalf("admission still runs: tool calls = %v", seen)
	}
	if act := ee.engine.Pool().Active(); act != 2 {
		t.Errorf("pool active = %d, want 2 (saturated; failed fork took no slot)", act)
	}
}

// blockingToolCaller detects overlapping in-flight center calls (maxInFlight) so the
// fork-mutex serialization can be asserted, and returns a canned get_task body.
type blockingToolCaller struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	getTasks    int
	delay       time.Duration
}

func (b *blockingToolCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.maxInFlight {
		b.maxInFlight = b.inFlight
	}
	if tool == "get_task" {
		b.getTasks++
	}
	b.mu.Unlock()

	if b.delay > 0 {
		time.Sleep(b.delay)
	}
	if tool == "get_task" && out != nil {
		rb, _ := json.Marshal(map[string]any{"id": "t", "title": "t", "status": "open"})
		*out = append((*out)[:0], rb...)
	}

	b.mu.Lock()
	b.inFlight--
	b.mu.Unlock()
	return nil
}

// TestSpawnExecutor_ForkMutexSerializes proves red line #1: two concurrent
// SpawnExecutor calls for ONE runtime never overlap inside the get_task→start_task→
// launch sequence (forkMu holds it end-to-end), so two forks can't both slip past the
// pool cap. Without forkMu the two get_task calls (each sleeping) would overlap and
// maxInFlight would reach 2.
func TestSpawnExecutor_ForkMutexSerializes(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-serial") // pool max 2 (both forks fit)
	attach(rt, ee)
	btc := &blockingToolCaller{delay: 25 * time.Millisecond}
	setToolCaller(rt, btc)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-" + string(rune('a'+n))})
		}(i)
	}
	wg.Wait()

	btc.mu.Lock()
	maxIn, got := btc.maxInFlight, btc.getTasks
	btc.mu.Unlock()

	if got != 2 {
		t.Fatalf("both SpawnExecutor calls must reach get_task, got %d", got)
	}
	if maxIn != 1 {
		t.Fatalf("forkMu must serialize the fork sequence: maxInFlight = %d, want 1 (concurrent get_task ⇒ double-fork risk)", maxIn)
	}
}

// TestSpawnExecutor_ModelNotAllowedBlocks: the task was admitted (start_task ok) but
// its task.model is not in allowed_executors → the fork returns ErrModelNotAllowed →
// the task is blocked (block_task), not forked.
func TestSpawnExecutor_ModelNotAllowedBlocks(t *testing.T) {
	trueBin := lookTrue(t)
	rt := newExecRuntime(t, t.TempDir(), "agent-block", trueBin)
	home, _, _, err := rt.agentPaths("agent-block")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID: "agent-block", MaxConcurrentTasks: 1, AllowedExecutors: testExecs, // only model "m"
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-x", "title": "t", "status": "open", "model": "forbidden-model",
	}}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-x"})
	if res != nil || err != nil {
		t.Fatalf("SpawnExecutor (model blocked) = (%v, %v), want (nil, nil)", res, err)
	}
	seen := sc.toolsSeen()
	if len(seen) != 3 || seen[0] != "get_task" || seen[1] != "start_task" || seen[2] != "block_task" {
		t.Fatalf("tool calls = %v, want [get_task start_task block_task]", seen)
	}
	if body, ok := sc.callFor("block_task"); !ok || body["reason_type"] != "obstacle" {
		t.Errorf("block_task body = %v", body)
	}
	// No executor was actually spawned (the fork was rejected before launch).
	if act := ee.engine.Pool().Active(); act != 0 {
		t.Errorf("pool active = %d, want 0 (model-not-allowed took no slot)", act)
	}
}

// TestNotifyWork_ExecutorBranchForks: a concurrency-enabled runtime with a live session
// forks via the executor branch of NotifyWork (createTaskDir + workViaExecutor) instead
// of injecting the brief.
func TestNotifyWork_ExecutorBranchForks(t *testing.T) {
	trueBin := lookTrue(t)
	rt := newExecRuntime(t, t.TempDir(), "agent-nw", trueBin)
	rt.cfg.TaskDirManager = taskexec.NewDirManager()
	home, _, _, err := rt.agentPaths("agent-nw")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{AgentID: "agent-nw", MaxConcurrentTasks: 2, DefaultExecutorModel: "d"})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)
	fs := &fakeSession{}
	rt.State().Session = fs // NotifyWork requires a live session before branching

	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-nw", TaskID: "t-1", TaskRef: "task-1", Brief: "do it"}); err != nil {
		t.Fatalf("NotifyWork: %v", err)
	}
	// Executor branch: the brief was NOT injected into the resident session.
	if msgs := fs.msgs(); len(msgs) != 0 {
		t.Errorf("executor branch must NOT inject into the session, got %v", msgs)
	}
	if probs := loadRouting(t, home); len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-1" {
		t.Fatalf("expected routing bound to task-1, got %+v", probs)
	}
	if got := rt.State().CurrentTaskID; got != "t-1" {
		t.Errorf("currentTaskID = %q, want t-1", got)
	}
}

// ---- Recover / RunWatchdog (crash-recovery + orphan poll) ----

// TestRecover_AdoptsRunningFinalizesTerminal: after a restart the orchestrator rebuilds
// in-flight executor state from durable files — a still-alive orphan is re-adopted
// (counts toward the cap + registered for watchdog polling), a terminal orphan is
// finalized (dir torn down). It re-spawns nothing.
func TestRecover_AdoptsRunningFinalizesTerminal(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-rec")
	attach(rt, ee)

	fx, tr := seedExchange(t, home)
	_, alivePID := liveChild(t)
	now := time.Now()

	seedOrphan(t, fx, tr, "exec-aaa111", alivePID,
		executor.Status{ExecutorID: "exec-aaa111", State: executor.StateRunning, Model: "m", StartedAt: now, LastProgressAt: now}, nil)
	seedOrphan(t, fx, tr, "exec-bbb222", deadPID(t),
		executor.Status{ExecutorID: "exec-bbb222", State: executor.StateDone, Model: "m", StartedAt: now, LastProgressAt: now},
		&executor.Output{ExecutorID: "exec-bbb222", Success: true, Result: "ok", FinishedAt: now})

	if err := rt.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	orphans := ee.snapshotOrphans()
	if pid, ok := orphans["exec-aaa111"]; !ok || pid != alivePID {
		t.Errorf("running orphan A not adopted for watchdog: %+v", orphans)
	}
	if _, ok := orphans["exec-bbb222"]; ok {
		t.Error("terminal orphan B must NOT be registered for polling")
	}
	if ee.engine.Pool().Active() != 1 {
		t.Errorf("pool active = %d, want 1 (A re-adopted toward the cap)", ee.engine.Pool().Active())
	}
	if !dirGone(t, fx, "exec-bbb222") {
		t.Error("terminal orphan B dir must be torn down by recovery")
	}
	if dirGone(t, fx, "exec-aaa111") {
		t.Error("running orphan A dir must be retained")
	}
}

// TestRunWatchdog_PollsAdoptedOrphanToCompletion: an adopted orphan (no reapable
// handle) is watched by the watchdog tick — when its process exits, the poll detects
// it, finalizes it, and stops polling.
func TestRunWatchdog_PollsAdoptedOrphanToCompletion(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-wd")
	attach(rt, ee)

	fx, tr := seedExchange(t, home)
	child, alivePID := liveChild(t)
	now := time.Now()
	seedOrphan(t, fx, tr, "exec-ccc333", alivePID,
		executor.Status{ExecutorID: "exec-ccc333", State: executor.StateRunning, Model: "m", StartedAt: now, LastProgressAt: now}, nil)

	if err := rt.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; !ok {
		t.Fatal("orphan C should be adopted for watchdog polling")
	}

	// Tick 1 (orphan alive): still polled, not finalized.
	rt.RunWatchdog(context.Background())
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; !ok {
		t.Fatal("orphan C must remain tracked while alive")
	}

	_ = child.Process.Kill()
	_, _ = child.Process.Wait()

	// Tick 2 (orphan gone): the poll finalizes it and drops it.
	rt.RunWatchdog(context.Background())
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; ok {
		t.Error("orphan C must be dropped after the watchdog observes its exit")
	}
	if ee.engine.Pool().Active() != 0 {
		t.Errorf("pool active = %d, want 0 after orphan finalized", ee.engine.Pool().Active())
	}
}

// TestRunWatchdog_NoEngineNoop: RunWatchdog on a runtime with no engine is a safe
// no-op (the daemon iterates ALL runtimes; non-concurrent ones must not panic).
func TestRunWatchdog_NoEngineNoop(t *testing.T) {
	rt := newExecRuntime(t, t.TempDir(), "agent-none", "true")
	rt.RunWatchdog(context.Background()) // must not panic
	if err := rt.Recover(context.Background()); err != nil {
		t.Errorf("Recover with no engine must be a no-op nil, got %v", err)
	}
	if snaps := rt.SnapshotConcurrency(); snaps != nil {
		t.Errorf("SnapshotConcurrency with no engine must be nil, got %+v", snaps)
	}
}
