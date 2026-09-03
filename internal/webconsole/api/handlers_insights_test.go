package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/insight"
	"github.com/oopslink/agent-center/internal/observability/collaborationeffect"
	obssql "github.com/oopslink/agent-center/internal/observability/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
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

func TestCollaborationEffectsAPIStableSemanticEdgesAcrossCursorPages(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	projectID, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: pm.IdentityRef("user:" + sess.IdentityID)})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := collaborationeffect.NewSQLiteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	events, err := obssql.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	deps.CollaborationInsight, err = collaborationeffect.NewQueryService(repo, events)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	for i, effect := range []collaborationeffect.Effect{
		{EffectID: "ce_01", ProjectID: string(projectID), TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: collaborationeffect.RelationComplete, Polarity: collaborationeffect.PolarityPositive, Magnitude: 1, OccurredAt: at, RuleVersion: collaborationeffect.RuleVersionV1, EvidenceEventIDs: []string{"evt-1"}},
		{EffectID: "ce_02", ProjectID: string(projectID), TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: collaborationeffect.RelationComplete, Polarity: collaborationeffect.PolarityPositive, Magnitude: 3, OccurredAt: at.Add(time.Hour), RuleVersion: collaborationeffect.RuleVersionV1, EvidenceEventIDs: []string{"evt-2"}},
		{EffectID: "ce_03", ProjectID: string(projectID), TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: collaborationeffect.RelationComplete, Polarity: collaborationeffect.PolarityNegative, Magnitude: 2, OccurredAt: at.Add(2 * time.Hour), RuleVersion: collaborationeffect.RuleVersionV1, EvidenceEventIDs: []string{"evt-3"}},
		{EffectID: "ce_04", ProjectID: string(projectID), TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: collaborationeffect.RelationBlock, Polarity: collaborationeffect.PolarityPositive, Magnitude: 2, OccurredAt: at.Add(3 * time.Hour), RuleVersion: collaborationeffect.RuleVersionV1, EvidenceEventIDs: []string{"evt-4"}},
	} {
		if err = repo.Apply(ctx, collaborationeffect.Fact{EventID: string(rune('a' + i)), OccurredAt: effect.OccurredAt}, collaborationeffect.RuleVersionV1, []collaborationeffect.Effect{effect}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	defer ts.Close()

	first := getCollaborationPage(t, ts.URL, sess, string(projectID), "")
	second := getCollaborationPage(t, ts.URL, sess, string(projectID), first.NextCursor)
	if len(first.Graph.Edges) != 1 || len(second.Graph.Edges) != 1 {
		t.Fatalf("paged graph edge counts = %d/%d, want 1/1", len(first.Graph.Edges), len(second.Graph.Edges))
	}
	if first.Graph.Edges[0].ID == "" || first.Graph.Edges[0].ID != second.Graph.Edges[0].ID {
		t.Fatalf("semantic edge id changed across cursor pages: %q vs %q", first.Graph.Edges[0].ID, second.Graph.Edges[0].ID)
	}
	full := getCollaborationPage(t, ts.URL, sess, string(projectID), "")
	if full.NextCursor == "" {
		t.Fatalf("test setup expected a real second page for limit=1")
	}
	fullReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/insights/collaboration-effects?project_id="+string(projectID)+"&limit=4", nil)
	fullReq.AddCookie(sess.Cookie)
	fullResp, err := http.DefaultClient.Do(fullReq)
	if err != nil {
		t.Fatal(err)
	}
	defer fullResp.Body.Close()
	if fullResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(fullResp.Body)
		t.Fatalf("full status=%d body=%s", fullResp.StatusCode, body)
	}
	var all collaborationeffect.QueryResult
	if err = json.NewDecoder(fullResp.Body).Decode(&all); err != nil {
		t.Fatal(err)
	}
	if len(all.Graph.Edges) != 3 {
		t.Fatalf("different polarity/relation merged incorrectly; edges=%+v", all.Graph.Edges)
	}
	pos := findCollaborationHTTPGraphEdge(all.Graph.Edges, collaborationeffect.RelationComplete, collaborationeffect.PolarityPositive)
	if pos == nil || pos.InteractionCount != 2 || pos.EvidenceCount != 2 || pos.Magnitude != 3 {
		t.Fatalf("positive aggregate edge=%+v", pos)
	}
	if findCollaborationHTTPGraphEdge(all.Graph.Edges, collaborationeffect.RelationComplete, collaborationeffect.PolarityNegative) == nil || findCollaborationHTTPGraphEdge(all.Graph.Edges, collaborationeffect.RelationBlock, collaborationeffect.PolarityPositive) == nil {
		t.Fatalf("missing distinct polarity/relation edges: %+v", all.Graph.Edges)
	}
}

func getCollaborationPage(t *testing.T, baseURL string, sess testSession, projectID, cursor string) collaborationeffect.QueryResult {
	t.Helper()
	url := baseURL + "/api/orgs/" + sess.OrgSlug + "/insights/collaboration-effects?project_id=" + projectID + "&limit=1"
	if cursor != "" {
		url += "&cursor=" + cursor
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("collaboration status=%d body=%s", resp.StatusCode, body)
	}
	var out collaborationeffect.QueryResult
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func findCollaborationHTTPGraphEdge(edges []collaborationeffect.GraphEdge, relation collaborationeffect.RelationType, polarity collaborationeffect.Polarity) *collaborationeffect.GraphEdge {
	for i := range edges {
		if edges[i].RelationType == relation && edges[i].Polarity == polarity {
			return &edges[i]
		}
	}
	return nil
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
