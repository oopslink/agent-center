package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/ruleregistry"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

type readModelActivityRepo struct {
	events []*agent.AgentActivityEvent
}

type readModelRuleAuditRepo struct {
	byExec map[string][]ruleregistry.LoadAudit
}

func TestTaskExecutionsIncludesPendingForkCommand(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Now().UTC())
	envSvc := envservice.New(envservice.Deps{
		DB: db, Workers: envsqlite.NewWorkerRepo(db), Events: envsqlite.NewControlEventRepo(db),
		IDGen: idgen.NewGenerator(clk), Clock: clk,
	})
	if _, err := envSvc.ConnectWorker(ctx, "worker-1"); err != nil {
		t.Fatal(err)
	}
	cmd, err := envSvc.EnqueueCommand(ctx, environment.AppendCommandInput{
		WorkerID: "worker-1", CommandType: cmdTypeAgentForkExecutor, IdempotencyKey: "fork-1",
		AgentID: "agent-1", TaskID: "task-1", Status: environment.CommandStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := taskExecutions(ctx, HandlerDeps{EnvControlSvc: envSvc}, "agent-1", "worker-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs=%d, want pending command row", len(runs))
	}
	if runs[0].CommandID != cmd.ID() || runs[0].State != "pending" || runs[0].HealthStatus != "pending" {
		t.Fatalf("pending command run = %+v", runs[0])
	}
}

func TestTaskExecutionsExpiresOldPendingForkCommand(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Now().Add(-forkCommandExpireAfter - time.Minute).UTC())
	envSvc := envservice.New(envservice.Deps{
		DB: db, Workers: envsqlite.NewWorkerRepo(db), Events: envsqlite.NewControlEventRepo(db),
		IDGen: idgen.NewGenerator(clk), Clock: clk,
	})
	if _, err := envSvc.ConnectWorker(ctx, "worker-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := envSvc.EnqueueCommand(ctx, environment.AppendCommandInput{
		WorkerID: "worker-1", CommandType: cmdTypeAgentForkExecutor, IdempotencyKey: "fork-old",
		AgentID: "agent-1", TaskID: "task-1", Status: environment.CommandStatusPending,
	}); err != nil {
		t.Fatal(err)
	}
	runs, err := taskExecutions(ctx, HandlerDeps{EnvControlSvc: envSvc}, "agent-1", "worker-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].State != "terminal" || runs[0].CommandStatus != environment.CommandStatusExpired ||
		runs[0].HealthStatus != environment.CommandStatusExpired || !runs[0].RecoveryRequired {
		t.Fatalf("expired command run = %+v", runs)
	}
}

