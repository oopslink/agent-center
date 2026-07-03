package agentruntime

// executor_testkit_test.go — shared helpers for the executor面 tests moved down from
// package workerdaemon in Phase 0c. They build a LocalRuntime with an executor engine
// whose forked executors are the harmless `true` binary, and provide the scripted
// tool-caller + orphan-seeding fixtures the fork/recover/watchdog tests drive.

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// testExecs is a one-entry allowed_executors list that opts an agent into concurrency.
var testExecs = []agent.ExecutorProfile{{CLI: "claude-code", Model: "m"}}

func lookTrue(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("`true` not available: %v", err)
	}
	return p
}

// newExecRuntime builds a LocalRuntime rooted at base whose forked executors run the
// given binary. The shared mutex is a fresh one (the runtime owns its state here).
func newExecRuntime(t *testing.T, base, agentID, binary string) *LocalRuntime {
	t.Helper()
	cfg := LocalRuntimeConfig{
		AgentID:       agentID,
		Reporter:      &nopReporter{},
		WorkerID:      "w-1",
		AgentHomeBase: base,
		BinaryPath:    binary,
		Log:           func(string, ...any) {},
	}
	return NewLocalRuntime(cfg, &SessionState{})
}

// setToolCaller wires (or rewires) the runtime's live center agent-tool transport.
func setToolCaller(rt *LocalRuntime, tc ToolCaller) {
	rt.cfg.ToolCaller = func() ToolCaller { return tc }
}

// engineForAgent builds an executor engine for a test agent whose forked executors
// are the harmless `true` binary. Mirrors the pre-move workerdaemon helper.
func engineForAgent(t *testing.T, agentID string) (*LocalRuntime, *ExecutorEngine, string) {
	t.Helper()
	trueBin := lookTrue(t)
	base := t.TempDir()
	rt := newExecRuntime(t, base, agentID, trueBin)
	home, _, _, err := rt.agentPaths(agentID)
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID:              agentID,
		MaxConcurrentTasks:   2,
		DefaultExecutorModel: "claude-default",
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	return rt, ee, home
}

// attach installs ee onto rt and returns rt (convenience for tests that drive the
// runtime's SpawnExecutor / NotifyWork executor branch).
func attach(rt *LocalRuntime, ee *ExecutorEngine) *LocalRuntime {
	rt.AttachExecutor(ee)
	return rt
}

// scriptedToolCaller is a TEST-ONLY ToolCaller that records calls in order and returns
// canned per-tool responses.
type scriptedToolCaller struct {
	mu          sync.Mutex
	calls       []scriptedCall
	getTaskBody map[string]any
	getTaskRaw  []byte
	getTaskErr  error
	startErr    error
	// seq, when set, also records each tool name into a shared cross-collaborator
	// order log (so a repo-workspace test can assert PrepareWorktree happens BEFORE
	// start_task). Nil for the pre-existing tests → no behavior change.
	seq *callSeq
}

type scriptedCall struct {
	tool string
	body map[string]any
}

func (s *scriptedToolCaller) CallAgentTool(_ context.Context, tool string, body any, out *json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var bm map[string]any
	b, _ := json.Marshal(body)
	_ = json.Unmarshal(b, &bm)
	s.calls = append(s.calls, scriptedCall{tool: tool, body: bm})
	if s.seq != nil {
		s.seq.add(tool)
	}
	switch tool {
	case "get_task":
		if s.getTaskErr != nil {
			return s.getTaskErr
		}
		if out != nil {
			rb := s.getTaskRaw
			if rb == nil {
				rb, _ = json.Marshal(s.getTaskBody)
			}
			*out = append((*out)[:0], rb...)
		}
		return nil
	case "start_task":
		return s.startErr
	default:
		return nil
	}
}

func (s *scriptedToolCaller) toolsSeen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	for i, c := range s.calls {
		out[i] = c.tool
	}
	return out
}

func (s *scriptedToolCaller) callFor(tool string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if c.tool == tool {
			return c.body, true
		}
	}
	return nil, false
}

// loadRouting reads the agent's routing.json problems (drain-safe).
func loadRouting(t *testing.T, home string) []executor.Problem {
	t.Helper()
	store, err := executor.NewRoutingStore(home, nil)
	if err != nil {
		t.Fatalf("NewRoutingStore: %v", err)
	}
	tbl, err := store.Load()
	if err != nil {
		t.Fatalf("routing Load: %v", err)
	}
	return tbl.Problems
}

// seedExchange builds a FileExchange + Tracker rooted at the same agent home the
// executor engine uses, so a test can plant orphan executor dirs.
func seedExchange(t *testing.T, home string) (*executor.FileExchange, *executor.Tracker) {
	t.Helper()
	layout, err := executor.NewLayout(home)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := executor.NewFileExchange(layout, nil)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	tr, err := executor.NewTracker(layout)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return fx, tr
}

// liveChild starts a harmless long-lived real process, standing in for an orphan
// executor still alive after a restart.
func liveChild(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "sleep 60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start live child: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	return cmd, cmd.Process.Pid
}

// deadPID starts then reaps a process so its pid is a real-but-gone pid.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	return pid
}

func seedOrphan(t *testing.T, fx *executor.FileExchange, tr *executor.Tracker, id string, pid int, st executor.Status, out *executor.Output) {
	t.Helper()
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision %s: %v", id, err)
	}
	if err := fx.WriteInput(executor.Input{ExecutorID: id, Goal: executor.Goal{Title: "t"}, Model: "m", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("WriteInput %s: %v", id, err)
	}
	if err := fx.WriteStatus(st); err != nil {
		t.Fatalf("WriteStatus %s: %v", id, err)
	}
	if out != nil {
		if err := fx.WriteOutput(*out); err != nil {
			t.Fatalf("WriteOutput %s: %v", id, err)
		}
	}
	if err := tr.Write(executor.Record{ExecutorID: id, PID: pid, SpawnedAt: time.Now()}); err != nil {
		t.Fatalf("Tracker.Write %s: %v", id, err)
	}
}

func dirGone(t *testing.T, fx *executor.FileExchange, id string) bool {
	t.Helper()
	snaps, err := fx.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, s := range snaps {
		if s.ExecutorID == id {
			return false
		}
	}
	return true
}
