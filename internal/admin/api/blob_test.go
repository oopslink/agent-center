// Package api — blob_test.go: HTTP handler tests for POST /admin/blob/put.
// v2.3-3b (task #29). Exercises happy round-trip, rel_path validation, and
// scope gating.
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/blobstore"
)

// withAuth wraps a request with a fake AuthContext carrying scopes. Used by
// per-handler tests that exercise RequireScope() — middleware is set up in
// auth_test.go but per-handler tests need explicit injection.
func withAuth(r *http.Request, scopes ...admintoken.Scope) *http.Request {
	ctx := context.WithValue(r.Context(), authKey{}, AuthContext{
		TokenID: "T-test",
		Owner:   "worker:test",
		Scopes:  scopes,
	})
	return r.WithContext(ctx)
}

// withDepsCtx installs HandlerDeps into request context (bypassing the
// WithDeps middleware that production code wraps the mux with).
func withDepsCtx(r *http.Request, d HandlerDeps) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), depsKey{}, d))
}

func newTestBlobServer(t *testing.T) (*Server, blobstore.BlobStore) {
	t.Helper()
	bs, err := blobstore.NewLocalDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDir: %v", err)
	}
	srv := NewServer("/tmp/unused.sock")
	return srv, bs
}

func TestBlobPut_HappyRoundTrip(t *testing.T) {
	srv, bs := newTestBlobServer(t)
	deps := HandlerDeps{BlobStore: bs}
	payload := []byte("hello secret blob")
	body := mustJSON(t, map[string]any{
		"rel_path":       "artifacts/E-1/log-1",
		"content_base64": base64.StdEncoding.EncodeToString(payload),
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/blob/put", bytes.NewReader(body))
	req = withAuth(req, "blob:put")
	req = withDepsCtx(req, deps)
	rec := httptest.NewRecorder()
	srv.blobPutHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Verify the bytes round-trip via BlobStore.Get.
	rc, err := bs.Get(context.Background(), "artifacts/E-1/log-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got=%q want=%q", got, payload)
	}
}

func TestBlobPut_LargeBody(t *testing.T) {
	srv, bs := newTestBlobServer(t)
	deps := HandlerDeps{BlobStore: bs}
	// 1 MiB payload — comfortably under the 100 MiB cap but big enough
	// to exercise non-trivial base64 paths.
	payload := bytes.Repeat([]byte("X"), 1024*1024)
	body := mustJSON(t, map[string]any{
		"rel_path":       "artifacts/big.bin",
		"content_base64": base64.StdEncoding.EncodeToString(payload),
	})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/blob/put", bytes.NewReader(body)),
		"blob:put"), deps)
	rec := httptest.NewRecorder()
	srv.blobPutHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBlobPut_InvalidRelPath(t *testing.T) {
	srv, bs := newTestBlobServer(t)
	deps := HandlerDeps{BlobStore: bs}
	for _, bad := range []string{"", "../escape", "/absolute", "foo/../bar"} {
		body := mustJSON(t, map[string]any{
			"rel_path":       bad,
			"content_base64": base64.StdEncoding.EncodeToString([]byte("x")),
		})
		req := withDepsCtx(withAuth(
			httptest.NewRequest(http.MethodPost, "/admin/blob/put", bytes.NewReader(body)),
			"blob:put"), deps)
		rec := httptest.NewRecorder()
		srv.blobPutHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad rel_path %q should 400, got status=%d body=%s",
				bad, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "invalid_rel_path") {
			t.Fatalf("body should mention invalid_rel_path: %s", rec.Body.String())
		}
	}
}

func TestBlobPut_InvalidBase64(t *testing.T) {
	srv, bs := newTestBlobServer(t)
	deps := HandlerDeps{BlobStore: bs}
	body := mustJSON(t, map[string]any{
		"rel_path":       "ok/path",
		"content_base64": "not-valid-base64!@#",
	})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/blob/put", bytes.NewReader(body)),
		"blob:put"), deps)
	rec := httptest.NewRecorder()
	srv.blobPutHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBlobPut_ForbiddenWithoutScope(t *testing.T) {
	srv, bs := newTestBlobServer(t)
	deps := HandlerDeps{BlobStore: bs}
	body := mustJSON(t, map[string]any{
		"rel_path":       "ok/path",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	// Auth context with the wrong scope (task:* instead of blob:put).
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/blob/put", bytes.NewReader(body)),
		"task:*"), deps)
	rec := httptest.NewRecorder()
	srv.blobPutHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "scope_forbidden") {
		t.Fatalf("body should mention scope_forbidden: %s", rec.Body.String())
	}
}

func TestBlobPut_UnauthorizedWithoutAuth(t *testing.T) {
	srv, bs := newTestBlobServer(t)
	deps := HandlerDeps{BlobStore: bs}
	body := mustJSON(t, map[string]any{
		"rel_path":       "ok/path",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	req := withDepsCtx(
		httptest.NewRequest(http.MethodPost, "/admin/blob/put", bytes.NewReader(body)),
		deps)
	rec := httptest.NewRecorder()
	srv.blobPutHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBlobPut_503WhenStoreNotWired(t *testing.T) {
	srv := NewServer("/tmp/unused.sock")
	// No BlobStore wired.
	body := mustJSON(t, map[string]any{
		"rel_path":       "ok/path",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	req := withDepsCtx(withAuth(
		httptest.NewRequest(http.MethodPost, "/admin/blob/put", bytes.NewReader(body)),
		"blob:put"), HandlerDeps{})
	rec := httptest.NewRecorder()
	srv.blobPutHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestValidateRelPath(t *testing.T) {
	cases := []struct {
		in   string
		want bool // true = OK
	}{
		{"a/b", true},
		{"single", true},
		{"a/b/c.txt", true},
		{"", false},
		{"  ", false},
		{"/abs", false},
		{"../escape", false},
		{"foo/../bar", false},
		{"foo\\..\\bar", false},
	}
	for _, c := range cases {
		err := validateRelPath(c.in)
		if c.want && err != nil {
			t.Errorf("validateRelPath(%q) want OK, got %v", c.in, err)
		}
		if !c.want && err == nil {
			t.Errorf("validateRelPath(%q) want err, got nil", c.in)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
