package persistence

import (
	"context"
	"database/sql"
	"testing"
)

func TestMigration_0130_AuthorizationRevisionTriggersAndRollback(t *testing.T) {
	db, err := Open(t.TempDir() + "/authz_revision.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := mig.Version(ctx); v != 130 {
		t.Fatalf("version after Up: got %d want 130", v)
	}
	if !tableExists(t, db, "authorization_revision") {
		t.Fatal("authorization_revision must exist")
	}
	rev0 := authorizationRevision(t, db)
	if _, err := db.ExecContext(ctx, `UPDATE permission_definitions SET category = 'access' WHERE key = 'org.read'`); err != nil {
		t.Fatal(err)
	}
	rev1 := authorizationRevision(t, db)
	if rev1 <= rev0 {
		t.Fatalf("permission_definitions trigger did not advance revision: before=%d after=%d", rev0, rev1)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at)
		VALUES ('rev-org', 'rev-org', 'Revision Org', 'tester', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	rev2 := authorizationRevision(t, db)
	if rev2 <= rev1 {
		t.Fatalf("legacy organizations trigger did not advance revision: before=%d after=%d", rev1, rev2)
	}
	if _, err := db.ExecContext(ctx, `UPDATE authorization_roles SET description = description WHERE id = 'sys-org-member'`); err != nil {
		t.Fatal(err)
	}
	rev3 := authorizationRevision(t, db)
	if rev3 <= rev2 {
		t.Fatalf("authorization_roles trigger did not advance revision: before=%d after=%d", rev2, rev3)
	}
	if err := mig.Down(ctx, 129); err != nil {
		t.Fatalf("Down(129): %v", err)
	}
	if tableExists(t, db, "authorization_revision") {
		t.Fatal("authorization_revision must be dropped after Down(129)")
	}
}

func authorizationRevision(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var revision int64
	if err := db.QueryRow(`SELECT revision FROM authorization_revision WHERE id = 1`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return revision
}
