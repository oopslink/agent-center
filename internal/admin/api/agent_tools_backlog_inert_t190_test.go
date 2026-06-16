package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// T190 — backlog-task-inert invariant + unified error.
//
// A backlog task (planID=="", still open) is INERT: claim_task / start_task /
// complete_task / block_task ALL reject it with the ONE unified envelope
// (409 task_backlog_not_actionable + add-to-plan/pool guidance), replacing the
// prior scattered not_claimable / task_not_runnable / not_agents_task. discard_task
// is the EXEMPTION — it succeeds on a backlog task (cleanup of a mis-created task).
// Non-backlog behavior must not regress (covered by the existing OK/terminal tests
// that run over seedRunningTask).
// =============================================================================

// seedBacklogTask creates a project, makes AG1 a member, and creates an OPEN task
// with NO plan (planID==""), i.e. inert backlog — the agent has no WorkItem for it.
// Returns the task id.
func (f *writeToolsFixture) seedBacklogTask(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: atTestOrg, Name: "Backlog", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	// AG1 is a project member (realistic: it create_task'd the backlog task) but the
	// task is NOT assigned → no WorkItem, so the old gates 403'd not_agents_task.
	if _, err := f.pmSvc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
		ProjectID: pid, IdentityID: pm.IdentityRef("agent:" + atAgent1), Actor: owner,
	}); err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "captured but unplaced", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t) // participant projector creates the task Conversation.
	return string(tid)
}

func TestClaimTask_Backlog_Unified_T190(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedBacklogTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/claim_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %v", status, body)
	}
	if body["error"] != "task_backlog_not_actionable" {
		t.Fatalf("error = %v, want task_backlog_not_actionable; body = %v", body["error"], body)
	}
	if msg, _ := body["message"].(string); !contains(msg, "add_task_to_plan") {
		t.Fatalf("message must point to add_task_to_plan/pool, got %q", msg)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskOpen {
		t.Fatalf("task status = %s, want open (claim rejected)", got)
	}
}

func TestCompleteTask_Backlog_Unified_T190(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedBacklogTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "summary": "done?"})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %v", status, body)
	}
	if body["error"] != "task_backlog_not_actionable" {
		t.Fatalf("error = %v, want task_backlog_not_actionable", body["error"])
	}
	// Inert: no state change, and the summary must NOT have been posted (the gate
	// short-circuits before the tx).
	if got := f.taskStatus(t, tid); got != pm.TaskOpen {
		t.Fatalf("task status = %s, want open (complete rejected)", got)
	}
	for _, m := range f.taskMessages(t, tid) {
		if m.Content() == "done?" {
			t.Fatalf("summary leaked despite backlog rejection")
		}
	}
}

func TestBlockTask_Backlog_Unified_T190(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedBacklogTask(t)
	srv := f.server(t)

	// Reason supplied so we pass the missing_reason 400 gate and reach the backlog gate.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/block_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "reason": "stuck"})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %v", status, body)
	}
	if body["error"] != "task_backlog_not_actionable" {
		t.Fatalf("error = %v, want task_backlog_not_actionable", body["error"])
	}
	if got := f.taskStatus(t, tid); got != pm.TaskOpen {
		t.Fatalf("task status = %s, want open (block rejected)", got)
	}
}

// start_task converges on the SAME unified envelope: a backlog task assigned
// directly mints a queued WorkItem, but with the T130 run gate wired (as production
// wires it) start_task refuses to activate it — surfaced as task_backlog_not_actionable.
func TestStartWork_Backlog_Unified_T190(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.deps.AgentSvc.SetTaskRunGate(pmservice.NewAgentTaskRunGate(f.pmSvc))

	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1), owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t) // work-item projector mints the queued WorkItem.
	items, err := f.workItems.ListByTask(ctx, "pm://tasks/"+string(tid))
	if err != nil || len(items) != 1 {
		t.Fatalf("want 1 queued work item, got %d (err=%v)", len(items), err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/start_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "work_item_id": items[0].ID()})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %v", status, body)
	}
	if body["error"] != "task_backlog_not_actionable" {
		t.Fatalf("error = %v, want task_backlog_not_actionable (converged from task_not_runnable)", body["error"])
	}
}

// EXEMPTION: discard_task SUCCEEDS on an inert backlog task (no WorkItem) for a
// project member — the cleanup path is deliberately not gated by the inert rule.
func TestDiscardTask_Backlog_OK_T190(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedBacklogTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/discard_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "reason": "mis-created backlog"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (discard exempt from inert rule); body = %v", status, body)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskDiscarded {
		t.Fatalf("task status = %s, want discarded", got)
	}
}

// Unit-level guard for the named invariant predicate (the single source the gates reuse).
func TestIsBacklogInert_T190(t *testing.T) {
	if !pm.IsBacklogInert("") {
		t.Fatalf("planID=='' must be inert backlog")
	}
	if pm.IsBacklogInert("plan-123") {
		t.Fatalf("a task in a plan must NOT be inert backlog")
	}
}
