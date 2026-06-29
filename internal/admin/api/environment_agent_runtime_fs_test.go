package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/runtimefs"
)

// I5 (issue-921db054) — POST /admin/environment/agent/runtime-fs/response: the worker's
// correlated reply to a runtime-fs read command. requireAgentOnWorker gates it on the
// posting worker owning the agent; a matching req_id wakes the waiting Web Console
// request via the shared dispatcher.
func TestEnvAgentRuntimeFsResponse_Resolves(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_rtfs_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	disp := runtimefs.NewDispatcher()
	f.deps.RuntimeFsDispatcher = disp
	srv := f.server(t)

	ch, release := disp.Register("req-abc")
	defer release()

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/runtime-fs/response", "acat_rtfs_w1", map[string]any{
		"agent_id": atAgent1,
		"req_id":   "req-abc",
		"result":   map[string]any{"path": "", "type": "directory", "entries": []any{}, "truncated": false},
	})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status=%d body=%v, want 200 ok", status, body)
	}
	if body["matched"] != true {
		t.Fatalf("matched=%v, want true (a waiter was registered)", body["matched"])
	}
	select {
	case got := <-ch:
		if got.ReqID != "req-abc" || got.AgentID != atAgent1 {
			t.Fatalf("delivered response = %+v, want req-abc / %s", got, atAgent1)
		}
		if len(got.Result) == 0 {
			t.Fatal("delivered response carries no result payload")
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not receive the worker reply")
	}
}

func TestEnvAgentRuntimeFsResponse_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_rtfs_w1", atWorker1)
	// agent2 is bound to worker2, but the caller presents a worker1 token.
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleRunning)
	f.deps.RuntimeFsDispatcher = runtimefs.NewDispatcher()
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/runtime-fs/response", "acat_rtfs_w1", map[string]any{
		"agent_id": atAgent2,
		"req_id":   "req-x",
		"result":   map[string]any{},
	})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("want 403 agent_not_bound_to_worker, got %d %v", status, body)
	}
}

// A reply for an unknown req_id (the waiter already timed out) is acknowledged with
// matched=false — never an error, so a slow late worker reply is a harmless no-op.
func TestEnvAgentRuntimeFsResponse_UnknownReq_NoopMatchedFalse(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_rtfs_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	f.deps.RuntimeFsDispatcher = runtimefs.NewDispatcher()
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/runtime-fs/response", "acat_rtfs_w1", map[string]any{
		"agent_id": atAgent1,
		"req_id":   "never-registered",
		"result":   map[string]any{},
	})
	if status != http.StatusOK || body["matched"] != false {
		t.Fatalf("status=%d matched=%v, want 200 matched=false", status, body["matched"])
	}
}
