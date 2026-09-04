package collaborationeffect

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type graphScopeReader interface {
	ReadGraphScope(context.Context, Filter, string) (graphScope, error)
}

type graphScope struct {
	Effects    []Effect
	Projects   []projectRow
	Plans      []planRow
	Stages     []stageRow
	Tasks      []taskRow
	Deps       []dependencyRow
	NextCursor string
}

type projectRow struct {
	ID, OrganizationID, Name string
}
type planRow struct {
	ID, ProjectID, Name, Status string
}
type stageRow struct {
	ID, PlanID, Name string
}
type taskRow struct {
	ID, ProjectID, PlanID, StageID, Title, Status, Assignee string
}
type dependencyRow struct {
	PlanID, FromTaskID, ToTaskID, Kind, When string
	MaxRounds                                int
}

type SQLiteGraphReader struct{ db *sql.DB }

func NewSQLiteGraphReader(db *sql.DB) (*SQLiteGraphReader, error) {
	if db == nil {
		return nil, errors.New("collaboration insight graph: nil db")
	}
	return &SQLiteGraphReader{db: db}, nil
}

func (r *SQLiteGraphReader) ReadGraphScope(ctx context.Context, f Filter, version string) (graphScope, error) {
	if strings.TrimSpace(f.OrganizationID) == "" && strings.TrimSpace(f.ProjectID) == "" {
		return graphScope{}, fmt.Errorf("%w: organization_id or project_id required", ErrInvalidQuery)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > MaxQueryLimit {
		return graphScope{}, fmt.Errorf("%w: limit must be 1..%d", ErrInvalidQuery, MaxQueryLimit)
	}
	var cursorAt, cursorID string
	if f.Cursor != "" {
		var err error
		cursorAt, cursorID, err = decodeGraphCursor(f.Cursor)
		if err != nil {
			return graphScope{}, ErrInvalidCursor
		}
	}
	q := strings.Builder{}
	q.WriteString(`SELECT ce.effect_id,ce.project_id,ce.target_task_id,ce.source_agent_ref,ce.target_agent_ref,ce.relation_type,ce.polarity,ce.magnitude,ce.confidence,ce.occurred_at,ce.rule_version,ce.evidence_event_ids,ce.before_state,ce.after_state,ce.explanation_key
FROM collaboration_effects ce
JOIN pm_projects pr ON pr.id=ce.project_id
LEFT JOIN pm_tasks t ON t.id=ce.target_task_id
WHERE ce.rule_version=?`)
	args := []any{version}
	add := func(clause string, v any) { q.WriteString(clause); args = append(args, v) }
	if f.OrganizationID != "" {
		add(" AND pr.organization_id=?", f.OrganizationID)
	}
	if f.ProjectID != "" {
		add(" AND ce.project_id=?", f.ProjectID)
	}
	if f.PlanID != "" {
		add(" AND t.plan_id=?", f.PlanID)
	}
	if f.TaskID != "" {
		add(" AND ce.target_task_id=?", f.TaskID)
	}
	if f.StageID != "" {
		add(" AND t.stage_id=?", f.StageID)
	}
	if f.AgentRef != "" {
		q.WriteString(" AND (ce.source_agent_ref=? OR ce.target_agent_ref=? OR t.assignee=?)")
		args = append(args, f.AgentRef, f.AgentRef, f.AgentRef)
	}
	if f.RelationType != "" {
		add(" AND ce.relation_type=?", f.RelationType)
	}
	if f.Polarity != "" {
		add(" AND ce.polarity=?", f.Polarity)
	}
	if f.Since != nil {
		add(" AND ce.occurred_at>=?", formatTime(*f.Since))
	}
	if f.Until != nil {
		add(" AND ce.occurred_at<?", formatTime(*f.Until))
	}
	if cursorID != "" {
		if cursorAt == "" {
			add(" AND ce.effect_id>?", cursorID)
		} else {
			q.WriteString(" AND (ce.occurred_at>? OR (ce.occurred_at=? AND ce.effect_id>?))")
			args = append(args, cursorAt, cursorAt, cursorID)
		}
	}
	q.WriteString(" ORDER BY ce.occurred_at, ce.effect_id LIMIT ?")
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return graphScope{}, err
	}
	defer rows.Close()
	var scope graphScope
	for rows.Next() {
		e, err := scanEffect(rows.Scan)
		if err != nil {
			return graphScope{}, err
		}
		scope.Effects = append(scope.Effects, e)
	}
	if err := rows.Err(); err != nil {
		return graphScope{}, err
	}
	if len(scope.Effects) > limit {
		last := scope.Effects[limit-1]
		scope.NextCursor = encodeGraphCursor(formatTime(last.OccurredAt), last.EffectID)
		scope.Effects = scope.Effects[:limit]
	}
	if err := r.readStructure(ctx, f, &scope); err != nil {
		return graphScope{}, err
	}
	return scope, nil
}

