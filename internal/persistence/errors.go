package persistence

import (
	"errors"

	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// IsUniqueViolation reports whether err is a SQLite UNIQUE-constraint violation
// (modernc.org/sqlite driver). Callers that race on a unique index use it to map
// the losing writer to a clean domain error instead of leaking the raw driver
// error (e.g. v2.8.1 #278 StartWork: the concurrent activate that loses the
// agent_work_items single-active UNIQUE index → ErrAgentHasActiveWork, same as
// the non-atomic pre-check path).
func IsUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code()
		return code == sqlitelib.SQLITE_CONSTRAINT_UNIQUE ||
			code == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}
