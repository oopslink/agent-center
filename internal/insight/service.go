package insight

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/oopslink/agent-center/internal/concurrency"
)

const replayOverlap = 48 * time.Hour

type Service struct {
	sqlite             *sql.DB
	duck               *sql.DB
	path               string
	ttl                time.Duration
	mu                 sync.RWMutex
	projectorFaultHook insightProjectorFaultHook
}

type insightProjectorCommitStage string

const (
	insightProjectorBeforeCommit insightProjectorCommitStage = "before_commit"
	insightProjectorAfterCommit  insightProjectorCommitStage = "after_commit"
)

type insightProjectorFaultHook func(ctx context.Context, sourceKind, sourceEventID string, stage insightProjectorCommitStage) error

func (s *Service) runProjectorFaultHook(ctx context.Context, sourceKind, sourceEventID string, stage insightProjectorCommitStage) error {
	if s.projectorFaultHook == nil {
		return nil
	}
	return s.projectorFaultHook(ctx, sourceKind, sourceEventID, stage)
}

func DefaultDuckDBPath(sqlitePath string) string {
	if strings.TrimSpace(sqlitePath) == "" || strings.Contains(sqlitePath, ":memory:") {
		return filepath.Join(os.TempDir(), "agent-center-insight.duckdb")
	}
	return filepath.Join(filepath.Dir(sqlitePath), "insight.duckdb")
}

func Open(ctx context.Context, sqlite *sql.DB, path string, ttl time.Duration) (*Service, error) {
	if sqlite == nil {
		return nil, errors.New("insight: sqlite db required")
	}
	if ttl <= 0 {
		ttl = DefaultFreshnessSLA
	}
	if path == "" {
		path = DefaultDuckDBPath("")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("insight: mkdir duckdb dir: %w", err)
	}
	duck, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	s := &Service{sqlite: sqlite, duck: duck, path: path, ttl: ttl}
	if err := s.ensureSchema(ctx); err != nil {
		_ = duck.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return s, nil
}

func (s *Service) Close() error {
	if s == nil || s.duck == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.duck.Close()
}

func (s *Service) Rebuild(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rebuildLocked(ctx)
}

func (s *Service) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureSchema(ctx); err != nil {
		if rerr := s.rebuildLocked(ctx); rerr != nil {
			return fmt.Errorf("insight refresh schema: %w; rebuild: %v", err, rerr)
		}
	}
	if err := s.projectQueue(ctx); err != nil {
		_ = s.markError(ctx, SourceQueue, err)
		return err
	}
	if err := s.projectActivity(ctx); err != nil {
		_ = s.markError(ctx, SourceActivity, err)
		return err
	}
	if err := s.projectSlots(ctx); err != nil {
		_ = s.markError(ctx, SourceSlotObservation, err)
		return err
	}
	return nil
}

