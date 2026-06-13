package persistence

import (
	"errors"
	"strings"

	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// IsUniqueViolation reports whether err is a SQLite UNIQUE-constraint violation
// (modernc.org/sqlite driver). Callers that race on a unique index use it to map
// the losing writer to a clean domain error instead of leaking the raw driver
// error (e.g. v2.8.1 #278 StartWork: the concurrent activate that loses the
// agent_work_items single-active UNIQUE index → ErrAgentHasActiveWork, same as
// the non-atomic pre-check path).
//
// It prefers the typed driver error code, but also falls back to matching the
// driver's message text. The fallback covers the common case where the original
// *sqlite.Error has been flattened into a plain error (e.g. wrapped with
// fmt.Errorf("...%v", err) rather than %w) before reaching this check — that is
// the form the per-repo helpers this consolidates all relied on.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code()
		if code == sqlitelib.SQLITE_CONSTRAINT_UNIQUE ||
			code == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY {
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
