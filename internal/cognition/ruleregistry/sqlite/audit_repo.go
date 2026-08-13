package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/ruleregistry"
	"github.com/oopslink/agent-center/internal/persistence"
)

var ErrAuditScopeRequired = errors.New("team rule audit: execution_id or planning_session_id required")

// AuditRepo stores team_rule.loaded read facts on SQLite.
type AuditRepo struct {
	db *sql.DB
}

func NewAuditRepo(db *sql.DB) *AuditRepo { return &AuditRepo{db: db} }

var _ ruleregistry.AuditRepository = (*AuditRepo)(nil)

func (r *AuditRepo) AppendLoaded(ctx context.Context, in ruleregistry.LoadAudit) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("team rule audit repo: nil db")
	}
	a := ruleregistry.NormalizeLoadAudit(in)
	if a.ExecutionID == "" && a.PlanningSessionID == "" {
		return false, ErrAuditScopeRequired
	}
	if a.TeamID == "" || a.TeamMemoryCommit == "" || a.RuleSlug == "" || a.Phase == "" {
		return false, errors.New("team rule audit: team_id, team_memory_commit, rule_slug and phase are required")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO team_rule_load_audits (
		execution_id, planning_session_id, team_id, team_memory_commit, rule_slug,
		phase, agent_id, loaded_at
	) VALUES (?,?,?,?,?,?,?,?)`,
		a.ExecutionID, a.PlanningSessionID, a.TeamID, a.TeamMemoryCommit, a.RuleSlug,
		a.Phase, a.AgentID, a.LoadedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if persistence.IsUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *AuditRepo) ListByExecutionIDs(ctx context.Context, executionIDs []string) (map[string][]ruleregistry.LoadAudit, error) {
	out := map[string][]ruleregistry.LoadAudit{}
	if r == nil || r.db == nil {
		return out, nil
	}
	ids := cleanExecutionIDs(executionIDs)
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		placeholders[i] = "?"
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, `SELECT execution_id, planning_session_id, team_id,
		team_memory_commit, rule_slug, phase, agent_id, loaded_at
		FROM team_rule_load_audits
		WHERE execution_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY execution_id ASC, rule_slug ASC, loaded_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanLoadAudit(rows)
		if err != nil {
			return nil, err
		}
		out[a.ExecutionID] = append(out[a.ExecutionID], a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool {
			if out[id][i].RuleSlug != out[id][j].RuleSlug {
				return out[id][i].RuleSlug < out[id][j].RuleSlug
			}
			return out[id][i].LoadedAt.Before(out[id][j].LoadedAt)
		})
	}
	return out, nil
}

func cleanExecutionIDs(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

type loadAuditScanner interface {
	Scan(dest ...any) error
}

func scanLoadAudit(row loadAuditScanner) (ruleregistry.LoadAudit, error) {
	var a ruleregistry.LoadAudit
	var loaded string
	if err := row.Scan(&a.ExecutionID, &a.PlanningSessionID, &a.TeamID,
		&a.TeamMemoryCommit, &a.RuleSlug, &a.Phase, &a.AgentID, &loaded); err != nil {
		return a, err
	}
	if loaded != "" {
		t, err := time.Parse(time.RFC3339Nano, loaded)
		if err != nil {
			return a, fmt.Errorf("parse loaded_at: %w", err)
		}
		a.LoadedAt = t.UTC()
	}
	return a, nil
}
