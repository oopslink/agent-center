package persistence

import (
	"context"
	"testing"
)

func TestMigration_0129_UnifiedAuthorizationRollback(t *testing.T) {
	db, err := Open(t.TempDir() + "/authz_migration.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{
		"permission_definitions",
		"authorization_roles",
		"authorization_role_permissions",
		"authorization_role_assignments",
		"authorization_idempotency_keys",
		"authorization_audit_events",
	} {
		if !tableExists(t, db, tbl) {
			t.Fatalf("post-Up: %s must exist", tbl)
		}
	}
	var definitions int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM permission_definitions`).Scan(&definitions); err != nil {
		t.Fatal(err)
	}
	if definitions == 0 {
		t.Fatal("permission_definitions must be seeded")
	}
	if err := mig.Down(ctx, 128); err != nil {
		t.Fatalf("Down(128): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 128 {
		t.Fatalf("version after Down(128): got %d want 128", v)
	}
	for _, tbl := range []string{
		"permission_definitions",
		"authorization_roles",
		"authorization_role_permissions",
		"authorization_role_assignments",
		"authorization_idempotency_keys",
		"authorization_audit_events",
	} {
		if tableExists(t, db, tbl) {
			t.Fatalf("after Down(128): %s must be dropped", tbl)
		}
	}
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 135 {
		t.Fatalf("version after re-Up: got %d want 135", v)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_roles WHERE kind = 'system'`).Scan(&definitions); err != nil {
		t.Fatal(err)
	}
	if definitions == 0 {
		t.Fatal("system roles must be restored after re-Up")
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_roles WHERE id IN ('sys-team-web-owner','sys-team-web-admin','sys-team-web-member')`).Scan(&definitions); err != nil {
		t.Fatal(err)
	}
	if definitions != 3 {
		t.Fatalf("S2 team web equivalence roles = %d want 3", definitions)
	}
}
