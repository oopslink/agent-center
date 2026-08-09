package agentruntime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type unfinishedRecoveryExecution struct {
	state            string
	health           string
	recoveryRequired bool
}

type unfinishedRecoveryCaller struct {
	mu sync.Mutex

	tasks      []InflightTask
	executions map[string][]unfinishedRecoveryExecution

	listTaskReads  int
	executionReads int
	getTaskCalls   int
	startCalls     int
}

func (c *unfinishedRecoveryCaller) CallAgentTool(_ context.Context, tool string, body any, out *json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var v any
	switch tool {
	case "list_my_tasks":
		c.listTaskReads++
		items := make([]map[string]any, 0, len(c.tasks))
		for _, task := range c.tasks {
			items = append(items, map[string]any{
				"task_id": task.TaskID,
				"status":  task.Status,
			})
		}
		v = map[string]any{"tasks": items}
	case "list_task_executions":
		c.executionReads++
		taskID, _ := body.(map[string]any)["task_id"].(string)
		items := make([]map[string]any, 0, len(c.executions[taskID]))
		for _, ex := range c.executions[taskID] {
			items = append(items, map[string]any{
				"state":             ex.state,
				"health_status":     ex.health,
				"recovery_required": ex.recoveryRequired,
			})
		}
		v = map[string]any{"items": items}
	case "get_task":
		c.getTaskCalls++
		taskID, _ := body.(map[string]any)["task_id"].(string)
		status := "running"
		for _, task := range c.tasks {
			if task.TaskID == taskID && task.Status != "" {
				status = task.Status
			}
		}
		v = map[string]any{"id": taskID, "title": "resume me", "status": status}
	case "get_team_rules":
		v = map[string]any{"rules": []any{}}
	case "start_task":
		c.startCalls++
	}
	if out != nil && v != nil {
		b, _ := json.Marshal(v)
		*out = b
	}
	return nil
}

func TestControlLoadedRecovery_OncePerGenerationAndAgainAfterReload(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-control-reload")
	attach(rt, ee)
	caller := &unfinishedRecoveryCaller{
		tasks: []InflightTask{{TaskID: "task-recover", Status: "running"}},
		executions: map[string][]unfinishedRecoveryExecution{
			"task-recover": {{state: "running", health: "active"}},
		},
	}
	setToolCaller(rt, caller)

	spec := StartSpec{AgentID: "agent-control-reload", CLI: CLICodex}
	rt.reportControlLoaded(spec.AgentID, spec, controlLoadedInfo{Session: CLICodex, Generation: 7})
	rt.reportControlLoaded(spec.AgentID, spec, controlLoadedInfo{Session: CLICodex, Generation: 7})
	rt.WaitBG()
	rt.reportControlLoaded(spec.AgentID, spec, controlLoadedInfo{Session: CLICodex, Generation: 8})
	rt.WaitBG()

	caller.mu.Lock()
	defer caller.mu.Unlock()
	if caller.listTaskReads != 2 {
		t.Fatalf("recovery checks = %d, want one for generation 7 and one for generation 8", caller.listTaskReads)
	}
	if caller.getTaskCalls != 0 {
		t.Fatalf("healthy execution must not fork, get_task calls = %d", caller.getTaskCalls)
	}
}

func TestRecoverUnfinishedWork_RunningWithoutExecutionForksAfterRestart(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-recover")
	attach(rt, ee)
	caller := &unfinishedRecoveryCaller{
		tasks:      []InflightTask{{TaskID: "task-recover", Status: "running"}},
		executions: map[string][]unfinishedRecoveryExecution{},
	}
	setToolCaller(rt, caller)

	if err := rt.RecoverUnfinishedWork(context.Background()); err != nil {
		t.Fatal(err)
	}

	caller.mu.Lock()
	defer caller.mu.Unlock()
	if caller.getTaskCalls != 1 {
		t.Fatalf("get_task calls = %d, want one recovery fork", caller.getTaskCalls)
	}
	if caller.startCalls != 0 {
		t.Fatalf("running task must reuse admission, start_task calls = %d", caller.startCalls)
	}
}

