package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// scriptedToolCaller is a TEST-ONLY agentToolCaller that records calls in order and
// returns canned per-tool responses: a JSON body for get_task, a configurable error
// for start_task (the W4c cap / already-running decline). It lets the W4a fork path
// be driven without a live center.
type scriptedToolCaller struct {
	mu          sync.Mutex
	calls       []scriptedCall
	getTaskBody map[string]any
	getTaskRaw  []byte // when set, written verbatim (to exercise a malformed response)
	getTaskErr  error
	startErr    error
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

// loadRouting reads the agent's routing.json problems (drain-safe: routing persists
// past the async drain that frees the pool slot + removes the executor dir).
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

// TestForkOnWorkAvailable_AdmitsThenForks is the happy path (acceptance ① + ③): a
// concurrency-enabled agent's work_available pulls the task, admits it via start_task,
// and forks an executor whose routing problem is bound to the CANONICAL task_id (so
// the W2 writeback completes the right task). get_task precedes start_task.
func TestForkOnWorkAvailable_AdmitsThenForks(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-fork")
	c.mu.Lock()
	c.agents["agent-fork"] = &managedAgent{agentID: "agent-fork", exec: ee}
	c.mu.Unlock()
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-9", "title": "Fix the bug", "description": "do the fix", "status": "open", "model": "claude-haiku",
	}}
	c.cfg.ToolCaller = sc

	c.forkOnWorkAvailable(context.Background(), "agent-fork", "task-9", ee)

	if seen := sc.toolsSeen(); len(seen) != 2 || seen[0] != "get_task" || seen[1] != "start_task" {
		t.Fatalf("tool call order = %v, want [get_task start_task]", seen)
	}
	if body, ok := sc.callFor("start_task"); !ok || body["task_id"] != "task-9" || body["agent_id"] != "agent-fork" {
		t.Errorf("start_task body = %v", body)
	}
	probs := loadRouting(t, home)
	if len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-9" {
		t.Fatalf("expected one problem bound to task-9, got %+v", probs)
	}
	c.mu.Lock()
	got := c.agents["agent-fork"].currentTaskID
	c.mu.Unlock()
	if got != "task-9" {
		t.Errorf("currentTaskID = %q, want task-9", got)
	}
}

// TestForkOnWorkAvailable_StartTaskDeclinedSkipsFork: at-cap / already-running →
// start_task errors → we DON'T fork (the task stays queued for a later re-emit).
func TestForkOnWorkAvailable_StartTaskDeclinedSkipsFork(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-cap")
	c.mu.Lock()
	c.agents["agent-cap"] = &managedAgent{agentID: "agent-cap", exec: ee}
	c.mu.Unlock()
	sc := &scriptedToolCaller{
		getTaskBody: map[string]any{"id": "task-2", "title": "t", "status": "open"},
		startErr:    errors.New("409 agent_busy"),
	}
	c.cfg.ToolCaller = sc

	c.forkOnWorkAvailable(context.Background(), "agent-cap", "task-2", ee)

	if seen := sc.toolsSeen(); len(seen) != 2 || seen[1] != "start_task" {
		t.Fatalf("tool calls = %v, want get_task then start_task", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("declined admission must NOT fork, got problems %+v", probs)
	}
	c.mu.Lock()
	got := c.agents["agent-cap"].currentTaskID
	c.mu.Unlock()
	if got != "" {
		t.Errorf("currentTaskID = %q, want empty (no fork)", got)
	}
}

// TestForkOnWorkAvailable_AlreadyRunningSkips: a re-emit for a non-open task is
// skipped BEFORE start_task (cheap idempotency precheck) — no double fork.
func TestForkOnWorkAvailable_AlreadyRunningSkips(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-again")
	c.mu.Lock()
	c.agents["agent-again"] = &managedAgent{agentID: "agent-again", exec: ee}
	c.mu.Unlock()
	sc := &scriptedToolCaller{getTaskBody: map[string]any{"id": "task-3", "title": "t", "status": "running"}}
	c.cfg.ToolCaller = sc

	c.forkOnWorkAvailable(context.Background(), "agent-again", "task-3", ee)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only (no start on a running task)", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("running task must NOT fork, got %+v", probs)
	}
}