func (r *SQLiteGraphReader) readStructure(ctx context.Context, f Filter, scope *graphScope) error {
	where, projectArgs := projectScopeWhere(f, "pm_projects")
	projectRows, err := r.db.QueryContext(ctx, `SELECT id, organization_id, name FROM pm_projects `+where+` ORDER BY organization_id, id`, projectArgs...)
	if err != nil {
		return err
	}
	for projectRows.Next() {
		var p projectRow
		if err := projectRows.Scan(&p.ID, &p.OrganizationID, &p.Name); err != nil {
			_ = projectRows.Close()
			return err
		}
		scope.Projects = append(scope.Projects, p)
	}
	if err := projectRows.Close(); err != nil {
		return err
	}
	if err := projectRows.Err(); err != nil {
		return err
	}

	planWhere, projectArgs := projectScopeWhere(f, "pr")
	qPlans := `SELECT pl.id, pl.project_id, pl.name, pl.status
FROM pm_plans pl JOIN pm_projects pr ON pr.id=pl.project_id ` + planWhere
	planArgs := append([]any(nil), projectArgs...)
	if f.PlanID != "" {
		qPlans += " AND pl.id=?"
		planArgs = append(planArgs, f.PlanID)
	}
	qPlans += " ORDER BY pl.project_id, pl.id"
	planRows, err := r.db.QueryContext(ctx, qPlans, planArgs...)
	if err != nil {
		return err
	}
	for planRows.Next() {
		var p planRow
		if err := planRows.Scan(&p.ID, &p.ProjectID, &p.Name, &p.Status); err != nil {
			_ = planRows.Close()
			return err
		}
		scope.Plans = append(scope.Plans, p)
	}
	if err := planRows.Close(); err != nil {
		return err
	}
	if err := planRows.Err(); err != nil {
		return err
	}

	taskWhere, projectArgs := projectScopeWhere(f, "pr")
	taskArgs := append([]any(nil), projectArgs...)
	qTasks := `SELECT t.id, t.project_id, COALESCE(t.plan_id,''), COALESCE(t.stage_id,''), t.title, t.status, COALESCE(t.assignee,'')
FROM pm_tasks t JOIN pm_projects pr ON pr.id=t.project_id ` + taskWhere
	if f.PlanID != "" {
		qTasks += " AND t.plan_id=?"
		taskArgs = append(taskArgs, f.PlanID)
	}
	if f.TaskID != "" {
		qTasks += " AND t.id=?"
		taskArgs = append(taskArgs, f.TaskID)
	}
	if f.StageID != "" {
		qTasks += " AND t.stage_id=?"
		taskArgs = append(taskArgs, f.StageID)
	}
	if f.AgentRef != "" {
		qTasks += " AND t.assignee=?"
		taskArgs = append(taskArgs, f.AgentRef)
	}
	qTasks += " ORDER BY t.project_id, t.plan_id, t.stage_id, t.id"
	taskRows, err := r.db.QueryContext(ctx, qTasks, taskArgs...)
	if err != nil {
		return err
	}
	for taskRows.Next() {
		var t taskRow
		if err := taskRows.Scan(&t.ID, &t.ProjectID, &t.PlanID, &t.StageID, &t.Title, &t.Status, &t.Assignee); err != nil {
			_ = taskRows.Close()
			return err
		}
		scope.Tasks = append(scope.Tasks, t)
	}
	if err := taskRows.Close(); err != nil {
		return err
	}
	if err := taskRows.Err(); err != nil {
		return err
	}

	stageRows, err := r.db.QueryContext(ctx, `SELECT st.id, st.plan_id, st.name
FROM pm_stages st
JOIN pm_plans pl ON pl.id=st.plan_id
JOIN pm_projects pr ON pr.id=pl.project_id `+planWhere+filterClause("st.id", f.StageID)+filterClause("pl.id", f.PlanID)+`
ORDER BY st.plan_id, st.id`, append(append([]any(nil), projectScopeArgs(f)...), nonEmpty(f.StageID, f.PlanID)...)...)
	if err != nil {
		return err
	}
	for stageRows.Next() {
		var st stageRow
		if err := stageRows.Scan(&st.ID, &st.PlanID, &st.Name); err != nil {
			_ = stageRows.Close()
			return err
		}
		scope.Stages = append(scope.Stages, st)
	}
	if err := stageRows.Close(); err != nil {
		return err
	}
	if err := stageRows.Err(); err != nil {
		return err
	}

	depRows, err := r.db.QueryContext(ctx, `SELECT d.plan_id,d.from_task_id,d.to_task_id,d.kind,d."when",d.max_rounds
FROM pm_task_dependencies d
JOIN pm_plans pl ON pl.id=d.plan_id
JOIN pm_projects pr ON pr.id=pl.project_id `+planWhere+filterClause("d.plan_id", f.PlanID)+`
ORDER BY d.plan_id,d.from_task_id,d.to_task_id`, append(projectScopeArgs(f), nonEmpty(f.PlanID)...)...)
	if err != nil {
		return err
	}
	for depRows.Next() {
		var d dependencyRow
		if err := depRows.Scan(&d.PlanID, &d.FromTaskID, &d.ToTaskID, &d.Kind, &d.When, &d.MaxRounds); err != nil {
			_ = depRows.Close()
			return err
		}
		scope.Deps = append(scope.Deps, d)
	}
	if err := depRows.Close(); err != nil {
		return err
	}
	return depRows.Err()
}

