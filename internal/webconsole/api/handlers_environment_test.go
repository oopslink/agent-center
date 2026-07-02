package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
	wfsql "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// saveWorkforceWorkerInOrg saves a workforce.Worker (the canonical enrolled-set
// model — v2.7 #140 step-2 convergence) owned by orgID directly via the sqlite
// repo, so the org-scoped /api/workers reads see it.
func saveWorkforceWorkerInOrg(t *testing.T, db *sql.DB, orgID, workerID string) {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             workforce.WorkerID(workerID),
		OrganizationID: orgID,
		Name:           workerID,
		EnrolledAt:     time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := wfsql.NewWorkerRepo(db).Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

// TestAPI_EnvWorkers_ListOrgScoped: GET /api/workers returns ONLY the caller org's
// workers — a worker in another org never leaks. v2.7 #140 step-2: sourced from the
// canonical workforce.Worker (enrolled set), org-filtered in-handler.
func TestAPI_EnvWorkers_ListOrgScoped(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.WorkerRepo = wfsql.NewWorkerRepo(db)
	sess := setupTestSession(t, db, deps)
	saveWorkforceWorkerInOrg(t, db, sess.OrgID, "w-mine")
	saveWorkforceWorkerInOrg(t, db, "org-other", "w-other")
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
	// v2.7 #140 step-2: the control-channel-only field is gone (workforce.Worker
	// has no acked offset); the enrolled-set shape carries enrolled_at instead.
	if _, ok := list.Workers[0]["last_acked_offset"]; ok {
		t.Fatalf("last_acked_offset must be dropped after workforce repoint: %+v", list.Workers[0])
	}
	if _, ok := list.Workers[0]["enrolled_at"]; !ok {
		t.Fatalf("enrolled_at missing (enrolled-set shape): %+v", list.Workers[0])
	}
}

// TestAPI_EnvWorkers_SystemInfoSurfaced (T752): a worker that has reported its
// host + build identity surfaces those fields on GET /api/workers/{id}; a worker
// that has not reported them omits the keys (frontend then shows the "Coming in
// v2.9" placeholder per field — honest gap, no fake values).
func TestAPI_EnvWorkers_SystemInfoSurfaced(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.WorkerRepo = wfsql.NewWorkerRepo(db)
	sess := setupTestSession(t, db, deps)

	// One worker WITH system info, one WITHOUT.
	repo := wfsql.NewWorkerRepo(db)
	info := workforce.SystemInfo{
		Hostname:           "dev001.local",
		OS:                 "darwin",
		Arch:               "arm64",
		AgentCenterVersion: "v2.10.2",
		InstallPath:        "/usr/local/bin/agent-center",
		WorkerVersion:      "v2.10.2+abc1234",
	}
	wWith, err := workforce.RehydrateWorker(workforce.RehydrateWorkerInput{
		ID: "w-info", OrganizationID: sess.OrgID, Name: "w-info", Status: workforce.WorkerOffline,
		SystemInfo: info, EnrolledAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), wWith); err != nil {
		t.Fatal(err)
	}
	saveWorkforceWorkerInOrg(t, db, sess.OrgID, "w-bare")

	s := newTestServer(t, deps)
	defer s.Close()

	// WITH info → all fields present + correct.
	var got map[string]any
	_ = json.NewDecoder(orgScopedGet(t, s.URL+"/api/workers/w-info", sess).Body).Decode(&got)
	for k, want := range map[string]string{
		"hostname":             info.Hostname,
		"os":                   info.OS,
		"arch":                 info.Arch,
		"agent_center_version": info.AgentCenterVersion,
		"install_path":         info.InstallPath,
		"worker_version":       info.WorkerVersion,
	} {
		if got[k] != want {
			t.Fatalf("worker w-info field %q = %v, want %q", k, got[k], want)
		}
	}

	// WITHOUT info → keys omitted (so the UI falls back to its placeholder).
	var bare map[string]any
	_ = json.NewDecoder(orgScopedGet(t, s.URL+"/api/workers/w-bare", sess).Body).Decode(&bare)
	for _, k := range []string{"hostname", "os", "arch", "agent_center_version", "install_path", "worker_version"} {
		if _, present := bare[k]; present {
			t.Fatalf("worker w-bare should omit %q when unreported, got %+v", k, bare)
		}
	}
}

// TestAPI_EnvWorkers_GetOwnAndCrossOrg404: detail returns 200 for the caller org's
// worker, and 404 (NOT 403/leak) for another org's worker id — the fetch-then-check
// guard prevents probing cross-org worker ids (E-10b hard invariant). v2.7 #140
// step-2: now workforce.WorkerRepository.FindByID + same org-check (NOT a scoped
// query — the invariant is preserved across the repoint).
func TestAPI_EnvWorkers_GetOwnAndCrossOrg404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.WorkerRepo = wfsql.NewWorkerRepo(db)
	sess := setupTestSession(t, db, deps)
	saveWorkforceWorkerInOrg(t, db, sess.OrgID, "w-mine")
	saveWorkforceWorkerInOrg(t, db, "org-other", "w-other")
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

// TestAPI_EnvWorkers_NotWired: with no WorkerRepo wired the reads return 501
// (graceful not-wired, not a panic).
func TestAPI_EnvWorkers_NotWired(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.WorkerRepo = nil
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/workers", sess)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("list not-wired: got %d, want 501", resp.StatusCode)
	}
}
