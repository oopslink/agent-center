package e2e

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// queryDB opens path read-only and returns rows as []map[string]string.
// Use only with SQL queries that produce string-friendly columns.
func queryDB(t *testing.T, path, query string) []map[string]string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	out := []map[string]string{}
	for rows.Next() {
		raw := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := map[string]string{}
		for i, c := range cols {
			row[c] = raw[i].String
		}
		out = append(out, row)
	}
	return out
}

// eventRow is a typed projection of one row in `events`.
type eventRow struct {
	ID        string
	EventType string
	Actor     string
	Refs      string
	Payload   string
}

// readEvents reads all rows from the events table in occurred_at order.
func readEvents(t *testing.T, path string) []eventRow {
	t.Helper()
	raw := queryDB(t, path, "SELECT id, event_type, actor, refs, payload FROM events ORDER BY occurred_at")
	out := make([]eventRow, len(raw))
	for i, r := range raw {
		out[i] = eventRow{
			ID:        r["id"],
			EventType: r["event_type"],
			Actor:     r["actor"],
			Refs:      r["refs"],
			Payload:   r["payload"],
		}
	}
	return out
}
