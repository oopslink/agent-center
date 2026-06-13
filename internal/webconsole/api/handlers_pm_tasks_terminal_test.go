package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestPM_ListTasks_ExcludesTerminalByDefault is the v2.9.1 task-c91805fe guard: the
// project task/backlog list excludes terminal tasks (completed/discarded) by
// default, but surfaces them under an explicit ?status= filter — the same
// default-exclude / filter-to-see contract as archived projects (#298/#310). It
// also pins the ADR-0046 terminal set so a future state-machine drift is caught.
func TestPM_ListTasks_ExcludesTerminalByDefault(t *testing.T) {
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

	mkTask := func(title string) string {
		resp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks", `{"title":"`+title+`"}`, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create task %q status=%d", title, resp.StatusCode)
		}
		var c map[string]any
		json.NewDecoder(resp.Body).Decode(&c)
		return c["id"].(string)
	}
	setStatus := func(tid, status string) {
		resp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks/"+tid+"/status", `{"status":"`+status+`"}`, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("set %s=%s status=%d", tid, status, resp.StatusCode)
		}
	}

	openT := mkTask("open one")
	runningT := mkTask("running one")
	setStatus(runningT, "running")
	completedT := mkTask("done one")
	setStatus(completedT, "completed")
	discardedT := mkTask("dropped one")
	setStatus(discardedT, "discarded")

	list := func(query string) map[string]bool {
		resp := orgScopedGet(t, s.URL+"/api/projects/"+pid+"/tasks"+query, sess)
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

	// Default → non-terminal only (open + running), terminal excluded.
	def := list("")
	if !def[openT] || !def[runningT] {
		t.Fatalf("default list must include non-terminal tasks, got %+v", def)
	}
	if def[completedT] || def[discardedT] {
		t.Fatalf("default list must EXCLUDE terminal (completed/discarded), got %+v", def)
	}

	// ?status=completed → only the completed task is reachable.
	if comp := list("?status=completed"); len(comp) != 1 || !comp[completedT] {
		t.Fatalf("?status=completed should be [completed], got %+v", comp)
	}

	// ?status=completed,discarded → both terminal, and ONLY those.
	term := list("?status=completed,discarded")
	if !term[completedT] || !term[discardedT] || term[openT] || term[runningT] {
		t.Fatalf("?status=completed,discarded should be exactly the terminal set, got %+v", term)
	}

	// The Backlog view (?unplanned=1) composes with the default terminal-exclude.
	unp := list("?unplanned=1")
	if !unp[openT] || !unp[runningT] || unp[completedT] || unp[discardedT] {
		t.Fatalf("unplanned backlog must exclude terminal by default, got %+v", unp)
	}
}
