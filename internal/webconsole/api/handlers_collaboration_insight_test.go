package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/observability/collaborationeffect"
	obssql "github.com/oopslink/agent-center/internal/observability/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

type collaborationHTTPExecContext interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func TestCollaborationEffectsHTTPProjectScopeAndFailures(t *testing.T) {
	ctx := context.Background()
	deps, db := setupAPIWithAuth(t)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	effects, err := collaborationeffect.NewSQLiteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	events, err := obssql.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	graphs, err := collaborationeffect.NewSQLiteGraphReader(db)
	if err != nil {
		t.Fatal(err)
	}
	deps.CollaborationInsight, err = collaborationeffect.NewQueryServiceWithGraph(effects, events, graphs)
	if err != nil {
		t.Fatal(err)
	}
	sess := setupTestSession(t, db, deps)
	projectA, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID,
		Name:           "Alpha",
		Description:    "in-scope",
		CreatedBy:      pm.IdentityRef("user:" + sess.IdentityID),
	})
	if err != nil {
		t.Fatal(err)
	}
	projectB, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID,
		Name:           "Beta",
		Description:    "organization-level peer",
		CreatedBy:      pm.IdentityRef("user:" + sess.IdentityID),
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	seedCollaborationHTTPProject(t, ctx, db, effects, sess.OrgID, string(projectA), "task-a", "agent:alpha", "ce_http_a", base.Add(time.Minute))
	seedCollaborationHTTPProject(t, ctx, db, effects, sess.OrgID, string(projectB), "task-b", "agent:beta", "ce_http_b", base.Add(2*time.Minute))
	seedCollaborationHTTPProject(t, ctx, db, effects, "org-other", "project-other", "task-other", "agent:other", "ce_http_other", base.Add(4*time.Minute))

	s := newTestServer(t, deps)
	defer s.Close()

	projectResp := orgScopedGet(t, s.URL+"/api/insights/collaboration-effects?limit=100&project_id="+string(projectA), sess)
	defer projectResp.Body.Close()
	if projectResp.StatusCode != http.StatusOK {
		t.Fatalf("project query status=%d body=%s", projectResp.StatusCode, readCollaborationHTTPBody(t, projectResp))
	}
	var projectBody collaborationeffect.QueryResult
	if err := json.NewDecoder(projectResp.Body).Decode(&projectBody); err != nil {
		t.Fatal(err)
	}
	if len(projectBody.Effects) != 1 || projectBody.Effects[0].ProjectID != string(projectA) {
		t.Fatalf("project query was not scoped to %s: %+v", projectA, projectBody.Effects)
	}
	t.Logf("project query params: limit=100 project_id=%s; status=%d effects=%d nodes=%d graph_version=%s", projectA, projectResp.StatusCode, len(projectBody.Effects), len(projectBody.Graph.Nodes), projectBody.GraphVersion)
	for _, node := range projectBody.Graph.Nodes {
		if node.ProjectID != "" && node.ProjectID != string(projectA) {
			t.Fatalf("project query leaked node %+v", node)
		}
	}

	orgResp := orgScopedGet(t, s.URL+"/api/insights/collaboration-effects?limit=100", sess)
	defer orgResp.Body.Close()
	if orgResp.StatusCode != http.StatusOK {
		t.Fatalf("org query status=%d body=%s", orgResp.StatusCode, readCollaborationHTTPBody(t, orgResp))
	}
	var orgBody collaborationeffect.QueryResult
	if err := json.NewDecoder(orgResp.Body).Decode(&orgBody); err != nil {
		t.Fatal(err)
	}
	if len(orgBody.Effects) != 2 {
		t.Fatalf("org query should include readable projects only, got %+v", orgBody.Effects)
	}
	t.Logf("organization query params: limit=100; status=%d effects=%d nodes=%d graph_version=%s", orgResp.StatusCode, len(orgBody.Effects), len(orgBody.Graph.Nodes), orgBody.GraphVersion)

	cursorResp := orgScopedGet(t, s.URL+"/api/insights/collaboration-effects?limit=100&cursor=not-opaque", sess)
	defer cursorResp.Body.Close()
	if cursorResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad cursor status=%d body=%s", cursorResp.StatusCode, readCollaborationHTTPBody(t, cursorResp))
	}
	t.Logf("invalid cursor query params: limit=100 cursor=not-opaque; status=%d", cursorResp.StatusCode)

	missingProjectResp := orgScopedGet(t, s.URL+"/api/insights/collaboration-effects?limit=100&project_id=project-missing", sess)
	defer missingProjectResp.Body.Close()
	if missingProjectResp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing project status=%d body=%s", missingProjectResp.StatusCode, readCollaborationHTTPBody(t, missingProjectResp))
	}
	t.Logf("missing project query params: limit=100 project_id=project-missing; status=%d", missingProjectResp.StatusCode)

	seedCollaborationHTTPProject(t, ctx, db, effects, sess.OrgID, "project-noauth", "task-noauth", "agent:blocked", "ce_http_noauth", base.Add(3*time.Minute))
	noauthResp := orgScopedGet(t, s.URL+"/api/insights/collaboration-effects?limit=100&project_id=project-noauth", sess)
	defer noauthResp.Body.Close()
	if noauthResp.StatusCode != http.StatusForbidden {
		t.Fatalf("noauth project status=%d body=%s", noauthResp.StatusCode, readCollaborationHTTPBody(t, noauthResp))
	}
	t.Logf("unauthorized project query params: limit=100 project_id=project-noauth; status=%d", noauthResp.StatusCode)
	crossOrgResp := orgScopedGet(t, s.URL+"/api/insights/collaboration-effects?limit=100&project_id=project-other", sess)
	defer crossOrgResp.Body.Close()
	if crossOrgResp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org project status=%d body=%s", crossOrgResp.StatusCode, readCollaborationHTTPBody(t, crossOrgResp))
	}
	t.Logf("cross-org project query params: limit=100 project_id=project-other; status=%d", crossOrgResp.StatusCode)
}

func seedCollaborationHTTPProject(t *testing.T, ctx context.Context, db collaborationHTTPExecContext, repo *collaborationeffect.SQLiteRepository, orgID, projectID, taskID, assignee, effectID string, at time.Time) {
	t.Helper()
	ts := at.UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'active', 'user:test', ?, ?)`, projectID, orgID, projectID, ts, ts); err != nil {
		t.Fatalf("seed project %s: %v", projectID, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO pm_tasks (id, project_id, title, description, status, assignee, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'running', ?, 'user:test', ?, ?)`, taskID, projectID, taskID, assignee, ts, ts); err != nil {
		t.Fatalf("seed task %s: %v", taskID, err)
	}
	effect := collaborationeffect.Effect{
		EffectID: effectID, ProjectID: projectID, TargetTaskID: taskID, SourceAgentRef: assignee,
		RelationType: collaborationeffect.RelationComplete, Polarity: collaborationeffect.PolarityPositive,
		Magnitude: 2, Confidence: "high", OccurredAt: at, RuleVersion: collaborationeffect.RuleVersionV1,
		EvidenceEventIDs: []string{effectID + "-event"},
		BeforeState:      map[string]any{"task_status": "running"},
		AfterState:       map[string]any{"task_status": "completed"},
		ExplanationKey:   "collaboration.effect.complete",
	}
	if err := repo.Apply(ctx, collaborationeffect.Fact{EventID: effectID, OccurredAt: at}, collaborationeffect.RuleVersionV1, []collaborationeffect.Effect{effect}, nil, nil); err != nil {
		t.Fatalf("seed effect %s: %v", effectID, err)
	}
}

func readCollaborationHTTPBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
