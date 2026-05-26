// v2.5.x #62 — POST /api/tasks (create-from-scratch branch) +
// POST /api/tasks/{id}/{suspend|resume|abandon}. Edit (PATCH metadata)
// is deferred to follow-up #65 — needs Task AR.UpdateMetadata.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func TestAPI_CreateTaskFromScratch_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"project_id":"p-1","title":"fix login","description":"x"}`
	resp, err := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["task_id"] == nil || out["task_id"] == "" {
		t.Fatalf("missing task_id: %v", out)
	}
}

func TestAPI_CreateTaskFromScratch_MissingTitle(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"project_id":"p-1"}`
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestAPI_CreateTaskFromScratch_MissingProject(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"title":"x"}`
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestAPI_CreateTaskFromScratch_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.TaskSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"project_id":"p-1","title":"x"}`
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if resp.StatusCode != 501 {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_SuspendTask_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	tk := seedTask(t, deps, "tk-1", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/tasks/"+string(tk.ID())+"/suspend",
		"application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	// Verify status flipped.
	got, err := deps.TaskRepo.FindByID(context.Background(), tk.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != task.StatusSuspended {
		t.Fatalf("status=%s want suspended", got.Status())
	}
}

func TestAPI_SuspendTask_AlreadySuspended_Rejected(t *testing.T) {
	deps, _ := setupAPI(t)
	tk := seedTask(t, deps, "tk-2", "p-1", "x", task.StatusSuspended, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/tasks/"+string(tk.ID())+"/suspend",
		"application/json", strings.NewReader(`{}`))
	if resp.StatusCode == 200 {
		t.Fatalf("status=%d expected non-200 (AR rejects suspend on non-open)", resp.StatusCode)
	}
}

func TestAPI_ResumeTask_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	tk := seedTask(t, deps, "tk-3", "p-1", "x", task.StatusSuspended, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/tasks/"+string(tk.ID())+"/resume",
		"application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	got, _ := deps.TaskRepo.FindByID(context.Background(), tk.ID())
	if got.Status() != task.StatusOpen {
		t.Fatalf("status=%s want open", got.Status())
	}
}

func TestAPI_AbandonTask_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	tk := seedTask(t, deps, "tk-4", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"reason":"obsolete","message":"requirements changed"}`
	resp, _ := http.Post(s.URL+"/api/tasks/"+string(tk.ID())+"/abandon",
		"application/json", strings.NewReader(body))
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	got, _ := deps.TaskRepo.FindByID(context.Background(), tk.ID())
	if got.Status() != task.StatusAbandoned {
		t.Fatalf("status=%s want abandoned", got.Status())
	}
}

func TestAPI_AbandonTask_MissingReason(t *testing.T) {
	deps, _ := setupAPI(t)
	tk := seedTask(t, deps, "tk-5", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"message":"x"}`
	resp, _ := http.Post(s.URL+"/api/tasks/"+string(tk.ID())+"/abandon",
		"application/json", strings.NewReader(body))
	if resp.StatusCode == 200 {
		t.Fatalf("status=%d expected non-200", resp.StatusCode)
	}
}

func TestAPI_TaskLifecycle_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.TaskSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/tasks/tk/suspend", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 501 {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

