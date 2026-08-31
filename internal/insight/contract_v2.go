package insight

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

var V2DeliveryBreakKinds = []string{
	"delivery_sha_lineage_mismatch",
	"done_plan_non_terminal_task",
	"done_plan_open_issue",
	"evolution_old_generation_residue",
	"issue_without_task",
	"task_multiple_containers",
	"task_without_plan",
}

func (s *Service) V2Overview(ctx context.Context, orgID string, asOf time.Time) (V2OverviewResponse, error) {
	env := s.v2Envelope(ctx, asOf, 0, nil, 0, true)
	execCount, unknown, err := s.v2ExecutionCount(ctx, orgID, "", "", asOf)
	if err != nil {
		return V2OverviewResponse{}, err
	}
	agents, err := s.V2Agents(ctx, orgID, asOf)
	if err != nil {
		return V2OverviewResponse{}, err
	}
	projects, err := s.V2Projects(ctx, orgID, asOf)
	if err != nil {
		return V2OverviewResponse{}, err
	}
	env.Meta.SampleCount = execCount
	env.Meta.UnknownCount = unknown
	env.Health = v2Health(env.Meta)
	return V2OverviewResponse{V2WindowedEnvelope: env, Executions: s.v2Count(execCount, execCount, nil, unknown, true, asOf), Agents: agents, Projects: projects}, nil
}

