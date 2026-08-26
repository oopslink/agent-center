package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func TestInsightsOverviewAndDrilldownUseRealTaskRows(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	now := time.Now().UTC()

	projectRepo := pmsql.NewProjectRepo(db)
	taskRepo := pmsql.NewTaskRepo(db)
	actionRepo := pmsql.NewTaskActionLogRepo(db, idgen.NewGenerator(clock.SystemClock{}))
	agentRepo := agentsql.NewAgentRepo(db)

	project, err := pm.NewProject(pm.NewProjectInput{
		ID: "project-insights", OrganizationID: sess.OrgID, Name: "Insight Project",
		CreatedBy: pm.IdentityRef("user:" + sess.IdentityID), CreatedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := projectRepo.Save(ctx, project); err != nil {
		t.Fatal(err)
	}
	agent, err := agentbc.NewAgent(agentbc.NewAgentInput{
		ID: "agent-entity-1", OrganizationID: sess.OrgID,
		Profile:  agentbc.Profile{Name: "Insight Agent", CLI: "claude-code", Model: "sonnet", MaxConcurrentTasks: 4},
		WorkerID: "worker-1", CreatedBy: agentbc.IdentityRef("user:" + sess.IdentityID),
		IdentityMemberID: "agent-member-1", CreatedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentRepo.Save(ctx, agent); err != nil {
		t.Fatal(err)
	}

	completed := seedInsightTask(t, taskRepo, actionRepo, "task-completed", project.ID(), "agent:agent-member-1", 1, now.Add(-70*time.Minute), now.Add(-60*time.Minute), now.Add(-50*time.Minute), pm.TaskCompleted)
	discarded := seedInsightTask(t, taskRepo, actionRepo, "task-discarded", project.ID(), "agent:agent-member-1", 2, now.Add(-40*time.Minute), now.Add(-30*time.Minute), now.Add(-20*time.Minute), pm.TaskDiscarded)
	running := seedInsightTask(t, taskRepo, actionRepo, "task-running", project.ID(), "agent:agent-member-1", 3, now.Add(-10*time.Minute), now.Add(-5*time.Minute), time.Time{}, pm.TaskRunning)
	_ = completed
	_ = discarded
	_ = running

	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/insights/overview", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	summary := got["summary"].(map[string]any)
	if summary["executions"].(float64) != 3 {
		t.Fatalf("executions=%v", summary["executions"])
	}
	if summary["failures"].(float64) != 1 {
		t.Fatalf("failures=%v", summary["failures"])
	}
	if got["refreshed_at"] == "" || got["window"] == nil || got["freshness"] != "fresh" {
		t.Fatalf("missing backend freshness/window fields: %+v", got)
	}
	wait := summary["queue_wait"].(map[string]any)
	if wait["p50_seconds"].(float64) != 600 {
		t.Fatalf("queue p50=%v", wait["p50_seconds"])
	}
	duration := summary["execution_duration"].(map[string]any)
	if duration["p95_seconds"].(float64) != 600 {
		t.Fatalf("duration p95=%v", duration["p95_seconds"])
	}
	slot := summary["slot_utilization"].(map[string]any)
	if slot["running"].(float64) != 1 || slot["capacity"].(float64) != 1 {
		t.Fatalf("slot utilization=%+v", slot)
	}

	resp = orgScopedGet(t, s.URL+"/api/insights/task-executions?filter=agent&value=agent-member-1", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drilldown status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var drill map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&drill); err != nil {
		t.Fatal(err)
	}
	if drill["total"].(float64) != 3 {
		t.Fatalf("drilldown total=%v", drill["total"])
	}
	items := drill["items"].([]any)
	first := items[0].(map[string]any)
	if first["task_id"] == "" || first["project_id"] != "project-insights" {
		t.Fatalf("bad drilldown row: %+v", first)
	}
}

func TestInsightsOverviewRequiresRealQueryDeps(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.PMTaskActions = nil
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/insights/overview", sess)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func seedInsightTask(t *testing.T, tasks *pmsql.TaskRepo, actions *pmsql.TaskActionLogRepo, id string, projectID pm.ProjectID, assignee pm.IdentityRef, orgNumber int, createdAt, startedAt, terminalAt time.Time, status pm.TaskStatus) *pm.Task {
	t.Helper()
	task, err := pm.NewTask(pm.NewTaskInput{
		ID: pm.TaskID(id), ProjectID: projectID, Title: id, CreatedBy: "user:test", CreatedAt: createdAt, OrgNumber: orgNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Assign(assignee, createdAt); err != nil {
		t.Fatal(err)
	}
	if err := task.Start(startedAt); err != nil {
		t.Fatal(err)
	}
	if err := task.RecordAgentStarted(assignee, startedAt); err != nil {
		t.Fatal(err)
	}
	switch status {
	case pm.TaskCompleted:
		if err := task.Complete(assignee, terminalAt); err != nil {
			t.Fatal(err)
		}
	case pm.TaskDiscarded:
		if err := task.Discard(terminalAt); err != nil {
			t.Fatal(err)
		}
	case pm.TaskRunning:
	default:
		t.Fatalf("unsupported status %s", status)
	}
	if err := tasks.Save(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := actions.Append(context.Background(), task.ID(), task.ActionLogs()); err != nil {
		t.Fatal(err)
	}
	return task
}
