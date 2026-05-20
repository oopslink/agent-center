// Package persistence provides SQLite connection helpers and transaction
// context plumbing (WithTx / TxFromCtx / SQLExecutor).
//
// Per 02-persistence-schema § 1.1: driver is modernc.org/sqlite (pure Go, no
// CGO). Per § 1.2: WAL + busy_timeout=5000 + foreign_keys=ON + synchronous=
// NORMAL. Per § 5: tx flows through context.
package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite" // sqlite driver
)

// Default DSN parameters required by 02-persistence-schema § 1.2.
const (
	paramJournal     = "_pragma=journal_mode(WAL)"
	paramBusyTimeout = "_pragma=busy_timeout(5000)"
	paramForeignKeys = "_pragma=foreign_keys(ON)"
	paramSync        = "_pragma=synchronous(NORMAL)"
)

// Open opens a SQLite database at dsn and applies pragmas required by 02-
// persistence-schema § 1.2. dsn may be a bare file path ("/tmp/foo.db") or a
// "file:..." URI; missing pragmas are appended.
func Open(dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("persistence: empty DSN")
	}
	finalDSN, err := normalizeDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("persistence: normalize DSN: %w", err)
	}
	db, err := sql.Open("sqlite", finalDSN)
	if err != nil {
		return nil, fmt.Errorf("persistence: open sqlite: %w", err)
	}
	// SQLite WAL allows N readers + 1 writer; writers serialize via the
	// busy_timeout pragma. We deliberately do NOT pin SetMaxOpenConns(1)
	// because that deadlocks any "tx + outside-tx read" pattern (the read
	// would wait forever for the tx's lone connection). Tests rely on
	// out-of-tx reads being visible (or invisible during the tx, then
	// visible after commit).
	//
	// In-memory DBs are an exception: each connection in a non-shared
	// "file::memory:" DSN is a SEPARATE database, so we must pin to 1
	// conn to avoid losing data across reconnects.
	if isInMemoryDSN(finalDSN) {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(0) // unlimited
		db.SetMaxIdleConns(2)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("persistence: ping sqlite: %w", err)
	}
	return db, nil
}

func isInMemoryDSN(dsn string) bool {
	return strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory")
}

// MemoryDSN returns an in-memory DSN with the busy_timeout / FK / sync
// pragmas. Used in unit tests that don't need cross-connection visibility.
//
// Note: in-memory databases do NOT support WAL journal mode (SQLite forces
// `memory` regardless of the pragma). For tests that need tx visibility
// semantics (active tx + concurrent read), prefer TestFileDSN which uses
// a temp-dir file DB with WAL.
func MemoryDSN() string {
	return "file::memory:?" +
		paramBusyTimeout + "&" + paramForeignKeys + "&" + paramSync
}

// FileDSN wraps a filesystem path into a "file:..." DSN with required pragmas.
func FileDSN(path string) string {
	u := &url.URL{Scheme: "file", Opaque: path}
	q := url.Values{}
	q.Set("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "synchronous(NORMAL)")
	u.RawQuery = q.Encode()
	return u.String()
}

// normalizeDSN ensures the four required pragmas are present.
func normalizeDSN(in string) (string, error) {
	// modernc.org/sqlite accepts both bare paths and "file:..." URIs. If it
	// looks like a URI we append missing pragmas; otherwise we wrap it.
	if !strings.HasPrefix(in, "file:") && !strings.HasPrefix(in, ":") {
		return FileDSN(in), nil
	}
	// in-memory short form (":memory:") -> wrap.
	if in == ":memory:" {
		return MemoryDSN(), nil
	}
	// URI form: parse, ensure pragmas.
	u, err := url.Parse(in)
	if err != nil {
		return "", err
	}
	q := u.Query()
	required := map[string]string{
		"journal_mode":  "journal_mode(WAL)",
		"busy_timeout":  "busy_timeout(5000)",
		"foreign_keys":  "foreign_keys(ON)",
		"synchronous":   "synchronous(NORMAL)",
	}
	have := map[string]bool{}
	for _, p := range q["_pragma"] {
		for k := range required {
			if strings.HasPrefix(p, k+"(") {
				have[k] = true
			}
		}
	}
	for k, v := range required {
		if !have[k] {
			q.Add("_pragma", v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
