package persistence

import (
	"context"
	"database/sql"
	"errors"
	"math/rand"
	"time"

	"modernc.org/sqlite"
)

// txKey is the unexported context key used to carry an active *sql.Tx.
type txKey struct{}

// WithTx returns ctx with tx attached. Repository implementations call
// SQLExecutor / ExecutorFromCtx and prefer the carried tx when present.
//
// Per 02-persistence-schema § 5: tx is plumbed through ctx, never as an
// explicit parameter on Repository method signatures.
func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
	if tx == nil {
		return ctx
	}
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFromCtx returns the active tx if any.
func TxFromCtx(ctx context.Context) (*sql.Tx, bool) {
	if ctx == nil {
		return nil, false
	}
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	return tx, ok && tx != nil
}

// SQLExecutor is the minimal surface a Repository needs from *sql.DB or
// *sql.Tx. Repository implementations should grab one via ExecutorFromCtx
// and never poke at *sql.DB directly when ctx carries a tx.
type SQLExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ExecutorFromCtx returns the tx if ctx has one, else fallback (typically
// *sql.DB). If both are nil it returns ErrNoExecutor.
func ExecutorFromCtx(ctx context.Context, fallback SQLExecutor) (SQLExecutor, error) {
	if tx, ok := TxFromCtx(ctx); ok {
		return tx, nil
	}
	if fallback == nil {
		return nil, ErrNoExecutor
	}
	return fallback, nil
}

// ErrNoExecutor is returned by ExecutorFromCtx when no tx and no fallback
// were provided. Repository implementations should never see this in normal
// operation.
var ErrNoExecutor = errors.New("persistence: no SQL executor (no tx in ctx and no fallback)")

// RunInTx opens a transaction, attaches it to ctx, invokes fn, and commits or
// rolls back depending on fn's return. If fn returns an error or panics the
// tx is rolled back and the error / panic is propagated.
//
// Use this from Application Services / CLI handlers — NOT from Repository
// methods. Per 02-persistence-schema § 5: Repository.* methods must not
// BeginTx themselves.
//
// If ctx already carries a tx (cross-BC nested call), RunInTx joins the
// existing tx instead of starting a new one — fn runs inside the outer tx
// and commit/rollback is the outer caller's responsibility. This makes
// services tx-reentrant for cross-BC scenarios like Discussion → TaskRuntime
// IssueConcludeSpawn (ADR-0014 § 2 same-tx double write).
//
// v2.7 #149: when RunInTx owns the transaction (not reentrant), it retries
// the WHOLE transaction on a transient SQLite write-lock conflict
// (SQLITE_BUSY / BUSY_SNAPSHOT 517). A deferred read-then-write tx takes its
// read snapshot at first access; if another writer commits before our write,
// SQLite returns BUSY_SNAPSHOT (517) — which busy_timeout does NOT retry
// internally (unlike plain BUSY). The only correct recovery is to roll back
// and replay the whole tx so fn re-reads a fresh snapshot
// (read→modify→write). This requires fn to be a pure DB read-modify-write
// (no non-idempotent external side effects), which is the convention for
// Application Service transactions. The reentrant branch does NOT retry —
// the outermost RunInTx owns the whole-tx replay.
func RunInTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) (retErr error) {
	if fn == nil {
		return errors.New("persistence: RunInTx requires non-nil fn")
	}
	if _, ok := TxFromCtx(ctx); ok {
		// Already in a tx — reuse it. Caller owns commit/rollback (and any
		// whole-tx retry, which only the outermost RunInTx can do safely).
		return fn(ctx)
	}
	if db == nil {
		return errors.New("persistence: RunInTx requires non-nil *sql.DB")
	}

	var lastErr error
	for attempt := 0; attempt < maxTxAttempts; attempt++ {
		err := runTxOnce(ctx, db, fn)
		if err == nil {
			return nil
		}
		if !retryableTxErr(err) {
			return err
		}
		lastErr = err
		// Back off with jitter before replaying; abort if ctx is done so a
		// canceled/timed-out request doesn't keep spinning.
		timer := time.NewTimer(txBackoff(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

// runTxOnce runs a single transaction attempt: begin → fn → commit/rollback.
// A returned error (from fn or Commit) or a panic rolls back. The Commit
// error is surfaced so RunInTx can detect a busy conflict that only appears
// at commit time and replay the whole tx.
func runTxOnce(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) (retErr error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if retErr != nil {
			_ = tx.Rollback()
			return
		}
		if cerr := tx.Commit(); cerr != nil {
			retErr = cerr
		}
	}()
	return fn(WithTx(ctx, tx))
}

const (
	// maxTxAttempts bounds whole-transaction replays on SQLITE_BUSY /
	// BUSY_SNAPSHOT. 6 attempts with capped jittered backoff stay well
	// under request timeouts while absorbing realistic write-contention
	// bursts (e.g. a worker daemon's capability report vs foreground writes).
	maxTxAttempts = 6
	txBackoffBase = 2 * time.Millisecond
	txBackoffMax  = 40 * time.Millisecond
)

// txBackoff returns a jittered backoff for the given (0-based) retry attempt:
// exponential base*2^attempt capped at txBackoffMax, with full jitter to
// de-synchronize competing writers.
func txBackoff(attempt int) time.Duration {
	d := txBackoffBase << attempt
	if d > txBackoffMax || d <= 0 {
		d = txBackoffMax
	}
	return time.Duration(rand.Int63n(int64(d) + 1))
}

// retryableTxErr reports whether err warrants a whole-tx replay. Overridable
// in tests; default isSQLiteBusy.
var retryableTxErr = isSQLiteBusy

// isSQLiteBusy reports whether err is a transient SQLite write-lock conflict
// (SQLITE_BUSY = 5 or SQLITE_BUSY_SNAPSHOT = 517). The primary result code
// lives in the low byte of the extended code, so both map to 5.
func isSQLiteBusy(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()&0xff == 5 // SQLITE_BUSY primary code
	}
	return false
}
