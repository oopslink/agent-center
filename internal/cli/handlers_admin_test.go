package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newTestAppWithFileDB(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "agent-center.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Server.SqlitePath = dbPath
	app, err := NewApp(cfg, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestAdminBackup_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "dest")
	app := newTestAppWithFileDB(t)
	cmd := findCmd(app.AdminCommands(), "backup")
	if cmd == nil {
		t.Fatal("backup command missing")
	}
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	h := cmd.Flags(fs)
	if err := fs.Parse([]string{"--dest", dest}); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	code := h(context.Background(), nil, &out, &errw)
	if code != ExitOK {
		t.Fatalf("code: %d errw=%q", code, errw.String())
	}
	// Verify dest contains at least one dated subdir.
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one dated subdir")
	}
}

func TestAdminBackup_MissingDest(t *testing.T) {
	app := newTestAppWithFileDB(t)
	cmd := findCmd(app.AdminCommands(), "backup")
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	h := cmd.Flags(fs)
	_ = fs.Parse([]string{})
	var out, errw bytes.Buffer
	code := h(context.Background(), nil, &out, &errw)
	if code != ExitUsage {
		t.Errorf("code: %d", code)
	}
	if !strings.Contains(errw.String(), "usage_error") {
		t.Errorf("err: %q", errw.String())
	}
}

func TestAdminBackup_RunFails(t *testing.T) {
	// Point DBPath to a non-existent file → Run will fail on copy.
	app := newTestAppWithFileDB(t)
	// Override the configured SQLite path so it doesn't match the
	// real (open) DB file → backup will try to open a non-existing
	// file and fail with copy_failed.
	app.Config.Server.SqlitePath = "/does/not/exist/agent-center.db"
	dest := filepath.Join(t.TempDir(), "dest")
	cmd := findCmd(app.AdminCommands(), "backup")
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	h := cmd.Flags(fs)
	_ = fs.Parse([]string{"--dest", dest})
	var out, errw bytes.Buffer
	code := h(context.Background(), nil, &out, &errw)
	if code != ExitBusinessError {
		t.Errorf("code: %d, want ExitBusinessError", code)
	}
	if !strings.Contains(errw.String(), "backup_failed") {
		t.Errorf("err: %q", errw.String())
	}
}

func TestAdminBackup_JSON(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dest")
	app := newTestAppWithFileDB(t)
	cmd := findCmd(app.AdminCommands(), "backup")
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	h := cmd.Flags(fs)
	if err := fs.Parse([]string{"--dest", dest, "--format", "json"}); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if code := h(context.Background(), nil, &out, &errw); code != ExitOK {
		t.Fatalf("code: %d errw=%q", code, errw.String())
	}
	if !strings.Contains(out.String(), `"dest_file"`) {
		t.Errorf("out: %q", out.String())
	}
}
