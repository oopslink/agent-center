package cli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

const orphanConditionMarkerTable = `CREATE TABLE IF NOT EXISTS pm_orphan_condition_migration_markers (
	id             TEXT PRIMARY KEY,
	plan_id        TEXT NOT NULL,
	task_id        TEXT NOT NULL,
	action         TEXT NOT NULL,
	reason         TEXT NOT NULL,
	before_json    TEXT NOT NULL,
	after_json     TEXT NOT NULL,
	audit_id       TEXT NOT NULL,
	rollback_hint  TEXT NOT NULL,
	applied_at     TEXT NOT NULL,
	reversible     INTEGER NOT NULL DEFAULT 1,
	UNIQUE(plan_id, task_id, action)
)`

// MigrateOrphanConditionsCommand implements
// `agent-center migrate orphan-conditions`.
func MigrateOrphanConditionsCommand() *Command {
	return &Command{
		Name:    "orphan-conditions",
		Summary: "Classify and migrate historical orphan plan conditions (idempotent dry-run/apply)",
		Examples: []string{
			"agent-center migrate orphan-conditions --db=/tmp/agent-center-copy.db --dry-run",
			"agent-center migrate orphan-conditions --config=/etc/agent-center/config.yaml --apply",
		},
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "config file path")
			dbPath := fs.String("db", "", "sqlite DB path; overrides --config")
			dryRun := fs.Bool("dry-run", false, "report classifications without mutating the DB")
			apply := fs.Bool("apply", false, "apply idempotent resolve/supersede/incident actions")
			deadlineHours := fs.Int("deadline-hours", 24, "incident owner deadline in hours")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if len(args) != 0 {
					fmt.Fprintln(errw, "Error: unexpected positional arguments")
					return ExitUsage
				}
				if !*dryRun && !*apply {
					fmt.Fprintln(errw, "Error: must pass exactly one of --dry-run or --apply")
					return ExitUsage
				}
				if *dryRun && *apply {
					fmt.Fprintln(errw, "Error: --dry-run and --apply are mutually exclusive")
					return ExitUsage
				}
				path := strings.TrimSpace(*dbPath)
				if path == "" {
					cfg, err := loadConfigForCLI(*cfgPath, nil)
					if err != nil {
						emitConfigErrors(errw, err)
						return ExitUsage
					}
					path = cfg.Server.SqlitePath
				}
				db, err := persistence.Open(path)
				if err != nil {
					fmt.Fprintf(errw, "Error: db_open: %v\n", err)
					return ExitBusinessError
				}
				defer db.Close()
				res, err := runOrphanConditionMigration(ctx, db, orphanConditionOptions{
					Apply:         *apply,
					Now:           time.Now().UTC(),
					DeadlineAfter: time.Duration(*deadlineHours) * time.Hour,
				})
				if err != nil {
					fmt.Fprintf(errw, "Error: orphan_conditions: %v\n", err)
					return ExitBusinessError
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(res); err != nil {
					fmt.Fprintf(errw, "Error: encode_result: %v\n", err)
					return ExitBusinessError
				}
				return ExitOK
			}
		},
	}
}

type orphanConditionOptions struct {
	Apply         bool
	Now           time.Time
	DeadlineAfter time.Duration
}

type orphanConditionResult struct {
	Mode    string                      `json:"mode"`
	Summary orphanConditionSummary      `json:"summary"`
	Items   []orphanConditionClassified `json:"items"`
}

type orphanConditionSummary struct {
	Scanned        int `json:"scanned"`
	ShadowCurrent  int `json:"shadow_current"`
	Resolved       int `json:"resolved"`
	Superseded     int `json:"superseded"`
	Incidents      int `json:"incidents"`
	AlreadyApplied int `json:"already_applied"`
}

