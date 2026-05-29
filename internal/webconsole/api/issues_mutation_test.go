// v2.5.x #61 — POST /api/issues (open-from-scratch branch) +
// POST /api/issues/{id}/conclude (Conclude flow, no_action + withdrawn
// paths; closed_with_tasks is exercised via the existing CLI integration
// tests since the spawner wiring is heavier than this surface needs).
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/discussion"
)

func TestAPI_OpenIssueFromScratch_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	// No project FK check on this path (lifecycle service's projectCheck
	// is unwired in setupAPI — see WithProjectExistenceChecker hook), so
	// any project_id is accepted.
	s := newTestServer(t, deps)
	defer s.Close()

	body := `{"project_id":"p-1","title":"login bug","description":"users can't sign in"}`
	resp := orgScopedPost(t, s.URL+"/api/issues", body, sess)
	if resp.StatusCode != 201 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"issue_id", "conversation_id", "event_id"} {
		if out[k] == nil || out[k] == "" {
			t.Fatalf("missing %s in %v", k, out)
		}
	}
	// Verify the issue is persisted by reading it back.
	issueID := out["issue_id"].(string)
	resp2 := orgScopedGet(t, s.URL+"/api/issues/"+issueID, sess)
	if resp2.StatusCode != 200 {
		t.Fatalf("show status=%d", resp2.StatusCode)
	}
	var iss map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&iss)
	if iss["title"] != "login bug" {
		t.Fatalf("title mismatch: %v", iss)
	}
	if iss["status"] != string(discussion.StatusOpen) {
		t.Fatalf("status mismatch: %v", iss)
	}
}

