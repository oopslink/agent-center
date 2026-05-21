package persistence

import (
	"context"
	"database/sql"
	"errors"
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
func RunInTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) (retErr error) {
	if fn == nil {
		return errors.New("persistence: RunInTx requires non-nil fn")
	}
	if _, ok := TxFromCtx(ctx); ok {
		// Already in a tx — reuse it. Caller owns commit/rollback.
		return fn(ctx)
	}
	if db == nil {
		return errors.New("persistence: RunInTx requires non-nil *sql.DB")
	}
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