type orphanConditionClassified struct {
	PlanID       string                 `json:"plan_id"`
	TaskID       string                 `json:"task_id"`
	NodeID       string                 `json:"node_id,omitempty"`
	Action       string                 `json:"action"`
	Reason       string                 `json:"reason"`
	OwnerRef     string                 `json:"owner_ref,omitempty"`
	DeadlineAt   string                 `json:"deadline_at,omitempty"`
	AuditID      string                 `json:"audit_id,omitempty"`
	MarkerID     string                 `json:"marker_id,omitempty"`
	RollbackHint string                 `json:"rollback_hint,omitempty"`
	Before       map[string]any         `json:"before,omitempty"`
	After        map[string]any         `json:"after,omitempty"`
	Raw          orphanConditionScanRow `json:"-"`
}

type orphanConditionScanRow struct {
	PlanID          string
	ProjectID       string
	PlanStatus      string
	PlanCreatorRef  string
	PlanVersion     int
	TaskID          string
	NodeID          string
	WaitType        string
	WaitKeys        string
	TriggerCond     string
	WaitedSince     string
	TaskExists      bool
	TaskPlanID      string
	TaskStatus      string
	TaskNodeID      string
	TaskArchivedAt  string
	TaskAssignee    string
	GraphNodeExists bool
	MarkerID        string
}

