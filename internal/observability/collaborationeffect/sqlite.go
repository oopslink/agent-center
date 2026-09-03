package collaborationeffect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

type SQLiteRepository struct{ db *sql.DB }

func NewSQLiteRepository(db *sql.DB) (*SQLiteRepository, error) {
	if db == nil {
		return nil, errors.New("collaboration effect repository: nil db")
	}
	return &SQLiteRepository{db: db}, nil
}

func (r *SQLiteRepository) Apply(ctx context.Context, f Fact, version string, effects []Effect, deps []Dependency, diagnostics []Diagnostic) error {
	return persistence.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		exec, err := persistence.ExecutorFromCtx(txCtx, r.db)
		if err != nil {
			return err
		}
		for _, d := range deps {
			_, err = exec.ExecContext(txCtx, `INSERT OR IGNORE INTO collaboration_effect_dependencies(rule_version,project_id,plan_id,upstream_task_id,downstream_task_id,source_event_id,occurred_at) VALUES(?,?,?,?,?,?,?)`, version, d.ProjectID, d.PlanID, d.UpstreamTaskID, d.DownstreamTaskID, d.SourceEventID, formatTime(d.OccurredAt))
			if err != nil {
				return err
			}
		}
		for _, e := range effects {
			evidence, _ := json.Marshal(e.EvidenceEventIDs)
			before, _ := json.Marshal(e.BeforeState)
			after, _ := json.Marshal(e.AfterState)
			_, err = exec.ExecContext(txCtx, `INSERT OR IGNORE INTO collaboration_effects(effect_id,project_id,target_task_id,source_agent_ref,target_agent_ref,relation_type,polarity,magnitude,confidence,occurred_at,rule_version,evidence_event_ids,before_state,after_state,explanation_key) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.EffectID, e.ProjectID, e.TargetTaskID, e.SourceAgentRef, e.TargetAgentRef, e.RelationType, e.Polarity, e.Magnitude, e.Confidence, formatTime(e.OccurredAt), e.RuleVersion, string(evidence), string(before), string(after), e.ExplanationKey)
			if err != nil {
				return err
			}
		}
		for _, d := range diagnostics {
			_, err = exec.ExecContext(txCtx, `INSERT OR IGNORE INTO collaboration_effect_diagnostics(source_event_id,rule_version,reason,occurred_at) VALUES(?,?,?,?)`, d.SourceEventID, d.RuleVersion, d.Reason, formatTime(d.OccurredAt))
			if err != nil {
				return err
			}
		}
		_, err = exec.ExecContext(txCtx, `INSERT INTO collaboration_effect_checkpoints(rule_version,last_event_id,updated_at) VALUES(?,?,?) ON CONFLICT(rule_version) DO UPDATE SET last_event_id=CASE WHEN excluded.last_event_id>last_event_id THEN excluded.last_event_id ELSE last_event_id END,updated_at=excluded.updated_at`, version, f.EventID, formatTime(time.Now()))
		return err
	})
}

func (r *SQLiteRepository) DependenciesForUpstream(ctx context.Context, version, projectID, taskID string) ([]Dependency, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT project_id,plan_id,upstream_task_id,downstream_task_id,source_event_id,occurred_at FROM collaboration_effect_dependencies WHERE rule_version=? AND project_id=? AND upstream_task_id=? ORDER BY source_event_id`, version, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dependency
	for rows.Next() {
		var d Dependency
		var at string
		if err := rows.Scan(&d.ProjectID, &d.PlanID, &d.UpstreamTaskID, &d.DownstreamTaskID, &d.SourceEventID, &at); err != nil {
			return nil, err
		}
		d.OccurredAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) List(ctx context.Context, f Filter) ([]Effect, string, error) {
	version := f.RuleVersion
	if version == "" {
		var err error
		version, err = r.ActiveVersion(ctx)
		if err != nil {
			return nil, "", err
		}
	}
	q := strings.Builder{}
	q.WriteString(`SELECT effect_id,project_id,target_task_id,source_agent_ref,target_agent_ref,relation_type,polarity,magnitude,confidence,occurred_at,rule_version,evidence_event_ids,before_state,after_state,explanation_key FROM collaboration_effects WHERE rule_version=?`)
	args := []any{version}
	add := func(clause string, v any) { q.WriteString(clause); args = append(args, v) }
	if f.ProjectID != "" {
		add(" AND project_id=?", f.ProjectID)
	}
	if f.TaskID != "" {
		add(" AND target_task_id=?", f.TaskID)
	}
	if f.AgentRef != "" {
		q.WriteString(" AND (source_agent_ref=? OR target_agent_ref=?)")
		args = append(args, f.AgentRef, f.AgentRef)
	}
	if f.RelationType != "" {
		add(" AND relation_type=?", f.RelationType)
	}
	if f.Polarity != "" {
		add(" AND polarity=?", f.Polarity)
	}
	if f.Since != nil {
		add(" AND occurred_at>=?", formatTime(*f.Since))
	}
	if f.Until != nil {
		add(" AND occurred_at<?", formatTime(*f.Until))
	}
	if f.Cursor != "" {
		add(" AND effect_id>?", f.Cursor)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		return nil, "", fmt.Errorf("collaboration effect: limit %d exceeds 500", limit)
	}
	q.WriteString(" ORDER BY effect_id LIMIT ?")
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []Effect
	for rows.Next() {
		var e Effect
		var at, evidence, before, after string
		if err := rows.Scan(&e.EffectID, &e.ProjectID, &e.TargetTaskID, &e.SourceAgentRef, &e.TargetAgentRef, &e.RelationType, &e.Polarity, &e.Magnitude, &e.Confidence, &at, &e.RuleVersion, &evidence, &before, &after, &e.ExplanationKey); err != nil {
			return nil, "", err
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, at)
		_ = json.Unmarshal([]byte(evidence), &e.EvidenceEventIDs)
		_ = json.Unmarshal([]byte(before), &e.BeforeState)
		_ = json.Unmarshal([]byte(after), &e.AfterState)
		out = append(out, e)
	}
	next := ""
	if len(out) > limit {
		next = out[limit-1].EffectID
		out = out[:limit]
	}
	return out, next, rows.Err()
}

func (r *SQLiteRepository) ReplaceVersion(ctx context.Context, from, to string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO collaboration_effect_active(id,rule_version,updated_at) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET rule_version=excluded.rule_version,updated_at=excluded.updated_at`, to, formatTime(time.Now()))
	return err
}
func (r *SQLiteRepository) ActiveVersion(ctx context.Context) (string, error) {
	var v string
	err := r.db.QueryRowContext(ctx, `SELECT rule_version FROM collaboration_effect_active WHERE id=1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return RuleVersionV1, nil
	}
	return v, err
}
func (r *SQLiteRepository) DeleteVersion(ctx context.Context, v string) error {
	return persistence.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		exec, err := persistence.ExecutorFromCtx(txCtx, r.db)
		if err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM collaboration_effects WHERE rule_version=?`, `DELETE FROM collaboration_effect_dependencies WHERE rule_version=?`, `DELETE FROM collaboration_effect_diagnostics WHERE rule_version=?`, `DELETE FROM collaboration_effect_checkpoints WHERE rule_version=?`} {
			if _, err = exec.ExecContext(txCtx, q, v); err != nil {
				return err
			}
		}
		return nil
	})
}
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
