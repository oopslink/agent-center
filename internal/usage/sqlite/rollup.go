package sqlite

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

// Rollup is the incremental daily-aggregation job (v2.15.0 I28/F3). It folds the
// raw token/cost telemetry (usage_events) and five activity sources into the
// agent_activity_daily read model, keyed by (agent_ref, day, project_id).
//
// INCREMENTAL STRATEGY — dirty-bucket recompute. Each source carries a per-source
// watermark in agent_activity_rollup_cursor (the largest monotonic key already
// folded in). A run:
//  1. scans every source for rows beyond its cursor, collecting the distinct
//     (agent_ref, day) buckets they touch (agent-kind rows only, ref LIKE 'agent:%'),
//  2. for each touched bucket, recomputes ALL measures from scratch across all six
//     sources grouped by project_id, then DELETE+INSERT the (agent_ref, day) slice
//     (idempotent — a re-run of the same input yields the same rows),
//  3. advances each source's cursor to the max key it saw.
//
// Recompute (not delta-apply) is deliberate: it is trivially correct under
// at-least-once reprocessing, out-of-order arrival, and a back-dated usage_events
// ts (the transcript-reconcile path writes a past ts but a fresh, larger ULID id —
// so the id cursor still catches it, and the affected day is fully recomputed).
//
// CURSOR KEYS differ by source (see migration 0078): usage_events / messages /
// task_action_logs use the ULID `id`; events(commit) uses the monotonic `seq`;
// pm_issues / pm_plans (non-monotonic ids) use the composite `created_at||'#'||id`.
//
// DAY is the UTC calendar date substr(<ts>,1,10), relying on the repo-wide
// UTC-normalized RFC3339 timestamp convention (same basis as the PM repos'
// created_at range filters). PROJECT_ID derivation per source is documented at
// each query below; an unresolvable project falls back to the "" bucket.
//
// AGENT-KIND ONLY. Human (user:) / system rows are excluded — the dashboard is
// per-agent. The filter is ref LIKE 'agent:%'; the ref is stored verbatim.
type Rollup struct {
	db *sql.DB
}

// NewRollup constructs the rollup job over db.
func NewRollup(db *sql.DB) *Rollup { return &Rollup{db: db} }

// RollupStats summarizes one RunIncremental pass (for logging / tests).
type RollupStats struct {
	BucketsRecomputed int // distinct (agent_ref, day) slices rebuilt
	RowsWritten       int // agent_activity_daily rows upserted
}

// source names (agent_activity_rollup_cursor.source keys).
const (
	srcUsage    = "usage_events"
	srcActions  = "task_action_logs"
	srcMessages = "messages"
	srcIssues   = "issues"
	srcPlans    = "plans"
	srcCommits  = "commits"
)

// bucket is a dirty (agent_ref, day) slice to recompute.
type bucket struct {
	agentRef string
	day      string
}

// scanQuery returns, for a source, the SQL that selects (cursor_key, agent_ref,
// day) for rows beyond the given cursor. The key column is source-specific (see
// type doc). All filter to agent-kind rows.
var scanQueries = map[string]string{
	srcUsage: `SELECT id, agent_ref, substr(ts,1,10)
	             FROM usage_events
	            WHERE id > ? AND agent_ref LIKE 'agent:%' ORDER BY id`,
	srcActions: `SELECT id, agent_ref, substr(occurred_at,1,10)
	               FROM pm_task_action_logs
	              WHERE id > ? AND agent_ref LIKE 'agent:%' ORDER BY id`,
	srcMessages: `SELECT id, sender_identity_id, substr(posted_at,1,10)
	                FROM messages
	               WHERE id > ? AND sender_identity_id LIKE 'agent:%' ORDER BY id`,
	srcIssues: `SELECT created_at||'#'||id, created_by, substr(created_at,1,10)
	              FROM pm_issues
	             WHERE created_at||'#'||id > ? AND created_by LIKE 'agent:%' ORDER BY 1`,
	srcPlans: `SELECT created_at||'#'||id, creator_ref, substr(created_at,1,10)
	             FROM pm_plans
	            WHERE created_at||'#'||id > ? AND creator_ref LIKE 'agent:%' ORDER BY 1`,
	// events(commit): seq is an INTEGER monotonic key; the cursor is stored as its
	// decimal text (CAST) and bound back as an int so the comparison stays numeric.
	srcCommits: `SELECT CAST(seq AS TEXT), actor, substr(occurred_at,1,10)
	               FROM events
	              WHERE seq > ? AND event_type = 'commit' AND actor LIKE 'agent:%' ORDER BY seq`,
}

