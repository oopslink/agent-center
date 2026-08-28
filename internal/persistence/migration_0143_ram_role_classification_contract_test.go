package persistence

import (
	"context"
	"testing"
)

func TestMigration_0143_RAMRoleClassificationContract(t *testing.T) {
	db, err := Open(t.TempDir() + "/m143.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := NewMigrator(db).Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var visibility string
	if err := db.QueryRowContext(ctx, `SELECT visibility FROM authorization_roles WHERE id = 'team-basic'`).Scan(&visibility); err != nil {
		t.Fatal(err)
	}
	if visibility != "reusable" {
		t.Fatalf("team-basic visibility=%q, want reusable", visibility)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO authorization_roles
			(id, org_id, kind, visibility, name, description, created_by, created_at, updated_at, version)
		VALUES
			('role-bad-system', 'org-1', 'system', 'reusable', 'Bad system', '', 'test', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z', 1)`); err == nil {
		t.Fatal("system role with org_id passed classification check")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO authorization_roles
			(id, org_id, kind, visibility, name, description, created_by, created_at, updated_at, version)
		VALUES
			('role-bad-prefix', 'org-1', 'custom', 'reusable', 'Access grant org.read on org', '', 'test', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z', 1)`); err == nil {
		t.Fatal("reusable role with reserved Access grant prefix passed classification check")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO authorization_roles
			(id, org_id, kind, visibility, name, description, created_by, created_at, updated_at, version)
		VALUES
			('role-managed-ok', 'org-1', 'managed', 'internal', 'Managed direct grant org.read on org', '', 'test', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z', 1)`); err != nil {
		t.Fatalf("managed internal role rejected: %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT role_id, stable_key, version, name, description, scope_kind, risk, responsibility_scenario,
		       least_privilege_permissions_json, reuse_entrypoints_json, maintained_by
		FROM authorization_system_role_contracts
		ORDER BY role_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var roleID, stableKey, name, description, scope, risk, scenario, permissions, entrypoints, maintainedBy string
		var version int
		if err := rows.Scan(&roleID, &stableKey, &version, &name, &description, &scope, &risk, &scenario, &permissions, &entrypoints, &maintainedBy); err != nil {
			t.Fatal(err)
		}
		if stableKey == "" || version != 1 || name == "" || description == "" || scope != "team" || risk == "" || scenario == "" || permissions == "" || entrypoints == "" || maintainedBy != "release_seed_migration" {
			t.Fatalf("incomplete system role contract row: role=%s key=%s version=%d name=%q description=%q scope=%q risk=%q scenario=%q permissions=%q entrypoints=%q maintained_by=%q", roleID, stableKey, version, name, description, scope, risk, scenario, permissions, entrypoints, maintainedBy)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count < 3 {
		t.Fatalf("system role contract rows=%d, want at least 3", count)
	}
}
