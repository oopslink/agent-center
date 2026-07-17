package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// THE lock for the tester3 finding (I107 review round 1): a PARKED task's dead executor
// must not be relaunched by BOOT SELF-RECONCILE.
//
// This drives the REAL reconcileExecutors path — scan → inflightTaskSet → classify →
// enact — and asserts the reconcile's own counters. It deliberately does NOT test
// taskCancelEvidence: that pure function was already correct when this bug shipped, and
// its unit test + mutation check were green while a delivered task's executor was being
// relaunched in a real deployment. The defect was that the inflight leg SHORT-CIRCUITS
// before cancel-evidence is ever consulted, so only a test that goes through the real
// entrypoint can see it. A pure-function test cannot prove the function gets CALLED.
type inflightCaller struct {
	mu       sync.Mutex
	status   string // the status list_my_inflight_tasks reports for task-1
	askedFor []string
}

func (c *inflightCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	c.mu.Lock()
	c.askedFor = append(c.askedFor, tool)
	c.mu.Unlock()
	if out == nil {
		return nil
	}
	switch tool {
	case "list_my_inflight_tasks":
		// The center's active set. A parked task is ACTIVE (non-terminal) and so is
		// returned by an unfiltered center — this is exactly the deployment shape
		// tester3 observed (`list_my_inflight_tasks → task-1 delivered`).
		rb, _ := json.Marshal(map[string]any{"tasks": []map[string]any{
			{"task_id": "task-1", "title": "t", "status": c.status},
		}})
		*out = append((*out)[:0], rb...)
	case "get_task":
		rb, _ := json.Marshal(map[string]any{"id": "task-1", "status": c.status, "assignee": "agent:ag-boot"})
		*out = append((*out)[:0], rb...)
	}
	return nil
}

func TestReconcileExecutors_ParkedTaskIsNeverRelaunched(t *testing.T) {
	for _, status := range []string{"delivered", "blocked"} {
		t.Run(status, func(t *testing.T) {
			var logs []string
			var logMu sync.Mutex
			rt, ee, home := engineForAgent(t, "ag-boot")
			rt.cfg.Log = func(format string, args ...any) {
				logMu.Lock()
				defer logMu.Unlock()
				logs = append(logs, fmt.Sprintf(format, args...))
			}
			attach(rt, ee)
			caller := &inflightCaller{status: status}
			setToolCaller(rt, caller)

			fx, tr := seedExchange(t, home)
			// A DEAD executor (dead pid, no output.json) of a parked task: the exact
			// shape tester3 saw relaunched.
			seedExecutorWithTask(t, fx, tr, "ex-boot", "task-1")

			if err := rt.reconcileExecutors(context.Background(), ee); err != nil {
				t.Fatalf("reconcileExecutors: %v", err)
			}

			logMu.Lock()
			defer logMu.Unlock()
			joined := strings.Join(logs, "\n")
			if strings.Contains(joined, "self-reconcile relaunched") {
				t.Fatalf("a %s task's dead executor was RELAUNCHED by boot self-reconcile — "+
					"an empty-context executor was just forked onto already-delivered work.\nlogs:\n%s", status, joined)
			}
			// The reconcile's own counter is the assertion pd asked for: recovered=0.
			if !strings.Contains(joined, "recovered=0") {
				t.Fatalf("want recovered=0 in the self-reconcile summary for a %s task.\nlogs:\n%s", status, joined)
			}
		})
	}
}

// A HEALTHY running task must still recover normally — the fix must stop parked work,
// not break the recovery this whole subsystem exists for.
func TestReconcileExecutors_RunningTaskStillRecovers(t *testing.T) {
	var logs []string
	var logMu sync.Mutex
	rt, ee, home := engineForAgent(t, "ag-boot")
	rt.cfg.Log = func(format string, args ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	attach(rt, ee)
	setToolCaller(rt, &inflightCaller{status: "running"})

	fx, tr := seedExchange(t, home)
	seedExecutorWithTask(t, fx, tr, "ex-boot", "task-1")

	if err := rt.reconcileExecutors(context.Background(), ee); err != nil {
		t.Fatalf("reconcileExecutors: %v", err)
	}
	logMu.Lock()
	defer logMu.Unlock()
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "recovered=1") {
		t.Fatalf("a crashed executor of a RUNNING task must still be recovered (want recovered=1).\nlogs:\n%s", joined)
	}
}
