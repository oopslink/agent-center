package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func TestMapFindingToolError(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{pm.ErrPlanNotFound, http.StatusNotFound},
		{pm.ErrTaskNotFound, http.StatusNotFound},
		{pm.ErrPlanFindingNotFound, http.StatusNotFound},
		{pmservice.ErrFindingsUnavailable, http.StatusNotImplemented},
		{pmservice.ErrPlansUnavailable, http.StatusNotImplemented},
		{pm.ErrProjectArchived, http.StatusConflict},
		{pm.ErrPlanFindingExists, http.StatusConflict},
		{pm.ErrFindingNotTaskAssignee, http.StatusUnprocessableEntity},
		{pm.ErrFindingTaskNotInPlan, http.StatusUnprocessableEntity},
		{pm.ErrInvalidFindingKind, http.StatusUnprocessableEntity},
		{pm.ErrEmptyFindingContent, http.StatusUnprocessableEntity},
		{pm.ErrFindingContentTooLong, http.StatusUnprocessableEntity},
		{pm.ErrFindingForbidden, http.StatusForbidden},
		{pmservice.ErrNotMember, http.StatusForbidden}, // default → mapDomainError
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mapFindingToolError(rec, c.err)
		if rec.Code != c.want {
			t.Errorf("mapFindingToolError(%v) = %d, want %d", c.err, rec.Code, c.want)
		}
	}
}

// =============================================================================
// v2.10 Plan Shared Findings — agent MCP passthrough tools (record_finding,
// list_findings). Reuses the writeToolsFixture (now Findings-capable). These
// assert the PASSTHROUGH WIRING — args parsed, the right pm AppService called with
// actor=agent, finding domain errors mapped, the requireAgentOnWorker guardrail —
// NOT the finding domain itself (covered in internal/projectmanager + service).
// =============================================================================

func TestRecordFinding_AsAssignee_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID) // assigned to AG1
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": tid, "kind": "fact", "content": "the real bug is on the tuple path"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	fid, _ := body["finding_id"].(string)
	if fid == "" {
		t.Fatalf("no finding_id in body: %v", body)
	}
	// persisted + attributed to the agent.
	list, err := f.pmSvc.ListPlanFindings(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1))
	if err != nil || len(list) != 1 {
		t.Fatalf("ListPlanFindings: err=%v len=%d", err, len(list))
	}
	if got := string(list[0].AuthorRef()); got != "agent:"+atAgent1 {
		t.Fatalf("author_ref = %q, want agent:%s", got, atAgent1)
	}
	if list[0].Kind() != pm.FindingFact {
		t.Fatalf("kind = %q, want fact", list[0].Kind())
	}
}

func TestRecordFinding_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID)
	srv := f.server(t)
	// W1 token operating AG2 (bound to W2) → guardrail 403, no AppService call.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID, "task_id": tid, "kind": "fact", "content": "x"})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

func TestRecordFinding_MissingContent_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": tid, "kind": "fact", "content": "   "})
	if status != http.StatusBadRequest || body["error"] != "missing_content" {
		t.Fatalf("status = %d err=%v, want 400 missing_content", status, body["error"])
	}
}

func TestRecordFinding_TaskNotInPlan_422(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	// a backlog task assigned to AG1 but NOT selected into the plan.
	backlog, err := f.pmSvc.CreateTask(context.Background(), pmservice.CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: pm.IdentityRef("user:owner")})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.AssignTask(context.Background(), backlog, pm.IdentityRef("agent:"+atAgent1), pm.IdentityRef("user:owner")); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": string(backlog), "kind": "fact", "content": "x"})
	if status != http.StatusUnprocessableEntity || body["error"] != "invalid_finding" {
		t.Fatalf("status = %d err=%v, want 422 invalid_finding", status, body["error"])
	}
}

func TestRecordFinding_MissingPlanID_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": "T-1", "kind": "fact", "content": "x"})
	if status != http.StatusBadRequest || body["error"] != "missing_plan_id" {
		t.Fatalf("status=%d err=%v, want 400 missing_plan_id", status, body["error"])
	}
}

func TestRecordFinding_MissingTaskID_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	_ = pid
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "kind": "fact", "content": "x"})
	if status != http.StatusBadRequest || body["error"] != "missing_task_id" {
		t.Fatalf("status=%d err=%v, want 400 missing_task_id", status, body["error"])
	}
}

func TestListFindings_MissingPlanID_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_findings", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusBadRequest || body["error"] != "missing_plan_id" {
		t.Fatalf("status=%d err=%v, want 400 missing_plan_id", status, body["error"])
	}
}

func TestRecordFinding_PMNotWired_501(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.deps.PMService = nil // defensive: never run the tool against an unwired pm
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": "p", "task_id": "t", "kind": "fact", "content": "x"})
	if status != http.StatusNotImplemented {
		t.Fatalf("status=%d, want 501; body=%v", status, body)
	}
}

func TestListFindings_PMNotWired_501(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.deps.PMService = nil
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_findings", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": "p"})
	if status != http.StatusNotImplemented {
		t.Fatalf("status=%d, want 501; body=%v", status, body)
	}
}

func TestListFindings_UnknownPlan_404(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedPlanMember(t) // AG1 is a member of some project (resolves on the worker)
	srv := f.server(t)
	// list_findings on a plan that does not exist → ListPlanFindings loads the plan
	// first → ErrPlanNotFound → 404 (review #2: no silent empty list).
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_findings", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": "no-such-plan"})
	if status != http.StatusNotFound || body["error"] != "not_found" {
		t.Fatalf("status=%d err=%v, want 404 not_found", status, body["error"])
	}
}

func TestListFindings_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	// record one finding, then list.
	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/record_finding", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": tid, "kind": "failure", "content": "printer layer is a red herring"})
	if status != http.StatusOK {
		t.Fatalf("record status = %d", status)
	}
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_findings", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK {
		t.Fatalf("list status = %d, body=%v", status, body)
	}
	arr, ok := body["findings"].([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("findings = %v, want 1", body["findings"])
	}
	first, _ := arr[0].(map[string]any)
	if first["kind"] != "failure" || first["content"] != "printer layer is a red herring" {
		t.Fatalf("finding payload mismatch: %v", first)
	}
}
