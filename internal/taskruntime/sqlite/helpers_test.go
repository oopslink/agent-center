package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Seed a project / worker so FKs hold.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id) VALUES ('P-1', 'Proj 1', '2026-05-21T12:00:00Z', '2026-05-21T12:00:00Z', 'user:hayang')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workers (id, status, capabilities, working_seconds, enrolled_at, created_at, updated_at) VALUES ('W-1', 'online', '["claude-code"]', 0, '2026-05-21T12:00:00Z', '2026-05-21T12:00:00Z', '2026-05-21T12:00:00Z')`); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	return db
}