func TestForkOnWorkAvailable_GetTaskErrorSkips(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-gterr")
	c.mu.Lock()
	c.agents["agent-gterr"] = &managedAgent{agentID: "agent-gterr", exec: ee}
	c.mu.Unlock()
	sc := &scriptedToolCaller{getTaskErr: errors.New("403 not_agents_task")}
	c.cfg.ToolCaller = sc

	c.forkOnWorkAvailable(context.Background(), "agent-gterr", "task-4", ee)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("get_task failure must NOT fork, got %+v", probs)
	}
}

func TestForkOnWorkAvailable_MalformedGetTaskSkips(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-bad")
	c.mu.Lock()
	c.agents["agent-bad"] = &managedAgent{agentID: "agent-bad", exec: ee}
	c.mu.Unlock()
	sc := &scriptedToolCaller{getTaskRaw: []byte("{not json")}
	c.cfg.ToolCaller = sc

	c.forkOnWorkAvailable(context.Background(), "agent-bad", "task-8", ee)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("malformed get_task must NOT fork, got %+v", probs)
	}
}

func TestForkOnWorkAvailable_NoToolCallerLeavesQueued(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-noc")
	c.cfg.ToolCaller = nil // explicit: no agent-tool transport
	c.mu.Lock()
	c.agents["agent-noc"] = &managedAgent{agentID: "agent-noc", exec: ee}
	c.mu.Unlock()

	c.forkOnWorkAvailable(context.Background(), "agent-noc", "task-5", ee) // must not panic

	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("no ToolCaller must NOT fork, got %+v", probs)
	}
}

func TestForkOnWorkAvailable_EmptyTaskID(t *testing.T) {
	c, ee, _ := engineForAgent(t, "agent-empty")
	sc := &scriptedToolCaller{}
	c.cfg.ToolCaller = sc
	c.forkOnWorkAvailable(context.Background(), "agent-empty", "  ", ee)
	if seen := sc.toolsSeen(); len(seen) != 0 {
		t.Errorf("empty task_id must short-circuit before any center call, got %v", seen)
	}
}

// TestForkOnWorkAvailable_ForkFailsAfterAdmission covers the reap-skew branch: the
// center admits (start_task ok) but the local pool is saturated, so the fork returns
// ErrAtCapacity. It must be logged, not panic; no extra slot is taken.
func TestForkOnWorkAvailable_ForkFailsAfterAdmission(t *testing.T) {
	c, ee, _ := engineForAgent(t, "agent-skew") // pool max 2
	c.mu.Lock()
	c.agents["agent-skew"] = &managedAgent{agentID: "agent-skew", exec: ee}
	c.mu.Unlock()
	// Saturate the pool (2 held, undrained).
	for i := 0; i < 2; i++ {
		if _, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
			TaskRef: "sat-" + string(rune('a'+i)), Goal: executor.Goal{Title: "g"},
		}); err != nil {
			t.Fatalf("saturate %d: %v", i, err)
		}
	}
	sc := &scriptedToolCaller{getTaskBody: map[string]any{"id": "task-6", "title": "t", "status": "open"}}
	c.cfg.ToolCaller = sc

	c.forkOnWorkAvailable(context.Background(), "agent-skew", "task-6", ee) // must not panic

	if seen := sc.toolsSeen(); len(seen) != 2 || seen[1] != "start_task" {
		t.Fatalf("admission still runs: tool calls = %v", seen)
	}
	if act := ee.engine.Pool().Active(); act != 2 {
		t.Errorf("pool active = %d, want 2 (saturated; failed fork took no slot)", act)
	}
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
	// Attach a concurrency executor engine to the SAME managed agent.
	home, _, _, err := c.agentPaths("agent-1")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := c.buildExecutorEngine(home, reconcilePayload{
		AgentID: "agent-1", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, DefaultExecutorModel: "d",
	})
	if err != nil {
		t.Fatalf("buildExecutorEngine: %v", err)
	}
	c.mu.Lock()
	c.agents["agent-1"].exec = ee
	c.mu.Unlock()

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
