// v2.3-5a — `GET /api/tasks` + `GET /api/tasks/{id}` (BC-native read).
// TaskRuntime BC owns the Task projection; SPA #5b switches its
// /conversations?kind=task feed to these endpoints.
//
// Coexistence note: `GET /api/tasks/{id}/trace` predates these
// endpoints. net/http's pattern matcher resolves the longer pattern
// first, so `/api/tasks/{id}` does not shadow `/trace`. Both are
// exercised in this package's tests.
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func seedTask(t *testing.T, deps HandlerDeps, id, projectID, title string, status task.Status, priority task.Priority) *task.Task {
	t.Helper()
	at := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	tk, err := task.New(task.NewInput{
		ID: taskruntime.TaskID(id), ProjectID: projectID, Title: title,
		Priority: priority, CreatedBy: "user:hayang", Now: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != "" && status != task.StatusOpen {
		tk, err = task.Rehydrate(task.RehydrateInput{
			ID: tk.ID(), ProjectID: tk.ProjectID(), Title: tk.Title(),
			Status: status, Priority: tk.Priority(),
			CreatedBy: tk.CreatedBy(),
			CreatedAt: tk.CreatedAt(), UpdatedAt: tk.UpdatedAt(),
			Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := deps.TaskRepo.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestAPI_ListTasks_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	seedTask(t, deps, "T-1", "p-1", "first", task.StatusOpen, task.PriorityHigh)
	seedTask(t, deps, "T-2", "p-1", "second", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, err := http.Get(s.URL + "/api/tasks?project_id=p-1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 2 {
		t.Fatalf("len=%d want 2", len(arr))
	}
	row := arr[0]
	for _, k := range []string{"id", "project_id", "title", "status", "priority", "created_at"} {
		if _, ok := row[k]; !ok {
			t.Fatalf("missing %q: %v", k, row)
		}
	}
	if _, ok := row["current_execution_id"]; ok {
		t.Fatalf("inactive task must not have current_execution_id: %v", row)
	}
	if _, ok := row["depends_on_task_ids"]; ok {
		t.Fatalf("no-deps task must not have depends_on_task_ids: %v", row)
	}
}

func TestAPI_ListTasks_StatusFilter(t *testing.T) {
	deps, _ := setupAPI(t)
	seedTask(t, deps, "T-1", "p-1", "open", task.StatusOpen, task.PriorityMedium)
	seedTask(t, deps, "T-2", "p-1", "done", task.StatusDone, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks?project_id=p-1&status=done")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 1 {
		t.Fatalf("len=%d want 1", len(arr))
	}
	if arr[0]["status"] != "done" {
		t.Fatalf("status=%v want done", arr[0]["status"])
	}
}

func TestAPI_ListTasks_MissingProjectID_400(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "missing_project_id" {
		t.Fatalf("err=%v", body["error"])
	}
}

func TestAPI_ListTasks_EmptyResult(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks?project_id=p-empty")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "[]\n" && string(body) != "[]" {
		t.Fatalf("expected []; got %q", body)
	}
}

func TestAPI_ListTasks_RepoNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.TaskRepo = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks?project_id=p-1")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_ListTasks_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks?project_id=p-1")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", resp.StatusCode)
	}
}

func TestAPI_ShowTask_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	seedTask(t, deps, "T-1", "p-1", "the task", task.StatusOpen, task.PriorityHigh)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks/T-1")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["id"] != "T-1" || got["project_id"] != "p-1" || got["title"] != "the task" {
		t.Fatalf("bad: %v", got)
	}
	if got["priority"] != "high" {
		t.Fatalf("priority=%v want high", got["priority"])
	}
}

func TestAPI_ShowTask_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks/ghost")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", resp.StatusCode)
	}
}

func TestAPI_ShowTask_RepoNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.TaskRepo = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks/T-1")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_ShowTask_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks/T-1")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", resp.StatusCode)
	}
}

// Coexistence guard: the existing `/api/tasks/{id}/trace` route must
// still match before the new `/api/tasks/{id}` detail route. If a
// future refactor breaks the registration order, this test fails.
func TestAPI_ShowTask_CoexistsWithTrace(t *testing.T) {
	deps, _ := setupAPI(t)
	seedTask(t, deps, "T-trace", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	// /trace should still hit the query endpoint, not the detail handler.
	resp, _ := http.Get(s.URL + "/api/tasks/T-trace/trace")
	if resp.StatusCode != 200 {
		t.Fatalf("/trace status=%d", resp.StatusCode)
	}
	var traceBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&traceBody)
	if traceBody["resource"] != "events" {
		t.Fatalf("trace handler payload missing 'resource':events — detail handler shadowed /trace? body=%v", traceBody)
	}
	// /detail should hit the new detail handler.
	resp, _ = http.Get(s.URL + "/api/tasks/T-trace")
	if resp.StatusCode != 200 {
		t.Fatalf("detail status=%d", resp.StatusCode)
	}
	var detail map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&detail)
	if detail["id"] != "T-trace" {
		t.Fatalf("detail body wrong: %v", detail)
	}
}

// Projection helper unit test: a task with deps + active execution
// surfaces both addenda; without them, the addenda are omitted.
func TestTaskPublicMap_ActiveExecutionAndDeps(t *testing.T) {
	at := time.Date(2026, 5, 24, 11, 0, 0, 0, time.UTC)
	tk, err := task.Rehydrate(task.RehydrateInput{
		ID: "T-A", ProjectID: "p-1", Title: "with deps",
		Status: task.StatusOpen, Priority: task.PriorityHigh,
		DependsOnTaskIDs:   []taskruntime.TaskID{"T-x", "T-y"},
		CurrentExecutionID: taskruntime.TaskExecutionID("E-99"),
		CreatedBy:          "user:hayang",
		CreatedAt:          at, UpdatedAt: at, Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := taskPublicMap(tk)
	if m["current_execution_id"] != "E-99" {
		t.Fatalf("current_execution_id: %v", m)
	}
	deps, ok := m["depends_on_task_ids"].([]string)
	if !ok || len(deps) != 2 {
		t.Fatalf("depends_on_task_ids: %v", m)
	}
}
