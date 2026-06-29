package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// TestMigration0085_AllowedExecutorsBackfill verifies the v2.18.1 BE-1 backfill:
// each legacy allowed_models entry becomes {cli: the agent's own cli (empty →
// claude-code), model: <m>}; a row with empty allowed_models keeps the column
// default '[]'. We migrate to head, Down to 84 (dropping allowed_executors), seed
// pre-0085 rows, then Up to re-run 0085 over real data.
func TestMigration0085_AllowedExecutorsBackfill(t *testing.T) {
	db, err := Open(MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mig := NewMigrator(db)
	ctx := context.Background()
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("initial Up: %v", err)
	}
	// Roll back just 0085 so the agents rows can be seeded in their pre-0085 shape
	// (allowed_executors column absent).
	if err := mig.Down(ctx, 84); err != nil {
		t.Fatalf("Down(84): %v", err)
	}

	seed := func(id, cli, allowedModels string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO agents (id, organization_id, name, worker_id, lifecycle, created_by,
				created_at, updated_at, cli, allowed_models)
			 VALUES (?,?,?,?,?,?,?,?,?,?)`,
			id, "org-1", id, "W1", "stopped", "user:a",
			"2026-06-29T00:00:00Z", "2026-06-29T00:00:00Z", cli, allowedModels)
		if err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("codexAgent", "codex", `["gpt-5-codex","gpt-5"]`)
	seed("emptyCLIAgent", "", `["claude-sonnet"]`) // empty cli → claude-code
	seed("defaultAgent", "claude-code", `[]`)      // empty list → stays '[]'
	seed("nullModelsAgent", "claude-code", ``)     // '' allowed_models → stays '[]'

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up (apply 0085): %v", err)
	}

	want := map[string][]map[string]string{
		"codexAgent": {
			{"cli": "codex", "model": "gpt-5-codex"},
			{"cli": "codex", "model": "gpt-5"},
		},
		"emptyCLIAgent": {
			{"cli": "claude-code", "model": "claude-sonnet"},
		},
		"defaultAgent":    {},
		"nullModelsAgent": {},
	}
	for id, exp := range want {
		var raw string
		if err := db.QueryRowContext(ctx, `SELECT allowed_executors FROM agents WHERE id=?`, id).Scan(&raw); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		var got []map[string]string
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: allowed_executors not valid JSON (%q): %v", id, raw, err)
		}
		if !sameExecList(got, exp) {
			t.Fatalf("%s: allowed_executors = %v, want %v", id, got, exp)
		}
	}

	// Down(84) must cleanly drop the column again (rollback convenience).
	if err := mig.Down(ctx, 84); err != nil {
		t.Fatalf("Down(84) after backfill: %v", err)
	}
	if colExists0085(t, db, "allowed_executors") {
		t.Fatal("allowed_executors column should be gone after Down(84)")
	}
}

func sameExecList(a, b []map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i]["cli"] != b[i]["cli"] || a[i]["model"] != b[i]["model"] {
			return false
		}
	}
	return true
}

func colExists0085(t *testing.T, db *sql.DB, col string) bool {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `PRAGMA table_info(agents)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == col {
			return true
		}
	}
	return false
}