func runOrphanConditionMigration(ctx context.Context, db *sql.DB, opt orphanConditionOptions) (orphanConditionResult, error) {
	if opt.Now.IsZero() {
		opt.Now = time.Now().UTC()
	}
	if opt.DeadlineAfter <= 0 {
		opt.DeadlineAfter = 24 * time.Hour
	}
	rows, err := scanOrphanConditionRows(ctx, db)
	if err != nil {
		return orphanConditionResult{}, err
	}
	res := orphanConditionResult{Mode: "dry-run"}
	if opt.Apply {
		res.Mode = "apply"
	}
	for _, row := range rows {
		item := classifyOrphanCondition(row, opt.Now.Add(opt.DeadlineAfter))
		res.Items = append(res.Items, item)
		res.Summary.Scanned++
		switch item.Action {
		case "shadow_current":
			res.Summary.ShadowCurrent++
		case "resolve":
			res.Summary.Resolved++
		case "supersede":
			res.Summary.Superseded++
		case "incident":
			res.Summary.Incidents++
		case "already_applied":
			res.Summary.AlreadyApplied++
		}
	}
	if !opt.Apply {
		return res, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, orphanConditionMarkerTable); err != nil {
		return res, err
	}
	for i := range res.Items {
		applied, err := applyOrphanCondition(ctx, tx, res.Items[i], opt.Now)
		if err != nil {
			return res, err
		}
		res.Items[i] = applied
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

func scanOrphanConditionRows(ctx context.Context, db *sql.DB) ([]orphanConditionScanRow, error) {
	markerJoin := ""
	markerIDSelect := "''"
	if tableExistsCLI(ctx, db, "pm_orphan_condition_migration_markers") {
		markerJoin = `LEFT JOIN pm_orphan_condition_migration_markers m
			ON m.plan_id = b.plan_id AND m.task_id = b.task_id`
		markerIDSelect = "COALESCE(m.id,'')"
	}
	q := `SELECT
		b.plan_id, p.project_id, p.status, p.creator_ref, p.version,
		b.task_id, b.node_id, b.wait_type, b.wait_keys, b.trigger_condition, b.waited_since,
		CASE WHEN t.id IS NULL THEN 0 ELSE 1 END,
		COALESCE(t.plan_id,''), COALESCE(t.status,''), COALESCE(t.node_id,''), COALESCE(t.archived_at,''), COALESCE(t.assignee,''),
		CASE WHEN b.node_id != '' AND EXISTS (
			SELECT 1 FROM pm_graph_nodes n
			JOIN pm_graphs g ON g.id = n.graph_id
			WHERE n.id = b.node_id AND (g.plan_id = b.plan_id OR g.id = p.graph_id)
		) THEN 1 ELSE 0 END,
		` + markerIDSelect + `
	FROM pm_plan_blocked_on b
	JOIN pm_plans p ON p.id = b.plan_id
	LEFT JOIN pm_tasks t ON t.id = b.task_id
	` + markerJoin + `
	WHERE p.status = 'running'
	ORDER BY b.plan_id, b.task_id`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orphanConditionScanRow
	for rows.Next() {
		var r orphanConditionScanRow
		var taskExists, graphNodeExists int
		if err := rows.Scan(&r.PlanID, &r.ProjectID, &r.PlanStatus, &r.PlanCreatorRef, &r.PlanVersion,
			&r.TaskID, &r.NodeID, &r.WaitType, &r.WaitKeys, &r.TriggerCond, &r.WaitedSince,
			&taskExists, &r.TaskPlanID, &r.TaskStatus, &r.TaskNodeID, &r.TaskArchivedAt, &r.TaskAssignee,
			&graphNodeExists, &r.MarkerID); err != nil {
			return nil, err
		}
		r.TaskExists = taskExists != 0
		r.GraphNodeExists = graphNodeExists != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func classifyOrphanCondition(r orphanConditionScanRow, deadline time.Time) orphanConditionClassified {
	item := orphanConditionClassified{
		PlanID: r.PlanID, TaskID: r.TaskID, NodeID: r.NodeID, Raw: r,
		OwnerRef: r.PlanCreatorRef, DeadlineAt: deadline.Format(time.RFC3339Nano),
		Before: orphanConditionBefore(r),
	}
	item.MarkerID = deterministicID("ocm", r.PlanID, r.TaskID, r.NodeID, r.WaitType)
	item.AuditID = deterministicID("oca", r.PlanID, r.TaskID, r.NodeID, r.WaitType)
	if strings.TrimSpace(r.MarkerID) != "" {
		item.Action = "already_applied"
		item.Reason = "migration marker already exists"
		return item
	}
	if !r.TaskExists {
		item.Action = "incident"
		item.Reason = "blocked_on references a missing task; ownership cannot be proven"
		return item
	}
	if !validHistoricalWaitType(r.WaitType) {
		item.Action = "incident"
		item.Reason = "blocked_on has an unknown wait_type"
		return item
	}
	if r.TaskStatus == "completed" || r.TaskStatus == "discarded" || r.TaskArchivedAt != "" {
		item.Action = "resolve"
		item.Reason = "blocked condition outlived a terminal or archived task"
		return item
	}
	if r.TaskPlanID != "" && r.TaskPlanID != r.PlanID {
		item.Action = "supersede"
		item.Reason = "task now belongs to a different plan"
		return item
	}
	if r.NodeID != "" && r.TaskNodeID != "" && r.TaskNodeID != r.NodeID {
		item.Action = "supersede"
		item.Reason = "task is bound to a different current node"
		return item
	}
	if r.NodeID != "" && !r.GraphNodeExists {
		item.Action = "incident"
		item.Reason = "blocked_on node is not present in the plan graph"
		return item
	}
	item.Action = "shadow_current"
	item.Reason = "blocked_on is still owned by the running plan"
	return item
}

func applyOrphanCondition(ctx context.Context, tx *sql.Tx, item orphanConditionClassified, now time.Time) (orphanConditionClassified, error) {
	switch item.Action {
	case "shadow_current", "already_applied":
		return item, nil
	case "resolve", "supersede":
		if _, err := tx.ExecContext(ctx, `DELETE FROM pm_plan_blocked_on WHERE plan_id=? AND task_id=?`, item.PlanID, item.TaskID); err != nil {
			return item, err
		}
		item.After = map[string]any{"blocked_on_present": false}
		item.RollbackHint = "restore pm_plan_blocked_on from before_json in pm_orphan_condition_migration_markers"
	case "incident":
		item.After = map[string]any{"incident_status": "open", "blocked_on_present": true}
		item.RollbackHint = "delete pm_progress_incidents row with episode_key=" + item.MarkerID
		if err := insertOrphanIncident(ctx, tx, item, now); err != nil {
			return item, err
		}
	default:
		return item, nil
	}
	if err := appendOrphanAudit(ctx, tx, item, now); err != nil {
		return item, err
	}
	if err := insertOrphanMarker(ctx, tx, item, now); err != nil {
		return item, err
	}
	return item, nil
}

func insertOrphanIncident(ctx context.Context, tx *sql.Tx, item orphanConditionClassified, now time.Time) error {
	refs, _ := json.Marshal([]string{
		"orphan_condition:" + item.PlanID + "/" + item.TaskID,
		"reason:" + item.Reason,
		"rollback:" + item.RollbackHint,
	})
	deadline := item.DeadlineAt
	return execInsertIgnore(ctx, tx, `INSERT INTO pm_progress_incidents
		(id, plan_id, task_id, node_id, kind, owner_ref, owner_display, deadline_at,
		 ack_required, acked_at, escalate_to_ref, escalation_deadline_at,
		 source_fact_refs_json, episode_key, status, created_at, updated_at, version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.MarkerID, item.PlanID, item.TaskID, item.NodeID, "migration_gap", item.OwnerRef, item.OwnerRef,
		deadline, 1, "", item.OwnerRef, deadline, string(refs), item.MarkerID, "open", tsCLI(now), tsCLI(now), 1)
}

func appendOrphanAudit(ctx context.Context, tx *sql.Tx, item orphanConditionClassified, now time.Time) error {
	detail, _ := json.Marshal(map[string]any{
		"migration":     "orphan_conditions",
		"reason":        item.Reason,
		"before":        item.Before,
		"after":         item.After,
		"rollback_hint": item.RollbackHint,
		"reversible":    true,
	})
	return execInsertIgnore(ctx, tx, `INSERT INTO pm_audit_log
		(id, project_id, object_type, object_id, change_type, field, from_value, to_value, actor_ref, detail, occurred_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		item.AuditID, item.Raw.ProjectID, "plan", item.PlanID, "migration", "orphan_condition",
		"present", item.Action, "system:orphan-condition-migration", string(detail), tsCLI(now))
}

func insertOrphanMarker(ctx context.Context, tx *sql.Tx, item orphanConditionClassified, now time.Time) error {
	before, _ := json.Marshal(item.Before)
	after, _ := json.Marshal(item.After)
	return execInsertIgnore(ctx, tx, `INSERT INTO pm_orphan_condition_migration_markers
		(id, plan_id, task_id, action, reason, before_json, after_json, audit_id, rollback_hint, applied_at, reversible)
		VALUES (?,?,?,?,?,?,?,?,?,?,1)`,
		item.MarkerID, item.PlanID, item.TaskID, item.Action, item.Reason, string(before), string(after),
		item.AuditID, item.RollbackHint, tsCLI(now))
}

func execInsertIgnore(ctx context.Context, tx *sql.Tx, q string, args ...any) error {
	_, err := tx.ExecContext(ctx, q, args...)
	if err == nil || isSQLiteUniqueErr(err) {
		return nil
	}
	return err
}

func orphanConditionBefore(r orphanConditionScanRow) map[string]any {
	return map[string]any{
		"plan_id":           r.PlanID,
		"project_id":        r.ProjectID,
		"plan_status":       r.PlanStatus,
		"plan_version":      r.PlanVersion,
		"task_id":           r.TaskID,
		"node_id":           r.NodeID,
		"wait_type":         r.WaitType,
		"wait_keys":         r.WaitKeys,
		"trigger_condition": r.TriggerCond,
		"waited_since":      r.WaitedSince,
		"task_exists":       r.TaskExists,
		"task_plan_id":      r.TaskPlanID,
		"task_status":       r.TaskStatus,
		"task_node_id":      r.TaskNodeID,
		"task_archived_at":  r.TaskArchivedAt,
		"task_assignee":     r.TaskAssignee,
		"graph_node_exists": r.GraphNodeExists,
	}
}

func validHistoricalWaitType(v string) bool {
	switch v {
	case "upstream_completion", "acceptance_verdict", "stage_barrier", "human_decision", "external_event", "executor_liveness", "timeout_only":
		return true
	default:
		return false
	}
}

func deterministicID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "-" + hex.EncodeToString(h[:])[:32]
}

func tableExistsCLI(ctx context.Context, db *sql.DB, name string) bool {
	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	return n > 0
}

func tsCLI(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func isSQLiteUniqueErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
