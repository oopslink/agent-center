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
//   - --apply runs them: bridge tables → JSON archive → drop via 0025 → Up to
//     targetSchemaVersion (currently 28 — v2.0 GA was 25, v2.1-C added 0026,
//     v2.1-E added 0027, v2.3-3a added 0028 admin_tokens)
//   - Idempotent: if already at v2 (currentVer >= targetSchemaVersion), exits 0
//     with "already at v2"
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

// targetSchemaVersion tracks the highest applied migration version
// for the currently-shipped v2.x line. v2.0 GA was 25; v2.1 added
// 0026 (user_conversation_read_state); v2.1-E added 0027;
// v2.3-3a added 0028 (admin_tokens); v2.4-D-A3 added 0029 (enroll
// token columns); v2.4-D-X1 added 0030 (workers.name); v2.5-B2
// added 0031 (admin_tokens.worker_id + plaintext ciphertext columns
// for the show-install-command flow); v2.5.5 (task #59) added 0032
// (projects drop+recreate — server-gen id, tags replace kind);
// v2.7-A0 (task #95) added 0037-0040 (Conversation owner_ref + kind
// convergence, Message context_refs + attachments, files seam
// blob_metadata/file_references, outbox_events/outbox_applied); v2.7-B1
// (task #96) added 0041 (ProjectManager pm_* tables); v2.7-C1 (task #99)
// added 0042 (agents); v2.7-C2 (task #100) added 0043 (agent_work_items,
// agent_activity_events); v2.7-D1 (task #102) added 0044 (env_workers,
// worker_control_events); v2.7-D3-a added 0045 (file_transfer_sessions);
// v2.7-#107 added 0046 (agent_work_item_projections); v2.7-#195 added 0047
// (channel name org-scoped unique); v2.7.1-#214 added 0048 (identity email +
// last_session_at); v2.7.1-#49 added 0049 (pm org sequence); v2.8-#268 added
// 0050 (user_conversation_follow_state); v2.8.1-#278 added 0051 (agent_work_items
// single-active UNIQUE index); v2.8.1 state-model-fix added 0052 (Task assigned→
// open + Task/Issue canceled/withdrawn→discarded); v2.8.1 Edit-Task added 0053
// (Task/Issue tags + status_changed_at columns); v2.9-#283 added 0054
// (plan orchestration — pm_plans + pm_task_dependencies + tasks.plan_id);
// v2.9-#285 added 0055 (pm_plan_dispatch_records — the only orchestrator-owned
// stored state, §9.3); v2.9 P3 Stage B added 0056 (pm_tasks.archived_at/by — the
// orthogonal Task archived state for Plan delete + archive). Update this constant
// when any future migration lands so `migrate v1-to-v2` always carries the install
// to the latest schema instead of leaving it mid-version.
// v2.9.1 Thread P1 added 0057 (messages.parent_message_id/root_message_id —
// depth-1 thread refs); v2.9.1 ADR-0046 added 0058 (task state machine 7→5:
// data-only blocked→running keep reason, verified→completed); v2.9.1 ADR-0047
// added 0059 (pm_plans.is_builtin + per-project built-in claimable pool, with the
// assigned-backlog-task backfill); v2.10.1 T99 added 0060 (pm_plans.org_sequence —
// P<number> plan refs); v2.10 ADR-0053 added 0061 (pm_plan_findings — the DeLM
// plan-scoped shared-findings table); v2.11.0 Cognition added 0062 (reminders +
// reminder_firings — the Reminder aggregate); v2.11.0 F-B added 0063
// (reminders.deliver_as_creator — per-reminder delivery identity flag); I7-D1
// (T216) added 0064 (center_settings — the wake-guardrail thresholds KV store);
// T236 added 0065 (agent_llm_config — agents.reasoning/mode/provider). NOTE: T236
// originally landed this as a SECOND 0064, colliding with center_settings — the
// migrator keys by version into a map, so the alphabetically-later file silently
// overwrote it and the ADD COLUMNs never ran on a fresh DB. Renumbered to 0065.
// T288 added 0066 (dm_dedup — conversations.dm_key + the partial unique index that
// makes one human↔agent DM unique, plus the duplicate-DM merge).
// v2.13.0 I18/F2 added 0067 (pm_tasks.branch/base/skip_merge_check — cycle-node
// git metadata for scaffold_cycle_plan + the F3 merge-check guard); v2.13.0 I18/F3
// added 0068 (pm_tasks.role — the persisted cycle-node role discriminator the F3
// merge guard + F4 unmerged-branch board key on). v2.13.0 I18/B1 added 0069
// (control-flow engine — pm_task_dependencies.kind/when/max_rounds + decision-outcomes
// + loop-rounds tables, all additive so existing DAG plans are unchanged).
// NOTE: dev/v2.13.0 authored these as 0066/0067/0068; renumbered to 0067/0068/0069
// at ship-merge to land after main's 0066 (dm_dedup) which arrived on main meanwhile.
// v2.14.0 AWI: 0070 (F2 schema-add: pm_tasks block/lease cols + task_action_logs),
// 0071 (F4 data-backfill AWI→pm_tasks), 0072 (F3 single-active UNIQUE index),
// 0074 (I14 naming cleanup: rename agent_activity_events.work_item_ref→task_ref +
// idx_aae_work_item→idx_aae_task) — all additive; const tracks the latest migration.
// T340 added 0075 (worker_control_events GC index — additive, retention-based stream pruning).
// T339 added 0076 (data backfill: archived + non-terminal tasks → discarded, closing the
// open+archived leak — see migration header; idempotent, archived-only WHERE guard).
// I28/F1 added 0077 (usage collection: model_prices + usage_events — additive, new tables).
// I28/F3 added 0078 (agent_activity_daily rollup + rollup cursor — additive, new tables).
// T461 added 0079 (agents.capability_tags — additive ADD COLUMN, dispatch labels).
// T468 added 0080 (pm_plan_review_verdicts — additive new table, B3 structured verdict).
// I41/T470 added 0081 (organizations.disabled_at — additive ADD COLUMN, reversible org disable gate).
// T515/F3 added 0082 (agents model-routing config: orchestrator_model / default_executor_model /
// max_concurrent_tasks / allowed_models — additive ADD COLUMN) and 0083 (pm_tasks.model hard-override
// — additive ADD COLUMN).
// incident-2026-06-30 added 0089 (idx_aae_occurred_at — additive index supporting the new
// agent_activity_events retention GC; 0084–0088 are the intervening v2.18.x migrations).
// T728 added 0090 (agents.include_description_in_system_prompt — additive ADD COLUMN, v2.27.0).
// Task-6 added 0091 (pm_graphs, pm_graph_nodes, pm_graph_edges — orchestration engine DAG tables).
// P2-T3 added 0092 (task.node_id, plan.graph_id — orchestration FK wiring).
// P2-T5 added 0093 (drop task cycle fields: branch/base/skip_merge_check/role).
// Template added 0094 (pm_templates — workflow template management).
// v2.28 added 0095 (worker_system_info — worker host/build identity fields).
// T-lifecycle-time added 0096 (agents.last_lifecycle_transition_at — additive ADD COLUMN).
// T862 added 0097 (pm_tasks.recovery_reset_count — additive ADD COLUMN NOT NULL DEFAULT 0,
// the durable tier-3 recovery reset circuit-breaker tally for reset_task).
// 2026-07 template quick-fix added 0098 (remove builtin cycle template row).
// 2026-07 chat 引用 added 0099 (messages.quoted_message_id — additive ADD COLUMN,
// the soft quote/reply pointer for the chat "quote a message" feature).
// change-log/audit added 0100 (pm_audit_log — the object-level semantic change ledger;
// renumbered from 0099 at ship-merge to land after main's 0099 message-quote).
// skill observability (issue-4a45e9cc) added 0101 (agent_installed_skills — the OBSERVED
// per-agent effective skill projection) + 0102 (drop the declared agents.skills column).
// renumbered from 0099 at ship-merge to land after main's 0099 message-quote);
// reminder-event added 0103 (reminders.on_event_* — event-driven reminder trigger).
// model-catalog added 0104 (model_catalog — org-managed model pricing/tier catalog) +
// 0105 (agents.judge_enabled — the per-agent difficulty-judge opt-in switch).
// Plan Stage model (2026-07-03 design) added 0106 (pm_stages — the lightweight
// first-class Stage aggregate + pm_tasks.stage_id node→stage membership).
// Team Phase-1 (S1-3) added 0107 (team/pm_teams tables — renumbered from 0106_v229_teams
// at T1002 integrate to land after main's 0106_pm_stages).
// I105 Phase 1 added 0110 (pm_tasks.dispatch_mode — the per-NODE fork override: ” =
// executor_fork = the pre-I105 routing, 'supervisor_inline' = route to the supervisor
// instead of forking a center-action node into an empty workspace).
// I107/ADR-0054 added 0111 (partial indexes over the parked task states — renumbered
// from 0110 at rebase to land after main's 0110_i105_task_dispatch_mode).
// Per-agent executor workspace isolation added 0112 (agents.executor_git_worktree).
// 2026-07-22 quick-fix added 0114 (collapse task status delivered back to completed
// and rebuild active-task indexes without delivered).
const targetSchemaVersion = 114

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
