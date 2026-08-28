package persistence

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestMigrator_UpCreatesAllPhase1Tables(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	m := NewMigrator(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	for _, tbl := range []string{
		"events",
		"workers",
		// v2.7 #131: worker_project_mappings / worker_project_proposals / projects retired.
		"conversations",
		"messages",
	} {
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&count); err != nil {
			t.Fatalf("query %s: %v", tbl, err)
		}
		if count != 1 {
			t.Fatalf("table %s missing after Up", tbl)
		}
	}
}

// TestMigrator_UpCreatesV2Tables verifies the v2 (Phase 8) migrations land:
// - bootstrap_tokens / agent_instances / user_secrets tables
// - workers.concurrency_json / discovery_json / capabilities_json columns
// - task_executions.agent_instance_id column
// - supervisor_invocations.agent_instance_id column
// - workers.capabilities column is dropped (per 0007)
func TestMigrator_UpCreatesV2Tables(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// v2 new tables
	for _, tbl := range []string{"bootstrap_tokens", "agent_instances", "user_secrets"} {
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&count); err != nil {
			t.Fatalf("query %s: %v", tbl, err)
		}
		if count != 1 {
			t.Fatalf("v2 table %s missing", tbl)
		}
	}

	// columns we expect (and a v1 column we expect to be GONE)
	type colCheck struct {
		table  string
		column string
		want   bool // true=must exist, false=must NOT exist
	}
	for _, c := range []colCheck{
		{"workers", "concurrency_json", true},
		{"workers", "discovery_json", true},
		{"workers", "capabilities_json", true},
		{"workers", "capabilities", false}, // dropped by 0007
		// v2.7 #131: task_executions table retired (0002/0010 no-op) — column check removed.
	} {
		var found bool
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, c.table)
		if err != nil {
			t.Fatalf("pragma_table_info(%s): %v", c.table, err)
		}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				t.Fatal(err)
			}
			if col == c.column {
				found = true
			}
		}
		rows.Close()
		if found != c.want {
			t.Fatalf("%s.%s: present=%v want=%v", c.table, c.column, found, c.want)
		}
	}
}

func TestMigrator_UpIdempotent(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewMigrator(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("second Up: %v", err)
	}
}

