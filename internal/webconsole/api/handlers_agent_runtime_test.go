package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/runtimefs"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// I5 (issue-921db054) — the org-scoped agent runtime browser endpoints. These cover the
// CENTER-side behaviours: org/agent auth gating and the worker-offline degrade. The
// read logic + security red lines are tested worker-side (internal/workerdaemon); the
// req_id correlation is tested in internal/runtimefs.

// runtimeDeps wires the runtime endpoints' dependencies onto an authed test deps bag.
func runtimeDeps(t *testing.T) (HandlerDeps, *sql.DB, testSession) {
	t.Helper()
	deps, db := setupAPIWithAuth(t)
	deps.WorkerRepo = wfsqlite.NewWorkerRepo(db)
	deps.RuntimeFsDispatcher = runtimefs.NewDispatcher()
	sess := setupTestSession(t, db, deps)
	return deps, db, sess
}

func TestRuntimeAPI_WorkerOffline_Unavailable(t *testing.T) {
	deps, db, sess := runtimeDeps(t)
	saveWorkerInOrg(t, db, sess.OrgID, "w-rt")
	s := newTestServer(t, deps)
	defer s.Close()

	// Agent on an offline worker (just enrolled, no heartbeat → offline).
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"rt","model":"claude","cli":"claude-code","worker_id":"w-rt"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)

	for _, op := range []string{"list", "read?path=memory/CLAUDE.md", "gitlog?path=memory"} {
		resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/runtime/"+op, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d want 200 (unavailable is a 200 + flag)", op, resp.StatusCode)
		}
		var got map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&got)
		if got["unavailable"] != true {
			t.Fatalf("%s: unavailable=%v want true (worker offline)", op, got["unavailable"])
		}
		if got["reason"] != "worker_offline" {
			t.Fatalf("%s: reason=%v want worker_offline", op, got["reason"])
		}
	}
}

func TestRuntimeAPI_UnknownAgent_NotFound(t *testing.T) {
	deps, _, sess := runtimeDeps(t)
	s := newTestServer(t, deps)
	defer s.Close()

	// An agent id not in the caller's org → agentRequireInOrg yields 404 (no
	// existence disclosure). This is the same gate every /agents/{id} route uses.
	resp := orgScopedGet(t, s.URL+"/api/agents/agent-does-not-exist/runtime/list", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown agent: status=%d want 404", resp.StatusCode)
	}
}

func TestRuntimeAPI_Unauthenticated_Rejected(t *testing.T) {
	deps, _, _ := runtimeDeps(t)
	s := newTestServer(t, deps)
	defer s.Close()

	// No session cookie → the org-member gate rejects before any agent resolution.
	resp, err := http.Get(s.URL + "/api/orgs/any/agents/x/runtime/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("unauthenticated request got 200, want auth rejection")
	}
}
