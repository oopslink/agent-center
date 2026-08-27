package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/insight"
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

func TestInsightsFreshnessAPI_ProductionClockBoundaryAndDuckDBRebuild(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedInsightHTTPFacts(t, db, sess.OrgID, time.Now().UTC().Add(-time.Minute))
	duckPath := t.TempDir() + "/insight.duckdb"
	const ttl = 250 * time.Millisecond

	svc := openInsightHTTPService(t, db, duckPath, ttl)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts := serveInsightHTTP(t, deps, svc)
	overview := getInsightFreshness(t, ts.URL, sess.Cookie, "/api/orgs/"+sess.OrgSlug+"/insights/overview?window=24h")
	if overview.State != "fresh" || overview.RefreshedAt == "" || overview.AgeMS < 0 || overview.AgeMS > overview.ThresholdMS {
		t.Fatalf("fresh overview freshness = %+v, want fresh within threshold", overview)
	}
	detail := getInsightFreshness(t, ts.URL, sess.Cookie, "/api/orgs/"+sess.OrgSlug+"/insights/executions/exec-api?window=24h")
	if detail.State != "fresh" || detail.RefreshedAt == "" || detail.AgeMS < 0 || detail.AgeMS > detail.ThresholdMS {
		t.Fatalf("fresh execution detail freshness = %+v, want fresh within threshold", detail)
	}

	time.Sleep(ttl + 150*time.Millisecond)
	staleOverview := getInsightFreshness(t, ts.URL, sess.Cookie, "/api/orgs/"+sess.OrgSlug+"/insights/overview?window=24h")
	if staleOverview.State != "stale" || staleOverview.AgeMS <= staleOverview.ThresholdMS {
		t.Fatalf("stale overview freshness = %+v, want stale after real clock crosses threshold", staleOverview)
	}
	staleDetail := getInsightFreshness(t, ts.URL, sess.Cookie, "/api/orgs/"+sess.OrgSlug+"/insights/executions/exec-api?window=24h")
	if staleDetail.State != "stale" || staleDetail.AgeMS <= staleDetail.ThresholdMS {
		t.Fatalf("stale execution detail freshness = %+v, want stale after real clock crosses threshold", staleDetail)
	}
	ts.Close()
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(duckPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(duckPath + ".wal"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	rebuilt := openInsightHTTPService(t, db, duckPath, ttl)
	if err := rebuilt.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	rebuildTS := serveInsightHTTP(t, deps, rebuilt)
	rebuiltOverview := getInsightFreshness(t, rebuildTS.URL, sess.Cookie, "/api/orgs/"+sess.OrgSlug+"/insights/overview?window=24h")
	if rebuiltOverview.State != "fresh" || rebuiltOverview.RefreshedAt == "" || rebuiltOverview.AgeMS < 0 || rebuiltOverview.AgeMS > rebuiltOverview.ThresholdMS {
		t.Fatalf("rebuilt overview freshness = %+v, want fresh after DuckDB delete and rebuild", rebuiltOverview)
	}
}

type insightFreshnessPayload struct {
	RefreshedAt string `json:"refreshed_at"`
	State       string
	AgeMS       int64
	ThresholdMS int64
}

func getInsightFreshness(t *testing.T, baseURL string, cookie *http.Cookie, path string) insightFreshnessPayload {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %d, want 200", path, resp.StatusCode)
	}
	var out struct {
		RefreshedAt string `json:"refreshed_at"`
		Freshness   struct {
			State       string `json:"state"`
			AgeMS       int64  `json:"age_ms"`
			ThresholdMS int64  `json:"threshold_ms"`
		} `json:"freshness"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return insightFreshnessPayload{
		RefreshedAt: out.RefreshedAt,
		State:       out.Freshness.State,
		AgeMS:       out.Freshness.AgeMS,
		ThresholdMS: out.Freshness.ThresholdMS,
	}
}

func openInsightHTTPService(t *testing.T, db *sql.DB, duckPath string, ttl time.Duration) *insight.Service {
	t.Helper()
	svc, err := insight.Open(context.Background(), db, duckPath, ttl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func serveInsightHTTP(t *testing.T, deps HandlerDeps, svc *insight.Service) *httptest.Server {
	t.Helper()
	deps.Insight = svc
	ts := httptest.NewServer(WithDeps(deps)(NewServer(":0", Deps{}).Handler()))
	t.Cleanup(ts.Close)
	return ts
}

func seedInsightHTTPFacts(t *testing.T, db *sql.DB, orgID string, finished time.Time) {
	seedInsightHTTPFactsWithIDs(t, db, orgID, "api", "exec-api", finished)
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
