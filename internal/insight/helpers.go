package insight

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

type dims struct {
	OrgID, ProjectID, ProjectName, TaskTitle, AgentName string
}

type queueDim struct {
	CommandID, OrgID, ProjectID, WorkerID, QueuedAt, StartedAt, CommandStatus, StatusReason, StatusMessage string
}

type sourceCursor struct {
	At time.Time
	ID string
	OK bool
}

func encodeSourceCursor(at time.Time, id string) string {
	if id == "" || at.IsZero() {
		return ""
	}
	return fmtTS(at) + "\t" + id
}

func parseSourceCursor(raw string) sourceCursor {
	parts := strings.SplitN(raw, "\t", 2)
	if len(parts) != 2 || parts[1] == "" {
		return sourceCursor{}
	}
	at, ok := parseTS(parts[0])
	if !ok {
		return sourceCursor{}
	}
	return sourceCursor{At: at, ID: parts[1], OK: true}
}

func (s *Service) checkpointCursor(ctx context.Context, kind string) sourceCursor {
	var raw string
	err := s.duck.QueryRowContext(ctx, `SELECT source_cursor FROM projector_checkpoint WHERE source_kind=?`, kind).Scan(&raw)
	if err == nil {
		if c := parseSourceCursor(raw); c.OK {
			return c
		}
	}

	var occurred, cursor string
	err = s.duck.QueryRowContext(ctx, `SELECT CAST(occurred_at AS VARCHAR), COALESCE(source_cursor,'')
		FROM projected_event WHERE source_kind=? ORDER BY occurred_at DESC, source_cursor DESC LIMIT 1`, kind).Scan(&occurred, &cursor)
	if err != nil {
		return sourceCursor{}
	}
	at, ok := parseTS(occurred)
	if !ok || cursor == "" {
		return sourceCursor{}
	}
	if c := parseSourceCursor(cursor); c.OK {
		return c
	}
	return sourceCursor{At: at, ID: cursor, OK: true}
}

func (s *Service) touchCheckpoint(ctx context.Context, kind string, cursor sourceCursor) error {
	now := time.Now().UTC()
	raw := ""
	if cursor.OK {
		raw = encodeSourceCursor(cursor.At, cursor.ID)
	}
	_, err := s.duck.ExecContext(ctx, `INSERT INTO projector_checkpoint (source_kind, source_cursor, refreshed_at, state, last_error)
		VALUES (?,?,?,?,NULL)
		ON CONFLICT (source_kind) DO UPDATE SET source_cursor=?, refreshed_at=?, state='fresh', last_error=NULL`,
		kind, raw, fmtTS(now), "fresh", raw, fmtTS(now))
	return err
}

func (s *Service) dimensions(ctx context.Context, taskID, agentID string) (dims, error) {
	var d dims
	if taskID != "" {
		_ = s.sqlite.QueryRowContext(ctx, `SELECT t.title, p.id, p.organization_id, p.name
			FROM pm_tasks t JOIN pm_projects p ON p.id=t.project_id WHERE t.id=?`, taskID).
			Scan(&d.TaskTitle, &d.ProjectID, &d.OrgID, &d.ProjectName)
	}
	if agentID != "" {
		var org, name string
		if err := s.sqlite.QueryRowContext(ctx, `SELECT organization_id, name FROM agents WHERE id=?`, agentID).Scan(&org, &name); err == nil {
			d.AgentName = name
			if d.OrgID == "" {
				d.OrgID = org
			}
		}
	}
	return d, nil
}

func (s *Service) queueByExecution(ctx context.Context, tx *sql.Tx, execID string) queueDim {
	var q queueDim
	if execID == "" {
		return q
	}
	_ = tx.QueryRowContext(ctx, `SELECT command_id, COALESCE(organization_id,''), COALESCE(project_id,''), worker_id, CAST(queued_at AS VARCHAR), COALESCE(CAST(started_at AS VARCHAR),''),
		COALESCE(command_status,''), COALESCE(status_reason,''), COALESCE(status_message,'')
		FROM queue_interval_fact WHERE execution_id=? LIMIT 1`, execID).
		Scan(&q.CommandID, &q.OrgID, &q.ProjectID, &q.WorkerID, &q.QueuedAt, &q.StartedAt, &q.CommandStatus, &q.StatusReason, &q.StatusMessage)
	return q
}

