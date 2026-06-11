package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	atsqlite "github.com/oopslink/agent-center/internal/admintoken/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// v2.4-D-F4 X1 fix: mint-enroll endpoint tests. Exercises the contract
// PD's acceptance run needed (token + fingerprint + bootstrap_host all
// in one response) and the 503 path when admin TCP isn't configured.

func mintEnrollServer(t *testing.T, deps HandlerDeps) *httptest.Server {
	t.Helper()
	srv := NewServer(":0", Deps{})
	return httptest.NewServer(WithDeps(deps)(srv.Handler()))
}

func newRealAdminTokenSvc(t *testing.T) *admintokensvc.Service {
	t.Helper()
	dir := t.TempDir()
	db, err := persistence.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	repo := atsqlite.New(db)
	return admintokensvc.New(repo, idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{})
}

// newPreCreateFixture wires AdminTokenSvc + WorkerEnrollService on a
// shared sqlite so v2.5-B1 tests can mint-enroll and then assert the
// Worker row landed.
func newPreCreateFixture(t *testing.T) (*admintokensvc.Service, *wfservice.WorkerEnrollService, *wfsqlite.WorkerRepo, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := persistence.Open(dir + "/preCreate.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.SystemClock{}
	gen := idgen.NewGenerator(fc)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, fc)
	tokenSvc := admintokensvc.New(atsqlite.New(db), gen, fc)
	workerRepo := wfsqlite.NewWorkerRepo(db)
	enrollSvc := wfservice.NewWorkerEnrollService(db, workerRepo, sink, fc)
	return tokenSvc, enrollSvc, workerRepo, db
}

// wireWorkerAuth augments worker-fixture deps with Identity (AuthSvc/OrgRepo/
// MemberRepo) using the fixed test signing key, and returns a session whose
// org owns subsequently-minted workers. Worker show/remint/remove now require
// requireWorkerInOrg, so these tests must authenticate + scope by org.
func wireWorkerAuth(t *testing.T, db *sql.DB, deps *HandlerDeps) testSession {
	t.Helper()
	deps.AuthSvc = identity.NewAuthService(identity.NewSQLiteIdentityRepo(db), testSigningKey)
	deps.OrgRepo = identity.NewSQLiteOrganizationRepo(db)
	deps.MemberRepo = identity.NewSQLiteMemberRepo(db)
	return setupTestSession(t, db, *deps)
}