func (s *Service) StartProjector(ctx context.Context, interval time.Duration, onError func(error)) context.CancelFunc {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if onError == nil {
		onError = func(error) {}
	}
	projCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.Refresh(projCtx); err != nil && !errors.Is(projCtx.Err(), context.Canceled) {
			onError(err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-projCtx.Done():
				return
			case <-ticker.C:
				if err := s.Refresh(projCtx); err != nil && !errors.Is(projCtx.Err(), context.Canceled) {
					onError(err)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s *Service) Overview(ctx context.Context, orgID string, asOf time.Time) (Overview, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	win := makeWindow(asOf)
	ref, fresh := s.freshness(ctx, asOf)
	sum, err := s.summary(ctx, orgID, "", "", asOf)
	if err != nil {
		return Overview{}, err
	}
	agents, err := s.leaderboard(ctx, orgID, "agent_ref", asOf)
	if err != nil {
		return Overview{}, err
	}
	projects, err := s.leaderboard(ctx, orgID, "project_id", asOf)
	if err != nil {
		return Overview{}, err
	}
	diag, err := s.diagnostics(ctx, orgID, asOf)
	if err != nil {
		return Overview{}, err
	}
	return Overview{Window: win, AsOf: fmtTS(asOf), RefreshedAt: ref, Freshness: fresh, Summary: sum, Agents: agents, Projects: projects, Diagnostics: diag}, nil
}

func (s *Service) Executions(ctx context.Context, orgID string, f ExecutionFilter) (ExecutionsResponse, error) {
	asOf := f.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	start := asOf.Add(-24 * time.Hour)
	args := []any{orgID, fmtTS(start), fmtTS(asOf)}
	where := `organization_id = ? AND COALESCE(finished_at, started_at, queued_at) >= CAST(? AS TIMESTAMPTZ) AND COALESCE(finished_at, started_at, queued_at) < CAST(? AS TIMESTAMPTZ)`
	if f.AgentRef != "" {
		where += ` AND agent_ref = ?`
		args = append(args, f.AgentRef)
	}
	if f.ProjectID != "" {
		where += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	if f.Cursor != "" {
		key, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return ExecutionsResponse{}, err
		}
		where += ` AND (COALESCE(finished_at, started_at, queued_at) < CAST(? AS TIMESTAMPTZ) OR (COALESCE(finished_at, started_at, queued_at) = CAST(? AS TIMESTAMPTZ) AND execution_id < ?))`
		args = append(args, key, key, id)
	}
	args = append(args, limit+1)
	rows, err := s.duck.QueryContext(ctx, `SELECT execution_id, command_id, task_id, task_title, agent_ref, agent_name,
		project_id, project_name, worker_id, outcome, failure_reason, queued_at, started_at, finished_at,
		CASE WHEN queued_at IS NOT NULL AND started_at IS NOT NULL AND started_at >= queued_at THEN date_diff('millisecond', CAST(queued_at AS TIMESTAMP), CAST(started_at AS TIMESTAMP)) END,
		CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL AND finished_at >= started_at THEN date_diff('millisecond', CAST(started_at AS TIMESTAMP), CAST(finished_at AS TIMESTAMP)) END,
		recovered, quality, COALESCE(finished_at, started_at, queued_at) AS sort_key
		FROM execution_fact WHERE `+where+`
		ORDER BY sort_key DESC, execution_id DESC LIMIT ?`, args...)
	if err != nil {
		return ExecutionsResponse{}, err
	}
	defer rows.Close()
	var out []ExecutionRow
	var cursorKey, cursorID string
	hasNext := false
	for rows.Next() {
		var r ExecutionRow
		var cmd, task, title, an, proj, pn, worker, outcome, reason sql.NullString
		var queued, started, finished, sortKey sql.NullString
		var qwait, dur sql.NullInt64
		if err := rows.Scan(&r.ExecutionID, &cmd, &task, &title, &r.AgentRef, &an, &proj, &pn, &worker, &outcome, &reason, &queued, &started, &finished, &qwait, &dur, &r.Recovered, &r.Quality, &sortKey); err != nil {
			return ExecutionsResponse{}, err
		}
		r.CommandID = strPtr(cmd)
		r.TaskID = strPtr(task)
		r.TaskRef = strPtr(task)
		r.TaskTitle = strPtr(title)
		r.AgentName = strPtr(an)
		r.ProjectID = strPtr(proj)
		r.ProjectName = strPtr(pn)
		r.WorkerID = strPtr(worker)
		r.Outcome = strPtr(outcome)
		r.FailureReason = strPtr(reason)
		r.QueuedAt = strPtr(queued)
		r.StartedAt = strPtr(started)
		r.FinishedAt = strPtr(finished)
		r.QueueWaitMS = intPtr(qwait)
		r.DurationMS = intPtr(dur)
		if len(out) == limit {
			hasNext = true
			continue
		}
		cursorKey, cursorID = sortKey.String, r.ExecutionID
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return ExecutionsResponse{}, err
	}
	next := ""
	if hasNext {
		next = encodeCursor(cursorKey, cursorID)
	}
	ref, fresh := s.freshness(ctx, asOf)
	return ExecutionsResponse{Window: makeWindow(asOf), AsOf: fmtTS(asOf), RefreshedAt: ref, Freshness: fresh, Executions: out, NextCursor: next}, nil
}

func (s *Service) rebuildLocked(ctx context.Context) error {
	if s.duck != nil {
		_ = s.duck.Close()
	}
	tmp := s.path + ".rebuild"
	_ = os.Remove(tmp)
	duck, err := sql.Open("duckdb", tmp)
	if err != nil {
		return err
	}
	old := s.duck
	oldPath := s.path
	s.duck = duck
	s.path = tmp
	if err := s.ensureSchema(ctx); err != nil {
		_ = duck.Close()
		s.duck = old
		s.path = oldPath
		return err
	}
	if err := s.projectQueue(ctx); err != nil {
		_ = duck.Close()
		s.duck = old
		s.path = oldPath
		return err
	}
	if err := s.projectActivity(ctx); err != nil {
		_ = duck.Close()
		s.duck = old
		s.path = oldPath
		return err
	}
	if err := s.projectSlots(ctx); err != nil {
		_ = duck.Close()
		s.duck = old
		s.path = oldPath
		return err
	}
	_ = duck.Close()
	if old != nil {
		_ = old.Close()
	}
	if err := os.Rename(tmp, oldPath); err != nil {
		return err
	}
	s.path = oldPath
	s.duck, err = sql.Open("duckdb", oldPath)
	if err != nil {
		return err
	}
	return s.ensureSchema(ctx)
}

func (s *Service) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS insight_meta (k VARCHAR PRIMARY KEY, v VARCHAR NOT NULL)`,
		`INSERT OR REPLACE INTO insight_meta VALUES ('schema_version', '1')`,
		`CREATE TABLE IF NOT EXISTS execution_fact (
			execution_id VARCHAR PRIMARY KEY, command_id VARCHAR, organization_id VARCHAR, project_id VARCHAR,
			task_id VARCHAR, task_title VARCHAR, agent_ref VARCHAR NOT NULL, agent_name VARCHAR, worker_id VARCHAR,
			cli VARCHAR, model VARCHAR, queued_at TIMESTAMPTZ, started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
			outcome VARCHAR, failure_reason VARCHAR, recovered BOOLEAN NOT NULL DEFAULT false,
			quality VARCHAR NOT NULL DEFAULT 'valid', first_event_id VARCHAR NOT NULL, last_event_id VARCHAR NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL, project_name VARCHAR)`,
		`CREATE TABLE IF NOT EXISTS queue_interval_fact (
			command_id VARCHAR PRIMARY KEY, execution_id VARCHAR, organization_id VARCHAR, project_id VARCHAR,
			task_id VARCHAR, agent_ref VARCHAR NOT NULL, worker_id VARCHAR NOT NULL, queued_at TIMESTAMPTZ NOT NULL,
			started_at TIMESTAMPTZ, command_status VARCHAR NOT NULL, status_reason VARCHAR,
			quality VARCHAR NOT NULL DEFAULT 'valid', last_event_id VARCHAR NOT NULL, observed_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS slot_interval_fact (
			worker_id VARCHAR NOT NULL, agent_ref VARCHAR NOT NULL, slot_index INTEGER NOT NULL,
			valid_from TIMESTAMPTZ NOT NULL, valid_to TIMESTAMPTZ, state VARCHAR NOT NULL, occupied BOOLEAN NOT NULL,
			admissible BOOLEAN NOT NULL, execution_id VARCHAR, task_id VARCHAR, integrity VARCHAR,
			source_event_id VARCHAR NOT NULL, organization_id VARCHAR, PRIMARY KEY (worker_id, agent_ref, slot_index, valid_from))`,
		`CREATE TABLE IF NOT EXISTS projected_event (
			source_event_id VARCHAR PRIMARY KEY, source_kind VARCHAR NOT NULL, source_cursor VARCHAR,
			occurred_at TIMESTAMPTZ NOT NULL, projected_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS projector_checkpoint (
			source_kind VARCHAR PRIMARY KEY, source_cursor VARCHAR NOT NULL, refreshed_at TIMESTAMPTZ NOT NULL,
			state VARCHAR NOT NULL, last_error VARCHAR)`,
	}
	for _, stmt := range stmts {
		if _, err := s.duck.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) projectQueue(ctx context.Context) error {
	rows, err := s.sqlite.QueryContext(ctx, `SELECT id, worker_id, command_type, COALESCE(agent_id,''), COALESCE(task_id,''),
		COALESCE(status,''), COALESCE(status_reason,''), COALESCE(execution_id,''), COALESCE(status_updated_at,''), created_at
		FROM worker_control_events WHERE command_type = 'agent.fork_executor' ORDER BY created_at, id`)
	if err != nil {
		return err
	}
	type queueRow struct{ id, workerID, typ, agentID, taskID, status, reason, execID, statusAt, createdAt string }
	var all []queueRow
	for rows.Next() {
		var r queueRow
		if err := rows.Scan(&r.id, &r.workerID, &r.typ, &r.agentID, &r.taskID, &r.status, &r.reason, &r.execID, &r.statusAt, &r.createdAt); err != nil {
			return err
		}
		all = append(all, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range all {
		id, workerID, agentID, taskID, status, reason, execID, statusAt, createdAt := r.id, r.workerID, r.agentID, r.taskID, r.status, r.reason, r.execID, r.statusAt, r.createdAt
		queuedAt, _ := parseTS(createdAt)
		observedAt := queuedAt
		if t, ok := parseTS(statusAt); ok {
			observedAt = t
		}
		sourceID := "queue:" + id + ":" + status + ":" + fmtTS(observedAt)
		d, _ := s.dimensions(ctx, taskID, agentID)
		tx, err := s.duck.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if seen, err := projected(ctx, tx, sourceID); err != nil || seen {
			_ = tx.Rollback()
			if err != nil {
				return err
			}
			continue
		}
		agentRef := normalizeAgent(agentID)
		if agentRef == "" {
			agentRef = "agent:unknown"
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO queue_interval_fact
			(command_id, execution_id, organization_id, project_id, task_id, agent_ref, worker_id, queued_at,
			 command_status, status_reason, quality, last_event_id, observed_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, nullArg(execID), nullArg(d.OrgID), nullArg(d.ProjectID), nullArg(taskID), agentRef, workerID, fmtTS(queuedAt),
			statusOrPending(status), nullArg(reason), "valid", sourceID, fmtTS(observedAt))
		if err == nil && execID != "" {
			_, err = tx.ExecContext(ctx, `UPDATE execution_fact SET command_id = COALESCE(command_id, ?),
				queued_at = COALESCE(queued_at, CAST(? AS TIMESTAMPTZ)), worker_id = COALESCE(worker_id, ?),
				organization_id = COALESCE(organization_id, ?), project_id = COALESCE(project_id, ?),
				project_name = COALESCE(project_name, ?), task_id = COALESCE(task_id, ?), task_title = COALESCE(task_title, ?)
				WHERE execution_id = ?`,
				id, fmtTS(queuedAt), workerID, nullArg(d.OrgID), nullArg(d.ProjectID), nullArg(d.ProjectName), nullArg(taskID), nullArg(d.TaskTitle), execID)
		} else if err == nil && execID == "" {
			pseudoID := "command:" + id
			_, err = tx.ExecContext(ctx, `INSERT INTO execution_fact
				(execution_id, command_id, organization_id, project_id, task_id, task_title, agent_ref, agent_name, worker_id,
				 queued_at, recovered, quality, first_event_id, last_event_id, observed_at, project_name)
				VALUES (?,?,?,?,?,?,?,?,?,?,false,'valid',?,?,?,?)
				ON CONFLICT (execution_id) DO UPDATE SET
					command_id = excluded.command_id, organization_id = COALESCE(execution_fact.organization_id, excluded.organization_id),
					project_id = COALESCE(execution_fact.project_id, excluded.project_id), project_name = COALESCE(execution_fact.project_name, excluded.project_name),
					task_id = COALESCE(execution_fact.task_id, excluded.task_id), task_title = COALESCE(execution_fact.task_title, excluded.task_title),
					agent_ref = excluded.agent_ref, agent_name = COALESCE(excluded.agent_name, execution_fact.agent_name),
					worker_id = excluded.worker_id, queued_at = excluded.queued_at, last_event_id = excluded.last_event_id, observed_at = excluded.observed_at`,
				pseudoID, id, nullArg(d.OrgID), nullArg(d.ProjectID), nullArg(taskID), nullArg(d.TaskTitle),
				agentRef, nullArg(d.AgentName), workerID, fmtTS(queuedAt), sourceID, sourceID, fmtTS(observedAt), nullArg(d.ProjectName))
		}
		if err == nil {
			err = markProjected(ctx, tx, sourceID, SourceQueue, id, observedAt)
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := s.runProjectorFaultHook(ctx, SourceQueue, sourceID, insightProjectorBeforeCommit); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if err := s.runProjectorFaultHook(ctx, SourceQueue, sourceID, insightProjectorAfterCommit); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) projectActivity(ctx context.Context) error {
	rows, err := s.sqlite.QueryContext(ctx, `SELECT id, agent_id, COALESCE(task_ref,''), COALESCE(interaction_ref,''), payload, occurred_at
		FROM agent_activity_events WHERE interaction_ref LIKE 'executor:%' ORDER BY occurred_at, id`)
	if err != nil {
		return err
	}
	type activityRow struct{ id, agentID, taskID, interaction, payload, occurredRaw string }
	var all []activityRow
	for rows.Next() {
		var r activityRow
		if err := rows.Scan(&r.id, &r.agentID, &r.taskID, &r.interaction, &r.payload, &r.occurredRaw); err != nil {
			return err
		}
		all = append(all, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range all {
		id, agentID, taskID, interaction, payload, occurredRaw := r.id, r.agentID, r.taskID, r.interaction, r.payload, r.occurredRaw
		occurred, _ := parseTS(occurredRaw)
		var p map[string]any
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			continue
		}
		ev, _ := p["event"].(string)
		if ev != "executor.start" && ev != "executor.stop" && ev != "executor.recovery_quiet_finalized" {
			continue
		}
		execID, _ := p["executor_id"].(string)
		if execID == "" {
			execID = strings.TrimPrefix(interaction, "executor:")
		}
		if execID == "" {
			continue
		}
		sourceID := "activity:" + id
		d, _ := s.dimensions(ctx, taskID, agentID)
		tx, err := s.duck.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if seen, err := projected(ctx, tx, sourceID); err != nil || seen {
			_ = tx.Rollback()
			if err != nil {
				return err
			}
			continue
		}
		q := s.queueByExecution(ctx, tx, execID)
		agentRef := normalizeAgent(agentID)
		switch ev {
		case "executor.start":
			cli, _ := p["cli"].(string)
			model, _ := p["model"].(string)
			_, err = tx.ExecContext(ctx, `INSERT INTO execution_fact
				(execution_id, command_id, organization_id, project_id, task_id, task_title, agent_ref, agent_name, worker_id,
				 cli, model, queued_at, started_at, recovered, quality, first_event_id, last_event_id, observed_at, project_name)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,false,'valid',?,?,?,?)
				ON CONFLICT (execution_id) DO UPDATE SET
					started_at = CASE WHEN execution_fact.started_at IS NULL OR CAST(? AS TIMESTAMPTZ) < execution_fact.started_at THEN CAST(? AS TIMESTAMPTZ) ELSE execution_fact.started_at END,
					cli = COALESCE(NULLIF(?, ''), execution_fact.cli), model = COALESCE(NULLIF(?, ''), execution_fact.model),
					agent_ref = ?, agent_name = COALESCE(excluded.agent_name, execution_fact.agent_name),
					command_id = COALESCE(execution_fact.command_id, excluded.command_id), queued_at = CASE WHEN execution_fact.queued_at IS NULL THEN excluded.queued_at ELSE execution_fact.queued_at END,
					worker_id = COALESCE(execution_fact.worker_id, excluded.worker_id), organization_id = COALESCE(execution_fact.organization_id, excluded.organization_id),
					project_id = COALESCE(execution_fact.project_id, excluded.project_id), project_name = COALESCE(execution_fact.project_name, excluded.project_name),
					task_id = COALESCE(execution_fact.task_id, excluded.task_id), task_title = COALESCE(execution_fact.task_title, excluded.task_title),
					last_event_id = ?, observed_at = ?, quality = CASE WHEN execution_fact.finished_at IS NOT NULL AND execution_fact.finished_at < CAST(? AS TIMESTAMPTZ) THEN 'invalid_time_order' ELSE 'valid' END`,
				execID, nullArg(q.CommandID), nullArg(coalesce(d.OrgID, q.OrgID)), nullArg(coalesce(d.ProjectID, q.ProjectID)), nullArg(taskID), nullArg(d.TaskTitle),
				agentRef, nullArg(d.AgentName), nullArg(q.WorkerID), cli, model, nullArg(q.QueuedAt), fmtTS(occurred), sourceID, sourceID, fmtTS(occurred), nullArg(d.ProjectName),
				fmtTS(occurred), fmtTS(occurred), cli, model, agentRef, sourceID, fmtTS(occurred), fmtTS(occurred))
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE queue_interval_fact SET started_at = COALESCE(started_at, CAST(? AS TIMESTAMPTZ)) WHERE execution_id = ?`, fmtTS(occurred), execID)
			}
		default:
			outcome := "quiet_finalized"
			if ev == "executor.stop" {
				outcome, _ = p["outcome"].(string)
			}
			reason, _ := p["reason"].(string)
			recovered, _ := p["recovered"].(bool)
			_, err = tx.ExecContext(ctx, `INSERT INTO execution_fact
				(execution_id, command_id, organization_id, project_id, task_id, task_title, agent_ref, agent_name, worker_id,
				 queued_at, finished_at, outcome, failure_reason, recovered, quality, first_event_id, last_event_id, observed_at, project_name)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT (execution_id) DO UPDATE SET
					finished_at = CASE WHEN execution_fact.finished_at IS NULL OR CAST(? AS TIMESTAMPTZ) > execution_fact.finished_at THEN CAST(? AS TIMESTAMPTZ) ELSE execution_fact.finished_at END,
					outcome = ?, failure_reason = COALESCE(NULLIF(?, ''), execution_fact.failure_reason),
					recovered = execution_fact.recovered OR ?, agent_ref = ?, agent_name = COALESCE(excluded.agent_name, execution_fact.agent_name),
					command_id = COALESCE(execution_fact.command_id, excluded.command_id), queued_at = CASE WHEN execution_fact.queued_at IS NULL THEN excluded.queued_at ELSE execution_fact.queued_at END,
					worker_id = COALESCE(execution_fact.worker_id, excluded.worker_id), organization_id = COALESCE(execution_fact.organization_id, excluded.organization_id),
					project_id = COALESCE(execution_fact.project_id, excluded.project_id), project_name = COALESCE(execution_fact.project_name, excluded.project_name),
					task_id = COALESCE(execution_fact.task_id, excluded.task_id), task_title = COALESCE(execution_fact.task_title, excluded.task_title),
					last_event_id = ?, observed_at = ?,
					quality = CASE WHEN execution_fact.started_at IS NOT NULL AND CAST(? AS TIMESTAMPTZ) < execution_fact.started_at THEN 'invalid_time_order' ELSE execution_fact.quality END`,
				execID, nullArg(q.CommandID), nullArg(coalesce(d.OrgID, q.OrgID)), nullArg(coalesce(d.ProjectID, q.ProjectID)), nullArg(taskID), nullArg(d.TaskTitle),
				agentRef, nullArg(d.AgentName), nullArg(q.WorkerID), nullArg(q.QueuedAt), fmtTS(occurred), outcome, nullArg(reason), recovered, qualityFor(q.StartedAt, occurred), sourceID, sourceID, fmtTS(occurred), nullArg(d.ProjectName),
				fmtTS(occurred), fmtTS(occurred), outcome, reason, recovered, agentRef, sourceID, fmtTS(occurred), fmtTS(occurred))
		}
		if err == nil {
			err = markProjected(ctx, tx, sourceID, SourceActivity, id, occurred)
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := s.runProjectorFaultHook(ctx, SourceActivity, sourceID, insightProjectorBeforeCommit); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if err := s.runProjectorFaultHook(ctx, SourceActivity, sourceID, insightProjectorAfterCommit); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) projectSlots(ctx context.Context) error {
	rows, err := s.sqlite.QueryContext(ctx, `SELECT id, worker_id, agent_id, snapshot, observed_at FROM agent_concurrency_observations ORDER BY observed_at, id`)
	if err != nil {
		return err
	}
	type slotRow struct{ id, workerID, agentID, raw, observedRaw string }
	var all []slotRow
	for rows.Next() {
		var r slotRow
		if err := rows.Scan(&r.id, &r.workerID, &r.agentID, &r.raw, &r.observedRaw); err != nil {
			return err
		}
		all = append(all, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range all {
		id, workerID, agentID, raw, observedRaw := r.id, r.workerID, r.agentID, r.raw, r.observedRaw
		observed, _ := parseTS(observedRaw)
		var snap concurrency.AgentSnapshot
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			continue
		}
		sourceID := "slot:" + id
		tx, err := s.duck.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if seen, err := projected(ctx, tx, sourceID); err != nil || seen {
			_ = tx.Rollback()
			if err != nil {
				return err
			}
			continue
		}
		agentRef := normalizeAgent(agentID)
		d, _ := s.dimensions(ctx, "", agentID)
		for _, slot := range normalizedSlots(snap) {
			occupied := occupiedState(slot.State)
			admissible := slot.SlotIndex < snap.AdmissionCap && slot.State != concurrency.StateDraining
			var prevState, prevExec, prevTask, prevIntegrity string
			var prevFrom string
			err = tx.QueryRowContext(ctx, `SELECT state, COALESCE(execution_id,''), COALESCE(task_id,''), COALESCE(integrity,''), valid_from
				FROM slot_interval_fact WHERE worker_id=? AND agent_ref=? AND slot_index=? AND valid_to IS NULL
				ORDER BY valid_from DESC LIMIT 1`, workerID, agentRef, slot.SlotIndex).Scan(&prevState, &prevExec, &prevTask, &prevIntegrity, &prevFrom)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				_ = tx.Rollback()
				return err
			}
			same := err == nil && prevState == slot.State && prevExec == slot.ExecutorID && prevTask == slot.TaskID && prevIntegrity == snap.Integrity
			if !same {
				if _, err := tx.ExecContext(ctx, `UPDATE slot_interval_fact SET valid_to=? WHERE worker_id=? AND agent_ref=? AND slot_index=? AND valid_to IS NULL`,
					fmtTS(observed), workerID, agentRef, slot.SlotIndex); err != nil {
					_ = tx.Rollback()
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO slot_interval_fact
					(worker_id, agent_ref, slot_index, valid_from, state, occupied, admissible, execution_id, task_id, integrity, source_event_id, organization_id)
					VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
					workerID, agentRef, slot.SlotIndex, fmtTS(observed), slot.State, occupied, admissible, nullArg(slot.ExecutorID), nullArg(slot.TaskID), nullArg(snap.Integrity), sourceID, nullArg(d.OrgID)); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
		}
		if err := markProjected(ctx, tx, sourceID, SourceSlotObservation, id, observed); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := s.runProjectorFaultHook(ctx, SourceSlotObservation, sourceID, insightProjectorBeforeCommit); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if err := s.runProjectorFaultHook(ctx, SourceSlotObservation, sourceID, insightProjectorAfterCommit); err != nil {
			return err
		}
	}
	return nil
}
