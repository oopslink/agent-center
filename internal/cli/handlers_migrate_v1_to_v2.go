package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

// MigrateV1ToV2Command implements `agent-center migrate v1-to-v2`.
//
// Per P12 S12 audit (docs/plans/phase-12-audits/s12-migration-tool-audit.md):
//   - --dry-run reports planned ops (bridge row counts; current vs target version)
//   - --apply runs them: bridge tables → JSON archive → drop via 0025 → Up to 25
//   - Idempotent: if already at v2 (version == 25), exits 0 with "already at v2"
//   - Refuses to run silently — neither flag → usage error
func MigrateV1ToV2Command() *Command {
	return &Command{
		Name:    "v1-to-v2",
		Summary: "One-shot v1 → v2 sqlite migration (idempotent; --dry-run / --apply)",
		Examples: []string{
			"agent-center migrate v1-to-v2 --config=/etc/agent-center/config.yaml --dry-run",
			"agent-center migrate v1-to-v2 --config=/etc/agent-center/config.yaml --apply",
		},
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "config file path")
			dryRun := fs.Bool("dry-run", false, "report planned operations without mutating the DB")
			apply := fs.Bool("apply", false, "actually run the migration")
			archiveDir := fs.String("archive-dir", "", "directory for bridge-archive JSON (defaults to <sqlite-dir>/migration-archive)")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if !*dryRun && !*apply {
					fmt.Fprintln(errw, "Error: must pass exactly one of --dry-run or --apply")
					return ExitUsage
				}
				if *dryRun && *apply {
					fmt.Fprintln(errw, "Error: --dry-run and --apply are mutually exclusive")
					return ExitUsage
				}

				cfg, err := loadConfigForCLI(*cfgPath, nil)
				if err != nil {
					emitConfigErrors(errw, err)
					return ExitUsage
				}
				db, err := persistence.Open(cfg.Server.SqlitePath)
				if err != nil {
					fmt.Fprintf(errw, "Error: db_open: %v\n", err)
					return ExitBusinessError
				}
				defer db.Close()

				archiveRoot := *archiveDir
				if archiveRoot == "" {
					archiveRoot = filepath.Join(filepath.Dir(cfg.Server.SqlitePath), "migration-archive")
				}

				return runMigrateV1ToV2(ctx, db, cfg.Server.SqlitePath, archiveRoot, *dryRun, out, errw)
			}
		},
	}
}

// targetSchemaVersion is the v2 GA highest applied migration version.
// Kept as a const so tests can refer to it explicitly.
const targetSchemaVersion = 25

func runMigrateV1ToV2(
	ctx context.Context,
	db *sql.DB,
	sqlitePath, archiveDir string,
	dryRun bool,
	out, errw io.Writer,
) ExitCode {
	mig := persistence.NewMigrator(db)
	currentVer, err := mig.Version(ctx)
	if err != nil {
		fmt.Fprintf(errw, "Error: version_query: %v\n", err)
		return ExitBusinessError
	}

	// Idempotent path.
	if currentVer >= targetSchemaVersion {
		fmt.Fprintf(out, "already at v2 (schema version %d); no action\n", currentVer)
		return ExitOK
	}

	// Snapshot bridge rows BEFORE applying migration 0025 (which drops
	// the tables). Tables may not exist if v1 install never ran 0005;
	// the helper handles that.
	bridge, err := snapshotBridgeRows(ctx, db)
	if err != nil {
		fmt.Fprintf(errw, "Error: bridge_snapshot: %v\n", err)
		return ExitBusinessError
	}

	fmt.Fprintf(out, "current schema version: %d\n", currentVer)
	fmt.Fprintf(out, "target  schema version: %d\n", targetSchemaVersion)
	fmt.Fprintf(out, "bridge rows to archive:\n")
	fmt.Fprintf(out, "  feishu_delivery_ledger:    %d\n", len(bridge.FeishuDeliveryLedger))
	fmt.Fprintf(out, "  bridge_subscription_cursors: %d\n", len(bridge.BridgeSubscriptionCursors))

	if dryRun {
		fmt.Fprintln(out, "dry-run: no changes applied")
		return ExitOK
	}

	// Apply path.
	var archivePath string
	if len(bridge.FeishuDeliveryLedger) > 0 || len(bridge.BridgeSubscriptionCursors) > 0 {
		archivePath, err = writeBridgeArchive(archiveDir, sqlitePath, currentVer, bridge)
		if err != nil {
			fmt.Fprintf(errw, "Error: archive_write: %v\n", err)
			return ExitBusinessError
		}
		fmt.Fprintf(out, "bridge archive written: %s\n", archivePath)
	} else {
		fmt.Fprintln(out, "bridge tables empty or absent; no archive needed")
	}

	if err := mig.Up(ctx); err != nil {
		fmt.Fprintf(errw, "Error: migrate_up: %v\n", err)
		return ExitBusinessError
	}
	finalVer, _ := mig.Version(ctx)
	fmt.Fprintf(out, "migration applied; new schema version: %d\n", finalVer)
	return ExitOK
}

// bridgeSnapshot holds the rows captured before 0025 drops the
// tables. Field names match table names verbatim so the archive JSON
// is self-describing.
type bridgeSnapshot struct {
	FeishuDeliveryLedger      []map[string]any `json:"feishu_delivery_ledger"`
	BridgeSubscriptionCursors []map[string]any `json:"bridge_subscription_cursors"`
}

func snapshotBridgeRows(ctx context.Context, db *sql.DB) (*bridgeSnapshot, error) {
	out := &bridgeSnapshot{
		FeishuDeliveryLedger:      []map[string]any{},
		BridgeSubscriptionCursors: []map[string]any{},
	}
	if exists, err := tableExists(ctx, db, "feishu_delivery_ledger"); err != nil {
		return nil, err
	} else if exists {
		rows, err := dumpTable(ctx, db, "feishu_delivery_ledger")
		if err != nil {
			return nil, err
		}
		out.FeishuDeliveryLedger = rows
	}
	if exists, err := tableExists(ctx, db, "bridge_subscription_cursors"); err != nil {
		return nil, err
	} else if exists {
		rows, err := dumpTable(ctx, db, "bridge_subscription_cursors")
		if err != nil {
			return nil, err
		}
		out.BridgeSubscriptionCursors = rows
	}
	return out, nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func dumpTable(ctx context.Context, db *sql.DB, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, "SELECT * FROM "+table)
	if err != nil {
		return nil, fmt.Errorf("dump %s: %w", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			m[c] = v
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type bridgeArchive struct {
	ExportedAt          string          `json:"exported_at"`
	SqlitePath          string          `json:"sqlite_path"`
	SchemaVersionBefore int             `json:"schema_version_before"`
	Bridge              *bridgeSnapshot `json:"bridge"`
}

func writeBridgeArchive(dir, sqlitePath string, schemaVerBefore int, b *bridgeSnapshot) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(dir, "bridge-archive-"+ts+".json")
	body := bridgeArchive{
		ExportedAt:          time.Now().UTC().Format(time.RFC3339),
		SqlitePath:          sqlitePath,
		SchemaVersionBefore: schemaVerBefore,
		Bridge:              b,
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return "", err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(body); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// Compile-time guard: ensure we still depend on errors so any
// future refactor doesn't break imports silently.
var _ = errors.New
