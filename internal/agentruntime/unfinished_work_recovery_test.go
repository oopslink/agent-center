package agentruntime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type unfinishedRecoveryCaller struct {
	mu             sync.Mutex
	executionReads int
	listTaskReads  int
	getTaskCalls   int
	startCalls     int
}

func (c *unfinishedRecoveryCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var v any
	switch tool {
	case "list_my_tasks":
		c.listTaskReads++
		v = map[string]any{"tasks": []map[string]any{{"task_id": "task-recover", "status": "running"}}}
	case "list_task_executions":
		c.executionReads++
		if c.executionReads == 1 {
			v = map[string]any{"items": []any{}}
		} else {
			v = map[string]any{"items": []map[string]any{{"state": "running", "health_status": "healthy"}}}
		}
	case "get_task":
		c.getTaskCalls++
		v = map[string]any{"id": "task-recover", "title": "resume me", "status": "running"}
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
	caller := &unfinishedRecoveryCaller{executionReads: 10} // every execution check is healthy
	setToolCaller(rt, caller)

	spec := StartSpec{AgentID: "agent-control-reload", CLI: CLICodex}
	rt.reportControlLoaded(spec.AgentID, spec, controlLoadedInfo{Session: CLICodex, Generation: 7})
	rt.reportControlLoaded(spec.AgentID, spec, controlLoadedInfo{Session: CLICodex, Generation: 7}) // duplicate event in the same generation
	rt.WaitBG()
	rt.reportControlLoaded(spec.AgentID, spec, controlLoadedInfo{Session: CLICodex, Generation: 8}) // reload/restart generation
	rt.WaitBG()

	caller.mu.Lock()
	defer caller.mu.Unlock()
	if caller.listTaskReads != 2 {
		t.Fatalf("recovery checks = %d, want one for generation 7 and one for generation 8", caller.listTaskReads)
	}
}

func TestRecoverUnfinishedWork_RunningWithoutExecutionForksAfterRestart(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-recover")
	attach(rt, ee)
	caller := &unfinishedRecoveryCaller{}
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
	caller := &unfinishedRecoveryCaller{executionReads: 1} // healthy from the first pass
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