func projected(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM projected_event WHERE source_event_id=?`, id).Scan(&one)
	if errorsIsNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

func markProjected(ctx context.Context, tx *sql.Tx, id, kind, cursor string, occurred time.Time) error {
	now := time.Now().UTC()
	checkpoint := encodeSourceCursor(occurred, cursor)
	if _, err := tx.ExecContext(ctx, `INSERT INTO projected_event (source_event_id, source_kind, source_cursor, occurred_at, projected_at)
		VALUES (?,?,?,?,?)`, id, kind, cursor, fmtTS(occurred), fmtTS(now)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO projector_checkpoint (source_kind, source_cursor, refreshed_at, state, last_error)
		VALUES (?,?,?,?,NULL)
		ON CONFLICT (source_kind) DO UPDATE SET source_cursor=?, refreshed_at=?, state='fresh', last_error=NULL`,
		kind, checkpoint, fmtTS(now), "fresh", checkpoint, fmtTS(now))
	return err
}

func (s *Service) markError(ctx context.Context, kind string, err error) error {
	_, e := s.duck.ExecContext(ctx, `INSERT INTO projector_checkpoint (source_kind, source_cursor, refreshed_at, state, last_error)
		VALUES (?,'',?,'unavailable',?)
		ON CONFLICT (source_kind) DO UPDATE SET state='unavailable', last_error=?`,
		kind, fmtTS(time.Now().UTC()), err.Error(), err.Error())
	return e
}

func (s *Service) freshness(ctx context.Context, asOf time.Time) (string, Freshness) {
	var ref string
	_ = s.duck.QueryRowContext(ctx, `SELECT COALESCE(CAST(MAX(refreshed_at) AS VARCHAR),'') FROM projector_checkpoint WHERE state='fresh'`).Scan(&ref)
	f := Freshness{State: "unavailable", ThresholdMS: s.ttl.Milliseconds()}
	if ref == "" {
		return "", f
	}
	t, ok := parseTS(ref)
	if !ok {
		return ref, f
	}
	age := asOf.Sub(t)
	if age < 0 {
		age = 0
	}
	f.AgeMS = age.Milliseconds()
	if age > s.ttl {
		f.State = "stale"
	} else {
		f.State = "fresh"
	}
	return fmtTS(t), f
}

func (s *Service) summary(ctx context.Context, orgID, agentRef, projectID string, asOf time.Time) (Summary, error) {
	start := asOf.Add(-24 * time.Hour)
	where := `organization_id=?`
	args := []any{orgID}
	if agentRef != "" {
		where += ` AND agent_ref=?`
		args = append(args, agentRef)
	}
	if projectID != "" {
		where += ` AND project_id=?`
		args = append(args, projectID)
	}
	var sum Summary
	row := s.duck.QueryRowContext(ctx, `SELECT
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND outcome IN ('succeeded','failed','crashed','quiet_finalized')),
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND outcome IN ('failed','crashed')),
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND outcome='quiet_finalized'),
		COUNT(*) FILTER (WHERE started_at >= CAST(? AS TIMESTAMPTZ) AND started_at < CAST(? AS TIMESTAMPTZ) AND queued_at IS NOT NULL AND started_at >= queued_at),
		ROUND(quantile_cont(CASE WHEN started_at >= CAST(? AS TIMESTAMPTZ) AND started_at < CAST(? AS TIMESTAMPTZ) AND queued_at IS NOT NULL AND started_at >= queued_at THEN date_diff('millisecond', CAST(queued_at AS TIMESTAMP), CAST(started_at AS TIMESTAMP)) END, 0.50)),
		ROUND(quantile_cont(CASE WHEN started_at >= CAST(? AS TIMESTAMPTZ) AND started_at < CAST(? AS TIMESTAMPTZ) AND queued_at IS NOT NULL AND started_at >= queued_at THEN date_diff('millisecond', CAST(queued_at AS TIMESTAMP), CAST(started_at AS TIMESTAMP)) END, 0.95)),
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND started_at IS NOT NULL AND finished_at >= started_at),
		ROUND(quantile_cont(CASE WHEN finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND started_at IS NOT NULL AND finished_at >= started_at THEN date_diff('millisecond', CAST(started_at AS TIMESTAMP), CAST(finished_at AS TIMESTAMP)) END, 0.50)),
		ROUND(quantile_cont(CASE WHEN finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND started_at IS NOT NULL AND finished_at >= started_at THEN date_diff('millisecond', CAST(started_at AS TIMESTAMP), CAST(finished_at AS TIMESTAMP)) END, 0.95))
		FROM execution_fact WHERE `+where,
		append([]any{fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf)}, args...)...)
	var q50, q95, d50, d95 sql.NullFloat64
	if err := row.Scan(&sum.CompletedExecutions, &sum.FailedExecutions, &sum.RecoveryFinalizedExecutions, &sum.QueueWaitMS.Samples, &q50, &q95, &sum.ExecutionDurationMS.Samples, &d50, &d95); err != nil {
		return sum, err
	}
	if sum.CompletedExecutions > 0 {
		v := float64(sum.FailedExecutions) / float64(sum.CompletedExecutions)
		sum.FailureRate = &v
	}
	sum.QueueWaitMS.P50 = roundPtr(q50)
	sum.QueueWaitMS.P95 = roundPtr(q95)
	sum.ExecutionDurationMS.P50 = roundPtr(d50)
	sum.ExecutionDurationMS.P95 = roundPtr(d95)
	util, cov, err := s.slotUtilization(ctx, orgID, agentRef, asOf)
	if err != nil {
		return sum, err
	}
	sum.SlotUtilization = util
	sum.SlotCoverageRatio = cov
	return sum, nil
}

