package persistence

import (
	"context"
	"testing"
)

// TestMigrator_ReconcileStateB_0064Collision proves the 0064/0065 renumber
// collision repair (reconcileLegacy0064Collision). A "State B" DB — agents
// already carries the agent_llm_config columns under version 64, and
// center_settings was never created — must run Up() to completion (no
// duplicate-column crash on the renumbered 0065), end at version 66, and have
// center_settings present. Regression for the production startup crash
// `migrate: apply up 0065_agent_llm_config: duplicate column name: reasoning`.
func TestMigrator_ReconcileStateB_0064Collision(t *testing.T) {
	db, err := Open(t.TempDir() + "/state_b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	// Start from a healthy v65 DB, then mutate it into the State B shape: drop
	// center_settings, forget version 65 (agents keeps the columns 0065 added),
	// and relabel 64 as agent_llm_config to match the historical record.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	for _, stmt := range []string{
		// Undo migrations AFTER 64 so the DB is back at the State-B version (64).
		// 0066 (dm_dedup) is newer than the historical collision; tear down its
		// schema + record too, else MAX(version) stays past 64.
		`DROP INDEX IF EXISTS uniq_conversations_dm_key`,
		`ALTER TABLE conversations DROP COLUMN dm_key`,
		`DELETE FROM schema_migrations WHERE version = 66`,
		`DROP TABLE center_settings`,
		`DELETE FROM schema_migrations WHERE version = 65`,
		`UPDATE schema_migrations SET name = 'agent_llm_config' WHERE version = 64`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	// Sanity: we really are in State B.
	if ok, err := mig.columnExists(ctx, "agents", "reasoning"); err != nil || !ok {
		t.Fatalf("setup: agents.reasoning should exist (ok=%v err=%v)", ok, err)
	}
	if tableExists(t, db, "center_settings") {
		t.Fatal("setup: center_settings should be gone")
	}
	// State B precondition is "64 applied, 65 unapplied" (NOT a literal max version —
	// later migrations like 0066 stay applied; only the v65 record was removed to
	// re-arm the 0064/0065 reconcile path). Assert that directly so this regression
	// survives schema-version bumps above 65.
	applied, err := mig.appliedVersions(ctx)
	if err != nil {
		t.Fatalf("setup applied: %v", err)
	}
	if !applied[64] || applied[65] {
		t.Fatalf("setup: want State B (64 applied, 65 unapplied), got applied[64]=%v applied[65]=%v", applied[64], applied[65])
	}

	// The fix under test: Up() must not crash on the renumbered 0065, and must
	// recreate the skipped center_settings table. Up ends at the latest version
	// (reconcile re-marks 65, the apply loop carries any later migrations).
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("Up on State B DB: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 105 {
		t.Fatalf("version after repair: got %d want 105", v)
	}
	if !tableExists(t, db, "center_settings") {
		t.Fatal("center_settings was not recreated")
	}

	// Idempotent: a second Up is a clean no-op.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if v, _ := mig.Version(ctx); v != 105 {
		t.Fatalf("version after second Up: got %d want 105", v)
	}
}