func TestRecoverUnfinishedWork_ConsecutiveStartsDoNotDuplicateHealthyExecution(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-idempotent")
	attach(rt, ee)
	caller := &unfinishedRecoveryCaller{
		tasks: []InflightTask{{TaskID: "task-healthy", Status: "running"}},
		executions: map[string][]unfinishedRecoveryExecution{
			"task-healthy": {{state: "running", health: "active"}},
		},
	}
	setToolCaller(rt, caller)

	if err := rt.RecoverUnfinishedWork(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := rt.RecoverUnfinishedWork(context.Background()); err != nil {
		t.Fatal(err)
	}

	caller.mu.Lock()
	defer caller.mu.Unlock()
	if caller.getTaskCalls != 0 {
		t.Fatalf("healthy execution was forked %d time(s)", caller.getTaskCalls)
	}
}

func TestRecoverUnfinishedWork_NonRecoverableExecutionDoesNotFork(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-nonrecoverable")
	attach(rt, ee)
	caller := &unfinishedRecoveryCaller{
		tasks: []InflightTask{{TaskID: "task-done", Status: "running"}},
		executions: map[string][]unfinishedRecoveryExecution{
			"task-done": {{state: "terminal", health: "terminal"}},
		},
	}
	setToolCaller(rt, caller)

	if err := rt.RecoverUnfinishedWork(context.Background()); err != nil {
		t.Fatal(err)
	}

	caller.mu.Lock()
	defer caller.mu.Unlock()
	if caller.getTaskCalls != 0 {
		t.Fatalf("non-recovery_required execution was forked %d time(s)", caller.getTaskCalls)
	}
}

func TestRecoverUnfinishedWork_StaleOrRecoveryRequiredExecutionSpawnsSuccessor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		agentID   string
		execution unfinishedRecoveryExecution
	}{
		{name: "stale", agentID: "agent-stale", execution: unfinishedRecoveryExecution{state: "running", health: "stale", recoveryRequired: true}},
		{name: "recovery required", agentID: "agent-recovery-required", execution: unfinishedRecoveryExecution{state: "terminal", health: "failed", recoveryRequired: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, ee, _ := engineForAgent(t, tc.agentID)
			attach(rt, ee)
			caller := &unfinishedRecoveryCaller{
				tasks: []InflightTask{{TaskID: "task-recover", Status: "running"}},
				executions: map[string][]unfinishedRecoveryExecution{
					"task-recover": {tc.execution},
				},
			}
			setToolCaller(rt, caller)

			if err := rt.RecoverUnfinishedWork(context.Background()); err != nil {
				t.Fatal(err)
			}

			caller.mu.Lock()
			getTaskCalls := caller.getTaskCalls
			startCalls := caller.startCalls
			caller.mu.Unlock()
			if getTaskCalls != 1 {
				t.Fatalf("get_task calls = %d, want recovery fork", getTaskCalls)
			}
			if startCalls != 0 {
				t.Fatalf("running recovery must not start_task again, got %d", startCalls)
			}
		})
	}
}

func TestRecoverUnfinishedWork_OpenTaskSpawnsThroughAdmission(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-open-recover")
	attach(rt, ee)
	caller := &unfinishedRecoveryCaller{
		tasks:      []InflightTask{{TaskID: "task-open", Status: "open"}},
		executions: map[string][]unfinishedRecoveryExecution{},
	}
	setToolCaller(rt, caller)

	if err := rt.RecoverUnfinishedWork(context.Background()); err != nil {
		t.Fatal(err)
	}

	caller.mu.Lock()
	defer caller.mu.Unlock()
	if caller.executionReads != 0 {
		t.Fatalf("open task should not need execution lookup, reads = %d", caller.executionReads)
	}
	if caller.getTaskCalls != 1 || caller.startCalls != 1 {
		t.Fatalf("open task recovery calls get_task/start_task = %d/%d, want 1/1", caller.getTaskCalls, caller.startCalls)
	}
}
