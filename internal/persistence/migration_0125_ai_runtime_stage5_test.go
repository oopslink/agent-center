package persistence

import (
	"context"
	"testing"
)

func TestMigration0125AIRuntimeStage5EvidenceTables(t *testing.T) {
	db, err := Open(t.TempDir() + "/m125.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"ai_runtime_object_selections",
		"ai_runtime_shadow_diffs",
		"ai_runtime_cutover_events",
	} {
		if !tableExists(t, db, table) {
			t.Fatalf("%s must exist after migration 0125", table)
		}
	}
	if err := mig.Down(ctx, 124); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"ai_runtime_object_selections",
		"ai_runtime_shadow_diffs",
		"ai_runtime_cutover_events",
	} {
		if tableExists(t, db, table) {
			t.Fatalf("%s must be removed after down to 0124", table)
		}
	}
}
