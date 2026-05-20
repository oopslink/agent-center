// Package sqlite implements the TaskRuntime BC repositories backed by SQLite.
package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// timeFormat is the canonical timestamp format used across all repositories.
const timeFormat = time.RFC3339Nano

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStringFromInt64(v int64, zeroIsNull bool) any {
	if zeroIsNull && v == 0 {
		return nil
	}
	return v
}

func nullTimeStr(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(timeFormat)
}

func nullTimePtrStr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeFormat)
}

func parseTimeStr(s sql.NullString) (time.Time, error) {
	if !s.Valid {
		return time.Time{}, nil
	}
	if strings.TrimSpace(s.String) == "" {
		return time.Time{}, nil
	}
	return time.Parse(timeFormat, s.String)
}

func parseTimePtrStr(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	if strings.TrimSpace(s.String) == "" {
		return nil, nil
	}
	t, err := time.Parse(timeFormat, s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func nullDuration(d *time.Duration) any {
	if d == nil {
		return nil
	}
	return int64(d.Seconds())
}

func parseDurationFromInt(v sql.NullInt64) *time.Duration {
	if !v.Valid {
		return nil
	}
	d := time.Duration(v.Int64) * time.Second
	return &d
}

func marshalStringList(items []string) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalStringList(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// IsUniqueConstraint reports whether err comes from a SQLite UNIQUE
// violation. modernc.org/sqlite spells the error message
// "constraint failed: UNIQUE ...".
func IsUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: UNIQUE")
}

// IsForeignKeyConstraint reports whether err is a FK violation.
func IsForeignKeyConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: FOREIGN KEY")
}

// errNotMatched is the sentinel used internally when CAS UPDATE rowsAffected=0
// and the caller cannot distinguish not-found vs version-conflict.
var errNotMatched = errors.New("taskruntime sqlite: row not matched by CAS UPDATE")
