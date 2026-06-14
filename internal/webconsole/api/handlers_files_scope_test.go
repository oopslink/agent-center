package api

// v2.10.0 [T73]: task/issue-scoped file attachment endpoints —
// list / create-upload / complete, project-member-gated (fail-closed).

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/oopslink/agent-center/internal/files"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// uploadScopedBlob runs the task/issue attachment flow (create → put → complete)
// against the nested endpoints and returns the file ULID. `coll` is "tasks" or
// "issues"; `scopeID` is the task/issue id.
func uploadScopedBlob(t *testing.T, baseURL, coll, pid, scopeID string, sess testSession, content []byte, filename string) string {
	t.Helper()
	base := fmt.Sprintf("%s/api/projects/%s/%s/%s/files", baseURL, pid, coll, scopeID)
	// create
	resp := orgScopedPost(t, base, `{"content_type":"text/plain","size":`+itoa(len(content))+`}`, sess)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create scoped upload: status=%d body=%s", resp.StatusCode, b)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	transferID, _ := created["transfer_id"].(string)
	fileURI, _ := created["file_uri"].(string)
	if transferID == "" || fileURI == "" {
		t.Fatalf("missing ids in create response: %v", created)
	}
	// put bytes (generic transfer route)
	resp = orgScopedPut(t, baseURL+"/api/files/transfer/"+transferID, content, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("put blob: status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	// complete (nested → creates the scope reference)
	resp = orgScopedPost(t, base+"/transfer/"+transferID+"/complete",
		fmt.Sprintf(`{"size":%d,"filename":%q}`, len(content), filename), sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("complete scoped upload: status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	return files.FileURI(fileURI).ULID()
}

// TestAPI_TaskFiles_Roundtrip: a project member uploads a task file, lists it,
// and downloads it through the generic reachability-gated download route.
func TestAPI_TaskFiles_Roundtrip(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := t.Context()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Mine", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "t", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	ulid := uploadScopedBlob(t, s.URL, "tasks", string(pid), string(tid), sess, []byte("hello task"), "note.txt")

	// list shows the file
	resp := orgScopedGet(t, fmt.Sprintf("%s/api/projects/%s/tasks/%s/files", s.URL, pid, tid), sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list: status=%d body=%s", resp.StatusCode, b)
	}
	var listed struct {
		Files []map[string]any `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed.Files) != 1 {
		t.Fatalf("want 1 file, got %d: %v", len(listed.Files), listed.Files)
	}
	if listed.Files[0]["filename"] != "note.txt" {
		t.Fatalf("want filename note.txt, got %v", listed.Files[0]["filename"])
	}

	// download via the generic reachability-gated route succeeds (task ref → member)
	resp = orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	body := responseBytes(t, resp)
	if resp.StatusCode != 200 {
		t.Fatalf("download: status=%d body=%s", resp.StatusCode, body)
	}
	if string(body) != "hello task" {
		t.Fatalf("download body mismatch: %q", body)
	}
}

// TestAPI_TaskFiles_NonMember_Forbidden: a caller who is not a member of the
// task's project cannot list or create-upload task files (403 fail-closed).
func TestAPI_TaskFiles_NonMember_Forbidden(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := t.Context()

	// Project + task owned by someone else; the caller is NOT a member.
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Foreign", CreatedBy: pm.IdentityRef("user:stranger"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "t", CreatedBy: pm.IdentityRef("user:stranger"),
	})
	if err != nil {
		t.Fatal(err)
	}

	listURL := fmt.Sprintf("%s/api/projects/%s/tasks/%s/files", s.URL, pid, tid)
	if resp := orgScopedGet(t, listURL, sess); resp.StatusCode != 403 {
		b := responseBytes(t, resp)
		t.Fatalf("non-member list: want 403, got %d body=%s", resp.StatusCode, b)
	}
	if resp := orgScopedPost(t, listURL, `{"content_type":"text/plain","size":3}`, sess); resp.StatusCode != 403 {
		b := responseBytes(t, resp)
		t.Fatalf("non-member create: want 403, got %d body=%s", resp.StatusCode, b)
	}
}

// TestAPI_TaskFiles_MissingOrCrossProject_404: an unknown task, or a task that
// belongs to a different project than the {pid} in the route, is 404
// (existence-non-disclosure).
func TestAPI_TaskFiles_MissingOrCrossProject_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := t.Context()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pidA, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "A", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	pidB, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "B", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A task in project B.
	tidB, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pidB, Title: "t", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Unknown task id under project A → 404.
	if resp := orgScopedGet(t, fmt.Sprintf("%s/api/projects/%s/tasks/task-does-not-exist/files", s.URL, pidA), sess); resp.StatusCode != 404 {
		b := responseBytes(t, resp)
		t.Fatalf("unknown task: want 404, got %d body=%s", resp.StatusCode, b)
	}
	// Real task, but routed under the WRONG project (A instead of B) → 404.
	if resp := orgScopedGet(t, fmt.Sprintf("%s/api/projects/%s/tasks/%s/files", s.URL, pidA, tidB), sess); resp.StatusCode != 404 {
		b := responseBytes(t, resp)
		t.Fatalf("cross-project task: want 404, got %d body=%s", resp.StatusCode, b)
	}
}

// TestAPI_IssueFiles_Roundtrip: the issue scope works the same as task scope.
func TestAPI_IssueFiles_Roundtrip(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := t.Context()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Mine", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "i", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	ulid := uploadScopedBlob(t, s.URL, "issues", string(pid), string(iid), sess, []byte("issue file"), "spec.txt")

	resp := orgScopedGet(t, fmt.Sprintf("%s/api/projects/%s/issues/%s/files", s.URL, pid, iid), sess)
	if resp.StatusCode != 200 {
		b := responseBytes(t, resp)
		t.Fatalf("issue list: status=%d body=%s", resp.StatusCode, b)
	}
	var listed struct {
		Files []map[string]any `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed.Files) != 1 || listed.Files[0]["filename"] != "spec.txt" {
		t.Fatalf("want 1 issue file spec.txt, got %v", listed.Files)
	}

	resp = orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	body := responseBytes(t, resp)
	if resp.StatusCode != 200 || string(body) != "issue file" {
		t.Fatalf("issue download: status=%d body=%q", resp.StatusCode, body)
	}
}
