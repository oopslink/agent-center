package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/insight"
	"github.com/oopslink/agent-center/internal/observability/collaborationeffect"
	obssql "github.com/oopslink/agent-center/internal/observability/sqlite"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func TestInsightsOverviewAPI_WindowValidationAndShape(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedInsightHTTPFacts(t, db, sess.OrgID, time.Now().UTC().Add(-time.Minute))
	duckPath := t.TempDir() + "/insight.duckdb"
	svc, err := insight.Open(context.Background(), db, duckPath, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	deps.Insight = svc
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/overview?window=12h", nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid window status = %d, want 400", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/overview?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Window struct {
			Duration string `json:"duration"`
		} `json:"window"`
		RefreshedAt string `json:"refreshed_at"`
		Freshness   struct {
			State       string `json:"state"`
			AgeMS       int64  `json:"age_ms"`
			ThresholdMS int64  `json:"threshold_ms"`
		} `json:"freshness"`
		Summary struct {
			Completed int64 `json:"completed_executions"`
			Failed    int64 `json:"failed_executions"`
		} `json:"summary"`
		Agents []any `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Window.Duration != "24h" || out.Summary.Completed != 1 || out.Summary.Failed != 0 || len(out.Agents) != 1 {
		t.Fatalf("overview body = %+v", out)
	}
	if out.RefreshedAt == "" || out.Freshness.State != "fresh" || out.Freshness.AgeMS < 0 || out.Freshness.ThresholdMS != time.Minute.Milliseconds() {
		t.Fatalf("overview freshness = %+v refreshed_at=%q, want fresh production checkpoint", out.Freshness, out.RefreshedAt)
	}
}

func TestInsightsHTTPReadDoesNotTriggerProjection(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	duckPath := t.TempDir() + "/insight.duckdb"
	svc, err := insight.Open(context.Background(), db, duckPath, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	deps.Insight = svc
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	seedInsightHTTPFacts(t, db, sess.OrgID, time.Now().UTC().Add(-time.Minute))
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/overview?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Summary struct {
			Completed int64 `json:"completed_executions"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Summary.Completed != 0 {
		t.Fatalf("completed from HTTP read = %d, want 0 without projector refresh", out.Summary.Completed)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	duck, err := sql.Open("duckdb", duckPath)
	if err != nil {
		t.Fatal(err)
	}
	defer duck.Close()
	var projected int
	if err := duck.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM projected_event`).Scan(&projected); err != nil {
		t.Fatal(err)
	}
	if projected != 0 {
		t.Fatalf("HTTP read projected_event count = %d, want 0", projected)
	}
}

func TestInsightsAPIUnavailableReturnsFreshnessEnvelope(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Insight = mustInsight(t, db, t.TempDir()+"/missing-parent/insight.duckdb")
	_ = deps.Insight.Close()
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/overview?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d, want 503", resp.StatusCode)
	}
	var out struct {
		Window struct {
			Duration string `json:"duration"`
		} `json:"window"`
		Freshness struct {
			State       string `json:"state"`
			ThresholdMS int64  `json:"threshold_ms"`
		} `json:"freshness"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Window.Duration != "24h" || out.Freshness.State != "unavailable" || out.Error.Code != "insight_unavailable" {
		t.Fatalf("503 envelope = %+v", out)
	}
}

func TestCollaborationEffectsAPI_RealPlanStageGraphAndPlanFilter(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	actor := pm.IdentityRef("user:" + sess.IdentityID)
	deps.PM = pmservice.New(pmservice.Deps{
		DB:         db,
		Projects:   pmsql.NewProjectRepo(db),
		Members:    pmsql.NewProjectMemberRepo(db),
		OrgMembers: deps.MemberRepo,
		Tasks:      pmsql.NewTaskRepo(db),
		Plans:      pmsql.NewPlanRepo(db),
		Stages:     pmsql.NewStageRepo(db),
		TaskSubs:   pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:  pmsql.NewIssueSubscriberRepo(db),
		Outbox:     outboxsql.NewOutboxRepo(db),
		IDGen:      idgen.NewGenerator(clock.SystemClock{}),
		Clock:      clock.SystemClock{},
	})
	projectID, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Graph Project", CreatedBy: actor})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: projectID, Title: "Real task", CreatedBy: actor})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: projectID, Name: "Real Plan", CreatedBy: actor})
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.PM.SelectTaskIntoPlan(ctx, planID, taskID, actor); err != nil {
		t.Fatal(err)
	}
	stageID, err := deps.PM.CreateStage(ctx, pmservice.CreateStageCommand{PlanID: planID, Name: "Real Stage", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.PM.AssignTaskToStage(ctx, planID, taskID, stageID, actor); err != nil {
		t.Fatal(err)
	}
	effectRepo, err := collaborationeffect.NewSQLiteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	events, err := obssql.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	deps.CollaborationInsight, err = collaborationeffect.NewQueryService(effectRepo, events)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	effects := []collaborationeffect.Effect{
		{EffectID: "ce_0001", ProjectID: string(projectID), TargetTaskID: string(taskID), SourceAgentRef: "agent:one", RelationType: collaborationeffect.RelationComplete, Polarity: collaborationeffect.PolarityPositive, Magnitude: 2, OccurredAt: at, RuleVersion: collaborationeffect.RuleVersionV1, EvidenceEventIDs: []string{"evt-1", "evt-2"}},
		{EffectID: "ce_0002", ProjectID: string(projectID), TargetTaskID: string(taskID), SourceAgentRef: "agent:one", RelationType: collaborationeffect.RelationComplete, Polarity: collaborationeffect.PolarityPositive, Magnitude: 3, OccurredAt: at.Add(time.Minute), RuleVersion: collaborationeffect.RuleVersionV1, EvidenceEventIDs: []string{"evt-3"}},
	}
	if err := effectRepo.Apply(ctx, collaborationeffect.Fact{EventID: "evt-apply", OccurredAt: at}, collaborationeffect.RuleVersionV1, effects, nil, nil); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/insights/collaboration-effects?plan_id="+string(planID)+"&limit=100", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		GraphVersion string `json:"graph_version"`
		Graph        struct {
			Nodes []struct {
				ID        string `json:"id"`
				Kind      string `json:"kind"`
				PlanID    string `json:"plan_id"`
				StageID   string `json:"stage_id"`
				TaskID    string `json:"task_id"`
				ProjectID string `json:"project_id"`
			} `json:"nodes"`
			Edges []struct {
				SemanticKey      string `json:"semantic_key"`
				Source           string `json:"source"`
				Target           string `json:"target"`
				RelationType     string `json:"relation_type"`
				InteractionCount int    `json:"interaction_count"`
				EvidenceCount    int    `json:"evidence_count"`
				FirstOccurredAt  string `json:"first_occurred_at"`
				LastOccurredAt   string `json:"last_occurred_at"`
			} `json:"edges"`
		} `json:"graph"`
		Effects []any `json:"effects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.GraphVersion != collaborationeffect.RuleVersionV1 || len(out.Effects) != 2 {
		t.Fatalf("version/effects = %q/%d", out.GraphVersion, len(out.Effects))
	}
	nodes := map[string]string{}
	for _, node := range out.Graph.Nodes {
		nodes[node.ID] = node.Kind
		if node.ID == "plan:"+string(projectID) {
			t.Fatalf("project was masqueraded as plan node: %+v", node)
		}
	}
	for id, kind := range map[string]string{"plan:" + string(planID): "plan", "stage:" + string(stageID): "stage", "task:" + string(taskID): "task", "agent:one": "agent"} {
		if nodes[id] != kind {
			t.Fatalf("node %s kind=%q, want %q; nodes=%v", id, nodes[id], kind, nodes)
		}
	}
	var aggregate, planTask, planStage, stageTask bool
	for _, edge := range out.Graph.Edges {
		switch edge.RelationType {
		case "complete":
			if edge.Source == "agent:one" && edge.Target == "task:"+string(taskID) && edge.InteractionCount == 2 && edge.EvidenceCount == 3 && edge.FirstOccurredAt != "" && edge.LastOccurredAt != "" {
				aggregate = true
			}
		case "agent_plan":
			if edge.Source == "agent:one" && edge.Target == "plan:"+string(planID) && edge.InteractionCount == 2 && edge.EvidenceCount == 3 {
				planTask = true
			}
		case "plan_stage":
			planStage = planStage || edge.Source == "plan:"+string(planID) && edge.Target == "stage:"+string(stageID)
		case "stage_task":
			stageTask = stageTask || edge.Source == "stage:"+string(stageID) && edge.Target == "task:"+string(taskID)
		}
	}
	if !aggregate || !planTask || !planStage || !stageTask {
		t.Fatalf("missing semantic edges aggregate=%v agentPlan=%v planStage=%v stageTask=%v edges=%+v", aggregate, planTask, planStage, stageTask, out.Graph.Edges)
	}
}

func TestInsightsV2ExecutionsRequiresContext(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Insight = mustInsight(t, db, t.TempDir()+"/insight.duckdb")
	if err := deps.Insight.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer deps.Insight.Close()
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/v2/executions?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("v2 executions without context status = %d, want 400", resp.StatusCode)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "execution_context_required" {
		t.Fatalf("v2 executions error = %q", out.Error)
	}
}

func TestInsightsV2OverviewRouteShape(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedInsightHTTPFacts(t, db, sess.OrgID, time.Now().UTC().Add(-time.Minute))
	deps.Insight = mustInsight(t, db, t.TempDir()+"/insight.duckdb")
	if err := deps.Insight.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer deps.Insight.Close()
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/v2/overview?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("v2 overview status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		MetricVersion string `json:"metric_version"`
		TimeWindow    struct {
			Duration string `json:"duration"`
		} `json:"time_window"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		Executions struct {
			Value *int64 `json:"value"`
		} `json:"executions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.MetricVersion != insight.MetricVersionV2 || out.TimeWindow.Duration != "24h" || out.Executions.Value == nil || *out.Executions.Value != 1 || out.Health.Status == "" {
		t.Fatalf("v2 overview body = %+v", out)
	}
}

func TestInsightsExecutionAPI_ReadsSingleProjectedExecution(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedInsightHTTPFacts(t, db, sess.OrgID, time.Now().UTC().Add(-time.Minute))
	duckPath := t.TempDir() + "/insight.duckdb"
	svc, err := insight.Open(context.Background(), db, duckPath, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	deps.Insight = svc
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/executions/exec-api?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("execution status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		RefreshedAt string `json:"refreshed_at"`
		Freshness   struct {
			State       string `json:"state"`
			AgeMS       int64  `json:"age_ms"`
			ThresholdMS int64  `json:"threshold_ms"`
		} `json:"freshness"`
		Execution struct {
			ExecutionID string  `json:"execution_id"`
			TaskTitle   *string `json:"task_title"`
			AgentRef    string  `json:"agent_ref"`
			ProjectID   *string `json:"project_id"`
			DurationMS  *int64  `json:"duration_ms"`
		} `json:"execution"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Execution.ExecutionID != "exec-api" || out.Execution.AgentRef != "agent:agent-api" || out.Execution.ProjectID == nil || *out.Execution.ProjectID != "project-api" || out.Execution.DurationMS == nil {
		t.Fatalf("execution body = %+v", out.Execution)
	}
	if out.RefreshedAt == "" || out.Freshness.State != "fresh" || out.Freshness.AgeMS < 0 || out.Freshness.ThresholdMS != time.Minute.Milliseconds() {
		t.Fatalf("execution freshness = %+v refreshed_at=%q, want fresh production checkpoint", out.Freshness, out.RefreshedAt)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/executions/missing?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", resp.StatusCode)
	}
}

func TestInsightsExecutionAPI_ForeignOrgExecutionIsNotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	now := time.Now().UTC()
	execWebSQL(t, db, `INSERT INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at) VALUES ('org-foreign', 'foreign', 'Foreign', ?, ?, ?)`, sess.IdentityID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	seedInsightHTTPFactsWithIDs(t, db, "org-foreign", "foreign", "exec-foreign", now.Add(-time.Minute))
	duckPath := t.TempDir() + "/insight.duckdb"
	svc, err := insight.Open(context.Background(), db, duckPath, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	deps.Insight = svc
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/executions/exec-foreign?window=24h", nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign execution status = %d, want 404", resp.StatusCode)
	}
}

func seedInsightHTTPFacts(t *testing.T, db *sql.DB, orgID string, finished time.Time) {
	seedInsightHTTPFactsWithIDs(t, db, orgID, "api", "exec-api", finished)
}

func mustInsight(t *testing.T, db *sql.DB, path string) *insight.Service {
	t.Helper()
	svc, err := insight.Open(context.Background(), db, path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func seedInsightHTTPFactsWithIDs(t *testing.T, db *sql.DB, orgID, suffix, executionID string, finished time.Time) {
	t.Helper()
	now := finished.UTC().Format(time.RFC3339Nano)
	workerID := "worker-" + suffix
	agentID := "agent-" + suffix
	projectID := "project-" + suffix
	taskID := "task-" + suffix
	execWebSQL(t, db, `INSERT INTO workers (id, organization_id, status, capabilities_json, enrolled_at, created_at, updated_at) VALUES (?, ?,'online','[]',?,?,?)`, workerID, orgID, now, now, now)
	execWebSQL(t, db, `INSERT INTO agents (id, organization_id, name, env_vars, worker_id, lifecycle, created_by, created_at, updated_at) VALUES (?, ?, 'Agent API', '{}', ?, 'running', 'user:test', ?, ?)`, agentID, orgID, workerID, now, now)
	execWebSQL(t, db, `INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, 'Project API', '', 'active', 'user:test', ?, ?)`, projectID, orgID, now, now)
	execWebSQL(t, db, `INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, created_by, created_at, updated_at) VALUES (?, ?, 'Task API', '', 'running', ?, 'user:test', ?, ?)`, taskID, projectID, "agent:"+agentID, now, now)
	started := finished.Add(-time.Second)
	payloadStart := `{"event":"executor.start","executor_id":"` + executionID + `","cli":"codex","model":"gpt-5"}`
	payloadStop := `{"event":"executor.stop","executor_id":"` + executionID + `","outcome":"succeeded"}`
	execWebSQL(t, db, `INSERT INTO agent_activity_events (id, agent_id, task_ref, interaction_ref, event_type, payload, occurred_at) VALUES (?, ?, ?, ?, 'lifecycle', ?, ?)`, suffix+"-start", agentID, taskID, "executor:"+executionID, payloadStart, started.Format(time.RFC3339Nano))
	execWebSQL(t, db, `INSERT INTO agent_activity_events (id, agent_id, task_ref, interaction_ref, event_type, payload, occurred_at) VALUES (?, ?, ?, ?, 'lifecycle', ?, ?)`, suffix+"-stop", agentID, taskID, "executor:"+executionID, payloadStop, finished.Format(time.RFC3339Nano))
}

func execWebSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %s: %v", q, err)
	}
}
