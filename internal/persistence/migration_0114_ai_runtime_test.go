package persistence

import (
	"context"
	"testing"
)

func TestMigration0114BackfillsLegacyModelCatalog(t *testing.T) {
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
	if err := m.Down(ctx, 113); err != nil {
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
	if err := m.Down(ctx, 113); err != nil {
		t.Fatal(err)
	}
	if columnExists(t, db, "pm_model_catalog", "runtime_key") {
		t.Fatal("runtime_key remains after down")
	}
}
