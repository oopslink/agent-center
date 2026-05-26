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
	"github.com/oopslink/agent-center/internal/admintoken/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
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
	repo := sqlite.New(db)
	return admintokensvc.New(repo, idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{})
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