func projectScopeWhere(f Filter, table string) (string, []any) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = "pm_projects"
	}
	args := projectScopeArgs(f)
	clauses := []string{"WHERE 1=1"}
	if f.OrganizationID != "" {
		clauses = append(clauses, "AND "+table+".organization_id=?")
	}
	if f.ProjectID != "" {
		clauses = append(clauses, "AND "+table+".id=?")
	}
	return strings.Join(clauses, " "), args
}

func projectScopeArgs(f Filter) []any {
	args := []any{}
	if f.OrganizationID != "" {
		args = append(args, f.OrganizationID)
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
	}
	return args
}

func filterClause(column, value string) string {
	if value == "" {
		return ""
	}
	return " AND " + column + "=?"
}

func nonEmpty(values ...string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func scanEffect(scan func(...any) error) (Effect, error) {
	var e Effect
	var at, evidence, before, after string
	if err := scan(&e.EffectID, &e.ProjectID, &e.TargetTaskID, &e.SourceAgentRef, &e.TargetAgentRef, &e.RelationType, &e.Polarity, &e.Magnitude, &e.Confidence, &at, &e.RuleVersion, &evidence, &before, &after, &e.ExplanationKey); err != nil {
		return Effect{}, err
	}
	e.OccurredAt, _ = time.Parse(time.RFC3339Nano, at)
	_ = json.Unmarshal([]byte(evidence), &e.EvidenceEventIDs)
	_ = json.Unmarshal([]byte(before), &e.BeforeState)
	_ = json.Unmarshal([]byte(after), &e.AfterState)
	return e, nil
}

func encodeGraphCursor(occurredAt, effectID string) string {
	return "cg_" + base64.RawURLEncoding.EncodeToString([]byte(occurredAt+"|"+effectID))
}

func decodeGraphCursor(cursor string) (string, string, error) {
	if strings.HasPrefix(cursor, "ce_") {
		return "", cursor, nil
	}
	if !strings.HasPrefix(cursor, "cg_") {
		return "", "", ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "cg_"))
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrInvalidCursor
	}
	return parts[0], parts[1], nil
}