// RunIncremental folds all new source rows into agent_activity_daily and advances
// the cursors, atomically in one tx. Safe to call repeatedly (a no-new-rows run is
// a cheap no-op).
func (r *Rollup) RunIncremental(ctx context.Context) (RollupStats, error) {
	var stats RollupStats
	err := persistence.RunInTx(ctx, r.db, func(ctx context.Context) error {
		exec, _ := persistence.ExecutorFromCtx(ctx, r.db)

		cursors, err := r.loadCursors(ctx, exec)
		if err != nil {
			return err
		}

		dirty := map[bucket]struct{}{}
		newCursors := map[string]string{}

		for _, src := range []string{srcUsage, srcActions, srcMessages, srcIssues, srcPlans, srcCommits} {
			maxKey, err := r.scanDirty(ctx, exec, src, cursors[src], dirty)
			if err != nil {
				return err
			}
			if maxKey != "" {
				newCursors[src] = maxKey
			}
		}

		for b := range dirty {
			n, err := r.recomputeBucket(ctx, exec, b.agentRef, b.day)
			if err != nil {
				return err
			}
			stats.BucketsRecomputed++
			stats.RowsWritten += n
		}

		for src, key := range newCursors {
			if err := r.saveCursor(ctx, exec, src, key); err != nil {
				return err
			}
		}
		return nil
	})
	return stats, err
}

// Run drives an immediate RunIncremental pass, then one every interval until ctx is
// done — the production wiring for the F3 rollup so the dashboard's rollup-backed
// reads (overview cards / heatmap / project trend) stay fresh. A zero/negative
// interval defaults to 2m; logf may be nil. (T471: the job existed but was never
// scheduled, so agent_activity_daily stayed empty and the dashboard read 0.)
func (r *Rollup) Run(ctx context.Context, interval time.Duration, logf func(string, ...any)) {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	pass := func() {
		stats, err := r.RunIncremental(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logf("incremental pass failed: %v", err)
			}
			return
		}
		if stats.BucketsRecomputed > 0 {
			logf("folded %d bucket(s) → %d daily row(s)", stats.BucketsRecomputed, stats.RowsWritten)
		}
	}
	pass()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pass()
		}
	}
}

