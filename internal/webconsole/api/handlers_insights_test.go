package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