func (s *Service) trend(ctx context.Context, orgID, agentRef, projectID string, asOf time.Time) ([]TrendPoint, error) {
	start := asOf.Add(-24 * time.Hour)
	startBucket := start.Truncate(time.Hour)
	endBucket := asOf.Truncate(time.Hour)
	buckets := make(map[time.Time]*TrendPoint)
	durationSum := make(map[time.Time]int64)
	durationSamples := make(map[time.Time]int64)
	for ts := startBucket; !ts.After(endBucket); ts = ts.Add(time.Hour) {
		buckets[ts] = &TrendPoint{BucketStart: fmtTS(ts)}
	}
	where := `organization_id=?`
	args := []any{orgID}
	if agentRef != "" {
		where += ` AND agent_ref=?`
		args = append(args, agentRef)
	}
	if projectID != "" {
		where += ` AND project_id=?`
		args = append(args, projectID)
	}
	args = append(args, fmtTS(start), fmtTS(asOf))
	rows, err := s.duck.QueryContext(ctx, `SELECT CAST(finished_at AS VARCHAR), COALESCE(outcome,''), date_diff('millisecond', CAST(started_at AS TIMESTAMP), CAST(finished_at AS TIMESTAMP))
		FROM execution_fact
		WHERE `+where+` AND finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ)
		AND outcome IN ('succeeded','failed','crashed','quiet_finalized')`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, outcome string
		var duration sql.NullInt64
		if err := rows.Scan(&raw, &outcome, &duration); err != nil {
			return nil, err
		}
		finished, ok := parseTS(raw)
		if !ok {
			continue
		}
		bucket := finished.Truncate(time.Hour)
		point := buckets[bucket]
		if point == nil {
			point = &TrendPoint{BucketStart: fmtTS(bucket)}
			buckets[bucket] = point
		}
		point.CompletedExecutions++
		switch outcome {
		case "failed", "crashed":
			point.FailedExecutions++
		case "quiet_finalized":
			point.RecoveryFinalizedExecutions++
		}
		if duration.Valid && duration.Int64 >= 0 {
			durationSum[bucket] += duration.Int64
			durationSamples[bucket]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]TrendPoint, 0, len(buckets))
	for ts := startBucket; !ts.After(endBucket); ts = ts.Add(time.Hour) {
		point := buckets[ts]
		if point == nil {
			point = &TrendPoint{BucketStart: fmtTS(ts)}
		}
		if samples := durationSamples[ts]; samples > 0 {
			avg := durationSum[ts] / samples
			point.AvgDurationMS = &avg
		}
		out = append(out, *point)
	}
	return out, nil
}

