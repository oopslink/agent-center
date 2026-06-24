package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T468: completing a task with review_verdict plumbs the structured verdict through
// to the PM store (single-slot, round-tagged). Verified end-to-end through the real
// admin handler + RecordReviewVerdict, then read back via ListReviewVerdicts.
func TestCompleteTask_RecordsReviewVerdict_T468(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	tid := f.seedRunningTask(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{
			"agent_id":        atAgent1,
			"task_id":         tid,
			"summary":         "reviewed",
			"review_verdict":  "pass",
			"review_blocking": false,
			"review_reason":   "looks good, one non-blocking nit",
			"review_sha":      "deadbeef",
		})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}

	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	verdicts, err := f.pmSvc.ListReviewVerdicts(context.Background(), tk.PlanID(), pm.IdentityRef("user:owner"))
	if err != nil {
		t.Fatal(err)
	}
	var got *pm.ReviewVerdict
	for i := range verdicts {
		if verdicts[i].TaskID == pm.TaskID(tid) {
			got = &verdicts[i]
		}
	}
	if got == nil {
		t.Fatalf("no review verdict recorded for %s; got %+v", tid, verdicts)
	}
	if got.Verdict != pm.ReviewPass || got.Blocking || got.SHA != "deadbeef" {
		t.Fatalf("recorded verdict wrong: %+v", *got)
	}
}

// An invalid verdict label fails the completion (the verdict + complete are one tx).
func TestCompleteTask_InvalidReviewVerdict_Rejected_T468(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	tid := f.seedRunningTask(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "review_verdict": "approve"})
	if status == http.StatusOK {
		t.Fatalf("an invalid review_verdict must not 200")
	}
	// The task must NOT have completed (the tx rolled back).
	if st := f.taskStatus(t, tid); st != pm.TaskRunning {
		t.Fatalf("task status = %s, want running (completion rolled back with the bad verdict)", st)
	}
}
