package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/oopslink/agent-center/internal/admintoken"
)

// fakeVerifier is a hand-rolled stub of the Verifier interface that
// lives entirely in-memory. Each call to VerifyPlaintext consults a
// map keyed by plaintext.
type fakeVerifier struct {
	mu       sync.Mutex
	tokens   map[string]*admintoken.AdminToken
	errors   map[string]error
	usedHits int64
}

func (f *fakeVerifier) VerifyPlaintext(ctx context.Context, plaintext string) (*admintoken.AdminToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.errors[plaintext]; ok {
		return nil, err
	}
	if tok, ok := f.tokens[plaintext]; ok {
		return tok, nil
	}
	return nil, admintoken.ErrTokenNotFound
}

func (f *fakeVerifier) MarkUsedAsync(id admintoken.TokenID) {
	atomic.AddInt64(&f.usedHits, 1)
}

// downstream is the handler the middleware wraps. It echoes whether
// AuthContext is present + records the scopes it saw.
type recordingHandler struct {
	called bool
	auth   AuthContext
	body   string
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	auth, _ := AuthFromContext(r.Context())
	h.auth = auth
	_, _ = io.WriteString(w, h.body)
}

func mintAR(t *testing.T, scopes ...admintoken.Scope) *admintoken.AdminToken {
	t.Helper()
	if len(scopes) == 0 {
		scopes = []admintoken.Scope{"*"}
	}
	ar, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID: "T-mid", Owner: "cli:test", Scopes: scopes,
		ValueHash: admintoken.HashPlaintext("acat_test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ar
}

// =============================================================================
// AuthMiddleware
// =============================================================================

func TestAuthMiddleware_PublicHealthPassThrough(t *testing.T) {
	v := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}}
	down := &recordingHandler{body: "ok"}
	h := AuthMiddleware(v)(down)

	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !down.called {
		t.Fatal("downstream should be called for /admin/health without bearer")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAuthMiddleware_MissingBearer401(t *testing.T) {
	v := &fakeVerifier{}
	down := &recordingHandler{}
	h := AuthMiddleware(v)(down)
	req := httptest.NewRequest(http.MethodGet, "/admin/workforce/worker/find-all", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth_missing") {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if down.called {
		t.Fatal("downstream should NOT be called without bearer")
	}
}

func TestAuthMiddleware_InvalidFormat401(t *testing.T) {
	v := &fakeVerifier{}
	h := AuthMiddleware(v)(&recordingHandler{})
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.Header.Set("Authorization", "Bearer xyz") // no acat_ prefix
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth_invalid_format") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAuthMiddleware_UnknownToken401(t *testing.T) {
	v := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}}
	h := AuthMiddleware(v)(&recordingHandler{})
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.Header.Set("Authorization", "Bearer acat_nope")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth_unknown") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAuthMiddleware_RevokedToken401(t *testing.T) {
	v := &fakeVerifier{
		errors: map[string]error{"acat_x": admintoken.ErrTokenRevoked},
	}
	h := AuthMiddleware(v)(&recordingHandler{})
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.Header.Set("Authorization", "Bearer acat_x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth_revoked") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAuthMiddleware_NilVerifierFailsClosed(t *testing.T) {
	h := AuthMiddleware(nil)(&recordingHandler{})
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.Header.Set("Authorization", "Bearer acat_x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAuthMiddleware_UnexpectedErrorReturnsGeneric401(t *testing.T) {
	v := &fakeVerifier{
		errors: map[string]error{"acat_x": errors.New("boom")},
	}
	h := AuthMiddleware(v)(&recordingHandler{})
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.Header.Set("Authorization", "Bearer acat_x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth_failed") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAuthMiddleware_HappyPath_InjectsAuthContextAndMarks(t *testing.T) {
	tok := mintAR(t)
	v := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{"acat_ok": tok}}
	down := &recordingHandler{body: "ok"}
	h := AuthMiddleware(v)(down)
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.Header.Set("Authorization", "Bearer acat_ok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !down.called {
		t.Fatal("downstream not invoked")
	}
	if down.auth.TokenID != tok.ID() {
		t.Fatalf("token id missing: %+v", down.auth)
	}
	if atomic.LoadInt64(&v.usedHits) != 1 {
		t.Fatalf("MarkUsedAsync expected 1 hit, got %d", v.usedHits)
	}
}

// =============================================================================
// RequireScope (handler-level helper)
// =============================================================================

func TestRequireScope_Missing401(t *testing.T) {
	// No AuthContext in ctx → 401.
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	if RequireScope(rec, req, "admin:token") {
		t.Fatal("RequireScope should fail without auth context")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestRequireScope_Forbidden403(t *testing.T) {
	ctx := context.WithValue(context.Background(), authKey{}, AuthContext{
		TokenID: "T-1", Owner: "cli:x",
		Scopes: []admintoken.Scope{"task:*"},
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	if RequireScope(rec, req, "admin:token") {
		t.Fatal("RequireScope should reject scope mismatch")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "scope_forbidden") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestRequireScope_HappyAndWildcard(t *testing.T) {
	// Exact match.
	ctx := context.WithValue(context.Background(), authKey{}, AuthContext{
		Scopes: []admintoken.Scope{"admin:token"},
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	if !RequireScope(rec, req, "admin:token") {
		t.Fatal("expected pass for exact scope match")
	}

	// Wildcard.
	ctx2 := context.WithValue(context.Background(), authKey{}, AuthContext{
		Scopes: []admintoken.Scope{"*"},
	})
	req2 := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx2)
	rec2 := httptest.NewRecorder()
	if !RequireScope(rec2, req2, "anything") {
		t.Fatal("expected pass for wildcard")
	}
}
