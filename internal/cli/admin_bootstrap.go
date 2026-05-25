// Package cli — admin_bootstrap.go: ensure the admin endpoint has at
// least one valid bearer token at server boot (v2.3-3a task #28).
//
// The first time the server comes up (admin_tokens table empty), we
// mint a system-owned superuser token, write the plaintext to
// `<datadir>/bootstrap_token` (mode 0600), and log a banner pointing
// at the file. Subsequent boots are a no-op — operators rotate the
// token via `agent-center admin token create / revoke`.
//
// Why a file and not stdout: the operator launching the server may not
// even be the same user as the one who later runs `agent-center admin
// token list`; a file under datadir keeps the secret reachable across
// processes / sessions while still under the same uid.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
)

// BootstrapTokenFilename is the on-disk name of the bootstrap token file
// (joined with the resolved datadir).
const BootstrapTokenFilename = "bootstrap_token"

// EnsureBootstrapToken mints a system token + writes plaintext to disk
// iff the admin_tokens table is currently empty.
//
// datadir may be empty — in that case we fall back to filepath.Dir of
// the configured sqlite path (cfg.Server.SqlitePath). logger is the
// boot-banner writer (typically a fmt.Fprintf wrapper on stderr).
//
// Returns nil on success or when bootstrap is unnecessary (table
// non-empty). Failure to write the file is fatal — the operator MUST
// have a way to authenticate, and silently swallowing the error would
// produce a deployment where the admin endpoint exists but nobody can
// reach it.
func EnsureBootstrapToken(ctx context.Context, app *App, datadir string, logger func(string)) error {
	if app == nil || app.AdminTokenSvc == nil {
		return errors.New("admin bootstrap: app.AdminTokenSvc not wired")
	}
	if logger == nil {
		logger = func(string) {}
	}
	existing, err := app.AdminTokenSvc.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("admin bootstrap: list tokens: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}
	// Resolve datadir. Empty → derive from sqlite_path (its containing
	// dir). If neither is set, fail loudly: writing the secret next to
	// the current working dir would be a footgun.
	dir := strings.TrimSpace(datadir)
	if dir == "" {
		sqlite := strings.TrimSpace(app.Config.Server.SqlitePath)
		if sqlite == "" {
			return errors.New("admin bootstrap: neither datadir nor server.sqlite_path set; refusing to write token to cwd")
		}
		dir = filepath.Dir(sqlite)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("admin bootstrap: mkdir %q: %w", dir, err)
	}
	res, err := app.AdminTokenSvc.Create(ctx, admintokensvc.CreateCommand{
		Owner:     "system:bootstrap",
		Scopes:    []admintoken.Scope{"*"},
		CreatedBy: "system",
	})
	if err != nil {
		return fmt.Errorf("admin bootstrap: create token: %w", err)
	}
	path := filepath.Join(dir, BootstrapTokenFilename)
	// Write with O_EXCL so a concurrent boot doesn't clobber a fresher
	// token that landed milliseconds earlier. If the file already
	// exists (unusual — would mean we passed the empty-table check but
	// someone else wrote the file out of band), surface the error.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("admin bootstrap: open %q: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(res.Plaintext + "\n"); err != nil {
		return fmt.Errorf("admin bootstrap: write %q: %w", path, err)
	}
	logger(fmt.Sprintf("admin token bootstrap: wrote %s (scope=* owner=system:bootstrap)", path))
	return nil
}
