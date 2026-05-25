// Package api — secret_resolve_test.go: HTTP handler tests for POST
// /admin/secret/user-secret/resolve. v2.3-3b (task #29). Exercises
// happy round-trip, scope gating, unknown secret, and missing-name.
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	secretsqlite "github.com/oopslink/agent-center/internal/secretmgmt/sqlite"
)

// setupSecretResolveFixture builds a real UserSecretService + a real
// SecretResolutionService backed by a freshly migrated in-tmp sqlite DB
// + a deterministic master key. Returns the wired-up Server plus a
// helper that creates a fresh secret and returns its name.
func setupSecretResolveFixture(t *testing.T) (*Server, HandlerDeps, func(name, plain string), func()) {
	t.Helper()
	dbPath := t.TempDir() + "/test.db"
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("migrate: %v", err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("event repo: %v", err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	repo := secretsqlite.NewUserSecretRepo(db)
	mk := mustGenerateMasterKey(t)
	createSvc := secretservice.NewUserSecretService(db, repo, gen, sink, clk, mk)
	resolveSvc := secretservice.NewSecretResolutionService(db, repo, sink, clk, mk)
	srv := NewServer("/tmp/unused.sock")
	deps := HandlerDeps{
		UserSecretSvc:        createSvc,
		UserSecretResolveSvc: resolveSvc,
		UserSecretRepo:       repo,
	}
	mkSecret := func(name, plain string) {
		_, err := createSvc.Create(context.Background(), secretservice.CreateSecretCommand{
			Name:          name,
			Kind:          secretmgmt.UserSecretKindMCP,
			Plaintext:     []byte(plain),
			ActorIdentity: observability.Actor("user:test"),
		})
		if err != nil {
			t.Fatalf("Create secret %s: %v", name, err)
		}
	}
	cleanup := func() {
		_ = db.Close()
	}
	return srv, deps, mkSecret, cleanup
}

// mustGenerateMasterKey returns a randomly-generated MasterKey suitable
// for one-shot test usage.
func mustGenerateMasterKey(t *testing.T) *secretmgmt.MasterKey {
	t.Helper()
	mk, err := secretmgmt.GenerateMasterKey()
	if err != nil {
		t.Fatalf("generate master key: %v", err)
	}
	return mk
}

func TestSecretResolve_HappyPath_ReturnsBase64Plaintext(t *testing.T) {
	srv, deps, mkSecret, cleanup := setupSecretResolveFixture(t)
	defer cleanup()
	mkSecret("db_password", "s3cret-val")
	body := mustJSON(t, map[string]any{"name": "db_password"})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve",
			bytes.NewReader(body)),
		"secret:resolve"), deps)
	rec := httptest.NewRecorder()
	srv.secretResolveHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		PlaintextBase64 string `json:"plaintext_base64"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if out.Name != "db_password" {
		t.Fatalf("name=%q", out.Name)
	}
	got, err := base64.StdEncoding.DecodeString(out.PlaintextBase64)
	if err != nil {
		t.Fatalf("decode plaintext: %v", err)
	}
	if string(got) != "s3cret-val" {
		t.Fatalf("plaintext round-trip mismatch: got=%q want=%q", got, "s3cret-val")
	}
	if out.ID == "" {
		t.Fatal("id empty")
	}
}

func TestSecretResolve_ForbiddenWithoutScope(t *testing.T) {
	srv, deps, mkSecret, cleanup := setupSecretResolveFixture(t)
	defer cleanup()
	mkSecret("x", "y")
	body := mustJSON(t, map[string]any{"name": "x"})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve",
			bytes.NewReader(body)),
		"task:*"), deps)
	rec := httptest.NewRecorder()
	srv.secretResolveHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "scope_forbidden") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSecretResolve_UnknownSecret_404(t *testing.T) {
	srv, deps, _, cleanup := setupSecretResolveFixture(t)
	defer cleanup()
	body := mustJSON(t, map[string]any{"name": "missing"})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve",
			bytes.NewReader(body)),
		"secret:resolve"), deps)
	rec := httptest.NewRecorder()
	srv.secretResolveHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSecretResolve_RevokedSecret_403(t *testing.T) {
	srv, deps, mkSecret, cleanup := setupSecretResolveFixture(t)
	defer cleanup()
	mkSecret("revoked_secret", "abc")
	// Revoke it via UserSecretSvc.
	sec, err := deps.UserSecretRepo.FindByName(context.Background(), "revoked_secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.UserSecretSvc.Revoke(context.Background(), secretservice.RevokeSecretCommand{
		ID:            sec.ID(),
		Reason:        secretmgmt.UserSecretRevokedReasonManual,
		Message:       "test",
		Version:       sec.Version(),
		ActorIdentity: observability.Actor("user:test"),
	}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	body := mustJSON(t, map[string]any{"name": "revoked_secret"})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve",
			bytes.NewReader(body)),
		"secret:resolve"), deps)
	rec := httptest.NewRecorder()
	srv.secretResolveHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("revoked secret should 403; got status=%d body=%s",
			rec.Code, rec.Body.String())
	}
}

func TestSecretResolve_MissingName_400(t *testing.T) {
	srv, deps, _, cleanup := setupSecretResolveFixture(t)
	defer cleanup()
	body := mustJSON(t, map[string]any{})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve",
			bytes.NewReader(body)),
		"secret:resolve"), deps)
	rec := httptest.NewRecorder()
	srv.secretResolveHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestSecretResolve_NotImplementedWhenSvcUnwired(t *testing.T) {
	srv := NewServer("/tmp/unused.sock")
	body := mustJSON(t, map[string]any{"name": "x"})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve",
			bytes.NewReader(body)),
		"secret:resolve"), HandlerDeps{})
	rec := httptest.NewRecorder()
	srv.secretResolveHandler(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d", rec.Code)
	}
}