func (s *Service) V2Agents(ctx context.Context, orgID string, asOf time.Time) ([]V2EntitySummary, error) {
	rows, err := s.sqlite.QueryContext(ctx, `SELECT id, name FROM agents WHERE organization_id=? ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	type entityRow struct{ id, name string }
	var raw []entityRow
	for rows.Next() {
		var r entityRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		raw = append(raw, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []V2EntitySummary
	for _, r := range raw {
		ref := normalizeAgent(r.id)
		execCount, unknown, err := s.v2ExecutionCount(ctx, orgID, ref, "", asOf)
		if err != nil {
			return nil, err
		}
		entity := V2EntitySummary{ID: ref, Name: nullableString(r.name), ExecutionCount: s.v2Count(execCount, execCount, nil, unknown, true, asOf)}
		entity.Health = v2Health(entity.ExecutionCount.Meta)
		entity.ReasonCodes = entity.Health.ReasonCodes
		out = append(out, entity)
	}
	return out, nil
}

func (s *Service) V2Projects(ctx context.Context, orgID string, asOf time.Time) ([]V2EntitySummary, error) {
	rows, err := s.sqlite.QueryContext(ctx, `SELECT id, name FROM pm_projects WHERE organization_id=? ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	type entityRow struct{ id, name string }
	var raw []entityRow
	for rows.Next() {
		var r entityRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		raw = append(raw, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []V2EntitySummary
	for _, r := range raw {
		execCount, unknown, err := s.v2ExecutionCount(ctx, orgID, "", r.id, asOf)
		if err != nil {
			return nil, err
		}
		openIssues, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_issues i JOIN pm_projects p ON p.id=i.project_id WHERE p.organization_id=? AND p.id=? AND i.status IN ('open','in_progress')`, orgID, r.id)
		blockedTasks, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_tasks t JOIN pm_projects p ON p.id=t.project_id WHERE p.organization_id=? AND p.id=? AND t.status='blocked'`, orgID, r.id)
		activePlans, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_plans pl JOIN pm_projects p ON p.id=pl.project_id WHERE p.organization_id=? AND p.id=? AND pl.status IN ('pending','running','paused')`, orgID, r.id)
		entity := V2EntitySummary{
			ID: r.id, Name: nullableString(r.name),
			ExecutionCount: s.v2Count(execCount, execCount, nil, unknown, true, asOf),
			OpenIssues:     s.v2Count(openIssues, openIssues, fullCoverage(), 0, true, asOf),
			BlockedTasks:   s.v2Count(blockedTasks, blockedTasks, fullCoverage(), 0, true, asOf),
			ActivePlans:    s.v2Count(activePlans, activePlans, fullCoverage(), 0, true, asOf),
		}
		entity.Health = v2Health(entity.ExecutionCount.Meta)
		entity.ReasonCodes = entity.Health.ReasonCodes
		out = append(out, entity)
	}
	return out, nil
}

func (s *Service) V2Agent(ctx context.Context, orgID, agentRef string, asOf time.Time) (V2EntitySummary, error) {
	agents, err := s.V2Agents(ctx, orgID, asOf)
	if err != nil {
		return V2EntitySummary{}, err
	}
	for _, a := range agents {
		if a.ID == agentRef {
			return a, nil
		}
	}
	return V2EntitySummary{}, ErrExecutionNotFound
}

func (s *Service) V2Project(ctx context.Context, orgID, projectID string, asOf time.Time) (V2EntitySummary, error) {
	projects, err := s.V2Projects(ctx, orgID, asOf)
	if err != nil {
		return V2EntitySummary{}, err
	}
	for _, p := range projects {
		if p.ID == projectID {
			return p, nil
		}
	}
	return V2EntitySummary{}, ErrExecutionNotFound
}

func (s *Service) V2ProjectDelivery(ctx context.Context, orgID, projectID string, asOf time.Time) (V2ProjectDeliveryResponse, error) {
	if ok, err := s.projectInOrg(ctx, orgID, projectID); err != nil || !ok {
		if err != nil {
			return V2ProjectDeliveryResponse{}, err
		}
		return V2ProjectDeliveryResponse{}, ErrExecutionNotFound
	}
	issues, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_issues WHERE project_id=? AND status IN ('open','in_progress','resolved','closed')`, projectID)
	tasks, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_tasks WHERE project_id=?`, projectID)
	plans, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_plans WHERE project_id=?`, projectID)
	done, _ := s.sqliteCount(ctx, `SELECT COUNT(DISTINCT pl.id) FROM pm_plans pl
		WHERE pl.project_id=? AND pl.status='done'
		AND NOT EXISTS (SELECT 1 FROM pm_tasks t WHERE t.plan_id=pl.id AND t.status NOT IN ('completed','failed','discarded','verified'))
		AND NOT EXISTS (SELECT 1 FROM pm_tasks t JOIN pm_issues i ON i.id=t.derived_from_issue WHERE t.plan_id=pl.id AND i.status NOT IN ('resolved','closed'))`, projectID)
	breaks, unknown, err := s.v2DeliveryBreaks(ctx, projectID, asOf)
	if err != nil {
		return V2ProjectDeliveryResponse{}, err
	}
	env := s.v2Envelope(ctx, asOf, issues+tasks+plans+done, fullCoverage(), unknown, true)
	return V2ProjectDeliveryResponse{
		V2WindowedEnvelope: env,
		ProjectID:          projectID,
		Funnel: V2Funnel{
			Issues: s.v2Count(issues, issues, fullCoverage(), 0, true, asOf),
			Tasks:  s.v2Count(tasks, tasks, fullCoverage(), 0, true, asOf),
			Plans:  s.v2Count(plans, plans, fullCoverage(), 0, true, asOf),
			Done:   s.v2Count(done, done, fullCoverage(), 0, true, asOf),
			Breaks: breaks,
		},
	}, nil
}

func (s *Service) V2ProjectEvolution(ctx context.Context, orgID, projectID string, asOf time.Time) (V2EvolutionResponse, error) {
	if ok, err := s.projectInOrg(ctx, orgID, projectID); err != nil || !ok {
		if err != nil {
			return V2EvolutionResponse{}, err
		}
		return V2EvolutionResponse{}, ErrExecutionNotFound
	}
	plans, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_plans WHERE project_id=?`, projectID)
	evolved, _ := s.sqliteCount(ctx, `SELECT COUNT(DISTINCT g.plan_id) FROM pm_plan_generations g JOIN pm_plans p ON p.id=g.plan_id WHERE p.project_id=? AND g.parent_generation_id<>''`, projectID)
	gens, _ := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_plan_generations g JOIN pm_plans p ON p.id=g.plan_id WHERE p.project_id=?`, projectID)
	rework, err := s.v2EvolutionReworkCount(ctx, projectID)
	if err != nil {
		return V2EvolutionResponse{}, err
	}
	recoveryAttempts, recoverySuccesses, err := s.v2EvolutionRecovery(ctx, projectID)
	if err != nil {
		return V2EvolutionResponse{}, err
	}
	maxLoopDepth, err := s.v2EvolutionMaxLoopDepth(ctx, projectID)
	if err != nil {
		return V2EvolutionResponse{}, err
	}
	residue, err := s.v2EvolutionResidue(ctx, projectID)
	if err != nil {
		return V2EvolutionResponse{}, err
	}
	env := s.v2Envelope(ctx, asOf, gens, fullCoverage(), 0, true)
	summary := V2EvolutionSummary{
		Plans:                 plans,
		EvolvedPlans:          evolved,
		EvolutionRate:         ratioPtr(evolved, plans),
		GenerationCount:       gens,
		ReworkCount:           rework,
		ReworkRatio:           ratioPtr(rework, gens),
		RecoveryAttempts:      recoveryAttempts,
		RecoverySuccesses:     recoverySuccesses,
		RecoveryEffectiveness: ratioPtr(recoverySuccesses, recoveryAttempts),
		MaxLoopDepth:          maxLoopDepth,
		StaleOrphanResidue:    residue,
		AnomalyDrilldowns: V2EvolutionAnomalyDrilldowns{
			Rework:    map[string]any{"project_id": projectID, "anomaly_kind": "rework", "generation_parent": "non_empty", "reason_in": []string{"review_reject", "execution_failure"}},
			Recovery:  map[string]any{"project_id": projectID, "anomaly_kind": "recovery", "generation_parent": "non_empty", "reason_in": []string{"blocked", "review_reject", "execution_failure"}},
			LoopDepth: map[string]any{"project_id": projectID, "anomaly_kind": "loop_depth", "metric": "max_loop_depth", "generation_parent": "non_empty"},
			Residue:   map[string]any{"project_id": projectID, "anomaly_kind": "residue", "task_status_in": []string{"open", "assigned", "running", "blocked", "pool"}, "generation_parent": "non_empty"},
		},
	}
	return V2EvolutionResponse{V2WindowedEnvelope: env, ProjectID: projectID, Evolution: summary}, nil
}

