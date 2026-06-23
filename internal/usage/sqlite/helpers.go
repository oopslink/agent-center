// Package sqlite holds the SQLite-backed repositories for the usage bounded
// context (model_prices + usage_events, migration 0077). It mirrors the
// conventions of the other sqlite repos: RFC3339Nano TEXT timestamps, NULL for an
// absent optional string, and ambient-tx execution via persistence.ExecutorFromCtx.
package sqlite

import (
	"database/sql"
	"time"
)

// ts formats a time as the RFC3339Nano TEXT this repo stores (UTC-normalized),
// matching every other timestamp column in the schema.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// parseTime parses an RFC3339Nano TEXT timestamp; a malformed/empty value yields
// the zero time (callers treat zero as "unset").
func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// nullString binds "" as SQL NULL (for the nullable task_id column) and any
// non-empty string as itself.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// strOrEmpty unwraps a scanned sql.NullString to "" when NULL.
func strOrEmpty(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
