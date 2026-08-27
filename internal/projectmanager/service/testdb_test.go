package service

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
)

var migratedTestDBTemplate = struct {
	once sync.Once
	path string
	err  error
}{}

func openMigratedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	migratedTestDBTemplate.once.Do(func() {
		dir, err := os.MkdirTemp("", "ac-pm-service-template-*")
		if err != nil {
			migratedTestDBTemplate.err = err
			return
		}
		path := filepath.Join(dir, "template.db")
		db, err := persistence.Open(path)
		if err != nil {
			migratedTestDBTemplate.err = err
			return
		}
		defer db.Close()
		if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
			migratedTestDBTemplate.err = err
			return
		}
		if _, err := db.ExecContext(context.Background(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			migratedTestDBTemplate.err = err
			return
		}
		migratedTestDBTemplate.path = path
	})
	if migratedTestDBTemplate.err != nil {
		t.Fatalf("prepare migrated test DB template: %v", migratedTestDBTemplate.err)
	}
	dst := filepath.Join(t.TempDir(), "test.db")
	if err := copyFile(dst, migratedTestDBTemplate.path); err != nil {
		t.Fatalf("copy migrated test DB template: %v", err)
	}
	db, err := persistence.Open(dst)
	if err != nil {
		t.Fatalf("open migrated test DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
