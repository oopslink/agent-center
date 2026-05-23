package spa

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// Tests inject a synthetic fs.FS via HandlerFromFS so they don't depend
// on a populated embed.

func newSPAFs() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!doctype html><html><body><div id=root></div></body></html>"),
		},
		"assets/index-abc123.js": &fstest.MapFile{
			Data: []byte("console.log('bundle');"),
		},
		"assets/index-abc123.css": &fstest.MapFile{
			Data: []byte("body{font-family:sans-serif}"),
		},
		"favicon.svg": &fstest.MapFile{
			Data: []byte("<svg/>"),
		},
	}
}

func TestHandler_ServesIndexAtRoot(t *testing.T) {
	h := HandlerFromFS(newSPAFs())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "id=root") {
		t.Fatalf("expected SPA root html; got %q", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type %q", ct)
	}
}

func TestHandler_ServesAssetsVerbatim(t *testing.T) {
	h := HandlerFromFS(newSPAFs())
	req := httptest.NewRequest(http.MethodGet, "/assets/index-abc123.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "bundle") {
		t.Fatalf("expected bundle content; got %q", body)
	}
}

func TestHandler_SPAFallbackForClientRoute(t *testing.T) {
	// A path that doesn't exist in the FS (e.g. react-router's
	// /channels/alpha) should serve index.html so client-side routing
	// can take over on reload + deep-link.
	h := HandlerFromFS(newSPAFs())
	req := httptest.NewRequest(http.MethodGet, "/channels/alpha", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "id=root") {
		t.Fatalf("expected index.html fallback; got %q", body)
	}
}

func TestHandler_NotBuiltWhenIndexMissing(t *testing.T) {
	h := HandlerFromFS(fstest.MapFS{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d (want 503)", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "SPA not built") {
		t.Fatalf("expected build hint; got %q", body)
	}
}

func TestHandler_DirectoryRequestFallsToIndex(t *testing.T) {
	// A request to a directory (no trailing-slash file) should also
	// fall through to index.html — react-router treats /channels as a
	// route, not a directory listing.
	fs := fstest.MapFS{
		"index.html":      &fstest.MapFile{Data: []byte("<root/>")},
		"assets/foo.txt":  &fstest.MapFile{Data: []byte("foo")},
		"assets/bar.txt":  &fstest.MapFile{Data: []byte("bar")},
	}
	h := HandlerFromFS(fs)
	req := httptest.NewRequest(http.MethodGet, "/assets", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "root") {
		t.Fatalf("expected index.html (directory request falls through); got %q", body)
	}
}

func TestFS_ReturnsRootedSubFS(t *testing.T) {
	// Exposed FS() should be rooted at dist/ contents — empty in tests
	// because the embed has only .gitkeep until pnpm build runs. Just
	// assert the function doesn't blow up + the type matches.
	f := FS()
	if f == nil {
		t.Fatal("FS() returned nil")
	}
}

func TestHandler_EmbeddedHandlerStableInterface(t *testing.T) {
	// Handler() reads the package-level go:embed FS — usually empty in
	// tests. Without index.html present it should fall back to the
	// not-built handler, NOT panic.
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	// In CI with an empty dist/ the response is 503 not-built; after
	// `make build-frontend` it's 200. Either is acceptable shape;
	// what we care about is the handler is non-nil + doesn't panic.
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status %d", w.Code)
	}
}