func TestMigration0144FailsClosedWhenRoleAccessReferencedByTeamRole(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	m := NewMigrator(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := m.Down(ctx, 143); err != nil {
		t.Fatalf("Down(143): %v", err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, stmt := range []string{
		`INSERT INTO teams (id, org_id, name, description, created_at, updated_at, version) VALUES ('team-t1499', 'org-t1499', 'T1499', '', '` + now + `', '` + now + `', 1)`,
		`INSERT INTO team_roles (team_id, role, cli, model, created_at) VALUES ('team-t1499', 'reviewer', 'codex', 'gpt-5', '` + now + `')`,
		`INSERT INTO authorization_roles (id, org_id, kind, visibility, stable_key, scope_kind, name, description, created_by, created_at, updated_at, version) VALUES ('role-access-blocked', 'org-t1499', 'custom', 'reusable', 'role-access-blocked', 'team', 'legacy direct carrier', '', 'system', '` + now + `', '` + now + `', 1)`,
		`INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at) VALUES ('role-access-blocked', 'team.read', 'team', 0, '` + now + `')`,
		`INSERT INTO team_role_ram_role_mappings (team_id, team_role, ram_role_id, created_at, created_by) VALUES ('team-t1499', 'reviewer', 'role-access-blocked', '` + now + `', 'system')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	err = m.Up(ctx)
	if err == nil || !strings.Contains(err.Error(), "migration 0144 blocked") || !strings.Contains(err.Error(), "role=role-access-blocked") || !strings.Contains(err.Error(), "team_role_refs=\"team-t1499:reviewer\"") {
		t.Fatalf("Up with Team Role role-access reference err=%v, want fail-closed guard", err)
	}
	version, err := m.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version != 143 {
		t.Fatalf("version after failed 0144 = %d want 143", version)
	}
}

func TestMigration0144FailsClosedForNonSinglePermissionRoleAccess(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	m := NewMigrator(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := m.Down(ctx, 143); err != nil {
		t.Fatalf("Down(143): %v", err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, stmt := range []string{
		`INSERT INTO authorization_roles (id, org_id, kind, visibility, stable_key, scope_kind, name, description, created_by, created_at, updated_at, version) VALUES ('role-access-multiperm', 'org-t1499', 'custom', 'reusable', 'role-access-multiperm', 'org', 'ambiguous role access', '', 'system', '` + now + `', '` + now + `', 1)`,
		`INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at) VALUES ('role-access-multiperm', 'org.read', 'org', 0, '` + now + `')`,
		`INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at) VALUES ('role-access-multiperm', 'org.analytics.read', 'org', 0, '` + now + `')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	err = m.Up(ctx)
	if err == nil || !strings.Contains(err.Error(), "role=role-access-multiperm") || !strings.Contains(err.Error(), "permission_count=2") {
		t.Fatalf("Up with multi-permission role-access err=%v, want actionable blocker", err)
	}
	version, err := m.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version != 143 {
		t.Fatalf("version after failed 0144 = %d want 143", version)
	}
}

func TestMigration0144DirectCarrierEquivalenceIdempotentAndRollback(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	m := NewMigrator(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	if err := m.Down(ctx, 143); err != nil {
		t.Fatalf("Down(143): %v", err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	expires := time.Date(2026, 8, 24, 13, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, stmt := range []string{
		`INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at) VALUES ('mig-user', 'user', 'Migration User', 'x', '` + now + `', '` + now + `')`,
		`INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at) VALUES ('mig-expired-user', 'user', 'Migration Expired User', 'x', '` + now + `', '` + now + `')`,
		`INSERT INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at) VALUES ('org-mig', 'org-mig', 'Migration Org', 'mig-user', '` + now + `', '` + now + `')`,
		`INSERT INTO members (id, organization_id, identity_id, role, status, joined_at) VALUES ('mem-mig', 'org-mig', 'mig-user', 'member', 'disabled', '` + now + `')`,
		`INSERT INTO members (id, organization_id, identity_id, role, status, joined_at) VALUES ('mem-mig-expired', 'org-mig', 'mig-expired-user', 'member', 'joined', '` + now + `')`,
		`INSERT INTO authorization_roles (id, org_id, kind, visibility, stable_key, scope_kind, name, description, created_by, created_at, updated_at, version) VALUES ('role-access-single', 'org-mig', 'custom', 'reusable', 'role-access-single', 'org', 'legacy direct carrier', '', 'system', '` + now + `', '` + now + `', 1)`,
		`INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at) VALUES ('role-access-single', 'org.analytics.read', 'org', 0, '` + now + `')`,
		`INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, expires_at, version) VALUES ('asgn-mig-live', 'org-mig', 'user:mig-user', 'role-access-single', 'org', 'org-mig', 'system', '` + now + `', '` + expires + `', 1)`,
		`INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, expires_at, version) VALUES ('asgn-mig-expired', 'org-mig', 'user:mig-expired-user', 'role-access-single', 'org', 'org-mig', 'system', '` + now + `', '` + now + `', 1)`,
		`INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version) VALUES ('asgn-mig-wrong-scope', 'org-mig', 'user:mig-user', 'role-access-single', 'org', 'org-other', 'system', '` + now + `', 1)`,
		`INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, revoked_at, revoked_by, revoked_reason, version) VALUES ('asgn-mig-revoked', 'org-mig', 'user:mig-user', 'role-access-single', 'org', 'org-mig', 'system', '` + now + `', '` + now + `', 'system', 'cleanup', 2)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	before := snapshot0144Carrier(t, db)
	if before.Kind != "custom" || before.Visibility != "reusable" || before.LiveAssignments != 3 || before.RevokedAssignments != 1 || before.PermissionCount != 1 || before.EligibleDirectGrants != 1 || before.ExpiredActive != 1 || before.WrongScopeActive != 1 || !before.DenyPrecedence {
		t.Fatalf("before snapshot=%+v", before)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up 0144: %v", err)
	}
	after := snapshot0144Carrier(t, db)
	if after.Kind != "managed" || after.Visibility != "internal" || after.LiveAssignments != before.LiveAssignments || after.RevokedAssignments != before.RevokedAssignments || after.PermissionCount != before.PermissionCount || after.ExpiresAt != before.ExpiresAt || after.EligibleDirectGrants != before.EligibleDirectGrants || after.ExpiredActive != before.ExpiredActive || after.WrongScopeActive != before.WrongScopeActive || after.DenyPrecedence != before.DenyPrecedence {
		t.Fatalf("after snapshot=%+v before=%+v", after, before)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	replayed := snapshot0144Carrier(t, db)
	if replayed.Version != after.Version || replayed.LiveAssignments != after.LiveAssignments || replayed.RevokedAssignments != after.RevokedAssignments {
		t.Fatalf("idempotent replay mutated snapshot=%+v after=%+v", replayed, after)
	}
	if err := m.Down(ctx, 143); err != nil {
		t.Fatalf("Down 143: %v", err)
	}
	rolledBack := snapshot0144Carrier(t, db)
	if rolledBack.Kind != "custom" || rolledBack.Visibility != "reusable" || rolledBack.LiveAssignments != before.LiveAssignments || rolledBack.RevokedAssignments != before.RevokedAssignments || rolledBack.PermissionCount != before.PermissionCount || rolledBack.ExpiresAt != before.ExpiresAt || rolledBack.EligibleDirectGrants != before.EligibleDirectGrants || rolledBack.ExpiredActive != before.ExpiredActive || rolledBack.WrongScopeActive != before.WrongScopeActive || rolledBack.DenyPrecedence != before.DenyPrecedence {
		t.Fatalf("rollback snapshot=%+v before=%+v", rolledBack, before)
	}
}

type migration0144Snapshot struct {
	Kind                 string
	Visibility           string
	Version              int
	PermissionCount      int
	LiveAssignments      int
	RevokedAssignments   int
	ExpiresAt            string
	EligibleDirectGrants int
	ExpiredActive        int
	WrongScopeActive     int
	DenyPrecedence       bool
}

func snapshot0144Carrier(t *testing.T, db *sql.DB) migration0144Snapshot {
	t.Helper()
	var out migration0144Snapshot
	if err := db.QueryRow(`SELECT kind, visibility, version FROM authorization_roles WHERE id = 'role-access-single'`).Scan(&out.Kind, &out.Visibility, &out.Version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_permissions WHERE role_id = 'role-access-single'`).Scan(&out.PermissionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_assignments WHERE role_id = 'role-access-single' AND revoked_at IS NULL`).Scan(&out.LiveAssignments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_assignments WHERE role_id = 'role-access-single' AND revoked_at IS NOT NULL`).Scan(&out.RevokedAssignments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(expires_at, '') FROM authorization_role_assignments WHERE id = 'asgn-mig-live'`).Scan(&out.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*)
		FROM authorization_role_assignments a
		JOIN authorization_role_permissions p ON p.role_id = a.role_id
		WHERE a.role_id = 'role-access-single'
		  AND a.subject_ref = 'user:mig-user'
		  AND a.resource_kind = 'org'
		  AND a.resource_id = 'org-mig'
		  AND a.revoked_at IS NULL
		  AND (a.expires_at IS NULL OR a.expires_at > '2026-08-24T12:00:00Z')
		  AND p.permission_key = 'org.analytics.read'
		  AND p.resource_kind = 'org'`).Scan(&out.EligibleDirectGrants); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_assignments WHERE id = 'asgn-mig-expired' AND revoked_at IS NULL AND expires_at <= '2026-08-24T12:00:00Z'`).Scan(&out.ExpiredActive); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_role_assignments WHERE id = 'asgn-mig-wrong-scope' AND revoked_at IS NULL AND resource_id <> 'org-mig'`).Scan(&out.WrongScopeActive); err != nil {
		t.Fatal(err)
	}
	var memberStatus string
	if err := db.QueryRow(`SELECT status FROM members WHERE id = 'mem-mig'`).Scan(&memberStatus); err != nil {
		t.Fatal(err)
	}
	out.DenyPrecedence = memberStatus == "disabled"
	return out
}

func TestMigrator_DownReverts(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewMigrator(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := m.Down(context.Background(), 0); err != nil {
		t.Fatalf("Down: %v", err)
	}
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, "events",
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("events table still present after Down")
	}
}

func TestMigrator_DownIdempotent(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewMigrator(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Down(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	// down on empty: no error
	if err := m.Down(context.Background(), 0); err != nil {
		t.Fatalf("second Down: %v", err)
	}
}

func TestMigrator_VersionTracksApplied(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewMigrator(db)
	ctx := context.Background()
	v, err := m.Version(ctx)
	if err != nil || v != 0 {
		t.Fatalf("version on empty: got (%d, %v)", v, err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = m.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := latestMigrationVersionForTest(t); v != want {
		t.Fatalf("version after Up: got %d want %d", v, want)
	}
	if err := m.Down(ctx, 0); err != nil {
		t.Fatal(err)
	}
	v, _ = m.Version(ctx)
	if v != 0 {
		t.Fatalf("version after Down 0: got %d want 0", v)
	}
}

func TestMigrator_DownPreviousVersion(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fsys := fstest.MapFS{
		"0001_a.up.sql":   {Data: []byte(`CREATE TABLE a (id TEXT)`)},
		"0001_a.down.sql": {Data: []byte(`DROP TABLE a`)},
		"0002_b.up.sql":   {Data: []byte(`CREATE TABLE b (id TEXT)`)},
		"0002_b.down.sql": {Data: []byte(`DROP TABLE b`)},
	}
	m := NewMigratorFS(db, fsys)
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	v, _ := m.Version(ctx)
	if v != 2 {
		t.Fatalf("version after Up: got %d want 2", v)
	}
	// Down to previous (1)
	if err := m.Down(ctx, -1); err != nil {
		t.Fatal(err)
	}
	v, _ = m.Version(ctx)
	if v != 1 {
		t.Fatalf("version after Down(-1): got %d want 1", v)
	}
	// b dropped, a still present
	var aCount, bCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='a'`).Scan(&aCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='b'`).Scan(&bCount)
	if aCount != 1 || bCount != 0 {
		t.Fatalf("expected a present, b dropped; got a=%d b=%d", aCount, bCount)
	}
}

// TestMigrator_DuplicateVersionRejected guards the renumber-collision class
// (T216/0062, T236/0064): two DIFFERENT migrations sharing one version number
// must FAIL loudly, not silently overwrite (which made one migration never run
// on a fresh DB). The up/down pair of the SAME migration shares a name and is
// fine.
func TestMigrator_DuplicateVersionRejected(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fsys := fstest.MapFS{
		"0001_a.up.sql":           {Data: []byte(`CREATE TABLE a (id TEXT)`)},
		"0001_a.down.sql":         {Data: []byte(`DROP TABLE a`)},
		"0002_b.up.sql":           {Data: []byte(`CREATE TABLE b (id TEXT)`)},
		"0002_b.down.sql":         {Data: []byte(`DROP TABLE b`)},
		"0002_collision.up.sql":   {Data: []byte(`CREATE TABLE c (id TEXT)`)},
		"0002_collision.down.sql": {Data: []byte(`DROP TABLE c`)},
	}
	m := NewMigratorFS(db, fsys)
	err = m.Up(context.Background())
	if err == nil {
		t.Fatal("expected duplicate-version error, got nil (silent overwrite regression)")
	}
	if !strings.Contains(err.Error(), "duplicate migration version 0002") {
		t.Fatalf("error = %q, want it to name the duplicate version 0002", err)
	}
}

func TestParseMigrationName(t *testing.T) {
	cases := []struct {
		name    string
		ver     int
		nm      string
		dir     string
		wantErr bool
	}{
		{"0001_init.up.sql", 1, "init", "up", false},
		{"0042_add_x.down.sql", 42, "add_x", "down", false},
		{"missing_marker.sql", 0, "", "", true},
		{"abc_init.up.sql", 0, "", "", true},
		{"badname.up.sql", 0, "", "", true},
	}
	for _, c := range cases {
		v, n, d, err := parseMigrationName(c.name)
		if c.wantErr {
			if err == nil {
				t.Fatalf("parse %q: expected error", c.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parse %q: %v", c.name, err)
		}
		if v != c.ver || n != c.nm || d != c.dir {
			t.Fatalf("parse %q: got (%d,%q,%q), want (%d,%q,%q)",
				c.name, v, n, d, c.ver, c.nm, c.dir)
		}
	}
}

func TestMigrator_RejectsMissingUp(t *testing.T) {
	db, _ := Open(t.TempDir() + "/test.db")
	defer db.Close()
	fsys := fstest.MapFS{
		"0001_only.down.sql": {Data: []byte(`SELECT 1`)},
	}
	m := NewMigratorFS(db, fsys)
	err := m.Up(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing up") {
		t.Fatalf("expected missing up error, got %v", err)
	}
}

func TestMigrator_RejectsMissingDown(t *testing.T) {
	db, _ := Open(t.TempDir() + "/test.db")
	defer db.Close()
	fsys := fstest.MapFS{
		"0001_only.up.sql": {Data: []byte(`SELECT 1`)},
	}
	m := NewMigratorFS(db, fsys)
	err := m.Up(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing down") {
		t.Fatalf("expected missing down error, got %v", err)
	}
}

func TestMigrator_EmptyFS(t *testing.T) {
	db, _ := Open(t.TempDir() + "/test.db")
	defer db.Close()
	m := NewMigratorFS(db, fstest.MapFS{})
	err := m.Up(context.Background())
	if err == nil {
		t.Fatal("expected error for empty migrations")
	}
}

func TestMigrator_BadSQLRollsBack(t *testing.T) {
	db, _ := Open(t.TempDir() + "/test.db")
	defer db.Close()
	fsys := fstest.MapFS{
		"0001_bad.up.sql":   {Data: []byte(`CREATE TABLE x (id TEXT); INVALID SQL HERE`)},
		"0001_bad.down.sql": {Data: []byte(`DROP TABLE x`)},
	}
	m := NewMigratorFS(db, fsys)
	err := m.Up(context.Background())
	if err == nil {
		t.Fatal("expected SQL error")
	}
	// table x must not exist (tx rolled back)
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='x'`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected rollback; table x present (count=%d)", count)
	}
}
