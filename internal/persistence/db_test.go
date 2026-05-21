package persistence

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpen_RejectsEmptyDSN(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("expected error for empty DSN")
	}
	if _, err := Open("   "); err == nil {
		t.Fatal("expected error for whitespace DSN")
	}
}

func TestOpen_AppliesPragmasInMemory(t *testing.T) {
	// In-memory DBs do NOT support WAL — SQLite forces 'memory'. We only
	// assert the non-journal pragmas land.
	db, err := Open(MemoryDSN())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var fk string
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != "1" {
		t.Fatalf("PRAGMA foreign_keys = %q, want 1", fk)
	}
}

func TestOpen_AppliesWALOnFileDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("PRAGMA journal_mode = %q, want wal", mode)
	}
}

func TestOpen_FilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestNormalizeDSN_AppendsMissingPragmas(t *testing.T) {
	in := "file:foo.db?_pragma=journal_mode(WAL)"
	out, err := normalizeDSN(in)
	if err != nil {
		t.Fatalf("normalizeDSN: %v", err)
	}
	// URL encoding turns '(' / ')' into %28 / %29; check both forms.
	for _, want := range []string{"busy_timeout", "foreign_keys", "synchronous"} {
		if !strings.Contains(out, want) {
			t.Fatalf("normalizeDSN missing pragma %s: %s", want, out)
		}
	}
}

func TestNormalizeDSN_MemoryShortForm(t *testing.T) {
	out, err := normalizeDSN(":memory:")
	if err != nil {
		t.Fatalf("normalizeDSN(:memory:): %v", err)
	}
	if !strings.Contains(out, "memory") {
		t.Fatalf("memory DSN missing :memory: token: %s", out)
	}
	// Memory DSN intentionally lacks journal_mode (SQLite forces 'memory').
	// We still expect the rest of the pragmas.
	for _, want := range []string{"foreign_keys", "busy_timeout", "synchronous"} {
		if !strings.Contains(out, want) {
			t.Fatalf("memory DSN missing %s pragma: %s", want, out)
		}
	}
}

func TestWithTx_StoresAndRetrievesTx(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	ctx := WithTx(context.Background(), tx)
	got, ok := TxFromCtx(ctx)
	if !ok {
		t.Fatal("TxFromCtx: ok=false")
	}
	if got != tx {
		t.Fatal("TxFromCtx returned different tx")
	}
}

func TestWithTx_NilTxNoop(t *testing.T) {
	ctx := WithTx(context.Background(), nil)
	if _, ok := TxFromCtx(ctx); ok {
		t.Fatal("expected no tx for nil input")
	}
}

func TestTxFromCtx_EmptyCtx(t *testing.T) {
	if _, ok := TxFromCtx(context.Background()); ok {
		t.Fatal("expected ok=false on empty ctx")
	}
	if _, ok := TxFromCtx(nil); ok {
		t.Fatal("expected ok=false on nil ctx")
	}
}

func TestExecutorFromCtx_PrefersTx(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	ctx := WithTx(context.Background(), tx)
	exec, err := ExecutorFromCtx(ctx, db)
	if err != nil {
		t.Fatalf("ExecutorFromCtx: %v", err)
	}
	if exec != SQLExecutor(tx) {
		t.Fatal("expected tx executor")
	}
}

func TestExecutorFromCtx_FallsBackToDB(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	exec, err := ExecutorFromCtx(context.Background(), db)
	if err != nil {
		t.Fatalf("ExecutorFromCtx: %v", err)
	}
	if exec != SQLExecutor(db) {
		t.Fatal("expected db executor")
	}
}

func TestExecutorFromCtx_NoTxNoFallback(t *testing.T) {
	if _, err := ExecutorFromCtx(context.Background(), nil); !errors.Is(err, ErrNoExecutor) {
		t.Fatalf("expected ErrNoExecutor, got %v", err)
	}
}

func TestRunInTx_CommitsOnSuccess(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	err := RunInTx(context.Background(), db, func(ctx context.Context) error {
		exec, err := ExecutorFromCtx(ctx, db)
		if err != nil {
			return err
		}
		_, err = exec.ExecContext(ctx, `INSERT INTO t (id) VALUES (?)`, "a")
		return err
	})
	if err != nil {
		t.Fatalf("RunInTx: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row committed, got %d", n)
	}
}

func TestRunInTx_RollsBackOnError(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("boom")
	err := RunInTx(context.Background(), db, func(ctx context.Context) error {
		exec, _ := ExecutorFromCtx(ctx, db)
		_, _ = exec.ExecContext(ctx, `INSERT INTO t (id) VALUES (?)`, "a")
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected rollback, got %d rows", n)
	}
}

func TestRunInTx_RollsBackOnPanic(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic")
			}
		}()
		_ = RunInTx(context.Background(), db, func(ctx context.Context) error {
			exec, _ := ExecutorFromCtx(ctx, db)
			_, _ = exec.ExecContext(ctx, `INSERT INTO t (id) VALUES (?)`, "a")
			panic("oops")
		})
	}()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected rollback on panic, got %d rows", n)
	}
}

func TestRunInTx_RejectsNilDB(t *testing.T) {
	err := RunInTx(context.Background(), nil, func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestRunInTx_RejectsNilFn(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	if err := RunInTx(context.Background(), db, nil); err == nil {
		t.Fatal("expected error for nil fn")
	}
}

// TestRunInTx_NestedReusesOuterTx verifies that a nested RunInTx call
// joins the outer tx rather than deadlocking on a fresh BeginTx (cross-BC
// scenario like Discussion → TaskRuntime IssueConcludeSpawn).
func TestRunInTx_NestedReusesOuterTx(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE n (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	err := RunInTx(context.Background(), db, func(outer context.Context) error {
		// Outer wrote one row inside its tx.
		if _, err := getExec(outer).ExecContext(outer, `INSERT INTO n (id) VALUES (1)`); err != nil {
			return err
		}
		// Nested call must reuse the same tx, not deadlock.
		return RunInTx(outer, db, func(inner context.Context) error {
			_, err := getExec(inner).ExecContext(inner, `INSERT INTO n (id) VALUES (2)`)
			return err
		})
	})
	if err != nil {
		t.Fatalf("nested RunInTx: %v", err)
	}
	var c int
	_ = db.QueryRow(`SELECT COUNT(*) FROM n`).Scan(&c)
	if c != 2 {
		t.Fatalf("expected 2 rows committed, got %d", c)
	}
}

// TestRunInTx_NestedRollsBackBothOnInnerError verifies that a nested
// RunInTx returning an error causes the outer tx to roll back too —
// because the inner call doesn't commit / rollback (outer owns it).
func TestRunInTx_NestedRollsBackBothOnInnerError(t *testing.T) {
	db := openMem(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE n (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("inner boom")
	err := RunInTx(context.Background(), db, func(outer context.Context) error {
		if _, err := getExec(outer).ExecContext(outer, `INSERT INTO n (id) VALUES (1)`); err != nil {
			return err
		}
		return RunInTx(outer, db, func(inner context.Context) error {
			if _, err := getExec(inner).ExecContext(inner, `INSERT INTO n (id) VALUES (2)`); err != nil {
				return err
			}
			return sentinel
		})
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	var c int
	_ = db.QueryRow(`SELECT COUNT(*) FROM n`).Scan(&c)
	if c != 0 {
		t.Fatalf("expected 0 rows (both rolled back), got %d", c)
	}
}

func getExec(ctx context.Context) SQLExecutor {
	tx, ok := TxFromCtx(ctx)
	if !ok {
		panic("expected tx in ctx")
	}
	return tx
}

func openMem(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
