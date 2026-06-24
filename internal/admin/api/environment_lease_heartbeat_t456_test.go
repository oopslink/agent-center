package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T456 P0 #1: the worker process-alive lease-heartbeat endpoint renews the assignee's
// running task lease (decoupled from the agent's LLM turn).
func TestEnvAgentLeaseHeartbeat_RenewsRunningTask(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	tid := f.seedRunningTask(t)

	before, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	leaseBefore := before.ExecutionLeaseExpiresAt()
	if leaseBefore == nil {
		t.Fatal("expected a lease granted by StartTask")
	}

	// Advance the clock so a renew is observably later than the StartTask lease.
	f.clk.Advance(5 * time.Minute)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/lease/heartbeat", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}

	after, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	leaseAfter := after.ExecutionLeaseExpiresAt()
	if leaseAfter == nil || !leaseAfter.After(*leaseBefore) {
		t.Fatalf("lease must be renewed forward: before=%v after=%v", leaseBefore, leaseAfter)
	}
	// The renew is a pure lease touch — the task stays running with the same assignee.
	if after.Status() != pm.TaskRunning || after.Assignee() != pm.IdentityRef("agent:"+atAgent1) {
		t.Fatalf("renew must not change status/assignee: status=%s assignee=%q",
			after.Status(), after.Assignee())
	}
}

// Missing task_id → 400.
func TestEnvAgentLeaseHeartbeat_MissingTaskID(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/environment/agent/lease/heartbeat", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing task_id)", status)
	}
}

// Cross-worker guardrail: W2's token may not renew AG1 (bound to W1) → 403, and the
// lease is untouched.
func TestEnvAgentLeaseHeartbeat_CrossWorkerRejected(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w2", atWorker2)
	srv := f.server(t)
	tid := f.seedRunningTask(t)

	before, _ := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	status, _ := postBearer(t, srv.URL, "/admin/environment/agent/lease/heartbeat", "acat_w2",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (cross-worker)", status)
	}
	after, _ := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if !before.ExecutionLeaseExpiresAt().Equal(*after.ExecutionLeaseExpiresAt()) {
		t.Fatalf("rejected renew must not touch the lease")
	}
}
