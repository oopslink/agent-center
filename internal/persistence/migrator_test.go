package persistence

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
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
		"worker_project_mappings",
		"worker_project_proposals",
		"projects",
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
	if v != 2 {
		t.Fatalf("version after Up: got %d want 2", v)
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
