package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
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

func TestCompleteTask_StructuredDeliveryRecordsReviewVerdict(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	tid := f.seedRunningTask(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{
			"agent_id": atAgent1,
			"task_id":  tid,
			"delivery": map[string]any{
				"summary": "reviewed structured payload",
				"review": map[string]any{
					"verdict":  "reject",
					"blocking": true,
					"reason":   "tests fail",
					"sha":      "cafebabe",
				},
			},
		})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}

	if got := f.taskStatus(t, tid); got != pm.TaskCompleted {
		t.Fatalf("task status = %s, want completed", got)
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
		t.Fatalf("no review verdict recorded for %s", tid)
	}
	if got.Verdict != pm.ReviewReject || !got.Blocking || got.Reason != "tests fail" || got.SHA != "cafebabe" {
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

func TestCompleteTask_StageGateRejectAppendsRemediationAndReplayIsIdempotent(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	ctx := context.Background()
	pid, planIDRaw := f.seedPlanMember(t)
	planID := pm.PlanID(planIDRaw)

	spec := pm.DefaultHumanGateSpec(pm.IdentityRef("agent:" + atAgent1))
	spec.AcceptanceContract = "Reject until the reviewed SHA passes the acceptance suite."
	stageID, err := f.pmSvc.CreateStage(ctx, pmservice.CreateStageCommand{
		PlanID: planID, Name: "Build", MaxRounds: 3, GateSpec: spec,
		Actor: pm.IdentityRef("agent:" + atAgent1),
	})
	if err != nil {
		t.Fatal(err)
	}
	memberID := pm.TaskID(f.seedPlanTask(t, pid, planIDRaw))
	if err := f.pmSvc.AssignTaskToStage(ctx, planID, memberID, stageID,
		pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	if err := f.pmSvc.StartPlan(ctx, planID, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pmSvc.AdvancePlan(ctx, planID, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	if err := f.pmSvc.StartTask(ctx, memberID, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	if err := f.pmSvc.CompleteTask(ctx, memberID, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pmSvc.AdvancePlan(ctx, planID, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	stage, err := f.pmSvc.GetStage(ctx, stageID)
	if err != nil {
		t.Fatal(err)
	}
	gateTaskID := stage.Stage.GateTaskID()
	if err := f.pmSvc.StartTask(ctx, gateTaskID, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"agent_id": atAgent1,
		"task_id":  string(gateTaskID),
		"delivery": map[string]any{
			"summary":         "blocking regression remains",
			"outcome":         "reject",
			"idempotency_key": "handler-stage-reject-1",
			"review": map[string]any{
				"verdict": "reject", "blocking": true,
				"reason": "blocking regression remains", "sha": "3f956f29",
			},
		},
	}
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1", payload)
	if status != http.StatusOK {
		t.Fatalf("first reject status=%d body=%v", status, body)
	}
	member, err := f.pmSvc.GetTask(ctx, memberID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Status() != pm.TaskCompleted {
		t.Fatalf("member status after reject = %s, want immutable completed", member.Status())
	}
	stage, err = f.pmSvc.GetStage(ctx, stageID)
	if err != nil {
		t.Fatal(err)
	}
	if stage.Rounds != 0 {
		t.Fatalf("historical stage rounds changed after reject: %d", stage.Rounds)
	}
	stages, err := f.pmSvc.ListStagesForPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 {
		t.Fatalf("stages after reject = %d, want original + one remediation", len(stages))
	}
	var remediation *pmservice.StageDetail
	for _, candidate := range stages {
		if candidate.Stage.Generation() == 1 {
			remediation = candidate
		}
	}
	if remediation == nil || remediation.Stage.OriginVerdictID() == "" || len(remediation.Members) == 0 {
		t.Fatalf("missing lineage-bearing remediation stage: %+v", remediation)
	}
	if remediation.Stage.SupersedesStageID() != stageID || remediation.Stage.ContinuationID() == "" {
		t.Fatalf("remediation supersedes/continuation = %s/%s, want %s/non-empty", remediation.Stage.SupersedesStageID(), remediation.Stage.ContinuationID(), stageID)
	}
	var repairTaskID pm.TaskID
	for _, member := range remediation.Members {
		if member.TaskID != remediation.Stage.GateTaskID() {
			repairTaskID = member.TaskID
			break
		}
	}
	if repairTaskID == "" {
		t.Fatalf("remediation stage has no repair member: %+v", remediation.Members)
	}
	repairTask, err := f.pmSvc.GetTask(ctx, repairTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if repairTask.OriginVerdictID() != remediation.Stage.OriginVerdictID() || repairTask.FollowsTaskID() != gateTaskID {
		t.Fatalf("repair lineage verdict/follows=%s/%s, want %s/%s", repairTask.OriginVerdictID(), repairTask.FollowsTaskID(), remediation.Stage.OriginVerdictID(), gateTaskID)
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1", payload)
	if status != http.StatusOK {
		t.Fatalf("idempotent replay status=%d body=%v", status, body)
	}
	stages, err = f.pmSvc.ListStagesForPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 {
		t.Fatalf("identical replay appended duplicate remediation stages: %d", len(stages))
	}

	conflictPayload := map[string]any{
		"agent_id": atAgent1,
		"task_id":  string(gateTaskID),
		"delivery": map[string]any{
			"summary":         "different blocking regression with same key",
			"outcome":         "reject",
			"idempotency_key": "handler-stage-reject-1",
			"review": map[string]any{
				"verdict": "reject", "blocking": true,
				"reason": "different blocking regression with same key", "sha": "3f956f29",
			},
		},
	}
	status, body = postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1", conflictPayload)
	if status != http.StatusConflict {
		t.Fatalf("conflicting duplicate status=%d body=%v, want 409", status, body)
	}
	if body["error"] != "idempotency_conflict" {
		t.Fatalf("conflicting duplicate body=%v, want idempotency_conflict", body)
	}
	stages, err = f.pmSvc.ListStagesForPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 {
		t.Fatalf("conflicting duplicate mutated remediation topology: stages=%d", len(stages))
	}
}
