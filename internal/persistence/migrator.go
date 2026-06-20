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
	// Repair DBs left inconsistent by the historical 0064 version collision
	// before the apply loop — on a "State B" DB the renumbered 0065 would
	// otherwise crash with a duplicate-column error (see the method doc).
	if err := m.reconcileLegacy0064Collision(ctx, applied); err != nil {
		return fmt.Errorf("reconcile 0064 collision: %w", err)
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

// reconcileLegacy0064Collision repairs DBs left inconsistent by the historical
// 0064 version collision: two DIFFERENT migrations were both numbered 0064 —
// center_settings (T216) and agent_llm_config (T236) — fixed structurally by
// renumbering agent_llm_config -> 0065 in d34dfe20.
//
// That renumber self-heals "State A" DBs (center_settings won the 0064 slot, so
// the agent_llm_config ADD COLUMNs never ran): the new 0065 adds the missing
// columns on the next Up. It does NOT handle "State B" DBs — ones that ran an
// intermediate build where agent_llm_config was the SOLE 0064. On those:
//   - agents already has reasoning/mode/provider (added under version 64), and
//   - center_settings was never created (its 0064 slot was consumed and recorded
//     as applied, so the renumbered 0064_center_settings is skipped forever).
//
// On such a DB the renumbered 0065_agent_llm_config re-runs `ALTER TABLE agents
// ADD COLUMN reasoning` and crashes startup with "duplicate column name".
//
// This runs once before the apply loop and, ONLY when the State B fingerprint
// matches (v64 applied, v65 not, agents.reasoning already present), creates the
// missing center_settings table and records version 65 as applied so the apply
// loop skips the colliding 0065. Idempotent and a no-op on every other DB shape
// (fresh, State A, already-healed). Mutates `applied` so the caller's apply loop
// sees version 65 as done.
func (m *Migrator) reconcileLegacy0064Collision(ctx context.Context, applied map[int]bool) error {
	if !applied[64] || applied[65] {
		return nil
	}
	hasReasoning, err := m.columnExists(ctx, "agents", "reasoning")
	if err != nil {
		return err
	}
	if !hasReasoning {
		// State A (column missing): let 0065 add the columns normally.
		return nil
	}
	if err := RunInTx(ctx, m.db, func(txCtx context.Context) error {
		exec, err := ExecutorFromCtx(txCtx, m.db)
		if err != nil {
			return err
		}
		// The center_settings table the consumed 0064 slot never created here;
		// DDL mirrors 0064_center_settings.up.sql.
		if _, err := exec.ExecContext(txCtx, `CREATE TABLE IF NOT EXISTS center_settings (
            key        TEXT PRIMARY KEY,
            value      TEXT NOT NULL,
            updated_at TEXT NOT NULL
        )`); err != nil {
			return err
		}
		// Record 0065 as applied: its columns already exist, so re-running its
		// ALTERs would fail. INSERT OR IGNORE keeps this idempotent.
		_, err = exec.ExecContext(txCtx,
			`INSERT OR IGNORE INTO schema_migrations (version, name, applied_at) VALUES (65, 'agent_llm_config', datetime('now'))`)
		return err
	}); err != nil {
		return err
	}
	applied[65] = true
	return nil
}

// columnExists reports whether table has a column named col, via PRAGMA
// table_info. table is a trusted internal literal (PRAGMA does not accept a
// bound parameter for the table name).
func (m *Migrator) columnExists(ctx context.Context, table, col string) (bool, error) {
	rows, err := m.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
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
