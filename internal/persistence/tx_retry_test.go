package persistence

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// errBusySentinel is recognized by the overridden retry predicate in the
// mechanics tests so we can exercise the retry loop deterministically
// without fabricating a driver-level *sqlite.Error (its fields are
// unexported).
var errBusySentinel = errors.New("busy-sentinel")

// withSentinelRetry overrides retryableTxErr to treat errBusySentinel as the
// transient busy error, restoring the default on cleanup.
func withSentinelRetry(t *testing.T) {
	t.Helper()
	prev := retryableTxErr
	retryableTxErr = func(err error) bool { return errors.Is(err, errBusySentinel) }
	t.Cleanup(func() { retryableTxErr = prev })
}

func memDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(MemoryDSN())
	if err != nil {
		t.Fatalf("open mem db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRunInTx_RetriesOnBusyThenSucceeds(t *testing.T) {
	withSentinelRetry(t)
	db := memDB(t)

	var attempts int
	err := RunInTx(context.Background(), db, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errBusySentinel // transient: whole tx replays
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInTx returned %v, want nil after retries", err)
	}
	if attempts != 3 {
		t.Fatalf("fn ran %d times, want 3 (2 busy retries + success)", attempts)
	}
}

func TestRunInTx_NonRetryableErrorReturnsImmediately(t *testing.T) {
	withSentinelRetry(t)
	db := memDB(t)

	sentinel := errors.New("boom")
	var attempts int
	err := RunInTx(context.Background(), db, func(ctx context.Context) error {
		attempts++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunInTx returned %v, want %v", err, sentinel)
	}
	if attempts != 1 {
		t.Fatalf("fn ran %d times, want 1 (non-retryable must not retry)", attempts)
	}
}

func TestRunInTx_BoundedAttemptsOnPersistentBusy(t *testing.T) {
	withSentinelRetry(t)
	db := memDB(t)

	var attempts int
	err := RunInTx(context.Background(), db, func(ctx context.Context) error {
		attempts++
		return errBusySentinel
	})
	if !errors.Is(err, errBusySentinel) {
		t.Fatalf("RunInTx returned %v, want busy sentinel after exhausting retries", err)
	}
	if attempts != maxTxAttempts {
		t.Fatalf("fn ran %d times, want maxTxAttempts=%d", attempts, maxTxAttempts)
	}
}

func TestRunInTx_ReentrantDoesNotRetry(t *testing.T) {
	withSentinelRetry(t)
	db := memDB(t)

	// Open an outer tx and carry it in ctx — RunInTx must delegate to fn
	// without owning commit OR retry (re-running part of an outer tx is
	// incorrect; the outermost RunInTx owns the whole-tx replay).
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	ctx := WithTx(context.Background(), tx)

	var attempts int
	err = RunInTx(ctx, db, func(ctx context.Context) error {
		attempts++
		return errBusySentinel
	})
	if !errors.Is(err, errBusySentinel) {
		t.Fatalf("RunInTx returned %v, want busy sentinel passed through", err)
	}
	if attempts != 1 {
		t.Fatalf("fn ran %d times, want 1 (reentrant must not retry)", attempts)
	}
}

func TestRunInTx_ContextCanceledDuringBackoffAborts(t *testing.T) {
	withSentinelRetry(t)
	db := memDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	var attempts int
	done := make(chan error, 1)
	go func() {
		done <- RunInTx(ctx, db, func(ctx context.Context) error {
			attempts++
			cancel() // cancel during the first attempt → backoff must abort
			return errBusySentinel
		})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunInTx returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunInTx did not abort promptly on context cancellation")
	}
}

func TestIsSQLiteBusy(t *testing.T) {
	if isSQLiteBusy(nil) {
		t.Fatal("isSQLiteBusy(nil) = true, want false")
	}
	if isSQLiteBusy(errors.New("not a sqlite error")) {
		t.Fatal("isSQLiteBusy(plain error) = true, want false")
	}

	// Induce a real SQLITE_BUSY: WAL + busy_timeout(0) so a second writer
	// fails immediately instead of waiting. Hold a write lock on one
	// connection, then attempt an autocommit write on another.
	dir := t.TempDir()
	dsn := "file:" + dir + "/busy.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(0)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	ctx := context.Background()
	connA, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("connA: %v", err)
	}
	defer func() { _ = connA.Close() }()
	txA, err := connA.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginA: %v", err)
	}
	defer func() { _ = txA.Rollback() }()
	if _, err := txA.ExecContext(ctx, "INSERT INTO t(v) VALUES('a')"); err != nil {
		t.Fatalf("writeA (acquire write lock): %v", err)
	}

	connB, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("connB: %v", err)
	}
	defer func() { _ = connB.Close() }()
	_, busyErr := connB.ExecContext(ctx, "INSERT INTO t(v) VALUES('b')")
	if busyErr == nil {
		t.Fatal("expected SQLITE_BUSY from contended write, got nil")
	}
	if !isSQLiteBusy(busyErr) {
		t.Fatalf("isSQLiteBusy(%v) = false, want true (real SQLITE_BUSY)", busyErr)
	}
}
