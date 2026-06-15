package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// T131: the PROJECT-scoped Task / Issue lists accept the SAME filter params as
// the org-wide lists (only the project dimension is fixed by the path), so the
// project Workspace lists reach filter parity with the global lists. These guard
// the assignee + time-range filters (tasks) and the terminal-default + status
// filter (issues) on the project-scoped endpoints.

func TestPM_ListTasks_ProjectFilters_AssigneeAndTime(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	presp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"P"}`, sess)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("create project status=%d", presp.StatusCode)
	}
	var pc map[string]any
	json.NewDecoder(presp.Body).Decode(&pc)
	pid := pc["id"].(string)
	base := s.URL + "/api/projects/" + pid

	mkTask := func(title string) string {
		resp := orgScopedPost(t, base+"/tasks", `{"title":"`+title+`"}`, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create task %q status=%d", title, resp.StatusCode)
		}
		var c map[string]any
		json.NewDecoder(resp.Body).Decode(&c)
		return c["id"].(string)
	}
	list := func(query string) map[string]bool {
		resp := orgScopedGet(t, base+"/tasks"+query, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list %q status=%d", query, resp.StatusCode)
		}
		var l struct {
			Tasks []map[string]any `json:"tasks"`
		}
		json.NewDecoder(resp.Body).Decode(&l)
		ids := map[string]bool{}
		for _, tk := range l.Tasks {
			ids[tk["id"].(string)] = true
		}
		return ids
	}

	assignedT := mkTask("assigned one")
	unassignedT := mkTask("unassigned one")
	meRef := "user:" + sess.IdentityID
	if r := orgScopedPost(t, base+"/tasks/"+assignedT+"/assign", `{"assignee":"`+meRef+`"}`, sess); r.StatusCode != http.StatusOK {
		t.Fatalf("assign status=%d", r.StatusCode)
	}

	// ?assignee=<me> → only the assigned task.
	if got := list("?assignee=" + meRef); !got[assignedT] || got[unassignedT] {
		t.Fatalf("?assignee filter should be exactly the assigned task, got %+v", got)
	}

	// Time-range: created_after far in the past → both; created_before far in the
	// past → none (parity with the org list's tz-safe absolute-instant compare).
	if all := list("?created_after=2000-01-01T00:00:00Z"); !all[assignedT] || !all[unassignedT] {
		t.Fatalf("created_after past should include all, got %+v", all)
	}
	if none := list("?created_before=2000-01-01T00:00:00Z"); len(none) != 0 {
		t.Fatalf("created_before past should be empty, got %+v", none)
	}

	// A malformed time param is a 400 (never silently ignored → over-return).
	if r := orgScopedGet(t, base+"/tasks?updated_after=2026-06-08", sess); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bare date must be 400, got %d", r.StatusCode)
	}
}

func TestPM_ListIssues_ProjectFilters_TerminalDefaultAndStatus(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	presp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"P"}`, sess)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("create project status=%d", presp.StatusCode)
	}
	var pc map[string]any
	json.NewDecoder(presp.Body).Decode(&pc)
	pid := pc["id"].(string)
	base := s.URL + "/api/projects/" + pid

	mkIssue := func(title string) string {
		resp := orgScopedPost(t, base+"/issues", `{"title":"`+title+`"}`, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create issue %q status=%d", title, resp.StatusCode)
		}
		var c map[string]any
		json.NewDecoder(resp.Body).Decode(&c)
		return c["id"].(string)
	}
	list := func(query string) map[string]bool {
		resp := orgScopedGet(t, base+"/issues"+query, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list %q status=%d", query, resp.StatusCode)
		}
		var l struct {
			Issues []map[string]any `json:"issues"`
		}
		json.NewDecoder(resp.Body).Decode(&l)
		ids := map[string]bool{}
		for _, is := range l.Issues {
			ids[is["id"].(string)] = true
		}
		return ids
	}

	openI := mkIssue("open one")
	resolvedI := mkIssue("resolved one")
	if r := orgScopedPost(t, base+"/issues/"+resolvedI+"/status", `{"status":"resolved"}`, sess); r.StatusCode != http.StatusOK {
		t.Fatalf("resolve issue status=%d", r.StatusCode)
	}

	// Default (no status) EXCLUDES the terminal (resolved) issue — aligned with
	// the org Issue list. Previously this endpoint returned every status.
	def := list("")
	if !def[openI] {
		t.Fatalf("default issue list must include the open issue, got %+v", def)
	}
	if def[resolvedI] {
		t.Fatalf("default issue list must EXCLUDE the terminal (resolved) issue, got %+v", def)
	}

	// ?status=resolved → the resolved issue is reachable.
	if got := list("?status=resolved"); len(got) != 1 || !got[resolvedI] {
		t.Fatalf("?status=resolved should be [resolved], got %+v", got)
	}

	// ?status=all → every status, terminal included.
	if all := list("?status=all"); !all[openI] || !all[resolvedI] {
		t.Fatalf("?status=all must include open + resolved, got %+v", all)
	}

	// An explicit assignee filter excludes all issues (issues are not assignable),
	// matching the org Issue list's param contract.
	if got := list("?assignee=user:" + sess.IdentityID); len(got) != 0 {
		t.Fatalf("issues are not assignable → assignee filter yields empty, got %+v", got)
	}
}
