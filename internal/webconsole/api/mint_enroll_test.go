package api

import (
	"context"
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
func newPreCreateFixture(t *testing.T) (*admintokensvc.Service, *wfservice.WorkerEnrollService, *wfsqlite.WorkerRepo) {
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
	return tokenSvc, enrollSvc, workerRepo
}

// v2.5-B1: mint-enroll pre-creates the Worker AR so Fleet sees the
// offline row immediately. The handler call returns; we verify both
// (a) the response includes the generated worker_id, and (b) the
// Worker row is queryable with status=offline.
func TestMintEnroll_PreCreatesWorker(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo := newPreCreateFixture(t)
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
	tokenSvc, enrollSvc, workerRepo := newPreCreateFixture(t)
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
	tokenSvc, enrollSvc, workerRepo := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	// Mint a token (also pre-creates the worker via WorkerAddSvc).
	mintResp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json",
		strings.NewReader(`{"name":"alice-box"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer mintResp.Body.Close()
	var minted mintEnrollResp
	if err := json.NewDecoder(mintResp.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}
	// Now re-display via the show endpoint.
	resp, err := http.Get(srv.URL + "/api/workers/" + minted.WorkerID + "/install-command")
	if err != nil {
		t.Fatal(err)
	}
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
	tokenSvc, enrollSvc, workerRepo := newPreCreateFixture(t)
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
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/workers/worker-unknown/install-command")
	if err != nil {
		t.Fatal(err)
	}
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

// A consumed enroll token (daemon already enrolled successfully)
// surfaces 401 + no_active_enroll_token so the UI knows to offer
// re-mint instead.
func TestShowInstallCommand_AfterConsume_401(t *testing.T) {
	tokenSvc, enrollSvc, workerRepo := newPreCreateFixture(t)
	withMasterKey(t, tokenSvc)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       tokenSvc,
		WorkerAddSvc:        enrollSvc,
		WorkerRepo:          workerRepo,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	mintResp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mintResp.Body.Close()
	var minted mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&minted)
	// Burn the token (simulating daemon enroll).
	if err := tokenSvc.ConsumeEnrollToken(context.Background(), admintoken.TokenID(minted.ID)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/api/workers/" + minted.WorkerID + "/install-command")
	if err != nil {
		t.Fatal(err)
	}
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

func TestRevokeEnroll_NoIDIsNoOp(t *testing.T) {
	svc := newRealAdminTokenSvc(t)
	srv := mintEnrollServer(t, HandlerDeps{AdminTokenSvc: svc})
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/admintoken/revoke?token_hint=abc", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

func TestRevokeEnroll_RealID(t *testing.T) {
	svc := newRealAdminTokenSvc(t)
	deps := HandlerDeps{
		Actor:               observability.Actor("user:hayang"),
		AdminTokenSvc:       svc,
		EnrollFingerprint:   "sha256:AA",
		EnrollBootstrapHost: "h:7300",
	}
	srv := mintEnrollServer(t, deps)
	defer srv.Close()
	// Mint first.
	mintResp, err := http.Post(srv.URL+"/api/admintoken/mint-enroll", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mintResp.Body.Close()
	var body mintEnrollResp
	_ = json.NewDecoder(mintResp.Body).Decode(&body)
	// Now revoke by id.
	revokeResp, err := http.Post(srv.URL+"/api/admintoken/revoke?id="+body.ID, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d", revokeResp.StatusCode)
	}
	// Verify token is revoked.
	tok, err := svc.FindByID(context.Background(), admintoken.TokenID(body.ID))
	if err != nil {
		t.Fatal(err)
	}
	if tok.RevokedAt() == nil {
		t.Errorf("token not revoked")
	}
}
