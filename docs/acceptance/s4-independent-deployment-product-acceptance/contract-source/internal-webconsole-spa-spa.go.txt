// Package spa serves the embedded React SPA build (web/dist/) as the
// catch-all handler under the web console. Vite outputs hashed assets
// to `dist/assets/*` and a root `dist/index.html` that bootstraps
// react-router; the handler serves /assets/* verbatim and falls back
// to index.html for every other path so client-side routing
// (`/channels/alpha`, `/secrets`, etc.) works on reload + deep-link.
//
// Build flow:
//  1. `make build-frontend` runs `pnpm run build` with vite outDir
//     pointing here (../../../web → internal/webconsole/spa/dist).
//  2. `make build-backend` runs `go build`; go:embed bakes the
//     populated dist into the binary.
//  3. `make build` runs both in order.
//
// Dev flow (vite proxy → :7100) is unchanged: the SPA chunk in the
// binary is not consulted when the developer hits the vite dev server.
package spa

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// all: prefix includes dotfiles + nested chunks. dist/ is a tracked
// directory holding `.gitkeep` so go:embed succeeds even before the
// frontend is built; in that case Handler renders a "not built" page.
//
//go:embed all:dist
var embeddedFS embed.FS

// FS returns the embedded SPA filesystem (rooted at the dist/ directory
// contents). Exposed for tests that want to assert against the asset
// list without going through HTTP.
func FS() fs.FS {
	sub, err := fs.Sub(embeddedFS, "dist")
	if err != nil {
		return embeddedFS
	}
	return sub
}

// Handler returns an http.Handler that serves the embedded SPA.
// If the embed is empty (no `make build-frontend` was run), returns a
// placeholder handler with a build hint.
func Handler() http.Handler {
	return HandlerFromFS(FS())
}

// HandlerFromFS is the testable core: callers can inject a synthetic
// fs.FS (e.g. fstest.MapFS) so tests don't depend on a populated embed.
func HandlerFromFS(spaFS fs.FS) http.Handler {
	// Readiness: a real SPA build always has index.html at the root.
	if _, err := fs.Stat(spaFS, "index.html"); err != nil {
		return notBuiltHandler()
	}
	fileServer := http.FileServer(http.FS(spaFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urlPath := strings.TrimPrefix(r.URL.Path, "/")
		if urlPath == "" {
			urlPath = "index.html"
		}
		// Assets fingerprinted by vite live under /assets/.
		// Anything that exists in the FS as-is is served directly;
		// everything else falls through to index.html so react-router
		// can take over.
		if _, err := fs.Stat(spaFS, urlPath); err != nil || isDir(spaFS, urlPath) {
			serveIndex(w, r, spaFS)
			return
		}
		// Set a default no-cache for index.html itself; vite's hashed
		// assets are content-addressed so they can cache long.
		if path.Base(urlPath) == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func isDir(spaFS fs.FS, p string) bool {
	info, err := fs.Stat(spaFS, p)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func serveIndex(w http.ResponseWriter, r *http.Request, spaFS fs.FS) {
	body, err := fs.ReadFile(spaFS, "index.html")
	if err != nil {
		notBuiltHandler().ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

func notBuiltHandler() http.Handler {
	const body = `<!doctype html>
<html>
  <head><meta charset="utf-8"><title>agent-center — SPA not built</title></head>
  <body style="font-family: ui-sans-serif, system-ui, sans-serif; padding: 2rem; max-width: 40rem; margin: 0 auto;">
    <h1>SPA not built</h1>
    <p>The embedded React SPA bundle is missing. This binary was compiled before <code>make build-frontend</code> ran.</p>
    <p>To run the dev frontend instead, start <code>vite</code> in <code>web/</code> (proxies <code>/api</code> to this server) and open <a href="http://localhost:5173">http://localhost:5173</a>.</p>
    <p>To build the embedded bundle: <code>make build</code> at the repo root.</p>
  </body>
</html>`
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body))
	})
}