func (s *Service) usageSummary(ctx context.Context, orgID, agentRef, projectID string, asOf time.Time) (UsageSummary, error) {
	start := asOf.Add(-24 * time.Hour)
	startBucket := start.Truncate(time.Hour)
	endBucket := asOf.Truncate(time.Hour)
	buckets := make(map[time.Time]*UsageTrendPoint)
	for ts := startBucket; !ts.After(endBucket); ts = ts.Add(time.Hour) {
		buckets[ts] = &UsageTrendPoint{BucketStart: fmtTS(ts)}
	}
	where := `p.organization_id=?`
	args := []any{orgID}
	if agentRef != "" {
		where += ` AND u.agent_ref=?`
		args = append(args, agentRef)
	}
	if projectID != "" {
		where += ` AND u.project_id=?`
		args = append(args, projectID)
	}
	args = append(args, fmtTS(start), fmtTS(asOf))
	rows, err := s.sqlite.QueryContext(ctx, `SELECT u.model, u.input_tokens, u.output_tokens, u.cache_read_tokens, u.cache_write_tokens, u.cost_micros, u.ts
		FROM usage_events u JOIN pm_projects p ON p.id=u.project_id
		WHERE `+where+` AND u.ts >= ? AND u.ts < ?`, args...)
	if err != nil {
		return UsageSummary{}, err
	}
	defer rows.Close()
	var out UsageSummary
	models := map[string]*UsageModelSummary{}
	for rows.Next() {
		var model, raw string
		var input, output, cacheRead, cacheWrite, cost int64
		if err := rows.Scan(&model, &input, &output, &cacheRead, &cacheWrite, &cost, &raw); err != nil {
			return UsageSummary{}, err
		}
		total := input + output + cacheRead + cacheWrite
		out.Events++
		out.InputTokens += input
		out.OutputTokens += output
		out.CacheReadTokens += cacheRead
		out.CacheWriteTokens += cacheWrite
		out.TotalTokens += total
		out.CostMicros += cost
		modelRow := models[model]
		if modelRow == nil {
			modelRow = &UsageModelSummary{Model: model}
			models[model] = modelRow
		}
		modelRow.Events++
		modelRow.TotalTokens += total
		modelRow.CostMicros += cost
		at, ok := parseTS(raw)
		if !ok {
			continue
		}
		bucket := at.Truncate(time.Hour)
		point := buckets[bucket]
		if point == nil {
			point = &UsageTrendPoint{BucketStart: fmtTS(bucket)}
			buckets[bucket] = point
		}
		point.InputTokens += input
		point.OutputTokens += output
		point.CacheReadTokens += cacheRead
		point.CacheWriteTokens += cacheWrite
		point.TotalTokens += total
		point.CostMicros += cost
	}
	if err := rows.Err(); err != nil {
		return UsageSummary{}, err
	}
	for _, model := range models {
		out.ByModel = append(out.ByModel, *model)
	}
	sort.Slice(out.ByModel, func(i, j int) bool {
		if out.ByModel[i].CostMicros != out.ByModel[j].CostMicros {
			return out.ByModel[i].CostMicros > out.ByModel[j].CostMicros
		}
		if out.ByModel[i].TotalTokens != out.ByModel[j].TotalTokens {
			return out.ByModel[i].TotalTokens > out.ByModel[j].TotalTokens
		}
		return out.ByModel[i].Model < out.ByModel[j].Model
	})
	for ts := startBucket; !ts.After(endBucket); ts = ts.Add(time.Hour) {
		point := buckets[ts]
		if point == nil {
			point = &UsageTrendPoint{BucketStart: fmtTS(ts)}
		}
		out.Trend = append(out.Trend, *point)
	}
	return out, nil
}

