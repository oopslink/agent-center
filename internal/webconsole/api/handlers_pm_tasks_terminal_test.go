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

	// ?status=all (T62/task-336335c5) → the escape hatch surfaces EVERY status,
	// terminal included, so a task-<id> reference resolver can resolve a completed
	// task. Must be the full set (non-terminal + terminal).
	if a := list("?status=all"); !a[openT] || !a[runningT] || !a[completedT] || !a[discardedT] {
		t.Fatalf("?status=all must include every status (terminal included), got %+v", a)
	}

	// The Backlog view (?unplanned=1) composes with the default terminal-exclude.
	unp := list("?unplanned=1")
	if !unp[openT] || !unp[runningT] || unp[completedT] || unp[discardedT] {
		t.Fatalf("unplanned backlog must exclude terminal by default, got %+v", unp)
	}
}

// TestListOrgTasks_StatusAll_IncludesTerminal_ForRefResolution (T62/task-336335c5)
// is the faithful reproduction of the reported bug: the cross-project ORG task
// list (GET /api/tasks — the data source behind the message task-<id> linkify
// resolver) excludes terminal tasks by default, so a reference to a COMPLETED
// task silently stayed plain text. ?status=all lets the resolver retrieve it.
func TestListOrgTasks_StatusAll_IncludesTerminal_ForRefResolution(t *testing.T) {
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
	openT := mkTask("open one")
	doneT := mkTask("done one")
	resp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks/"+doneT+"/status", `{"status":"completed"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete task status=%d", resp.StatusCode)
	}

	orgList := func(query string) map[string]bool {
		r := orgScopedGet(t, s.URL+"/api/tasks"+query, sess)
		ids := map[string]bool{}
		for _, tk := range decodeItems(t, r) {
			ids[tk["id"].(string)] = true
		}
		return ids
	}

	// Default ORG list: the completed task is INVISIBLE — this is exactly why a
	// task-<id> ref to a completed task did not linkify.
	def := orgList("")
	if !def[openT] {
		t.Fatalf("default org list must include the open task, got %+v", def)
	}
	if def[doneT] {
		t.Fatalf("default org list must EXCLUDE the completed task (the bug source), got %+v", def)
	}

	// ?status=all: the resolver can now retrieve the completed task → ref linkifies.
	all := orgList("?status=all")
	if !all[openT] || !all[doneT] {
		t.Fatalf("?status=all org list must include BOTH open and completed (so refs to terminal tasks resolve), got %+v", all)
	}
}
