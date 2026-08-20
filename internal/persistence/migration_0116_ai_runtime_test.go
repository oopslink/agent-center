package persistence

import (
	"context"
	"strings"
	"testing"
)

func TestMigration0116BackfillsLegacyModelCatalog(t *testing.T) {
	db, err := Open(t.TempDir() + "/legacy.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewMigrator(db)
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Down(ctx, 115); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO pm_model_catalog(id,org_id,model_id,display_name,created_at,updated_at) VALUES('m1','org-old','gpt-legacy','Legacy','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var key, compatible string
	var enabled int
	if err := db.QueryRow(`SELECT runtime_key,compatible_cli_keys_json,enabled FROM pm_model_catalog WHERE id='m1'`).Scan(&key, &compatible, &enabled); err != nil {
		t.Fatal(err)
	}
	if key != "gpt-legacy" || compatible != `["codex"]` || enabled != 1 {
		t.Fatalf("backfill = %q %q %d", key, compatible, enabled)
	}
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_clis WHERE org_id='org-old' AND key='codex'`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != 1 {
		t.Fatalf("codex seed count=%d", seeded)
	}
	if err := m.Down(ctx, 115); err != nil {
		t.Fatal(err)
	}
	if columnExists(t, db, "pm_model_catalog", "runtime_key") {
		t.Fatal("runtime_key remains after down")
	}
}

func TestMigration0126RemovesAIRuntimeProfileSchemaWhenUnbound(t *testing.T) {
	db, err := Open(t.TempDir() + "/runtime-profile-remove.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	m := NewMigrator(db)
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, db, "ai_runtime_profiles") {
		t.Fatal("ai_runtime_profiles table remains after migration 0126")
	}
	if columnExists(t, db, "ai_runtime_catalogs", "default_profile_id") {
		t.Fatal("ai_runtime_catalogs.default_profile_id remains after migration 0126")
	}
	if !tableExists(t, db, "ai_runtime_clis") || !tableExists(t, db, "pm_model_catalog") {
		t.Fatal("runtime CLI/model catalog schema was removed with profiles")
	}

	// Rollback restores only the historical schema needed by an older binary;
	// rolling forward again must return to the direct CLI/Model-only schema
	// without disturbing either catalog.
	if err := m.Down(ctx, 125); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, db, "ai_runtime_profiles") || !columnExists(t, db, "ai_runtime_catalogs", "default_profile_id") {
		t.Fatal("migration 0126 rollback did not restore historical schema")
	}
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, db, "ai_runtime_profiles") || columnExists(t, db, "ai_runtime_catalogs", "default_profile_id") {
		t.Fatal("migration 0126 re-apply left historical schema behind")
	}
	if !tableExists(t, db, "ai_runtime_clis") || !tableExists(t, db, "pm_model_catalog") {
		t.Fatal("runtime CLI/model catalogs did not survive rollback round trip")
	}
}

func TestMigration0126BlocksActiveAIRuntimeProfileBindings(t *testing.T) {
	db, err := Open(t.TempDir() + "/runtime-profile-guard.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	m := NewMigrator(db)
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Down(ctx, 125); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ai_runtime_profiles(id,org_id,key,name,cli_key,model_key,parameters_json,created_at,updated_at)
		VALUES('profile-live','org-live','default-coding','Default coding','codex','gpt','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	err = m.Up(ctx)
	if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("migration 0126 should block active profiles, got %v", err)
	}
	if !tableExists(t, db, "ai_runtime_profiles") || !columnExists(t, db, "ai_runtime_catalogs", "default_profile_id") {
		t.Fatal("guard failure removed profile schema")
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_profiles WHERE id='profile-live'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("profile row was not preserved after guarded failure: %d", rows)
	}
	if v, _ := m.Version(ctx); v != 125 {
		t.Fatalf("version after guarded failure: got %d want 125", v)
	}
}
