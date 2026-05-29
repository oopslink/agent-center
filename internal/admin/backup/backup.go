// Package backup implements the `agent-center admin backup` CLI handler
// + the underlying SQLite backup runtime. Per plan-7 § 3.7 +
// implementation/06 § 6.2.
//
// Design choices:
//
//   - WAL checkpoint(FULL) + cp the database file. We do NOT use
//     `sqlite3 .dump` (textual SQL is much slower at GB scale; binary
//     copy is the standard SQLite hot-backup pattern).
//   - We require the caller to provide the SQLite file path; the CLI
//     handler resolves it from the config (server.sqlite_path). This
//     keeps the runtime decoupled from cli/config plumbing.
//   - Retention: post-copy, prune dated subdirectories older than
//     --retention-days (default 30). Same as the design § 6.2 shell
//     script.
//   - All failure modes emit observability events
//     (`admin.backup_ok` / `admin.backup_failed`) so backups participate
//     in the same audit story as the rest of the domain (conventions
//     § 2 + § 17).
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
)

// DefaultRetentionDays is the default retention window for dated
// backup subdirectories.
const DefaultRetentionDays = 30

// Runner executes the backup workflow.
type Runner struct {
	db        *sql.DB
	dbPath    string
	destRoot  string
	retention time.Duration
	sink      *observability.EventSink
	clock     clock.Clock
	actor     observability.Actor
	// fs hooks (for testing)
	mkdirAll   func(path string, perm os.FileMode) error
	copyFile   func(src, dst string) error
	removeAll  func(path string) error
	readDirAll func(path string) ([]os.DirEntry, error)
}

// Config wires the runner.
type Config struct {
	DB        *sql.DB
	DBPath    string
	DestRoot  string
	Retention time.Duration
	Sink      *observability.EventSink
	Clock     clock.Clock
	Actor     observability.Actor
}

// WithFS replaces the default filesystem hooks for tests. Production
// code should never call this. Returns the runner for chaining.
func (r *Runner) WithFS(mkdir func(string, os.FileMode) error,
	copyFile func(string, string) error,
	remove func(string) error,
	readDir func(string) ([]os.DirEntry, error)) *Runner {
	if mkdir != nil {
		r.mkdirAll = mkdir
	}
	if copyFile != nil {
		r.copyFile = copyFile
	}
	if remove != nil {
		r.removeAll = remove
	}
	if readDir != nil {
		r.readDirAll = readDir
	}
	return r
}

// NewRunner builds the backup runner. Failure cases (missing deps)
// surface immediately so the caller does not need to special-case nil.
func NewRunner(cfg Config) (*Runner, error) {
	if cfg.DB == nil {
		return nil, errors.New("backup: db required")
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return nil, errors.New("backup: db_path required")
	}
	if strings.TrimSpace(cfg.DestRoot) == "" {
		return nil, errors.New("backup: dest_root required")
	}
	if cfg.Sink == nil {
		return nil, errors.New("backup: event sink required")
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.SystemClock{}
	}
	if err := cfg.Actor.Validate(); err != nil {
		return nil, fmt.Errorf("backup: actor: %w", err)
	}
	r := &Runner{
		db:         cfg.DB,
		dbPath:     cfg.DBPath,
		destRoot:   cfg.DestRoot,
		retention:  cfg.Retention,
		sink:       cfg.Sink,
		clock:      cfg.Clock,
		actor:      cfg.Actor,
		mkdirAll:   os.MkdirAll,
		copyFile:   defaultCopyFile,
		removeAll:  os.RemoveAll,
		readDirAll: func(p string) ([]os.DirEntry, error) { return os.ReadDir(p) },
	}
	if r.retention <= 0 {
		r.retention = DefaultRetentionDays * 24 * time.Hour
	}
	return r, nil
}

// RunResult captures one execution.
type RunResult struct {
	DestDir       string
	DestFile      string
	BytesCopied   int64
	Pruned        []string
	WALCheckpoint bool
}

