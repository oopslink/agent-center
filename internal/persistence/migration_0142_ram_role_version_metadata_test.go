package persistence

import (
	"context"
	"testing"
)

func TestMigration_0142_BackfillsRAMRoleVersionMetadata(t *testing.T) {
	db, err := Open(t.TempDir() + "/m142.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := mig.Down(ctx, 141); err != nil {
		t.Fatalf("prepare Down(141): %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO authorization_roles
			(id, org_id, kind, stable_key, name, description, scope_kind, created_by, created_at, updated_at, version)
		VALUES
			('role-m142', 'org-1', 'custom', 'role-m142', 'Original role', 'Original description', 'project', 'user:creator', '2026-08-21T01:00:00Z', '2026-08-21T01:00:00Z', 1);
		INSERT INTO authorization_role_versions
			(role_id, version, permissions_json, risk, created_by, created_at)
		VALUES
			('role-m142', 1, '["project.read"]', 'low', 'user:creator', '2026-08-21T01:00:00Z');`)
	if err != nil {
		t.Fatalf("seed v141 role: %v", err)
	}

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("0142 Up: %v", err)
	}
	var stableKey, name, description, scope string
	if err := db.QueryRowContext(ctx, `SELECT stable_key, name, description, scope_kind FROM authorization_role_versions WHERE role_id = 'role-m142' AND version = 1`).Scan(&stableKey, &name, &description, &scope); err != nil {
		t.Fatal(err)
	}
	if stableKey != "role-m142" || name != "Original role" || description != "Original description" || scope != "project" {
		t.Fatalf("backfilled snapshot=(%q,%q,%q,%q)", stableKey, name, description, scope)
	}

	if _, err := db.ExecContext(ctx, `UPDATE authorization_roles SET stable_key='renamed-role', name='Renamed role', description='Renamed description', scope_kind='team' WHERE id='role-m142'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT stable_key, name, description, scope_kind FROM authorization_role_versions WHERE role_id = 'role-m142' AND version = 1`).Scan(&stableKey, &name, &description, &scope); err != nil {
		t.Fatal(err)
	}
	if stableKey != "role-m142" || name != "Original role" || description != "Original description" || scope != "project" {
		t.Fatalf("snapshot changed with current row=(%q,%q,%q,%q)", stableKey, name, description, scope)
	}

	if err := mig.Down(ctx, 141); err != nil {
		t.Fatalf("0142 Down: %v", err)
	}
	for _, column := range []string{"stable_key", "name", "description", "scope_kind"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('authorization_role_versions') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("column %q remains after down migration", column)
		}
	}
}