func TestTaskExecutionsStartedForkCommandIsTelemetryGap(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Now().Add(-executionStaleAfter - time.Minute).UTC())
	envSvc := envservice.New(envservice.Deps{
		DB: db, Workers: envsqlite.NewWorkerRepo(db), Events: envsqlite.NewControlEventRepo(db),
		IDGen: idgen.NewGenerator(clk), Clock: clk,
	})
	if _, err := envSvc.ConnectWorker(ctx, "worker-1"); err != nil {
		t.Fatal(err)
	}
	cmd, err := envSvc.EnqueueCommand(ctx, environment.AppendCommandInput{
		WorkerID: "worker-1", CommandType: cmdTypeAgentForkExecutor, IdempotencyKey: "fork-started",
		AgentID: "agent-1", TaskID: "task-1", Status: environment.CommandStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Minute)
	if _, err := envSvc.UpdateCommandStatus(ctx, environment.UpdateCommandStatusInput{
		WorkerID: "worker-1", CommandID: cmd.ID(), AgentID: "agent-1", TaskID: "task-1",
		Status: environment.CommandStatusStarted, ExecutionID: "exec-no-lifecycle", StatusUpdatedAt: clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(executionStaleAfter + time.Minute)
	runs, err := taskExecutions(ctx, HandlerDeps{EnvControlSvc: envSvc}, "agent-1", "worker-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs=%d, want started command row", len(runs))
	}
	got := runs[0]
	if got.State == "running" || got.State != "spawned" || got.HealthStatus != "telemetry_gap" ||
		got.ControlPlaneHealth != "telemetry_gap" || got.RecoveryStatus != "spawned_no_lifecycle" || !got.RecoveryRequired {
		t.Fatalf("started command run = %+v", got)
	}
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

func (r readModelRuleAuditRepo) AppendLoaded(context.Context, ruleregistry.LoadAudit) (bool, error) {
	return false, nil
}
func (r readModelRuleAuditRepo) ListByExecutionIDs(_ context.Context, ids []string) (map[string][]ruleregistry.LoadAudit, error) {
	out := map[string][]ruleregistry.LoadAudit{}
	for _, id := range ids {
		if rows := r.byExec[id]; len(rows) > 0 {
			out[id] = append([]ruleregistry.LoadAudit(nil), rows...)
		}
	}
	return out, nil
}
func (r readModelRuleAuditRepo) ListByPlanningSessionIDs(context.Context, []string) (map[string][]ruleregistry.LoadAudit, error) {
	return map[string][]ruleregistry.LoadAudit{}, nil
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
		readModelEvent(t, "02", `{"event":"executor.stop","outcome":"failed","reason":"repo_source_unavailable","detail":"token=must-not-leak","recovered":true,"git":{"branch":"feat/x","head_sha":"abc","probed":true,"pushed":false,"dirty":false,"base_ref":"origin/main","base_known":true,"ahead_of_base":1}}`, start.Add(time.Minute)),
	}}
	runs, err := taskExecutions(context.Background(), HandlerDeps{AgentActivityRepo: repo}, "agent-1", "worker-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.ExecutionID != "exec-1" || got.CLI != "codex" || got.Model != "gpt-5" ||
		got.Outcome != "failed" || got.ErrorKind != "repo_source_unavailable" ||
		got.ErrorDetail != "[redacted]" || !got.Recovered || got.LastCommitSHA != "abc" ||
		got.Branch != "feat/x" || got.Pushed == nil || *got.Pushed || got.HealthStatus != "non_delivery" {
		t.Fatalf("run = %+v", got)
	}
	if len(got.NonDeliveryReasons) == 0 || got.NonDeliveryReasons[0].Code != "head_not_pushed" {
		t.Fatalf("non-delivery reasons = %+v", got.NonDeliveryReasons)
	}
}

func TestTaskExecutionsIncludesTeamRuleAuditSnapshot(t *testing.T) {
	start := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	activity := readModelActivityRepo{events: []*agent.AgentActivityEvent{
		readModelEvent(t, "01", `{"event":"executor.start","cli":"codex","model":"gpt-5"}`, start),
	}}
	ruleAudits := readModelRuleAuditRepo{byExec: map[string][]ruleregistry.LoadAudit{
		"exec-1": {
			{ExecutionID: "exec-1", TeamID: "team-1", TeamMemoryCommit: "c1", RuleSlug: "z-rule", Phase: "execute", LoadedAt: start},
			{ExecutionID: "exec-1", TeamID: "team-1", TeamMemoryCommit: "c1", RuleSlug: "a-rule", Phase: "execute", LoadedAt: start},
			{ExecutionID: "exec-1", TeamID: "team-1", TeamMemoryCommit: "c1", RuleSlug: "a-rule", Phase: "execute", LoadedAt: start},
		},
	}}
	runs, err := taskExecutions(context.Background(), HandlerDeps{
		AgentActivityRepo: activity,
		TeamRuleAuditRepo: ruleAudits,
	}, "agent-1", "worker-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.TeamRuleSnapshot == nil || got.TeamRuleSnapshot.TeamID != "team-1" ||
		got.TeamRuleSnapshot.Commit != "c1" || got.TeamRuleSnapshot.Phase != "execute" {
		t.Fatalf("team rule snapshot = %+v", got.TeamRuleSnapshot)
	}
	if len(got.LoadedRuleIDs) != 2 || got.LoadedRuleIDs[0] != "a-rule" || got.LoadedRuleIDs[1] != "z-rule" {
		t.Fatalf("loaded rule ids = %+v", got.LoadedRuleIDs)
	}
}

func TestTaskExecutionsProjectsRecoveryDiagnostics(t *testing.T) {
	start := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	repo := readModelActivityRepo{events: []*agent.AgentActivityEvent{
		readModelEvent(t, "01", `{"event":"executor.start","cli":"codex","model":"gpt-5"}`, start),
		readModelEvent(t, "02", `{"event":"executor.recovery_slot_conflict","reason":"duplicate_running_slot","detail":"slot conflict","decision":"not_adopted","outcome":"running"}`, start.Add(time.Minute)),
		readModelEvent(t, "03", `{"event":"executor.recovery_quiet_finalized","reason":"no_backfill_guard","detail":"old terminal","decision":"quiet_finalized","outcome":"succeeded"}`, start.Add(2*time.Minute)),
	}}
	runs, err := taskExecutions(context.Background(), HandlerDeps{AgentActivityRepo: repo}, "agent-1", "worker-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.HealthStatus != "control_plane_lost" || got.ControlPlaneHealth != "control_plane_lost" ||
		got.RecoveryStatus != "quiet_finalized" || got.Outcome != "quiet_finalized" ||
		got.ErrorKind != "no_backfill_guard" || got.ErrorDetail != "old terminal" {
		t.Fatalf("recovery diagnostic run = %+v", got)
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