func (s *Service) V2PlanLineage(ctx context.Context, orgID, projectID, planID string, asOf time.Time) (V2PlanLineageResponse, error) {
	if ok, err := s.planInProjectOrg(ctx, orgID, projectID, planID); err != nil || !ok {
		if err != nil {
			return V2PlanLineageResponse{}, err
		}
		return V2PlanLineageResponse{}, ErrExecutionNotFound
	}
	rows, err := s.sqlite.QueryContext(ctx, `SELECT id, parent_generation_id, reason, evidence, creator_ref, diff_json, created_at
		FROM pm_plan_generations WHERE plan_id=? ORDER BY created_at, id`, planID)
	if err != nil {
		return V2PlanLineageResponse{}, err
	}
	defer rows.Close()
	var out []V2Generation
	for rows.Next() {
		var id, parent, reason, evidenceRaw, creator, diffRaw, created string
		if err := rows.Scan(&id, &parent, &reason, &evidenceRaw, &creator, &diffRaw, &created); err != nil {
			return V2PlanLineageResponse{}, err
		}
		evidence := decodeObjectArray(evidenceRaw)
		changes := decodeObjectArray(diffRaw)
		g := V2Generation{Generation: len(out), CreatedAt: created, TriggeredBy: creator, Reason: normalizeEvolutionReason(reason), Evidence: evidence, NodeChanges: changes, RecoveryOutcome: "unknown"}
		branch, sha, verdict := s.latestDeliveryForGeneration(ctx, planID, created)
		g.DeliveryBranch, g.DeliverySHA, g.AcceptanceVerdict = branch, sha, verdict
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return V2PlanLineageResponse{}, err
	}
	env := s.v2Envelope(ctx, asOf, int64(len(out)), fullCoverage(), 0, len(out) > 0)
	return V2PlanLineageResponse{V2WindowedEnvelope: env, ProjectID: projectID, PlanID: planID, Generations: out}, nil
}

