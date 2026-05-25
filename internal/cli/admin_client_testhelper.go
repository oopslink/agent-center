// Package cli — admin_client_testhelper.go: test scaffolding that spins
// up an in-process admin endpoint and returns a Client pointing at it.
//
// Use this in CLI handler tests instead of the legacy newTestApp() path
// (which wires Services directly on the App). v2.2 Phase B per
// docs/plans/v2.2-audits/v22-B-cli-refactor-audit.md: handlers must
// route through Client, not the Service fields.
//
// Usage:
//
//	app, cleanup := setupAdminServerForTests(t)
//	defer cleanup()
//	// app.Client is wired; app.DB / app.WorkerRepo / ... are also
//	// populated because the helper builds a full App + serves it.
//
// The helper is exported (lowercase but referenced from _test.go files
// in this same package) and lives in a non-test file so it can be
// referenced from package-level documentation/examples. It compiles
// into the production binary as dead code — harmless because the
// listener never starts until the helper is called.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admin/api"
	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

// setupAdminServerForTests builds an App with a fresh DB, starts the
// admin endpoint on a unix socket, and returns the App with its Client
// wired to dial that socket. The cleanup func shuts the server down,
// closes the DB, and removes the socket file.
//
// The socket lives under /tmp (NOT t.TempDir()) because macOS limits
// unix socket paths to 104 bytes and /var/folders/... eats most of the
// budget — see admin/api/server_test.go::shortSocketPath.
func setupAdminServerForTests(t *testing.T) (*App, func()) {
	t.Helper()

	// Fresh on-disk DB with migrations applied (same pattern as
	// handlers_test.go::newTestApp so existing test fixtures keep
	// working when ported).
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("setupAdminServerForTests: open db: %v", err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("setupAdminServerForTests: migrate: %v", err)
	}

	cfg := config.DefaultConfig()
	app, err := NewApp(cfg, db, clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)))
	if err != nil {
		_ = db.Close()
		t.Fatalf("setupAdminServerForTests: NewApp: %v", err)
	}

	sock := testShortSocketPath(t, "admin.sock")
	srv := api.NewServerWithDeps(sock, api.ServerDeps{Queue: app.DispatchQueue})
	// v2.3-3a (task #28): mint a test bearer with `*` scope so all
	// endpoints pass auth + scope checks, then wrap the mux with the
	// same AuthMiddleware production uses. Tests that want to
	// exercise the unauthenticated / unscoped paths can clear the
	// Client token via app.Client.WithToken("") or construct their
	// own request.
	res, terr := app.AdminTokenSvc.Create(context.Background(), admintokensvc.CreateCommand{
		Owner:     "system:test",
		Scopes:    []admintoken.Scope{"*"},
		CreatedBy: "test",
	})
	if terr != nil {
		_ = db.Close()
		t.Fatalf("setupAdminServerForTests: mint test token: %v", terr)
	}
	// Tests don't care about LastUsedAt bookkeeping; closing the pump up
	// front ensures the background UPDATE never races against the test's
	// foreground writes on the same SQLite file. Without this, a quick
	// burst of admin requests (e.g. open + send + send) could see
	// SQLITE_BUSY (5) when the pump's first UPDATE happens to overlap
	// with a foreground transaction. Idempotent: cleanup() Close()s again
	// safely.
	app.AdminTokenSvc.Close()
	srv.SetHandler(api.AuthMiddleware(app.AdminTokenSvc)(
		api.WithDeps(adminDepsFromApp(app))(srv.Handler())))

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	waitForUnixSocket(t, sock, errCh, 2*time.Second)

	app.Client = NewClient(sock, 5*time.Second).WithToken(res.Plaintext)

	cleanup := func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		// v2.3-3a (task #28): stop the AdminToken bookkeeping pump
		// BEFORE closing the DB to avoid a goroutine racing with the
		// torn-down sql.DB handle on the next test.
		if app.AdminTokenSvc != nil {
			app.AdminTokenSvc.Close()
		}
		_ = db.Close()
	}
	return app, cleanup
}

// testShortSocketPath mirrors admin/api/server_test.go::shortSocketPath
// (108-byte unix socket limit). Lives here so cli/_test.go files don't
// need to dive into the api package's testing helpers.
func testShortSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ac-cli-adm-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// waitForUnixSocket polls dial(unix, sock) until success or deadline.
// Mirrors admin/api/server_test.go::waitForSocket.
func waitForUnixSocket(t *testing.T, sock string, errCh <-chan error, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case lserr := <-errCh:
			if lserr != nil && !errors.Is(lserr, http.ErrServerClosed) {
				t.Fatalf("admin ListenAndServe: %v", lserr)
			}
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("admin socket %q never accepted dial in %s (last err=%v)",
				sock, timeout, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// _ keeps fmt imported for future helper signatures that emit
// diagnostics (e.g. logging admin endpoint errors during tests).
var _ = fmt.Sprintf
