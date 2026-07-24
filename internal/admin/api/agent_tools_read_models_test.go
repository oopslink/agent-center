package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

type readModelActivityRepo struct {
	events []*agent.AgentActivityEvent
}

func (r readModelActivityRepo) Append(context.Context, *agent.AgentActivityEvent) error { return nil }
func (r readModelActivityRepo) ListByAgent(context.Context, agent.AgentID, int, string) ([]*agent.AgentActivityEvent, error) {
	return nil, nil
}
func (r readModelActivityRepo) ListByTask(context.Context, string) ([]*agent.AgentActivityEvent, error) {
	return r.events, nil
}
func (r readModelActivityRepo) LatestByAgents(context.Context, []agent.AgentID) (map[agent.AgentID]*agent.AgentActivityEvent, error) {
	return nil, nil
}

func readModelEvent(t *testing.T, id, payload string, at time.Time) *agent.AgentActivityEvent {
	t.Helper()
	ev, err := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID: id, AgentID: "agent-1", TaskRef: "task-1",
		InteractionRef: "executor:exec-1", EventType: agent.EventTypeLifecycle,
		Payload: payload, OccurredAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

type readModelAgentDir string

func (d readModelAgentDir) OrgOfAgent(context.Context, string) (string, error) { return string(d), nil }
func (d readModelAgentDir) ConcurrencyCapOfAgent(context.Context, string) (int, error) {
	return 0, nil
}

func TestTaskExecutionsProjectsPersistedLifecycle(t *testing.T) {
	start := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	repo := readModelActivityRepo{events: []*agent.AgentActivityEvent{
		readModelEvent(t, "01", `{"event":"executor.start","cli":"codex","model":"gpt-5"}`, start),
		readModelEvent(t, "02", `{"event":"executor.stop","outcome":"failed","reason":"repo_source_unavailable","detail":"token=must-not-leak","recovered":true}`, start.Add(time.Minute)),
	}}
	runs, err := taskExecutions(context.Background(), HandlerDeps{AgentActivityRepo: repo}, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.ExecutionID != "exec-1" || got.CLI != "codex" || got.Model != "gpt-5" ||
		got.Outcome != "failed" || got.ErrorKind != "repo_source_unavailable" ||
		got.ErrorDetail != "[redacted]" || !got.Recovered {
		t.Fatalf("run = %+v", got)
	}
}

func TestRedactAuditNote(t *testing.T) {
	for _, note := range []string{"token=abc", "Authorization: Bearer abc", "PASSWORD=hunter2"} {
		if got := redactAuditNote(note); got != "[redacted]" {
			t.Errorf("redactAuditNote(%q) = %q", note, got)
		}
	}
	if got := redactAuditNote("normal operator note"); got != "normal operator note" {
		t.Fatalf("ordinary note changed: %q", got)
	}
}

func wireReadModelPM(t *testing.T, f *agentToolsFixture) *pmservice.Service {
	t.Helper()
	gen := idgen.NewGenerator(f.clk)
	svc := pmservice.New(pmservice.Deps{
		DB: f.db, Projects: pmsql.NewProjectRepo(f.db), Members: pmsql.NewProjectMemberRepo(f.db),
		Tasks: pmsql.NewTaskRepo(f.db), TaskSubs: pmsql.NewTaskSubscriberRepo(f.db), Outbox: outboxsql.NewOutboxRepo(f.db),
		TaskActionLogs: pmsql.NewTaskActionLogRepo(f.db, gen),
		AgentDir:       readModelAgentDir(atTestOrg),
		IDGen:          gen, Clock: f.clk,
	})
	f.deps.PMService = svc
	return svc
}

func seedAuditedTaskForAgent(t *testing.T, svc *pmservice.Service, assignee string) string {
	t.Helper()
	ctx := context.Background()
	pid, err := svc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "Audit P", CreatedBy: "user:owner"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "audit me", CreatedBy: "user:owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignTask(ctx, tid, pm.IdentityRef(assignee), "user:owner"); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartTask(ctx, tid, pm.IdentityRef(assignee)); err != nil {
		t.Fatal(err)
	}
	if err := svc.BlockTask(ctx, tid, "token=must-not-leak", pm.BlockReasonObstacle, pm.IdentityRef(assignee)); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnblockTask(ctx, pmservice.UnblockTaskCommand{TaskID: tid, Actor: pm.IdentityRef(assignee), Comment: "approved"}); err != nil {
		t.Fatal(err)
	}
	return string(tid)
}

func TestGetTaskAuditReadsPersistedLogsPagedAndRedacted(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	svc := wireReadModelPM(t, f)
	taskID := seedAuditedTaskForAgent(t, svc, "agent:"+atAgent1)
	s := f.server(t)

	status, body := postBearer(t, s.URL, "/admin/agent-tools/get_task_audit", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": taskID, "page_size": 2, "offset": 1})
	if status != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%v", status, body)
	}
	if body["total"] != float64(3) || body["offset"] != float64(1) || body["has_more"] != false {
		t.Fatalf("page envelope = %v, want total=3 offset=1 has_more=false", body)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v, want 2 entries", body["items"])
	}
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	if first["action"] != string(pm.TaskActionBlocked) || second["action"] != string(pm.TaskActionUnblocked) {
		t.Fatalf("actions = %v, %v; want blocked, unblocked", first["action"], second["action"])
	}
	if first["note"] != "[redacted]" {
		t.Fatalf("blocked note = %v, want [redacted]", first["note"])
	}
}

func TestGetTaskAuditRejectsCrossProjectAccess(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	svc := wireReadModelPM(t, f)
	taskID := seedAuditedTaskForAgent(t, svc, "agent:someone-else")
	s := f.server(t)

	status, body := postBearer(t, s.URL, "/admin/agent-tools/get_task_audit", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": taskID})
	if status != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%v", status, body)
	}
	if body["error"] != "not_agents_task" {
		t.Fatalf("error=%v, want not_agents_task", body["error"])
	}
}