// scanDirty runs a source's scan query, adds each row's (agent_ref, day) to dirty,
// and returns the max cursor key observed ("" when no new rows).
func (r *Rollup) scanDirty(ctx context.Context, exec persistence.SQLExecutor, src, cursor string, dirty map[bucket]struct{}) (string, error) {
	var arg any = cursor
	if src == srcCommits {
		arg = cursorInt(cursor) // numeric comparison on events.seq
	}
	rows, err := exec.QueryContext(ctx, scanQueries[src], arg)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	// Every scan query ORDERs BY its key ascending, so the LAST row carries the
	// true max — keep last-wins rather than a lexical compare (which would mis-rank
	// the commits decimal seq, e.g. "9" vs "10").
	maxKey := ""
	for rows.Next() {
		var key, agentRef, day string
		if err := rows.Scan(&key, &agentRef, &day); err != nil {
			return "", err
		}
		dirty[bucket{agentRef: agentRef, day: day}] = struct{}{}
		maxKey = key
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return maxKey, nil
}

// measures accumulates the five rollup measures for one project bucket.
type measures struct {
	events    int64
	tokensIn  int64
	tokensOut int64
	cache     int64
	cost      int64
}

// recomputeBucket rebuilds every agent_activity_daily row for (agentRef, day) from
// the raw sources and returns the number of rows written. It DELETEs the slice
// first so a recompute never leaves a stale project row behind.
func (r *Rollup) recomputeBucket(ctx context.Context, exec persistence.SQLExecutor, agentRef, day string) (int, error) {
	byProject := map[string]*measures{}
	m := func(pid string) *measures {
		v := byProject[pid]
		if v == nil {
			v = &measures{}
			byProject[pid] = v
		}
		return v
	}

	// --- activity counts (events_count) ---
	// task_action_logs: project via task_id → pm_tasks.project_id; an orphaned log
	// (task deleted) falls back to the "" bucket (LEFT JOIN + COALESCE).
	if err := r.addCounts(ctx, exec,
		`SELECT COALESCE(t.project_id,''), COUNT(*)
		   FROM pm_task_action_logs l LEFT JOIN pm_tasks t ON l.task_id = t.id
		  WHERE l.agent_ref = ? AND substr(l.occurred_at,1,10) = ?
		  GROUP BY 1`, agentRef, day, m); err != nil {
		return 0, err
	}
	// pm_issues: project_id is a direct column; creation counts as one activity.
	if err := r.addCounts(ctx, exec,
		`SELECT project_id, COUNT(*) FROM pm_issues
		  WHERE created_by = ? AND substr(created_at,1,10) = ? GROUP BY project_id`,
		agentRef, day, m); err != nil {
		return 0, err
	}
	// pm_plans: project_id direct column; creation counts as one activity.
	if err := r.addCounts(ctx, exec,
		`SELECT project_id, COUNT(*) FROM pm_plans
		  WHERE creator_ref = ? AND substr(created_at,1,10) = ? GROUP BY project_id`,
		agentRef, day, m); err != nil {
		return 0, err
	}
	// commits: events.event_type='commit'; project from refs JSON (else "").
	if err := r.addCounts(ctx, exec,
		`SELECT COALESCE(json_extract(refs,'$.project_id'),''), COUNT(*)
		   FROM events
		  WHERE event_type = 'commit' AND actor = ? AND substr(occurred_at,1,10) = ?
		  GROUP BY 1`, agentRef, day, m); err != nil {
		return 0, err
	}
	// messages: project derived from the conversation owner_ref —
	// pm://projects/{id} directly, or pm://{tasks,issues,plans}/{id} resolved to
	// that object's project; a DM / channel (no project owner) → "" bucket.
	if err := r.addCounts(ctx, exec,
		`SELECT CASE
		          WHEN c.owner_ref LIKE 'pm://projects/%' THEN substr(c.owner_ref, 15)
		          WHEN c.owner_ref LIKE 'pm://tasks/%'    THEN COALESCE((SELECT project_id FROM pm_tasks  WHERE id = substr(c.owner_ref, 12)), '')
		          WHEN c.owner_ref LIKE 'pm://issues/%'   THEN COALESCE((SELECT project_id FROM pm_issues WHERE id = substr(c.owner_ref, 13)), '')
		          WHEN c.owner_ref LIKE 'pm://plans/%'    THEN COALESCE((SELECT project_id FROM pm_plans  WHERE id = substr(c.owner_ref, 12)), '')
		          ELSE ''
		        END AS pid, COUNT(*)
		   FROM messages m LEFT JOIN conversations c ON m.conversation_id = c.id
		  WHERE m.sender_identity_id = ? AND substr(m.posted_at,1,10) = ?
		  GROUP BY pid`, agentRef, day, m); err != nil {
		return 0, err
	}

	// --- token/cost totals (usage_events) ---
	urows, err := exec.QueryContext(ctx,
		`SELECT project_id,
		        COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(cache_read_tokens + cache_write_tokens),0), COALESCE(SUM(cost_micros),0)
		   FROM usage_events
		  WHERE agent_ref = ? AND substr(ts,1,10) = ? GROUP BY project_id`, agentRef, day)
	if err != nil {
		return 0, err
	}
	func() {
		defer urows.Close()
		for urows.Next() {
			var pid string
			var in, out, cache, cost int64
			if err = urows.Scan(&pid, &in, &out, &cache, &cost); err != nil {
				return
			}
			v := m(pid)
			v.tokensIn += in
			v.tokensOut += out
			v.cache += cache
			v.cost += cost
		}
		err = urows.Err()
	}()
	if err != nil {
		return 0, err
	}

	// Rewrite the (agent_ref, day) slice.
	if _, err := exec.ExecContext(ctx,
		`DELETE FROM agent_activity_daily WHERE agent_ref = ? AND day = ?`, agentRef, day); err != nil {
		return 0, err
	}
	written := 0
	for pid, v := range byProject {
		if _, err := exec.ExecContext(ctx,
			`INSERT INTO agent_activity_daily
			   (agent_ref, day, project_id, events_count, tokens_in, tokens_out, cache_tokens, cost_micros)
			 VALUES (?,?,?,?,?,?,?,?)`,
			agentRef, day, pid, v.events, v.tokensIn, v.tokensOut, v.cache, v.cost); err != nil {
			return 0, err
		}
		written++
	}
	return written, nil
}

// addCounts runs a `SELECT project_id, COUNT(*)` query and adds each count to the
// matching project bucket's events tally.
func (r *Rollup) addCounts(ctx context.Context, exec persistence.SQLExecutor, q, agentRef, day string, m func(string) *measures) error {
	rows, err := exec.QueryContext(ctx, q, agentRef, day)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		var n int64
		if err := rows.Scan(&pid, &n); err != nil {
			return err
		}
		m(pid).events += n
	}
	return rows.Err()
}

func (r *Rollup) loadCursors(ctx context.Context, exec persistence.SQLExecutor) (map[string]string, error) {
	rows, err := exec.QueryContext(ctx, `SELECT source, cursor_key FROM agent_activity_rollup_cursor`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var src, key string
		if err := rows.Scan(&src, &key); err != nil {
			return nil, err
		}
		out[src] = key
	}
	return out, rows.Err()
}

func (r *Rollup) saveCursor(ctx context.Context, exec persistence.SQLExecutor, src, key string) error {
	_, err := exec.ExecContext(ctx,
		`INSERT INTO agent_activity_rollup_cursor (source, cursor_key, updated_at)
		 VALUES (?,?,?)
		 ON CONFLICT(source) DO UPDATE SET cursor_key = excluded.cursor_key, updated_at = excluded.updated_at`,
		src, key, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// cursorInt parses a commits cursor (decimal seq text) to an int; empty/garbage
// means "from the beginning" (0). events.seq is positive, so 0 includes all rows.
func cursorInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