// Run executes one backup pass. The destination directory is
// `<dest_root>/<YYYYMMDD-HHMMSS>/` and the dump file is
// `agent-center.db`.
func (r *Runner) Run(ctx context.Context) (RunResult, error) {
	now := r.clock.Now().UTC()
	stamp := now.Format("20060102-150405")
	destDir := filepath.Join(r.destRoot, stamp)
	if err := r.mkdirAll(destDir, 0o700); err != nil {
		r.emitFailed(ctx, "mkdir_failed", err.Error())
		return RunResult{}, fmt.Errorf("backup: mkdir %q: %w", destDir, err)
	}
	if _, err := r.db.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL);"); err != nil {
		r.emitFailed(ctx, "wal_checkpoint_failed", err.Error())
		return RunResult{}, fmt.Errorf("backup: wal_checkpoint: %w", err)
	}
	dst := filepath.Join(destDir, filepath.Base(r.dbPath))
	if err := r.copyFile(r.dbPath, dst); err != nil {
		r.emitFailed(ctx, "copy_failed", err.Error())
		return RunResult{}, fmt.Errorf("backup: copy %q→%q: %w", r.dbPath, dst, err)
	}
	stat, err := os.Stat(dst)
	if err != nil {
		r.emitFailed(ctx, "stat_failed", err.Error())
		return RunResult{}, fmt.Errorf("backup: stat dst: %w", err)
	}
	pruned, perr := r.pruneOldDirs(now)
	if perr != nil {
		// Pruning failure is not fatal — emit warning event but keep
		// the fresh backup. The next run retries.
		_, _ = r.sink.Emit(ctx, observability.EmitCommand{
			EventType: "admin.backup_prune_failed",
			Actor:     r.actor,
			Payload: map[string]any{
				"reason":  "prune_failed",
				"message": perr.Error(),
			},
		})
	}
	_, _ = r.sink.Emit(ctx, observability.EmitCommand{
		EventType: "admin.backup_ok",
		Actor:     r.actor,
		Payload: map[string]any{
			"dest_dir":  destDir,
			"dest_file": dst,
			"bytes":     stat.Size(),
			"retention": r.retention.String(),
			"pruned":    pruned,
		},
	})
	return RunResult{
		DestDir:       destDir,
		DestFile:      dst,
		BytesCopied:   stat.Size(),
		Pruned:        pruned,
		WALCheckpoint: true,
	}, nil
}

func (r *Runner) emitFailed(ctx context.Context, reason, msg string) {
	_, _ = r.sink.Emit(ctx, observability.EmitCommand{
		EventType: "admin.backup_failed",
		Actor:     r.actor,
		Payload: map[string]any{
			"reason":  reason,
			"message": msg,
		},
	})
}

// pruneOldDirs removes <dest_root>/<YYYYMMDD-HHMMSS> subdirectories
// older than `now - retention`. Returns the list of pruned paths.
//
// Robustness:
//   - Subdirectories whose name doesn't match the timestamp format are
//     ignored (left intact).
//   - Filesystem errors on individual entries are surfaced via the
//     returned error (best-effort: we delete what we can and report
//     the first error encountered).
func (r *Runner) pruneOldDirs(now time.Time) ([]string, error) {
	entries, err := r.readDirAll(r.destRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := now.Add(-r.retention)
	var pruned []string
	var firstErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ts, perr := time.Parse("20060102-150405", entry.Name())
		if perr != nil {
			continue
		}
		if ts.After(cutoff) {
			continue
		}
		path := filepath.Join(r.destRoot, entry.Name())
		if err := r.removeAll(path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		pruned = append(pruned, path)
	}
	return pruned, firstErr
}

// defaultCopyFile copies src → dst at file level. The destination file
// inherits 0600 perms (backups carry credentials in encoded form).
func defaultCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
