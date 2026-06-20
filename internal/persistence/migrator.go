package persistence

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// MigrationsFS embeds the migration scripts.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Migrator applies versioned SQL migrations stored in an FS.
//
// Each migration consists of two files named "{NNNN}_{name}.up.sql" and
// "{NNNN}_{name}.down.sql". The migrator tracks applied versions in the
// "schema_migrations" table.
//
// Per plan-1 § 3.1.2: we use a hand-rolled migrator (not golang-migrate/v4)
// to avoid depending on its built-in driver registrations conflicting with
// modernc.org/sqlite — the scope (Phase 1: one migration; v1: append-only
// migrations per conventions § 9.1) doesn't justify the dependency.
type Migrator struct {
	db *sql.DB
	fs fs.FS
}

// NewMigrator returns a Migrator that applies migrations from MigrationsFS.
func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db, fs: subFS(MigrationsFS, "migrations")}
}

// NewMigratorFS returns a Migrator that reads from a custom FS. Tests use
// this to inject malformed / empty migration sets.
func NewMigratorFS(db *sql.DB, fsys fs.FS) *Migrator {
	return &Migrator{db: db, fs: fsys}
}

type migration struct {
	version int
	name    string
	up      string
	down    string
}

// Up applies all migrations not yet recorded in schema_migrations.
// Idempotent: calling Up on a fully-migrated DB is a no-op.
func (m *Migrator) Up(ctx context.Context) error {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return err
	}
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}
	all, err := m.loadMigrations()
	if err != nil {
		return err
	}
	for _, mig := range all {
		if applied[mig.version] {
			continue
		}
		if err := m.applyOne(ctx, mig, true); err != nil {
			return fmt.Errorf("apply up %04d_%s: %w", mig.version, mig.name, err)
		}
	}
	return nil
}

// Down reverts migrations until current version == target.
// target=0 reverts everything; target=-1 means "previous version".
func (m *Migrator) Down(ctx context.Context, target int) error {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return err
	}
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}
	all, err := m.loadMigrations()
	if err != nil {
		return err
	}
	// Determine effective target.
	current := highestApplied(applied)
	if target < 0 {
		if current == 0 {
			return nil // nothing to revert
		}
		target = current - 1
	}
	if target < 0 {
		target = 0
	}
	// Revert in descending order.
	sort.Slice(all, func(i, j int) bool { return all[i].version > all[j].version })
	for _, mig := range all {
		if mig.version <= target {
			break
		}
		if !applied[mig.version] {
			continue
		}
		if err := m.applyOne(ctx, mig, false); err != nil {
			return fmt.Errorf("apply down %04d_%s: %w", mig.version, mig.name, err)
		}
	}
	return nil
}

// Version returns the highest applied migration version (0 = none applied).
func (m *Migrator) Version(ctx context.Context) (int, error) {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return 0, err
	}
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return 0, err
	}
	return highestApplied(applied), nil
}

func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	const ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
        version    INTEGER PRIMARY KEY,
        name       TEXT NOT NULL,
        applied_at TEXT NOT NULL
    )`
	_, err := m.db.ExecContext(ctx, ddl)
	return err
}

func (m *Migrator) appliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func (m *Migrator) loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(m.fs, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	byVersion := map[int]*migration{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version, name, direction, err := parseMigrationName(e.Name())
		if err != nil {
			return nil, err
		}
		content, err := fs.ReadFile(m.fs, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		mig, ok := byVersion[version]
		if !ok {
			mig = &migration{version: version, name: name}
			byVersion[version] = mig
		} else if mig.name != name {
			// Two DIFFERENT migrations claim the same version number (the up/down
			// pair of ONE migration share a name, so that is fine). The map keys by
			// version, so without this guard the alphabetically-later file would
			// SILENTLY overwrite the earlier one's SQL and that migration would
			// never run on a fresh DB (this bit T216/0062 and T236/0064 — fresh DBs
			// missed columns with no error). Fail loudly so a renumber collision
			// surfaces at the first migration run, not in production.
			return nil, fmt.Errorf("duplicate migration version %04d: %q and %q — renumber one (a number must map to exactly one migration)", version, mig.name, name)
		}
		switch direction {
		case "up":
			mig.up = string(content)
		case "down":
			mig.down = string(content)
		}
	}
	if len(byVersion) == 0 {
		return nil, errors.New("no migrations found in FS")
	}
	out := make([]migration, 0, len(byVersion))
	for _, mig := range byVersion {
		if mig.up == "" {
			return nil, fmt.Errorf("migration %04d_%s missing up.sql", mig.version, mig.name)
		}
		if mig.down == "" {
			return nil, fmt.Errorf("migration %04d_%s missing down.sql", mig.version, mig.name)
		}
		out = append(out, *mig)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// applyOne runs the up or down SQL of a single migration inside a tx and
// updates schema_migrations.
func (m *Migrator) applyOne(ctx context.Context, mig migration, up bool) error {
	sqlText := mig.up
	if !up {
		sqlText = mig.down
	}
	return RunInTx(ctx, m.db, func(txCtx context.Context) error {
		exec, err := ExecutorFromCtx(txCtx, m.db)
		if err != nil {
			return err
		}
		if _, err := exec.ExecContext(txCtx, sqlText); err != nil {
			return err
		}
		if up {
			_, err = exec.ExecContext(txCtx,
				`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, datetime('now'))`,
				mig.version, mig.name)
		} else {
			_, err = exec.ExecContext(txCtx,
				`DELETE FROM schema_migrations WHERE version = ?`, mig.version)
		}
		return err
	})
}

func parseMigrationName(filename string) (version int, name, direction string, err error) {
	// expected: NNNN_name.up.sql or NNNN_name.down.sql
	base := strings.TrimSuffix(filename, ".sql")
	if strings.HasSuffix(base, ".up") {
		direction = "up"
		base = strings.TrimSuffix(base, ".up")
	} else if strings.HasSuffix(base, ".down") {
		direction = "down"
		base = strings.TrimSuffix(base, ".down")
	} else {
		err = fmt.Errorf("migration %q: missing .up/.down marker", filename)
		return
	}
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("migration %q: malformed name", filename)
		return
	}
	version, err = strconv.Atoi(parts[0])
	if err != nil {
		err = fmt.Errorf("migration %q: parse version: %w", filename, err)
		return
	}
	name = parts[1]
	return
}

func highestApplied(applied map[int]bool) int {
	max := 0
	for v := range applied {
		if v > max {
			max = v
		}
	}
	return max
}

func subFS(root fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		panic(fmt.Sprintf("subFS: %v", err))
	}
	return sub
}