func (s *Service) v2ExecutionCount(ctx context.Context, orgID, agentRef, projectID string, asOf time.Time) (int64, int64, error) {
	start := asOf.Add(-24 * time.Hour)
	where := `organization_id=? AND COALESCE(finished_at, started_at, queued_at) >= CAST(? AS TIMESTAMPTZ) AND COALESCE(finished_at, started_at, queued_at) < CAST(? AS TIMESTAMPTZ)`
	args := []any{orgID, fmtTS(start), fmtTS(asOf)}
	if agentRef != "" {
		where += ` AND agent_ref=?`
		args = append(args, agentRef)
	}
	if projectID != "" {
		where += ` AND project_id=?`
		args = append(args, projectID)
	}
	var count, unknown int64
	err := s.duck.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE finished_at IS NOT NULL AND COALESCE(outcome,'') NOT IN ('succeeded','failed','crashed','quiet_finalized')) FROM execution_fact WHERE `+where, args...).Scan(&count, &unknown)
	return count, unknown, err
}

func (s *Service) v2DeliveryBreaks(ctx context.Context, projectID string, asOf time.Time) ([]V2FunnelBreak, int64, error) {
	counts := map[string]int64{}
	queries := map[string]string{
		"issue_without_task":               `SELECT COUNT(*) FROM pm_issues i WHERE i.project_id=? AND i.status IN ('open','in_progress') AND NOT EXISTS (SELECT 1 FROM pm_tasks t WHERE t.derived_from_issue=i.id)`,
		"task_without_plan":                `SELECT COUNT(*) FROM pm_tasks t WHERE t.project_id=? AND COALESCE(t.plan_id,'')='' AND t.status NOT IN ('completed','failed','discarded','verified')`,
		"task_multiple_containers":         `SELECT COUNT(*) FROM pm_tasks t WHERE t.project_id=? AND COALESCE(t.plan_id,'')<>'' AND EXISTS (SELECT 1 FROM pm_assignment_pool_tasks ap WHERE ap.task_id=t.id)`,
		"done_plan_non_terminal_task":      `SELECT COUNT(DISTINCT pl.id) FROM pm_plans pl JOIN pm_tasks t ON t.plan_id=pl.id WHERE pl.project_id=? AND pl.status='done' AND t.status NOT IN ('completed','failed','discarded','verified')`,
		"done_plan_open_issue":             `SELECT COUNT(DISTINCT pl.id) FROM pm_plans pl JOIN pm_tasks t ON t.plan_id=pl.id JOIN pm_issues i ON i.id=t.derived_from_issue WHERE pl.project_id=? AND pl.status='done' AND i.status IN ('open','in_progress')`,
		"evolution_old_generation_residue": `SELECT COUNT(*) FROM pm_tasks t WHERE t.project_id=? AND t.status IN ('open','assigned','running','blocked','pool') AND EXISTS (SELECT 1 FROM pm_plan_generations g JOIN pm_plans p ON p.id=g.plan_id WHERE p.id=t.plan_id AND p.project_id=t.project_id AND g.parent_generation_id<>'')`,
		"delivery_sha_lineage_mismatch":    `SELECT COUNT(*) FROM pm_delivery_subjects ds JOIN pm_plans p ON p.id=ds.plan_id WHERE p.project_id=? AND ds.candidate_sha NOT GLOB '[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'`,
	}
	var unknown int64
	for kind, q := range queries {
		n, err := s.sqliteCount(ctx, q, projectID)
		if err != nil {
			return nil, 0, err
		}
		counts[kind] = n
		unknown += n
	}
	kinds := append([]string(nil), V2DeliveryBreakKinds...)
	sort.Strings(kinds)
	out := make([]V2FunnelBreak, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, V2FunnelBreak{Kind: kind, Count: s.v2Count(counts[kind], counts[kind], fullCoverage(), 0, true, asOf), Drilldown: map[string]any{"project_id": projectID, "break_kind": kind}})
	}
	return out, unknown, nil
}

func (s *Service) v2EvolutionReworkCount(ctx context.Context, projectID string) (int64, error) {
	return s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_plan_generations g JOIN pm_plans p ON p.id=g.plan_id WHERE p.project_id=? AND g.parent_generation_id<>'' AND g.reason IN ('review_reject','execution_failure')`, projectID)
}

