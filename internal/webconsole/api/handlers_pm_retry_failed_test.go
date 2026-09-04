package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func decodeWebBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPMRetryFailedTask_WebContract(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	mkRunning := func(title string) pm.TaskID {
		t.Helper()
		tid, terr := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: caller})
		if terr != nil {
			t.Fatal(terr)
		}
		if terr := deps.PM.AssignTask(ctx, tid, caller, caller); terr != nil {
			t.Fatal(terr)
		}
		if terr := deps.PM.StartTask(ctx, tid, caller); terr != nil {
			t.Fatal(terr)
		}
		return tid
	}
	post := func(tid pm.TaskID, suffix string) (int, map[string]any) {
		resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks/"+string(tid)+suffix, `{}`, sess)
		return resp.StatusCode, decodeWebBody(t, resp)
	}

	standaloneFailed := mkRunning("standalone failed")
	if err := deps.PM.FailTask(ctx, standaloneFailed, "failed attempt", caller); err != nil {
		t.Fatal(err)
	}
	status, body := post(standaloneFailed, "/retry_failed")
	if status != http.StatusOK || body["status"] != "open" {
		t.Fatalf("retry_failed status=%d body=%v, want 200 open", status, body)
	}
	status, body = post(standaloneFailed, "/retry_failed")
	if status != http.StatusOK || body["status"] != "open" {
		t.Fatalf("open idempotent retry status=%d body=%v, want 200 open", status, body)
	}

	legacyFailed := mkRunning("legacy alias")
	if err := deps.PM.FailTask(ctx, legacyFailed, "failed attempt", caller); err != nil {
		t.Fatal(err)
	}
	status, body = post(legacyFailed, "/reopen")
	if status != http.StatusOK || body["status"] != "open" {
		t.Fatalf("legacy reopen alias status=%d body=%v, want 200 open", status, body)
	}

	planBound := mkRunning("plan bound")
	if err := deps.PM.FailTask(ctx, planBound, "failed in plan", caller); err != nil {
		t.Fatal(err)
	}
	taskRepo := pmsql.NewTaskRepo(db)
	task, err := taskRepo.FindByID(ctx, planBound)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.SetPlan("PL1", task.UpdatedAt()); err != nil {
		t.Fatal(err)
	}
	if err := taskRepo.Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	status, body = post(planBound, "/retry_failed")
	if status != http.StatusConflict || body["error"] != "task_retry_plan_bound" {
		t.Fatalf("plan-bound retry status=%d body=%v, want 409 task_retry_plan_bound", status, body)
	}

	for _, tc := range []struct {
		name   string
		status pm.TaskStatus
		prep   func(*testing.T, pm.TaskID)
	}{
		{name: "completed", status: pm.TaskCompleted, prep: func(t *testing.T, tid pm.TaskID) {
			if err := deps.PM.CompleteTask(ctx, tid, caller); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "discarded", status: pm.TaskDiscarded, prep: func(t *testing.T, tid pm.TaskID) {
			if err := deps.PM.DiscardTaskWithReason(ctx, tid, caller, "not needed"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "running", status: pm.TaskRunning, prep: func(*testing.T, pm.TaskID) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tid := mkRunning(tc.name)
			tc.prep(t, tid)
			status, body := post(tid, "/retry_failed")
			if status != http.StatusConflict || body["error"] != "task_retry_not_applicable" {
				t.Fatalf("retry %s status=%d body=%v, want 409 task_retry_not_applicable", tc.name, status, body)
			}
			got, err := deps.PM.GetTask(ctx, tid)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status() != tc.status {
				t.Fatalf("task status=%s want %s", got.Status(), tc.status)
			}
		})
	}
}