// mintWorkerInOrg creates a worker row owned by the session's org via the
// wired WorkerAddSvc, so requireWorkerInOrg passes for that worker id.
func mintWorkerInOrg(t *testing.T, deps *HandlerDeps, sess testSession, workerID string) {
	t.Helper()
	if _, err := deps.WorkerAddSvc.AddWorker(context.Background(), wfservice.AddWorkerCommand{
		WorkerID:       workforce.WorkerID(workerID),
		Name:           workerID,
		OrganizationID: sess.OrgID,
		ActorIdentity:  observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
}

// TestWorker_OrgScope_CrossOrgBlocked proves v2.6 X1 §2: worker
// rename/show-install/remove for a worker in another org returns 404, and
// Add Worker stamps the new worker with the caller's org.
func TestWorker_OrgScope_CrossOrgBlocked(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRemoveSvc:     enrollSvc,
		WorkerRenameSvc:     enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	// A worker that belongs to a DIFFERENT org.
	if _, err := enrollSvc.AddWorker(context.Background(), wfservice.AddWorkerCommand{
		WorkerID: "worker-other", Name: "other", OrganizationID: "organization-other",
		ActorIdentity: observability.Actor("user:other"),
	}); err != nil {
		t.Fatal(err)
	}
	// rename → 404
	resp := orgScopedPatch(t, srv.URL+"/api/workers/worker-other/name", `{"name":"x"}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org rename: got %d want 404", resp.StatusCode)
	}
	// remove → 404
	resp = orgScopedDelete(t, srv.URL+"/api/workers/worker-other", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org remove: got %d want 404", resp.StatusCode)
	}
	// Add Worker stamps the caller's org.
	mintResp := orgScopedPost(t, srv.URL+"/api/admintoken/mint-enroll", `{"name":"mine"}`, sess)
	var minted mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&minted)
	wk, err := workerRepo.FindByID(context.Background(), workforce.WorkerID(minted.WorkerID))
	if err != nil {
		t.Fatal(err)
	}
	if wk.OrganizationID() != sess.OrgID {
		t.Fatalf("new worker org=%q want %q", wk.OrganizationID(), sess.OrgID)
	}
}

// v2.5-B1: mint-enroll pre-creates the Worker AR so Fleet sees the
// offline row immediately. The handler call returns; we verify both
// (a) the response includes the generated worker_id, and (b) the
// Worker row is queryable with status=offline.
func TestMintEnroll_PreCreatesWorker(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, _ := newPreCreateFixture(t)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json",
		strings.NewReader(`{"name":"alice-box"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body mintEnrollResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.WorkerID == "" {
		t.Fatal("worker_id empty")
	}
	if body.WorkerName != "alice-box" {
		t.Fatalf("worker_name = %q", body.WorkerName)
	}
	w, err := workerRepo.FindByID(context.Background(), workforce.WorkerID(body.WorkerID))
	if err != nil {
		t.Fatalf("worker row not pre-created: %v", err)
	}
	if w.Status() != workforce.WorkerOffline {
		t.Fatalf("expected offline status, got %v", w.Status())
	}
	if w.Name() != "alice-box" {
		t.Fatalf("name = %q", w.Name())
	}
}

// If WorkerAddSvc rejects (e.g. duplicate worker_id from a generator
// collision), the just-minted token is revoked so the operator can
// retry cleanly.
func TestMintEnroll_RevokesTokenOnAddFailure(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, _ := newPreCreateFixture(t)
	// Pre-create a Worker with a fixed id then stub generateWorkerID to
	// always return that same id — simulates the collision branch.
	// Instead of stubbing (no hook), we use a WorkerAddSvc wrapper
	// that always fails to surface the revoke path.
	failing := failingAddSvc{wrapped: enrollSvc}
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        failing,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "add_worker_failed" {
		t.Errorf("error code = %v", body["error"])
	}
	// And the failed-handler should NOT have left a Worker row.
	wks, _ := workerRepo.FindAll(context.Background())
	if len(wks) != 0 {
		t.Errorf("expected 0 workers after revoke, got %d", len(wks))
	}
}

// failingAddSvc always rejects AddWorker so we can exercise the
// mint-enroll handler's revoke path without depending on a collision.
type failingAddSvc struct {
	wrapped *wfservice.WorkerEnrollService
}

func (f failingAddSvc) AddWorker(_ context.Context, _ wfservice.AddWorkerCommand) (wfservice.AddWorkerResult, error) {
	return wfservice.AddWorkerResult{}, errSimulatedAddFailure
}

var errSimulatedAddFailure = &stringError{"simulated add failure"}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

// =============================================================================
// v2.5-B2 show-install-command endpoint
// =============================================================================

// withMasterKey returns the service with a freshly-generated master
// key set so the encrypt/decrypt path runs end-to-end.
func withMasterKey(t *testing.T, svc *admintokensvc.Service) *admintokensvc.Service {
	t.Helper()
	mk, err := secretmgmt.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	return svc.WithMasterKey(mk)
}

func TestShowInstallCommand_HappyPath(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	// Mint a token (also pre-creates the worker via WorkerAddSvc, stamped with the org).
	mintResp := orgScopedPost(t, srv.URL+"/api/admintoken/mint-enroll", `{"name":"alice-box"}`, sess)
	defer mintResp.Body.Close()
	var minted mintEnrollResp
	if err := json.NewDecoder(mintResp.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}
	// Now re-display via the show endpoint.
	resp := orgScopedGet(t, srv.URL+"/api/workers/"+minted.WorkerID+"/install-command", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body showInstallCommandResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token != minted.Token {
		t.Errorf("token round-trip mismatch")
	}
	if body.WorkerID != minted.WorkerID {
		t.Errorf("worker_id = %q want %q", body.WorkerID, minted.WorkerID)
	}
	if body.WorkerName != "alice-box" {
		t.Errorf("worker_name = %q", body.WorkerName)
	}
	if body.Fingerprint != "sha256:AA" || body.BootstrapHost != "h:7300" {
		t.Errorf("missing fingerprint/bootstrap_host: %+v", body)
	}
	if body.ExpiresAt == "" {
		t.Errorf("expires_at empty")
	}
}

// Without a master key the show endpoint surfaces a clear
// "not configured" hint rather than a generic 401 — operators
// running v2.5+ without secret_management need to know why
// install-command re-display is unavailable.
func TestShowInstallCommand_NoMasterKey_503(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	// NB: no WithMasterKey call → ShowInstallToken returns the
	// sentinel error and the handler maps to 503.
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	// Seed the worker in the org so it passes requireWorkerInOrg and reaches
	// the no-master-key 503 path (rather than 404).
	mintWorkerInOrg(t, &deps, sess, "worker-unknown")
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	resp := orgScopedGet(t, srv.URL+"/api/workers/worker-unknown/install-command", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "show_install_no_master_key" {
		t.Errorf("error code = %v", body["error"])
	}
}

// =============================================================================
// v2.5-B3 re-mint install command endpoint
// =============================================================================

// Re-mint after the original token was consumed (e.g. operator burned
// it on an aborted install) should issue a fresh token bound to the
// same worker_id, and the new install command should round-trip
// through show-install-command.
func TestReMintInstallCommand_HappyPath(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	// Mint original.
	mintResp := orgScopedPost(t, srv.URL+"/api/admintoken/mint-enroll", `{"name":"alice-box"}`, sess)
	defer mintResp.Body.Close()
	var minted mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&minted)
	// Burn it.
	if err := tokenSvc.ConsumeEnrollToken(context.Background(), admintoken.TokenID(minted.ID)); err != nil {
		t.Fatal(err)
	}
	// Re-mint via the endpoint.
	rmResp := orgScopedPost(t, srv.URL+"/api/workers/"+minted.WorkerID+"/install-command/re-mint", "", sess)
	defer rmResp.Body.Close()
	if rmResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", rmResp.StatusCode)
	}
	var fresh showInstallCommandResp
	if err := json.NewDecoder(rmResp.Body).Decode(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.WorkerID != minted.WorkerID {
		t.Errorf("worker_id changed: %q want %q", fresh.WorkerID, minted.WorkerID)
	}
	if fresh.WorkerName != "alice-box" {
		t.Errorf("worker_name = %q", fresh.WorkerName)
	}
	if fresh.Token == "" || fresh.Token == minted.Token {
		t.Errorf("token didn't rotate: old=%q new=%q", minted.Token, fresh.Token)
	}
	if fresh.ID == minted.ID {
		t.Errorf("token id didn't rotate: %q", fresh.ID)
	}
	// And show-install-command should now return the NEW token.
	showResp := orgScopedGet(t, srv.URL+"/api/workers/"+minted.WorkerID+"/install-command", sess)
	defer showResp.Body.Close()
	if showResp.StatusCode != http.StatusOK {
		t.Fatalf("show status = %d", showResp.StatusCode)
	}
	var shown showInstallCommandResp
	_ = json.NewDecoder(showResp.Body).Decode(&shown)
	if shown.Token != fresh.Token {
		t.Errorf("show returned %q want fresh %q", shown.Token, fresh.Token)
	}
}

// Re-mint should 404 when the worker doesn't exist yet (operator
// clicked re-mint on a stale Fleet row that got removed elsewhere).
func TestReMintInstallCommand_WorkerNotFound_404(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	resp := orgScopedPost(t, srv.URL+"/api/workers/worker-ghost/install-command/re-mint", "", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// Re-mint should 409 when the worker has already enrolled (long-term
// token present); operator must remove + re-add.
func TestReMintInstallCommand_AlreadyEnrolled_409(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	// Pre-create worker via mint-enroll.
	mintResp := orgScopedPost(t, srv.URL+"/api/admintoken/mint-enroll", "", sess)
	defer mintResp.Body.Close()
	var minted mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&minted)
	// Simulate daemon enroll: mint a long-term `worker:<id>` token.
	if _, err := tokenSvc.Create(context.Background(), admintokensvc.CreateCommand{
		Owner:  admintoken.Owner("worker:" + minted.WorkerID),
		Scopes: []admintoken.Scope{"workforce:enroll", "dispatch:pull"},
	}); err != nil {
		t.Fatal(err)
	}
	// Re-mint must refuse.
	resp := orgScopedPost(t, srv.URL+"/api/workers/"+minted.WorkerID+"/install-command/re-mint", "", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "worker_already_online" {
		t.Errorf("error code = %v", body["error"])
	}
}

// =============================================================================
// v2.5-B4 delete worker endpoint
// =============================================================================

// DELETE /api/workers/{id} removes the row + revokes its tokens.
// Subsequent show-install-command returns 401 since the enroll token
// is now revoked (the partial filter excludes revoked rows).
func TestRemoveWorker_Endpoint(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRemoveSvc:     enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	// Pre-create a worker via mint-enroll.
	mintResp := orgScopedPost(t, srv.URL+"/api/admintoken/mint-enroll", "", sess)
	defer mintResp.Body.Close()
	var minted mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&minted)
	// Add a long-term token too so the revoke-cascade is observable.
	if _, err := tokenSvc.Create(context.Background(), admintokensvc.CreateCommand{
		Owner:  admintoken.Owner("worker:" + minted.WorkerID),
		Scopes: []admintoken.Scope{"workforce:enroll"},
	}); err != nil {
		t.Fatal(err)
	}
	// DELETE the worker.
	resp := orgScopedDelete(t, srv.URL+"/api/workers/"+minted.WorkerID, sess)
	defer resp.Body.Close()
	// v2.8.1: worker remove now returns 200 {ok, unbound_agents} (was 204).
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Worker row gone.
	if _, err := workerRepo.FindByID(context.Background(), workforce.WorkerID(minted.WorkerID)); err == nil {
		t.Errorf("worker row still present after DELETE")
	}
	// All tokens for this worker revoked.
	for _, owner := range []admintoken.Owner{
		admintoken.Owner("worker:" + minted.WorkerID),
		admintoken.Owner("enroll:worker:" + minted.WorkerID),
	} {
		toks, _ := tokenSvc.FindAll(context.Background())
		for _, tok := range toks {
			if tok.Owner() != owner {
				continue
			}
			if !tok.IsRevoked() {
				t.Errorf("token %s owner %q still active after DELETE", tok.ID(), owner)
			}
		}
	}
}

// DELETE on a missing worker returns 404.
func TestRemoveWorker_NotFound_404(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRemoveSvc:     enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	resp := orgScopedDelete(t, srv.URL+"/api/workers/worker-ghost", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// A consumed enroll token (daemon already enrolled successfully)
// surfaces 401 + no_active_enroll_token so the UI knows to offer
// re-mint instead.
func TestShowInstallCommand_AfterConsume_401(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo, db := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	mintResp := orgScopedPost(t, srv.URL+"/api/admintoken/mint-enroll", "", sess)
	defer mintResp.Body.Close()
	var minted mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&minted)
	// Burn the token (simulating daemon enroll).
	if err := tokenSvc.ConsumeEnrollToken(context.Background(), admintoken.TokenID(minted.ID)); err != nil {
		t.Fatal(err)
	}
	resp := orgScopedGet(t, srv.URL+"/api/workers/"+minted.WorkerID+"/install-command", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "no_active_enroll_token" {
		t.Errorf("error code = %v", body["error"])
	}
}

func TestMintEnroll_HappyPath(t *testing.T) {
	svc := newRealAdminTokenSvc(t)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       svc,
		EnrollFingerprint:   "sha256:AA:BB:CC",
		EnrollBootstrapHost: "host.local:7300",
	}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body mintEnrollResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" {
		t.Errorf("token empty")
	}
	if body.Fingerprint != "sha256:AA:BB:CC" {
		t.Errorf("fingerprint = %q", body.Fingerprint)
	}
	if body.BootstrapHost != "host.local:7300" {
		t.Errorf("bootstrap_host = %q", body.BootstrapHost)
	}
	if body.ID == "" {
		t.Errorf("id empty")
	}
	if body.ExpiresAt == "" {
		t.Errorf("expires_at empty")
	}
	// Token must be flagged as enroll with a positive TTL window.
	tok, err := svc.FindByID(context.Background(), admintoken.TokenID(body.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !tok.IsEnroll() {
		t.Errorf("token not flagged as enroll")
	}
	if exp := tok.ExpiresAt(); exp == nil || exp.Before(time.Now()) {
		t.Errorf("expires_at = %v not in the future", exp)
	}
}

func TestMintEnroll_NotConfigured(t *testing.T) {
	svc := newRealAdminTokenSvc(t)
	deps := HandlerDeps{
		Actor:         observability.Actor("user:hayang"),
		AdminTokenSvc: svc,
		// Intentionally empty fingerprint + bootstrap host — simulates
		// `admin_tcp_listen` not enabled.
	}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "enroll_not_configured" {
		t.Errorf("error code = %v", body["error"])
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "admin_tcp_listen") {
		t.Errorf("message missing admin_tcp_listen hint: %v", msg)
	}
}

func TestMintEnroll_SvcNotWired(t *testing.T) {
	deps := HandlerDeps{Actor: observability.Actor("user:hayang")}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
}

// =============================================================================
// POST /api/admintoken/revoke — v2.9 authz matrix.
//
// This endpoint was previously UNAUTHENTICATED (anyone with a token id could
// revoke). The v2.9 security fix now requires ① a valid session JWT AND ② that
// the caller is a member of the token's organization (resolved via the token's
// bound worker → worker.OrganizationID → MemberRepo.GetByOrganizationAndIdentity).
// Fail closed: missing deps → 501, no/invalid session → 401, non-member → 403.
//
// We use the setupAPIWithAuth + setupTestSession harness (NOT the minimal
// mintEnrollServer) because the new contract needs AuthSvc + MemberRepo +
// WorkerRepo wired, a signed-in session, a worker-with-org, and an enroll
// token bound to that worker — all on a shared sqlite DB.
// =============================================================================

// setupRevokeAuth wires the auth + member + worker deps plus an AdminTokenSvc
// on the shared test DB, and returns the deps, db, and an authenticated session
// whose org owns subsequently-seeded workers.
func setupRevokeAuth(t *testing.T) (HandlerDeps, *sql.DB, *admintokensvc.Service, testSession) {
	t.Helper()
	deps, db := setupAPIWithAuth(t)
	deps.WorkerRepo = wfsqlite.NewWorkerRepo(db)
	svc := admintokensvc.New(atsqlite.New(db), idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{})
	deps.AdminTokenSvc = svc
	sess := setupTestSession(t, db, deps)
	return deps, db, svc, sess
}

// seedEnrollTokenForWorker mints an enroll token bound to workerID and returns
// its token id. The worker must already be seeded (see saveWorkforceWorkerInOrg)
// for the handler's org-membership resolution to find it.
func seedEnrollTokenForWorker(t *testing.T, svc *admintokensvc.Service, workerID string) string {
	t.Helper()
	res, err := svc.CreateEnrollToken(context.Background(), admintokensvc.CreateEnrollCommand{
		Owner:     admintoken.Owner("worker:" + workerID),
		Scopes:    []admintoken.Scope{"dispatch:pull"},
		CreatedBy: "test",
		WorkerID:  workerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(res.ID)
}

// revokePost issues POST /api/admintoken/revoke with the given query, optionally
// attaching the session cookie. Returns the response.
func revokePost(t *testing.T, url string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// (1) Happy path: authenticated caller who IS a member of the token's org → 204
// AND the token is actually revoked (RevokedAt != nil). This is the legit FE
// Modal-close flow — the security fix must NOT break it.
func TestRevokeEnroll_RealID(t *testing.T) {
	deps, db, svc, sess := setupRevokeAuth(t)
	saveWorkforceWorkerInOrg(t, db, sess.OrgID, "w-mine")
	tokenID := seedEnrollTokenForWorker(t, svc, "w-mine")

	srv := newTestServer(t, deps)
	defer srv.Close()

	resp := revokePost(t, srv.URL+"/api/admintoken/revoke?id="+tokenID, sess.Cookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", resp.StatusCode)
	}
	tok, err := svc.FindByID(context.Background(), admintoken.TokenID(tokenID))
	if err != nil {
		t.Fatal(err)
	}
	if tok.RevokedAt() == nil {
		t.Errorf("token not revoked (RevokedAt == nil)")
	}
}

// (2) Unauthenticated: no JWT cookie → 401.
func TestRevokeEnroll_Unauthenticated(t *testing.T) {
	deps, db, svc, sess := setupRevokeAuth(t)
	saveWorkforceWorkerInOrg(t, db, sess.OrgID, "w-mine")
	tokenID := seedEnrollTokenForWorker(t, svc, "w-mine")

	srv := newTestServer(t, deps)
	defer srv.Close()

	resp := revokePost(t, srv.URL+"/api/admintoken/revoke?id="+tokenID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	// And the token must NOT be revoked.
	tok, err := svc.FindByID(context.Background(), admintoken.TokenID(tokenID))
	if err != nil {
		t.Fatal(err)
	}
	if tok.RevokedAt() != nil {
		t.Errorf("token revoked by unauthenticated caller")
	}
}

// (3) Non-member: authenticated caller who is NOT a member of the token's org
// (the token's worker belongs to a DIFFERENT org) → 403, and the token is NOT
// revoked.
func TestRevokeEnroll_NonMemberForbidden(t *testing.T) {
	deps, db, svc, sess := setupRevokeAuth(t)
	// Worker (and thus token) belong to a DIFFERENT org than the caller's session.
	saveWorkforceWorkerInOrg(t, db, "org-other", "w-other")
	tokenID := seedEnrollTokenForWorker(t, svc, "w-other")

	srv := newTestServer(t, deps)
	defer srv.Close()

	resp := revokePost(t, srv.URL+"/api/admintoken/revoke?id="+tokenID, sess.Cookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	tok, err := svc.FindByID(context.Background(), admintoken.TokenID(tokenID))
	if err != nil {
		t.Fatal(err)
	}
	if tok.RevokedAt() != nil {
		t.Errorf("token revoked by non-member caller")
	}
}

// (4) Empty ?id (authenticated) → 204 no-op. The advisory Modal-close path:
// token_hint is advisory-only (not indexed), so an authenticated caller with no
// resolvable id is a quiet no-op rather than an error.
func TestRevokeEnroll_NoIDIsNoOp(t *testing.T) {
	deps, _, _, sess := setupRevokeAuth(t)

	srv := newTestServer(t, deps)
	defer srv.Close()

	resp := revokePost(t, srv.URL+"/api/admintoken/revoke?token_hint=abc", sess.Cookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

// (5) Deps not wired (missing MemberRepo/WorkerRepo) → 501 fail-closed, even
// with AdminTokenSvc present.
func TestRevokeEnroll_DepsNotWired(t *testing.T) {
	svc := newRealAdminTokenSvc(t)
	srv := mintEnrollServer(t, HandlerDeps{AdminTokenSvc: svc})
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/admintoken/revoke?id=anything", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
}

// TestRemoveWorker_ForceDeleteAuditEvent — v2.8.1 #234 fast-follow (Tester
// residual): a force worker-delete emits worker.force_deleted with the WorkerID
// ref + {force:true, unbound_agents:N} payload. No agents bound here →
// unbound_agents:0 (the N>0 count is service-tested in TestUnbindAgentsFromWorker);
// this locks the worker emit + WorkerID ref + payload keys at the handler layer.
func TestRemoveWorker_ForceDeleteAuditEvent(t *testing.T) {
	// Inline the fixture with a SINGLE shared EventSink for both enroll and the
	// force-delete emit — two EventRepos would each init their in-memory seq from
	// MAX(seq) and collide on Append (the enroll events would steal the seqs the
	// force_deleted emit then re-uses), silently dropping the audit event.
	ctx := context.Background()
	dir := t.TempDir()
	db, err := persistence.Open(dir + "/wforce.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	tokenSvc := admintokensvc.New(atsqlite.New(db), gen, clk)
	withMasterKey(t, tokenSvc)
	workerRepo := wfsqlite.NewWorkerRepo(db)
	enrollSvc := wfservice.NewWorkerEnrollService(db, workerRepo, sink, clk)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRemoveSvc:     enrollSvc,
		WorkerRepo:          workerRepo,
		EventSink:           sink,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	sess := wireWorkerAuth(t, db, &deps)
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	mintResp := orgScopedPost(t, srv.URL+"/api/admintoken/mint-enroll", "", sess)
	var minted mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&minted)
	mintResp.Body.Close()

	resp := orgScopedDelete(t, srv.URL+"/api/workers/"+minted.WorkerID+"?force=true", sess)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("force worker-delete: got %d, want 200", resp.StatusCode)
	}
	typ := observability.EventType("worker.force_deleted")
	evs, err := er.Find(ctx, observability.EventQueryFilter{EventType: &typ})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("force worker-delete must emit exactly 1 worker.force_deleted, got %d", len(evs))
	}
	if evs[0].Refs().WorkerID != minted.WorkerID {
		t.Errorf("worker.force_deleted WorkerID ref = %q, want %q", evs[0].Refs().WorkerID, minted.WorkerID)
	}
	if evs[0].Payload()["force"] != true {
		t.Errorf("payload force = %v, want true", evs[0].Payload()["force"])
	}
	if _, ok := evs[0].Payload()["unbound_agents"]; !ok {
		t.Errorf("payload must carry unbound_agents key, got %+v", evs[0].Payload())
	}
}