func (s *Service) planScale(ctx context.Context, orgID string, limit int) ([]PlanScaleSummary, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.sqlite.QueryContext(ctx, `SELECT
		pl.id,
		pl.name,
		pl.project_id,
		p.name,
		pl.status,
		COUNT(DISTINCT t.id) AS task_count,
		COUNT(DISTINCT d.from_task_id || '>' || d.to_task_id) AS edge_count,
		COUNT(DISTINCT g.id) AS generation_count,
		COUNT(DISTINCT CASE WHEN g.parent_generation_id<>'' THEN g.id END) AS evolution_count,
		COUNT(DISTINCT CASE WHEN t.status IN ('open','assigned','running','blocked','pool') THEN t.id END) AS active_task_count,
		COUNT(DISTINCT CASE WHEN t.status='blocked' THEN t.id END) AS blocked_task_count,
		COUNT(DISTINCT CASE WHEN t.status='failed' THEN t.id END) AS failed_task_count,
		COUNT(DISTINCT CASE WHEN t.status IN ('completed','verified') THEN t.id END) AS done_task_count
		FROM pm_plans pl
		JOIN pm_projects p ON p.id=pl.project_id
		LEFT JOIN pm_tasks t ON t.plan_id=pl.id AND t.archived_at=''
		LEFT JOIN pm_task_dependencies d ON d.plan_id=pl.id
		LEFT JOIN pm_plan_generations g ON g.plan_id=pl.id
		WHERE p.organization_id=? AND pl.archived_at=''
		GROUP BY pl.id, pl.name, pl.project_id, p.name, pl.status
		ORDER BY task_count DESC, evolution_count DESC, generation_count DESC, pl.updated_at DESC
		LIMIT ?`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanScaleSummary
	for rows.Next() {
		var row PlanScaleSummary
		var projectName sql.NullString
		if err := rows.Scan(&row.PlanID, &row.PlanName, &row.ProjectID, &projectName, &row.Status, &row.TaskCount, &row.EdgeCount, &row.GenerationCount, &row.EvolutionCount, &row.ActiveTaskCount, &row.BlockedTaskCount, &row.FailedTaskCount, &row.DoneTaskCount); err != nil {
			return nil, err
		}
		if projectName.Valid {
			row.ProjectName = &projectName.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) leaderboard(ctx context.Context, orgID, by string, asOf time.Time) ([]LeaderRow, error) {
	if by != "agent_ref" && by != "project_id" {
		return nil, fmt.Errorf("insight: unsupported leaderboard dimension %q", by)
	}
	start := asOf.Add(-24 * time.Hour)
	rows, err := s.duck.QueryContext(ctx, `SELECT `+by+`, COUNT(*) AS c,
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND outcome IN ('succeeded','failed','crashed','quiet_finalized')),
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND outcome IN ('failed','crashed')),
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND outcome='quiet_finalized'),
		COUNT(*) FILTER (WHERE started_at >= CAST(? AS TIMESTAMPTZ) AND started_at < CAST(? AS TIMESTAMPTZ) AND queued_at IS NOT NULL AND started_at >= queued_at),
		ROUND(quantile_cont(CASE WHEN started_at >= CAST(? AS TIMESTAMPTZ) AND started_at < CAST(? AS TIMESTAMPTZ) AND queued_at IS NOT NULL AND started_at >= queued_at THEN date_diff('millisecond', CAST(queued_at AS TIMESTAMP), CAST(started_at AS TIMESTAMP)) END, 0.50)),
		ROUND(quantile_cont(CASE WHEN started_at >= CAST(? AS TIMESTAMPTZ) AND started_at < CAST(? AS TIMESTAMPTZ) AND queued_at IS NOT NULL AND started_at >= queued_at THEN date_diff('millisecond', CAST(queued_at AS TIMESTAMP), CAST(started_at AS TIMESTAMP)) END, 0.95)),
		COUNT(*) FILTER (WHERE finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND started_at IS NOT NULL AND finished_at >= started_at),
		ROUND(quantile_cont(CASE WHEN finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND started_at IS NOT NULL AND finished_at >= started_at THEN date_diff('millisecond', CAST(started_at AS TIMESTAMP), CAST(finished_at AS TIMESTAMP)) END, 0.50)),
		ROUND(quantile_cont(CASE WHEN finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ) AND started_at IS NOT NULL AND finished_at >= started_at THEN date_diff('millisecond', CAST(started_at AS TIMESTAMP), CAST(finished_at AS TIMESTAMP)) END, 0.95))
		FROM execution_fact
		WHERE organization_id=? AND `+by+` IS NOT NULL AND finished_at >= CAST(? AS TIMESTAMPTZ) AND finished_at < CAST(? AS TIMESTAMPTZ)
		AND outcome IN ('succeeded','failed','crashed','quiet_finalized')
		GROUP BY `+by+` ORDER BY c DESC, `+by+` ASC LIMIT 20`,
		fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf),
		fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf), fmtTS(start), fmtTS(asOf),
		orgID, fmtTS(start), fmtTS(asOf))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaderRow
	for rows.Next() {
		var id string
		var rankCount int64
		var sum Summary
		var q50, q95, d50, d95 sql.NullFloat64
		if err := rows.Scan(&id, &rankCount, &sum.CompletedExecutions, &sum.FailedExecutions, &sum.RecoveryFinalizedExecutions, &sum.QueueWaitMS.Samples, &q50, &q95, &sum.ExecutionDurationMS.Samples, &d50, &d95); err != nil {
			return nil, err
		}
		_ = rankCount
		if sum.CompletedExecutions > 0 {
			v := float64(sum.FailedExecutions) / float64(sum.CompletedExecutions)
			sum.FailureRate = &v
		}
		sum.QueueWaitMS.P50 = roundPtr(q50)
		sum.QueueWaitMS.P95 = roundPtr(q95)
		sum.ExecutionDurationMS.P50 = roundPtr(d50)
		sum.ExecutionDurationMS.P95 = roundPtr(d95)
		r := LeaderRow{Summary: sum}
		if by == "agent_ref" {
			r.AgentRef = id
			name := s.nameForAgent(ctx, id)
			r.DisplayName = nullableString(name)
		} else {
			r.ProjectID = id
			name := s.nameForProject(ctx, id)
			r.Name = nullableString(name)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) slotUtilization(ctx context.Context, orgID, agentRef string, asOf time.Time) (*float64, *float64, error) {
	start := asOf.Add(-24 * time.Hour)
	where := `organization_id=?`
	whereArgs := []any{orgID}
	if agentRef != "" {
		where += ` AND agent_ref=?`
		whereArgs = append(whereArgs, agentRef)
	}
	ttlMS := s.ttl.Milliseconds()
	args := []any{fmtTS(start), fmtTS(start), ttlMS, fmtTS(asOf), fmtTS(start)}
	args = append(args, whereArgs...)
	args = append(args, fmtTS(asOf), fmtTS(asOf), fmtTS(start))
	row := s.duck.QueryRowContext(ctx, `SELECT
		SUM(CASE WHEN occupied AND admissible THEN date_diff('millisecond', CAST(GREATEST(valid_from, CAST(? AS TIMESTAMPTZ)) AS TIMESTAMP), CAST(known_to AS TIMESTAMP)) ELSE 0 END),
		SUM(CASE WHEN admissible THEN date_diff('millisecond', CAST(GREATEST(valid_from, CAST(? AS TIMESTAMPTZ)) AS TIMESTAMP), CAST(known_to AS TIMESTAMP)) ELSE 0 END)
		FROM (
			SELECT *, LEAST(
				CASE WHEN valid_to IS NULL THEN CAST(valid_from AS TIMESTAMP) + (? * INTERVAL 1 MILLISECOND) ELSE CAST(valid_to AS TIMESTAMP) END,
				CAST(? AS TIMESTAMP)
			) AS known_to
			FROM slot_interval_fact
		) WHERE known_to > CAST(? AS TIMESTAMP) AND `+where+` AND valid_from < CAST(? AS TIMESTAMPTZ) AND COALESCE(valid_to, CAST(? AS TIMESTAMPTZ)) > CAST(? AS TIMESTAMPTZ) AND state <> 'unknown'`, args...)
	var occ, avail sql.NullFloat64
	if err := row.Scan(&occ, &avail); err != nil {
		return nil, nil, err
	}
	var util, cov *float64
	if avail.Valid && avail.Float64 > 0 {
		v := occ.Float64 / avail.Float64
		util = &v
	}
	capArgs := []any{fmtTS(start), fmtTS(asOf), fmtTS(asOf)}
	capArgs = append(capArgs, whereArgs...)
	capArgs = append(capArgs, fmtTS(asOf), fmtTS(asOf), fmtTS(start))
	var capacity sql.NullFloat64
	if err := s.duck.QueryRowContext(ctx, `SELECT
		SUM(CASE WHEN admissible THEN date_diff('millisecond', CAST(GREATEST(valid_from, CAST(? AS TIMESTAMPTZ)) AS TIMESTAMP), CAST(LEAST(COALESCE(valid_to, CAST(? AS TIMESTAMPTZ)), CAST(? AS TIMESTAMPTZ)) AS TIMESTAMP)) ELSE 0 END)
		FROM slot_interval_fact WHERE `+where+`
		AND valid_from < CAST(? AS TIMESTAMPTZ)
		AND COALESCE(valid_to, CAST(? AS TIMESTAMPTZ)) > CAST(? AS TIMESTAMPTZ)`, capArgs...).Scan(&capacity); err != nil {
		return nil, nil, err
	}
	if avail.Valid && capacity.Valid && capacity.Float64 > 0 {
		v := avail.Float64 / capacity.Float64
		cov = &v
	}
	return util, cov, nil
}

func (s *Service) diagnostics(ctx context.Context, orgID string, asOf time.Time) (Diagnostics, error) {
	start := asOf.Add(-24 * time.Hour)
	var d Diagnostics
	err := s.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_fact
		WHERE organization_id=? AND quality <> 'valid' AND COALESCE(finished_at, started_at, queued_at) >= CAST(? AS TIMESTAMPTZ) AND COALESCE(finished_at, started_at, queued_at) < CAST(? AS TIMESTAMPTZ)`,
		orgID, fmtTS(start), fmtTS(asOf)).Scan(&d.InvalidFacts)
	if err != nil {
		return d, err
	}
	_ = s.duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM projected_event WHERE projected_at > occurred_at + INTERVAL 48 HOUR`).Scan(&d.LateEvents)
	return d, nil
}

func (s *Service) nameForAgent(ctx context.Context, ref string) string {
	id := strings.TrimPrefix(ref, "agent:")
	var name string
	_ = s.sqlite.QueryRowContext(ctx, `SELECT name FROM agents WHERE id=?`, id).Scan(&name)
	return name
}

func (s *Service) nameForProject(ctx context.Context, id string) string {
	var name string
	_ = s.sqlite.QueryRowContext(ctx, `SELECT name FROM pm_projects WHERE id=?`, id).Scan(&name)
	return name
}

func normalizedSlots(snap concurrency.AgentSnapshot) []concurrency.SlotSnapshot {
	if len(snap.Slots) > 0 {
		return snap.Slots
	}
	extra := snap.AdmissionCap - len(snap.Executors)
	if extra < 0 {
		extra = 0
	}
	out := make([]concurrency.SlotSnapshot, 0, len(snap.Executors)+extra)
	seen := map[int]bool{}
	for _, ex := range snap.Executors {
		idx := len(out)
		if ex.SlotIndex != nil {
			idx = *ex.SlotIndex
		}
		seen[idx] = true
		out = append(out, concurrency.SlotSnapshot{SlotIndex: idx, ExecutorID: ex.ExecutorID, TaskID: ex.TaskID, CLI: ex.CLI, Model: ex.Model, State: ex.State, StartedAt: ex.StartedAt, PID: ex.PID, LastProgressAt: ex.LastProgressAt, CurrentActivity: ex.CurrentActivity})
	}
	for i := 0; i < snap.AdmissionCap; i++ {
		if !seen[i] {
			out = append(out, concurrency.SlotSnapshot{SlotIndex: i, State: concurrency.StateIdle})
		}
	}
	return out
}

func occupiedState(state string) bool {
	switch state {
	case concurrency.StateStarting, concurrency.StateRunning, concurrency.StateFinishing, concurrency.StateOrphan:
		return true
	default:
		return false
	}
}

func makeWindow(asOf time.Time) Window {
	return Window{Kind: "rolling", Duration: "24h", Start: fmtTS(asOf.Add(-24 * time.Hour)), End: fmtTS(asOf)}
}

func parseTS(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05-07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func fmtTS(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func normalizeAgent(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "agent:") {
		return id
	}
	return "agent:" + id
}

func nullArg(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func coalesce(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func statusOrPending(s string) string {
	if s == "" {
		return "pending"
	}
	return s
}

func qualityFor(started string, finished time.Time) string {
	if t, ok := parseTS(started); ok && finished.Before(t) {
		return "invalid_time_order"
	}
	return "valid"
}

func strPtr(ns sql.NullString) *string {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	v := ns.String
	return &v
}

func intPtr(ni sql.NullInt64) *int64 {
	if !ni.Valid {
		return nil
	}
	v := ni.Int64
	return &v
}

func roundPtr(n sql.NullFloat64) *int64 {
	if !n.Valid {
		return nil
	}
	v := int64(math.Round(n.Float64))
	return &v
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func encodeCursor(key, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key + "\n" + id))
}

func decodeCursor(c string) (string, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(b), "\n", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid cursor")
	}
	return parts[0], parts[1], nil
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }
