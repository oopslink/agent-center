package workerdaemon

// concurrent_work_available_test.go — the DAEMON-side end-to-end mutual-exclusion
// guard kept after Phase 0c (the fork-sequence unit tests moved into package
// agentruntime as the SpawnExecutor tests). It drives a real reconcile + work_available
// through AgentController.Handle to prove the concurrency branch forks via the runtime
// and does NOT also nudge the resident session (防双跑).

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// scriptedToolCaller is a TEST-ONLY agentToolCaller recording calls + canned responses.
type scriptedToolCaller struct {
	mu          sync.Mutex
	calls       []scriptedCall
	getTaskBody map[string]any
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
	if tool == "get_task" && out != nil {
		rb, _ := json.Marshal(s.getTaskBody)
		*out = append((*out)[:0], rb...)
	}
	return nil
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

// TestWorkAvailable_ConcurrencyAgentForksAndDoesNotNudge is the end-to-end mutual
// exclusion guard (acceptance ②): a concurrency-enabled agent that ALSO has a live
// resident session forks an executor on work_available and does NOT inject the pull
// nudge into the resident claude (防双跑).
func TestWorkAvailable_ConcurrencyAgentForksAndDoesNotNudge(t *testing.T) {
	base := t.TempDir()
	c, _, rs := newTestController(t, base)
	c.cfg.BinaryPath = lookTrue(t) // forked executors run `true`
	defer c.Shutdown(context.Background())

	// Bring up a live resident session for the agent (fake starter).
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Build + attach a concurrency executor engine onto the SAME agent's runtime.
	home, _, _, err := c.agentPaths("agent-1")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	rt := c.runtimeFor("agent-1")
	if rt == nil {
		t.Fatal("expected a runtime after reconcile")
	}
	ee, err := rt.BuildExecutorEngine(home, execConfigOf(reconcilePayload{
		AgentID: "agent-1", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, DefaultExecutorModel: "d",
	}))
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	rt.AttachExecutor(ee)

	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-77", "title": "do it", "description": "details", "status": "open",
	}}
	c.cfg.ToolCaller = sc

	if err := c.Handle(context.Background(), workAvailableCmd(t, "agent-1", "task-77", 2)); err != nil {
		t.Fatalf("work_available must ack, got %v", err)
	}

	// Mutual exclusion: the resident session got NO nudge.
	if in := rs.last().injectedMsgs(); len(in) != 0 {
		t.Errorf("concurrency agent must NOT nudge the resident session, got %v", in)
	}
	// Fork happened: admitted via start_task + routing bound to the task.
	if _, ok := sc.callFor("start_task"); !ok {
		t.Errorf("expected a start_task admission, tools seen = %v", sc.toolsSeen())
	}
	probs := loadRouting(t, home)
	if len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-77" {
		t.Errorf("expected one problem bound to task-77, got %+v", probs)
	}
}
