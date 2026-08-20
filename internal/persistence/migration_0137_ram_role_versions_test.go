package persistence

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

type migration0137ProfileState struct {
	profile  []string
	versions [][]string
}

func TestMigration_0137_RoundTripPreservesAccessProfileData(t *testing.T) {
	db, err := Open(t.TempDir() + "/m137.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := mig.Down(ctx, 136); err != nil {
		t.Fatalf("prepare Down(136): %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO access_profiles
			(id, org_id, name, description, disabled_at, created_by, created_at, updated_at)
		VALUES
			('profile-round-trip', 'org-1', 'Round trip', 'kept verbatim',
			 '2026-08-19T01:00:00Z', 'user:creator', '2026-08-18T01:00:00Z', '2026-08-19T01:00:00Z');
		INSERT INTO access_profile_versions
			(profile_id, version, permissions_json, risk, created_by, created_at)
		VALUES
			('profile-round-trip', 1, '["team.read"]', 'low', 'user:creator', '2026-08-18T01:00:00Z'),
			('profile-round-trip', 2, '["team.read","team.write"]', 'high', 'user:editor', '2026-08-19T01:00:00Z');`)
	if err != nil {
		t.Fatalf("seed v136 access profile: %v", err)
	}
	want136 := readMigration0137ProfileState(t, db)
	want136Schema := snapshotSelectedSchema(t, db, "access_profiles", "access_profile_versions")

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("0137 Up: %v", err)
	}
	assertMigration0137RoleState(t, db)
	want137Schema := snapshotSelectedSchema(t, db, "authorization_role_versions")

	if err := mig.Down(ctx, 136); err != nil {
		t.Fatalf("0137 Down(136): %v", err)
	}
	if got := readMigration0137ProfileState(t, db); !reflect.DeepEqual(got, want136) {
		t.Fatalf("v136 data after Down differs:\n got: %#v\nwant: %#v", got, want136)
	}
	if got := snapshotSelectedSchema(t, db, "access_profiles", "access_profile_versions"); !reflect.DeepEqual(got, want136Schema) {
		t.Fatalf("v136 schema after Down differs:\n got: %v\nwant: %v", got, want136Schema)
	}

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("0137 re-Up: %v", err)
	}
	assertMigration0137RoleState(t, db)
	if got := snapshotSelectedSchema(t, db, "authorization_role_versions"); !reflect.DeepEqual(got, want137Schema) {
		t.Fatalf("v137 schema after re-Up differs:\n got: %v\nwant: %v", got, want137Schema)
	}
}

func readMigration0137ProfileState(t *testing.T, db *sql.DB) migration0137ProfileState {
	t.Helper()
	var got migration0137ProfileState
	row := db.QueryRow(`SELECT id, org_id, name, description, disabled_at, created_by, created_at, updated_at
		FROM access_profiles WHERE id = 'profile-round-trip'`)
	var disabled sql.NullString
	var id, orgID, name, description, createdBy, createdAt, updatedAt string
	if err := row.Scan(&id, &orgID, &name, &description, &disabled, &createdBy, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	got.profile = []string{id, orgID, name, description, disabled.String, createdBy, createdAt, updatedAt}
	rows, err := db.Query(`SELECT CAST(version AS TEXT), permissions_json, risk, created_by, created_at
		FROM access_profile_versions WHERE profile_id = 'profile-round-trip' ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var version, permissions, risk, by, at string
		if err := rows.Scan(&version, &permissions, &risk, &by, &at); err != nil {
			t.Fatal(err)
		}
		got.versions = append(got.versions, []string{version, permissions, risk, by, at})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func assertMigration0137RoleState(t *testing.T, db *sql.DB) {
	t.Helper()
	if tableExists(t, db, "access_profiles") || tableExists(t, db, "access_profile_versions") {
		t.Fatal("v137 must retire access-profile tables")
	}
	var orgID, kind, name, description, revokedAt string
	if err := db.QueryRow(`SELECT org_id, kind, name, description, COALESCE(revoked_at, '')
		FROM authorization_roles WHERE id = 'profile-round-trip'`).Scan(&orgID, &kind, &name, &description, &revokedAt); err != nil {
		t.Fatal(err)
	}
	if got, want := []string{orgID, kind, name, description, revokedAt}, []string{"org-1", "custom", "Round trip", "kept verbatim", "2026-08-19T01:00:00Z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role metadata: got %v want %v", got, want)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_versions
		WHERE role_id = 'profile-round-trip' AND
		((version = 1 AND permissions_json = '["team.read"]' AND risk = 'low' AND created_by = 'user:creator') OR
		 (version = 2 AND permissions_json = '["team.read","team.write"]' AND risk = 'high' AND created_by = 'user:editor'))`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("role versions preserved: got %d want 2", count)
	}
}

func snapshotSelectedSchema(t *testing.T, db *sql.DB, names ...string) map[string]string {
	t.Helper()
	got := make(map[string]string, len(names))
	for _, name := range names {
		var schema string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = ?`, name).Scan(&schema); err != nil {
			t.Fatalf("schema for %s: %v", name, err)
		}
		got[name] = schema
	}
	return got
}