func TestAPI_OpenIssueFromScratch_MissingTitle(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"project_id":"p-1","description":"x"}`
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestAPI_OpenIssueFromScratch_MissingProjectID(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"title":"x"}`
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestAPI_OpenIssueFromScratch_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.IssueLifecycleSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"project_id":"p-1","title":"x"}`
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(body))
	if resp.StatusCode != 501 {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_ConcludeIssue_NoAction(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	s := newTestServer(t, deps)
	defer s.Close()

	// Open an issue first.
	openBody := `{"project_id":"p-1","title":"feature request"}`
	resp := orgScopedPost(t, s.URL+"/api/issues", openBody, sess)
	var openOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&openOut)
	issueID := openOut["issue_id"].(string)

	// Conclude with no_action.
	conclBody := `{"kind":"closed_no_action","summary":"decided not to do this"}`
	resp2 := orgScopedPost(t, s.URL+"/api/issues/"+issueID+"/conclude", conclBody, sess)
	if resp2.StatusCode != 200 {
		t.Fatalf("conclude status=%d", resp2.StatusCode)
	}
	var conclOut map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&conclOut)
	if conclOut["issue_id"] != issueID {
		t.Fatalf("issue_id mismatch: %v", conclOut)
	}

	// Verify the issue is now closed_no_action.
	resp3 := orgScopedGet(t, s.URL+"/api/issues/"+issueID, sess)
	var iss map[string]any
	_ = json.NewDecoder(resp3.Body).Decode(&iss)
	if iss["status"] != string(discussion.StatusClosedNoAction) {
		t.Fatalf("status not flipped to closed_no_action: %v", iss)
	}
}

func TestAPI_ConcludeIssue_Withdrawn(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	s := newTestServer(t, deps)
	defer s.Close()

	openBody := `{"project_id":"p-1","title":"oops"}`
	resp := orgScopedPost(t, s.URL+"/api/issues", openBody, sess)
	var openOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&openOut)
	issueID := openOut["issue_id"].(string)

	conclBody := `{"kind":"withdrawn","summary":"never mind"}`
	resp2 := orgScopedPost(t, s.URL+"/api/issues/"+issueID+"/conclude", conclBody, sess)
	if resp2.StatusCode != 200 {
		t.Fatalf("withdraw status=%d", resp2.StatusCode)
	}

	resp3 := orgScopedGet(t, s.URL+"/api/issues/"+issueID, sess)
	var iss map[string]any
	_ = json.NewDecoder(resp3.Body).Decode(&iss)
	if iss["status"] != string(discussion.StatusWithdrawn) {
		t.Fatalf("status not flipped: %v", iss)
	}
}

func TestAPI_ConcludeIssue_InvalidKind(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"kind":"bogus","summary":"x"}`
	resp, _ := http.Post(s.URL+"/api/issues/I-1/conclude", "application/json", strings.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestAPI_ConcludeIssue_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.IssueLifecycleSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"kind":"closed_no_action","summary":"x"}`
	resp, _ := http.Post(s.URL+"/api/issues/I-1/conclude", "application/json", strings.NewReader(body))
	if resp.StatusCode != 501 {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

// v2.5.x #64 — Edit + Reopen tests.

func TestAPI_UpdateIssue_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	s := newTestServer(t, deps)
	defer s.Close()
	openBody := `{"project_id":"p-1","title":"old title"}`
	resp := orgScopedPost(t, s.URL+"/api/issues", openBody, sess)
	var openOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&openOut)
	id := openOut["issue_id"].(string)
	patch := `{"title":"new title","description":"new desc"}`
	r2 := orgScopedPatch(t, s.URL+"/api/issues/"+id, patch, sess)
	if r2.StatusCode != 200 {
		t.Fatalf("status=%d", r2.StatusCode)
	}
	r3 := orgScopedGet(t, s.URL+"/api/issues/"+id, sess)
	var iss map[string]any
	_ = json.NewDecoder(r3.Body).Decode(&iss)
	if iss["title"] != "new title" {
		t.Fatalf("title=%v", iss["title"])
	}
}

func TestAPI_UpdateIssue_TerminalRejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/issues", `{"project_id":"p-1","title":"x"}`, sess)
	var openOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&openOut)
	id := openOut["issue_id"].(string)
	// Conclude first → terminal.
	_ = orgScopedPost(t, s.URL+"/api/issues/"+id+"/conclude", `{"kind":"closed_no_action","summary":"x"}`, sess)
	// Edit should reject.
	r2 := orgScopedPatch(t, s.URL+"/api/issues/"+id, `{"title":"new"}`, sess)
	if r2.StatusCode == 200 {
		t.Fatalf("status=%d expected non-200", r2.StatusCode)
	}
}

func TestAPI_UpdateIssue_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.IssueLifecycleSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("PATCH", s.URL+"/api/issues/I-1", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 501 {
		t.Fatalf("status=%d", r.StatusCode)
	}
}

func TestAPI_ReopenIssue_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/issues", `{"project_id":"p-1","title":"x"}`, sess)
	var openOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&openOut)
	id := openOut["issue_id"].(string)
	// Conclude.
	_ = orgScopedPost(t, s.URL+"/api/issues/"+id+"/conclude", `{"kind":"closed_no_action","summary":"x"}`, sess)
	// Reopen.
	r2 := orgScopedPost(t, s.URL+"/api/issues/"+id+"/reopen", `{}`, sess)
	if r2.StatusCode != 200 {
		t.Fatalf("status=%d", r2.StatusCode)
	}
	r3 := orgScopedGet(t, s.URL+"/api/issues/"+id, sess)
	var iss map[string]any
	_ = json.NewDecoder(r3.Body).Decode(&iss)
	if iss["status"] != string(discussion.StatusOpen) {
		t.Fatalf("status=%v want open", iss["status"])
	}
}

func TestAPI_ReopenIssue_NonTerminalRejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/issues", `{"project_id":"p-1","title":"x"}`, sess)
	var openOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&openOut)
	id := openOut["issue_id"].(string)
	// Already open — reopen should reject.
	r2 := orgScopedPost(t, s.URL+"/api/issues/"+id+"/reopen", `{}`, sess)
	if r2.StatusCode == 200 {
		t.Fatalf("status=%d expected non-200 (already open)", r2.StatusCode)
	}
}

func TestAPI_ReopenIssue_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.IssueLifecycleSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	r, _ := http.Post(s.URL+"/api/issues/I-1/reopen", "application/json", strings.NewReader(`{}`))
	if r.StatusCode != 501 {
		t.Fatalf("status=%d", r.StatusCode)
	}
}

// Sanity: derive path is unaffected by the open-from-scratch branch.
// Specifically, the previous deriveIssueHandler entry point still fires
// when source_conversation_id is provided.
func TestAPI_PostIssues_DerivePath_StillRoutes(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.DerivationSvc = nil // drop derive svc so we get 501 not nil-deref
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"source_conversation_id":"C-1","source_message_ids":["M-1"],"project_id":"p-1","title":"x"}`
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(body))
	if resp.StatusCode != 501 {
		t.Fatalf("status=%d want 501 (derive_not_wired)", resp.StatusCode)
	}
}