func (s *Service) v2EvolutionRecovery(ctx context.Context, projectID string) (int64, int64, error) {
	attempts, err := s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_plan_generations g JOIN pm_plans p ON p.id=g.plan_id WHERE p.project_id=? AND g.parent_generation_id<>'' AND g.reason IN ('blocked','review_reject','execution_failure')`, projectID)
	if err != nil {
		return 0, 0, err
	}
	successes, err := s.sqliteCount(ctx, `SELECT COUNT(DISTINCT g.id) FROM pm_plan_generations g
		JOIN pm_plans p ON p.id=g.plan_id
		JOIN pm_delivery_subjects ds ON ds.plan_id=g.plan_id AND ds.created_at>=g.created_at
		JOIN pm_acceptances a ON a.subject_id=ds.id
		WHERE p.project_id=? AND g.parent_generation_id<>'' AND g.reason IN ('blocked','review_reject','execution_failure')
		AND a.verdict IN ('passed','waived_by_authority')`, projectID)
	if err != nil {
		return 0, 0, err
	}
	return attempts, successes, nil
}

func (s *Service) v2EvolutionMaxLoopDepth(ctx context.Context, projectID string) (int64, error) {
	rows, err := s.sqlite.QueryContext(ctx, `SELECT COUNT(*) FROM pm_plan_generations g JOIN pm_plans p ON p.id=g.plan_id WHERE p.project_id=? AND g.parent_generation_id<>'' GROUP BY g.plan_id`, projectID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var max int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
		if n > max {
			max = n
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return max, nil
}

func (s *Service) v2EvolutionResidue(ctx context.Context, projectID string) (int64, error) {
	return s.sqliteCount(ctx, `SELECT COUNT(*) FROM pm_tasks t WHERE t.project_id=? AND t.status IN ('open','assigned','running','blocked','pool') AND EXISTS (SELECT 1 FROM pm_plan_generations g JOIN pm_plans p ON p.id=g.plan_id WHERE p.id=t.plan_id AND p.project_id=t.project_id AND g.parent_generation_id<>'')`, projectID)
}

func ratioPtr(numerator, denominator int64) *float64 {
	if denominator <= 0 || numerator < 0 || numerator > denominator {
		return nil
	}
	v := float64(numerator) / float64(denominator)
	return &v
}

func (s *Service) v2Envelope(ctx context.Context, asOf time.Time, sample int64, coverage *float64, unknown int64, known bool) V2WindowedEnvelope {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	_, fresh := s.freshness(ctx, asOf)
	meta := V2Meta{MetricVersion: MetricVersionV2, SampleCount: sample, Coverage: coverage, Freshness: fresh, UnknownCount: unknown, Known: known}
	return V2WindowedEnvelope{MetricVersion: MetricVersionV2, TimeWindow: makeWindow(asOf), AsOf: fmtTS(asOf), Meta: meta, Health: v2Health(meta)}
}

func (s *Service) v2Count(value, sample int64, coverage *float64, unknown int64, known bool, asOf time.Time) V2CountMetric {
	_, fresh := s.freshness(context.Background(), asOf)
	v := value
	if !known {
		return V2CountMetric{Value: nil, Meta: V2Meta{MetricVersion: MetricVersionV2, SampleCount: sample, Coverage: coverage, Freshness: fresh, UnknownCount: unknown, Known: false}}
	}
	return V2CountMetric{Value: &v, Meta: V2Meta{MetricVersion: MetricVersionV2, SampleCount: sample, Coverage: coverage, Freshness: fresh, UnknownCount: unknown, Known: true}}
}

func v2Health(meta V2Meta) V2Health {
	reasons := map[string]struct{}{}
	status := "healthy"
	if !meta.Known {
		status = "unknown"
		reasons["metric_unknown"] = struct{}{}
	}
	if meta.Coverage == nil {
		status = "unknown"
		reasons["coverage_unknown"] = struct{}{}
	} else if *meta.Coverage < 0.90 {
		status = "unknown"
		reasons["coverage_low"] = struct{}{}
	}
	if meta.Freshness.State != "fresh" {
		status = "unknown"
		reasons["freshness_"+meta.Freshness.State] = struct{}{}
	}
	if meta.UnknownCount > 0 {
		status = "unknown"
		reasons["unknown_source_state"] = struct{}{}
	}
	codes := make([]string, 0, len(reasons))
	for c := range reasons {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return V2Health{Status: status, ReasonCodes: codes, Evidence: []map[string]any{}}
}

func (s *Service) sqliteCount(ctx context.Context, q string, args ...any) (int64, error) {
	var n int64
	err := s.sqlite.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

func (s *Service) projectInOrg(ctx context.Context, orgID, projectID string) (bool, error) {
	var one int
	err := s.sqlite.QueryRowContext(ctx, `SELECT 1 FROM pm_projects WHERE organization_id=? AND id=?`, orgID, projectID).Scan(&one)
	if errorsIsNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) planInProjectOrg(ctx context.Context, orgID, projectID, planID string) (bool, error) {
	var one int
	err := s.sqlite.QueryRowContext(ctx, `SELECT 1 FROM pm_plans pl JOIN pm_projects p ON p.id=pl.project_id WHERE p.organization_id=? AND p.id=? AND pl.id=?`, orgID, projectID, planID).Scan(&one)
	if errorsIsNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

func fullCoverage() *float64 {
	v := 1.0
	return &v
}

func decodeObjectArray(raw string) []map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []map[string]any{}
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		return []map[string]any{obj}
	}
	return []map[string]any{{"raw": raw}}
}

func normalizeEvolutionReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "blocked", "review_reject", "requirement_change", "execution_failure", "manual_adjustment":
		return reason
	default:
		return "unknown"
	}
}

func (s *Service) latestDeliveryForGeneration(ctx context.Context, planID, createdAt string) (string, string, string) {
	var branch, sha string
	_ = s.sqlite.QueryRowContext(ctx, `SELECT branch, candidate_sha FROM pm_delivery_subjects WHERE plan_id=? AND created_at>=? ORDER BY created_at DESC, id DESC LIMIT 1`, planID, createdAt).Scan(&branch, &sha)
	verdict := "pending"
	if sha != "" {
		var v string
		_ = s.sqlite.QueryRowContext(ctx, `SELECT a.verdict FROM pm_acceptances a JOIN pm_delivery_subjects ds ON ds.id=a.subject_id WHERE ds.plan_id=? AND ds.candidate_sha=? ORDER BY a.authority_rank DESC, a.created_at DESC, a.id DESC LIMIT 1`, planID, sha).Scan(&v)
		switch v {
		case "passed", "waived_by_authority":
			verdict = "pass"
		case "rejected":
			verdict = "reject"
		}
	}
	return branch, sha, verdict
}
