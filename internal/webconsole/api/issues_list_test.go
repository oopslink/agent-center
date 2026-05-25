// v2.3-5a — `GET /api/issues` + `GET /api/issues/{id}` (BC-native read).
// Discussion BC owns the Issue projection; SPA #5b cuts its
// /conversations?kind=issue read over to these endpoints.
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/discussion"
)

// seedIssue saves one Issue AR via the wired IssueRepo. Returns the
// AR so callers can assert against its fields.
func seedIssue(t *testing.T, deps HandlerDeps, id, projectID, title string, status discussion.Status) *discussion.Issue {
	t.Helper()
	is, err := discussion.NewIssue(discussion.NewIssueInput{
		ID:                 discussion.IssueID(id),
		ProjectID:          projectID,
		Title:              title,
		OpenedByIdentityID: "user:hayang",
		Origin:             discussion.OriginCLI,
		OpenedAt:           time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != "" && status != discussion.StatusOpen {
		// Rehydrate at the requested status so we don't have to walk the
		// full lifecycle for filter assertions. We use Rehydrate because
		// the AR's MarkUnderDiscussion / Withdraw / Conclude operations
		// require additional state we don't need to exercise here.
		is, err = discussion.RehydrateIssue(discussion.RehydrateIssueInput{
			ID:                 is.ID(),
			ProjectID:          is.ProjectID(),
			Title:              is.Title(),
			OpenedByIdentityID: is.OpenedByIdentityID(),
			Origin:             is.Origin(),
			OpenedAt:           is.OpenedAt(),
			Status:             status,
			CreatedAt:          is.CreatedAt(),
			UpdatedAt:          is.UpdatedAt(),
			Version:            1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := deps.IssueRepo.Save(context.Background(), is); err != nil {
		t.Fatal(err)
	}
	return is
}

func TestAPI_ListIssues_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	seedIssue(t, deps, "I-1", "p-1", "first", discussion.StatusOpen)
	seedIssue(t, deps, "I-2", "p-1", "second", discussion.StatusOpen)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, err := http.Get(s.URL + "/api/issues?project_id=p-1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 2 {
		t.Fatalf("len=%d want 2", len(arr))
	}
	// Projection shape: id, project_id, conversation_id, title, status,
	// opened_at, opener (and conditional closed_at / closed_reason which
	// should be absent for open issues).
	row := arr[0]
	for _, k := range []string{"id", "project_id", "title", "status", "opened_at", "opener"} {
		if _, ok := row[k]; !ok {
			t.Fatalf("missing field %q: %v", k, row)
		}
	}
	if _, ok := row["closed_at"]; ok {
		t.Fatalf("open issue must not have closed_at: %v", row)
	}
	// Issue AR has neither a "kind" getter nor a "priority" getter —
	// the projection MUST NOT invent them.
	if _, ok := row["kind"]; ok {
		t.Fatalf("projection must not include kind: %v", row)
	}
	if _, ok := row["priority"]; ok {
		t.Fatalf("projection must not include priority: %v", row)
	}
}

func TestAPI_ListIssues_StatusFilter(t *testing.T) {
	deps, _ := setupAPI(t)
	seedIssue(t, deps, "I-1", "p-1", "open one", discussion.StatusOpen)
	seedIssue(t, deps, "I-2", "p-1", "underdisc", discussion.StatusUnderDiscussion)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues?project_id=p-1&status=open")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 1 {
		t.Fatalf("len=%d want 1 (status filter to open)", len(arr))
	}
	if arr[0]["status"] != "open" {
		t.Fatalf("wrong status: %v", arr[0])
	}
}

func TestAPI_ListIssues_MissingProjectID_400(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "missing_project_id" {
		t.Fatalf("err=%v want missing_project_id", body["error"])
	}
}

func TestAPI_ListIssues_EmptyResult(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues?project_id=p-empty")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// Must serialise as `[]`, not `null` — SPA assumes array.
	if string(body) != "[]\n" && string(body) != "[]" {
		t.Fatalf("expected []; got %q", body)
	}
}

func TestAPI_ListIssues_RepoNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.IssueRepo = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues?project_id=p-1")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_ListIssues_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues?project_id=p-1")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", resp.StatusCode)
	}
}

func TestAPI_ShowIssue_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	seedIssue(t, deps, "I-1", "p-1", "the issue", discussion.StatusOpen)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues/I-1")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["id"] != "I-1" || got["project_id"] != "p-1" || got["title"] != "the issue" {
		t.Fatalf("bad: %v", got)
	}
}

func TestAPI_ShowIssue_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues/ghost")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", resp.StatusCode)
	}
}

func TestAPI_ShowIssue_RepoNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.IssueRepo = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues/I-1")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_ShowIssue_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/issues/I-x")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", resp.StatusCode)
	}
}

// Projection helper: a withdrawn issue includes both closed_at +
// closed_reason. Conclude-path concluded_at is intentionally surfaced
// via the same `closed_at` field; the helper unit test below pins the
// shape directly.
func TestIssuePublicMap_WithdrawnHasClosedFields(t *testing.T) {
	at := time.Date(2026, 5, 24, 11, 0, 0, 0, time.UTC)
	is, err := discussion.RehydrateIssue(discussion.RehydrateIssueInput{
		ID:                    "I-W",
		ProjectID:             "p-1",
		Title:                 "withdrawn",
		OpenedByIdentityID:    "user:hayang",
		Origin:                discussion.OriginCLI,
		OpenedAt:              at,
		Status:                discussion.StatusWithdrawn,
		ConcludedAt:           &at,
		ConcludedByIdentityID: "user:hayang",
		WithdrawReason:        "duplicate",
		WithdrawMessage:       "covered by I-2",
		CreatedAt:             at,
		UpdatedAt:             at,
		Version:               2,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := issuePublicMap(is)
	if m["closed_at"] == nil {
		t.Fatalf("expected closed_at: %v", m)
	}
	if m["closed_reason"] != "duplicate" {
		t.Fatalf("closed_reason=%v want duplicate", m["closed_reason"])
	}
	if m["status"] != "withdrawn" {
		t.Fatalf("status=%v", m["status"])
	}
}
