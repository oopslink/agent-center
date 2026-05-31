package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
)

// saveEnvWorkerInOrg saves an environment.Worker (control-connected view) owned by
// orgID directly via the sqlite repo, so the org-scoped /api/workers reads see it.
func saveEnvWorkerInOrg(t *testing.T, db *sql.DB, orgID, workerID string) {
	t.Helper()
	w, err := environment.NewWorker(environment.NewWorkerInput{
		ID:             environment.WorkerID(workerID),
		OrganizationID: orgID,
		Name:           workerID,
		CreatedAt:      time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := envsql.NewWorkerRepo(db).Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

// TestAPI_EnvWorkers_ListOrgScoped: GET /api/workers returns ONLY the caller org's
// control-connected workers (ListByOrg) — a worker in another org never leaks.
func TestAPI_EnvWorkers_ListOrgScoped(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.EnvWorkerRepo = envsql.NewWorkerRepo(db)
	sess := setupTestSession(t, db, deps)
	saveEnvWorkerInOrg(t, db, sess.OrgID, "w-mine")
	saveEnvWorkerInOrg(t, db, "org-other", "w-other")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/workers", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: got %d", resp.StatusCode)
	}
	var list struct {
		Workers []map[string]any `json:"workers"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Workers) != 1 || list.Workers[0]["worker_id"] != "w-mine" {
		t.Fatalf("list = %+v, want only w-mine (org-scoped, no cross-org leak)", list.Workers)
	}
	if list.Workers[0]["organization_id"] != sess.OrgID {
		t.Fatalf("worker org = %v, want %s", list.Workers[0]["organization_id"], sess.OrgID)
	}
}

// TestAPI_EnvWorkers_GetOwnAndCrossOrg404: detail returns 200 for the caller org's
// worker, and 404 (NOT 403/leak) for another org's worker id — the fetch-then-check
// guard prevents probing cross-org worker ids (E-10b hard invariant).
func TestAPI_EnvWorkers_GetOwnAndCrossOrg404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.EnvWorkerRepo = envsql.NewWorkerRepo(db)
	sess := setupTestSession(t, db, deps)
	saveEnvWorkerInOrg(t, db, sess.OrgID, "w-mine")
	saveEnvWorkerInOrg(t, db, "org-other", "w-other")
	s := newTestServer(t, deps)
	defer s.Close()

	// Own worker → 200.
	resp := orgScopedGet(t, s.URL+"/api/workers/w-mine", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get own: got %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["worker_id"] != "w-mine" {
		t.Fatalf("get own = %+v", got)
	}

	// Another org's worker id → 404 (no cross-org read / no existence leak).
	resp = orgScopedGet(t, s.URL+"/api/workers/w-other", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get cross-org: got %d, want 404 (no cross-org leak)", resp.StatusCode)
	}

	// Unknown id → also 404 (same shape as cross-org, so existence is not probeable).
	resp = orgScopedGet(t, s.URL+"/api/workers/w-nope", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get unknown: got %d, want 404", resp.StatusCode)
	}
}

// TestAPI_EnvWorkers_NotWired: with no EnvWorkerRepo wired the reads return 501
// (graceful not-wired, not a panic).
func TestAPI_EnvWorkers_NotWired(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/workers", sess)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("list not-wired: got %d, want 501", resp.StatusCode)
	}
}
