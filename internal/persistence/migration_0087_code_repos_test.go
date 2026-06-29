package persistence

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration0087_WorkspaceCodeRepos verifies the v2.18.4 BE-1 schema: the new
// code_repos table and the additive pm_code_repo_refs columns (repo_id nullable,
// is_primary DEFAULT 0) with correct defaults on existing refs.
func TestMigration0087_WorkspaceCodeRepos(t *testing.T) {
	db, err := Open(MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mig := NewMigrator(db)
	ctx := context.Background()
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := mig.Down(ctx, 86); err != nil {
		t.Fatalf("Down(86): %v", err)
	}
	if tableExists0087(t, db, "code_repos") {
		t.Fatal("code_repos should be absent at v86")
	}
	// Seed a pre-0087 url-only ref (without the new columns).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pm_code_repo_refs (id, project_id, url, added_by, created_at)
		 VALUES ('ref1','P1','https://x/app','user:a','2026-06-29T00:00:00Z')`); err != nil {
		t.Fatalf("seed ref: %v", err)
	}

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up (apply 0087): %v", err)
	}

	// Existing ref reads the column defaults (repo_id NULL, is_primary 0).
	var repoID interface{}
	var isPrimary int
	if err := db.QueryRowContext(ctx, `SELECT repo_id, is_primary FROM pm_code_repo_refs WHERE id='ref1'`).Scan(&repoID, &isPrimary); err != nil {
		t.Fatal(err)
	}
	if repoID != nil {
		t.Fatalf("legacy ref repo_id = %v, want NULL", repoID)
	}
	if isPrimary != 0 {
		t.Fatalf("legacy ref is_primary = %d, want 0", isPrimary)
	}
	// code_repos exists and accepts a row (incl. NULL credential).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO code_repos (id, organization_id, label, url, provider, created_by, created_at, updated_at, version)
		 VALUES ('r1','org1','app','https://x/app','github','user:a','2026-06-29T00:00:00Z','2026-06-29T00:00:00Z',1)`); err != nil {
		t.Fatalf("insert code_repo: %v", err)
	}

	if err := mig.Down(ctx, 86); err != nil {
		t.Fatalf("Down(86) after apply: %v", err)
	}
	if tableExists0087(t, db, "code_repos") {
		t.Fatal("code_repos should be gone after Down(86)")
	}
	if colExists0087(t, db, "pm_code_repo_refs", "repo_id") {
		t.Fatal("repo_id column should be gone after Down(86)")
	}
}

func tableExists0087(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func colExists0087(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == col {
			return true
		}
	}
	return false
}
