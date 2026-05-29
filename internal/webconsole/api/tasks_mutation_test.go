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
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/tasks", `{"project_id":"p-1","title":"fix login","description":"x"}`, sess)
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

// v2.5.16 (#69): POST /api/tasks/{id}/bind-conversation creates and
// attaches a Conversation in auto mode for a task that was created
// without one (the legacy /api/tasks path used to default
// with_conversation=false). After binding, the projection's
// conversation_id is populated.
func TestAPI_BindTaskConversation_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	tk := seedTask(t, deps, "tk-bind", "p-1", "feat abc", task.StatusOpen, task.PriorityMedium)
	if tk.ConversationID() != "" {
		t.Fatalf("precondition: seed task should have no conversation, got %q", tk.ConversationID())
	}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/tasks/"+string(tk.ID())+"/bind-conversation", `{}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	convID, _ := out["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("missing conversation_id in response: %v", out)
	}
	// Repo state mirrors the response.
	got, err := deps.TaskRepo.FindByID(context.Background(), tk.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.ConversationID() != convID {
		t.Fatalf("task.conversation_id=%q want %q", got.ConversationID(), convID)
	}
}

func TestAPI_BindTaskConversation_AlreadyBound_Rejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	tk := seedTask(t, deps, "tk-already", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	// First bind: succeeds.
	if resp := orgScopedPost(t, s.URL+"/api/tasks/"+string(tk.ID())+"/bind-conversation", `{}`, sess); resp.StatusCode != 200 {
		t.Fatalf("first bind status=%d", resp.StatusCode)
	}
	// Second bind: AR rejects (no unbind in v1).
	resp := orgScopedPost(t, s.URL+"/api/tasks/"+string(tk.ID())+"/bind-conversation", `{}`, sess)
	if resp.StatusCode == 200 {
		t.Fatalf("second bind unexpectedly succeeded; expected rejection")
	}
}

func TestAPI_BindTaskConversation_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.TaskSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(
		s.URL+"/api/tasks/whatever/bind-conversation",
		"application/json",
		strings.NewReader(`{}`),
	)
	if resp.StatusCode != 501 {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_SuspendTask_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	tk := seedTask(t, deps, "tk-1", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/tasks/"+string(tk.ID())+"/suspend", `{}`, sess)
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
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	tk := seedTask(t, deps, "tk-3", "p-1", "x", task.StatusSuspended, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/tasks/"+string(tk.ID())+"/resume", `{}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	got, _ := deps.TaskRepo.FindByID(context.Background(), tk.ID())
	if got.Status() != task.StatusOpen {
		t.Fatalf("status=%s want open", got.Status())
	}
}

func TestAPI_AbandonTask_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	tk := seedTask(t, deps, "tk-4", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"reason":"obsolete","message":"requirements changed"}`
	resp := orgScopedPost(t, s.URL+"/api/tasks/"+string(tk.ID())+"/abandon", body, sess)
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

func TestAPI_UpdateTask_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	tk := seedTask(t, deps, "tk-edit", "p-1", "old title", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"title":"new title","description":"new desc","priority":"high"}`
	resp := orgScopedPatch(t, s.URL+"/api/tasks/"+string(tk.ID()), body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	got, err := deps.TaskRepo.FindByID(context.Background(), tk.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "new title" {
		t.Fatalf("title=%q want %q", got.Title(), "new title")
	}
	if got.Description() != "new desc" {
		t.Fatalf("description=%q", got.Description())
	}
	if got.Priority() != task.PriorityHigh {
		t.Fatalf("priority=%s", got.Priority())
	}
}

func TestAPI_UpdateTask_MissingTitle(t *testing.T) {
	deps, _ := setupAPI(t)
	tk := seedTask(t, deps, "tk-edit2", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("PATCH", s.URL+"/api/tasks/"+string(tk.ID()),
		strings.NewReader(`{"description":"x","priority":"medium"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode == 200 {
		t.Fatalf("status=%d expected non-200", resp.StatusCode)
	}
}

func TestAPI_UpdateTask_TerminalRejected(t *testing.T) {
	deps, _ := setupAPI(t)
	// Seed a task and abandon it via the service so it lands in terminal.
	tk := seedTask(t, deps, "tk-edit3", "p-1", "x", task.StatusOpen, task.PriorityMedium)
	s := newTestServer(t, deps)
	defer s.Close()
	// Abandon first.
	abandon := `{"reason":"obsolete","message":"x"}`
	_, _ = http.Post(s.URL+"/api/tasks/"+string(tk.ID())+"/abandon",
		"application/json", strings.NewReader(abandon))
	// Edit should now fail.
	req, _ := http.NewRequest("PATCH", s.URL+"/api/tasks/"+string(tk.ID()),
		strings.NewReader(`{"title":"new","priority":"medium"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode == 200 {
		t.Fatalf("status=%d expected non-200 (terminal task)", resp.StatusCode)
	}
}

func TestAPI_UpdateTask_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.TaskSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("PATCH", s.URL+"/api/tasks/tk", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 501 {
		t.Fatalf("status=%d want 501", resp.StatusCode)
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
