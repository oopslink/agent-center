package query_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsqlite "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsqlite "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

type qenv struct {
	db   *sql.DB
	deps query.Deps
	svc  *query.Service
	sink *observability.EventSink
	er   *obsqlite.EventRepo
	clk  *clock.FakeClock
	gen  idgen.Generator
}

func newQEnv(t *testing.T) *qenv {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	deps := query.Deps{
		Events:        er,
		Conversations: convsqlite.NewConversationRepo(db),
		Messages:      convsqlite.NewMessageRepo(db),
		Workers:       wfsqlite.NewWorkerRepo(db),
		PMTasks:       pmsqlite.NewTaskRepo(db),
		PMProjects:    pmsqlite.NewProjectRepo(db),
		PMIssues:      pmsqlite.NewIssueRepo(db),
		Agents:        agentsqlite.NewAgentRepo(db),
	}
	return &qenv{db: db, deps: deps, svc: query.NewService(deps), sink: sink, er: er, clk: clk, gen: gen}
}

// seedTask seeds a pm.Task (v2.7 #107 Phase-2 proj-B: observability tasks read
// the pm model). Created in the default open status.
func (e *qenv) seedTask(t *testing.T, id, project, title string) *pm.Task {
	t.Helper()
	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: pm.TaskID(id), ProjectID: pm.ProjectID(project), Title: title,
		CreatedBy: "user:test", CreatedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.PMTasks.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	return tk
}

// seedTaskStatus seeds a pm.Task in a specific status (via rehydrate) for the
// status-set query tests. v2.7 #107 Phase-2 proj-B.
func (e *qenv) seedTaskStatus(t *testing.T, id, project string, status pm.TaskStatus) {
	t.Helper()
	tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(id), ProjectID: pm.ProjectID(project), Title: id,
		Status: status, CreatedBy: "user:test",
		CreatedAt: e.clk.Now(), UpdatedAt: e.clk.Now(), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.PMTasks.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
}

func (e *qenv) seedWorker(t *testing.T, id string, status workforce.WorkerStatus) *workforce.Worker {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID: workforce.WorkerID(id), Capabilities: []string{"claude-code"}, EnrolledAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Workers.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if status == workforce.WorkerOnline {
		_ = e.deps.Workers.UpdateStatus(context.Background(), w.ID(), workforce.WorkerOffline, workforce.WorkerOnline, w.Version())
	}
	return w
}

// seedWorkerOrg seeds an online worker assigned to a specific organization —
// for the #131 §-1 #4 ActiveCount org-scope tests (worker.org is the gate the
// ActiveCount path scopes by). Workers carry organization_id directly (v2.6 X1).
func (e *qenv) seedWorkerOrg(t *testing.T, id, orgID string) *workforce.Worker {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID: workforce.WorkerID(id), Capabilities: []string{"claude-code"},
		OrganizationID: orgID, EnrolledAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Workers.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	_ = e.deps.Workers.UpdateStatus(context.Background(), w.ID(), workforce.WorkerOffline, workforce.WorkerOnline, w.Version())
	return w
}

func (e *qenv) seedConversation(t *testing.T, id string, kind conversation.ConversationKind) *conversation.Conversation {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID(id), Kind: kind, Name: "t",
		CreatedBy: conversation.IdentityRef("system"), OpenedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Conversations.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInspect_Task_HappyPath(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "proj", "hello")
	res, err := env.svc.Inspect(context.Background(), "task", "T-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["id"] != "T-1" || data["title"] != "hello" {
		t.Fatalf("inspect task: %+v", data)
	}
	// v2.7 #107 Phase-2 (proj-B): reads pm.Task. priority/conversation_id dropped;
	// pm fields present.
	if _, ok := data["assignee"]; !ok {
		t.Fatalf("expected assignee key (pm field): %+v", data)
	}
	if _, ok := data["priority"]; ok {
		t.Fatalf("priority should be dropped (no pm.Task field): %+v", data)
	}
	if data["created_by"] != "user:test" {
		t.Fatalf("expected created_by from pm.Task: %+v", data)
	}
}

// TestInspect_Task_WorkItemsSubSection removed — the inspectTask "work_items"
// sub-section is dropped in v2.14.0 F7 (issue I14): the AgentWorkItem model was
// retired, the task IS the unit of agent work now, so there is no per-task
// work-item list to enumerate.

func TestInspect_Task_NotFound(t *testing.T) {
	env := newQEnv(t)
	_, err := env.svc.Inspect(context.Background(), "task", "T-missing")
	if !errors.Is(err, query.ErrInspectNotFound) {
		t.Fatalf("expected ErrInspectNotFound, got %v", err)
	}
}

func TestInspect_UnknownKind(t *testing.T) {
	env := newQEnv(t)
	_, err := env.svc.Inspect(context.Background(), "blob", "X")
	if !errors.Is(err, query.ErrInspectKindUnknown) {
		t.Fatalf("expected ErrInspectKindUnknown, got %v", err)
	}
}

// v2.14.0 F7 (issue I14): inspect "execution" reads pm_tasks (the task is the
// unit of agent work). The id is a TASK id; the projection row's CurrentActivity
// surfaces the blocked_reason, and WorkItemID == task id.
func TestInspect_Execution_WithProjection(t *testing.T) {
	env := newQEnv(t)
	// waiting_input → running + blocked_reason "edit" → CurrentActivity "edit".
	env.seedAgentTask(t, "WI-1", "AG-1", "proj", "waiting_input")
	res, err := env.svc.Inspect(context.Background(), "execution", "WI-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	proj, ok := data["projection"].(query.TaskExecRow)
	if !ok {
		t.Fatalf("projection key missing/wrong type: %+v", data)
	}
	if proj.CurrentActivity != "edit" || proj.TaskID != "WI-1" {
		t.Fatalf("projection: %+v", proj)
	}
}

func TestInspect_Worker(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	res, err := env.svc.Inspect(context.Background(), "worker", "W-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["id"] != "W-1" {
		t.Fatalf("worker id: %+v", data)
	}
}

func TestInspect_Issue(t *testing.T) {
	env := newQEnv(t)
	env.seedPMIssue(t, "I-1", "proj", "discuss", pm.IssueOpen)
	res, err := env.svc.Inspect(context.Background(), "issue", "I-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	// v2.7 #125: inspect-issue reads pm_issues. opened_by←created_by,
	// opened_at←created_at, +description/updated_at; origin/conversation_id/
	// messages dropped (pm.Issue has no such concepts).
	if data["id"] != "I-1" || data["status"] != "open" || data["opened_by"] != "user:test" {
		t.Fatalf("pm issue inspect fields: %+v", data)
	}
	if _, ok := data["origin"]; ok {
		t.Fatal("origin must be dropped (pm.Issue has no origin)")
	}
	if _, ok := data["messages"]; ok {
		t.Fatal("messages section must be dropped (pm.Issue has no conversation link)")
	}
}

func TestQuery_Issues_DefaultNonTerminalAndStatus(t *testing.T) {
	env := newQEnv(t)
	env.seedPMIssue(t, "I-open", "proj", "o", pm.IssueOpen)
	env.seedPMIssue(t, "I-inprog", "proj", "p", pm.IssueInProgress)
	env.seedPMIssue(t, "I-resolved", "proj", "r", pm.IssueResolved) // terminal
	// default (no filter) = non-terminal set → excludes resolved.
	res, err := env.svc.Query(context.Background(), "issues", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("default non-terminal: want 2 (open+in_progress), got %d", len(res.Items))
	}
	// explicit status filter surfaces the requested status (incl terminal).
	res, err = env.svc.Query(context.Background(), "issues", query.QueryFilter{Status: "resolved"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("status=resolved: want 1, got %d", len(res.Items))
	}
}

func TestInspect_Conversation(t *testing.T) {
	env := newQEnv(t)
	env.seedConversation(t, "C-1", conversation.ConversationKindTask)
	res, err := env.svc.Inspect(context.Background(), "conversation", "C-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Data.(map[string]any)["id"] != "C-1" {
		t.Fatal("conv id mismatch")
	}
}

func TestInspect_Project(t *testing.T) {
	env := newQEnv(t)
	// v2.7 #131: inspect-project reads pm_projects (workforce project model retired).
	env.seedOrgProject(t, "proj-x", "org-1")
	res, err := env.svc.Inspect(context.Background(), "project", "proj-x")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["id"] != "proj-x" || data["name"] != "proj-x" || data["organization_id"] != "org-1" {
		t.Fatalf("project: %+v", data)
	}
}

// TestInspect_Worktree removed — inspect "worktree" verb dropped in v2.7 #107
// Phase-2 (proj-A): worktree detail is execution-keyed with no work-item model
// equivalent (worktree state lives in supervisormanager runtime).

// TestInspect_Supervisor_Decision_Removed verifies that supervisor/decision
// inspect kinds return an error in v2.6 (both were removed in BE-9).
func TestInspect_Supervisor_Decision_Removed(t *testing.T) {
	env := newQEnv(t)
	for _, kind := range []string{"supervisor", "decision"} {
		_, err := env.svc.Inspect(context.Background(), kind, "X-1")
		if err == nil {
			t.Fatalf("inspect %q: expected error for removed kind, got nil", kind)
		}
	}
}

func TestInspect_EmptyID_Errors(t *testing.T) {
	env := newQEnv(t)
	if _, err := env.svc.Inspect(context.Background(), "task", ""); !errors.Is(err, query.ErrInspectIDRequired) {
		t.Fatalf("expected ErrInspectIDRequired, got %v", err)
	}
}

func TestQuery_UnknownResource(t *testing.T) {
	env := newQEnv(t)
	_, err := env.svc.Query(context.Background(), "blob", query.QueryFilter{})
	if !errors.Is(err, query.ErrQueryResourceUnknown) {
		t.Fatalf("expected ErrQueryResourceUnknown, got %v", err)
	}
}

func TestQuery_Tasks_ByProject(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "proj", "a")
	env.seedTask(t, "T-2", "proj", "b")
	env.seedTask(t, "T-3", "other", "c")
	res, err := env.svc.Query(context.Background(), "tasks", query.QueryFilter{ProjectID: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2, got %d", len(res.Items))
	}
}

// v2.14.0 F7 (issue I14): query executions by-worker = worker→its agents→
// their agent-assigned tasks (ListByWorker → ListByAssignee).
func TestQuery_Executions_ByWorker(t *testing.T) {
	env := newQEnv(t)
	env.seedAgent(t, "AG-1", "W-1")
	env.seedAgent(t, "AG-2", "W-2")
	env.seedAgentTask(t, "WI-1", "AG-1", "p", "active")
	env.seedAgentTask(t, "WI-2", "AG-2", "p", "active")
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{WorkerID: "W-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 (W-1's agent's task), got %d", len(res.Items))
	}
}

// TestQuery_Executions_FailedReasonFilter removed — the exec-specific
// FailedReason query filter is dropped in v2.7 #107 Phase-2 (proj-A, P3):
// "why failed" is now observable via `inspect execution <work_item_id>`
// recent_events (the failed transition's Cause), not a query filter.

func TestQuery_Workers_FilterByStatus(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedWorker(t, "W-2", workforce.WorkerOffline)
	res, err := env.svc.Query(context.Background(), "workers", query.QueryFilter{Status: "online"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 online worker, got %d", len(res.Items))
	}
}

func TestQuery_Issues_ByProject(t *testing.T) {
	env := newQEnv(t)
	env.seedPMIssue(t, "I-1", "proj", "x", pm.IssueOpen)
	res, err := env.svc.Query(context.Background(), "issues", query.QueryFilter{ProjectID: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(res.Items))
	}
}

func TestQuery_Events_PrefixTypeMatch(t *testing.T) {
	env := newQEnv(t)
	for i := 0; i < 6; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: observability.EventType("task.created"),
			Refs:      observability.EventRefs{TaskID: "T-1"},
			Actor:     observability.Actor("user:hayang"),
		})
	}
	for i := 0; i < 6; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: observability.EventType("issue.opened"),
			Refs:      observability.EventRefs{IssueID: "I-1"},
			Actor:     observability.Actor("user:hayang"),
		})
	}
	res, err := env.svc.Query(context.Background(), "events", query.QueryFilter{EventType: "task.", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 6 {
		t.Fatalf("prefix filter: got %d, want 6", len(res.Items))
	}
	for _, it := range res.Items {
		m := it.(map[string]any)
		if !strings.HasPrefix(m["event_type"].(string), "task.") {
			t.Fatalf("non-task event leaked: %v", m["event_type"])
		}
	}
}

func TestQuery_Events_RefsFilter(t *testing.T) {
	env := newQEnv(t)
	_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "task.created", Refs: observability.EventRefs{TaskID: "T-42"}, Actor: "user:h",
	})
	_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "task.created", Refs: observability.EventRefs{TaskID: "T-99"}, Actor: "user:h",
	})
	res, err := env.svc.Query(context.Background(), "events", query.QueryFilter{TaskID: "T-42"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("refs filter: %d", len(res.Items))
	}
}

func TestQuery_Events_LimitTooLarge(t *testing.T) {
	env := newQEnv(t)
	_, err := env.svc.Query(context.Background(), "events", query.QueryFilter{Limit: observability.MaxEventQueryLimit + 1})
	if !errors.Is(err, observability.ErrEventQueryLimitTooLarge) {
		t.Fatalf("expected ErrEventQueryLimitTooLarge, got %v", err)
	}
}

// TestQuery_Decisions_Removed verifies that "decisions" returns an error in v2.6.
func TestQuery_Decisions_Removed(t *testing.T) {
	env := newQEnv(t)
	_, err := env.svc.Query(context.Background(), "decisions", query.QueryFilter{})
	if err == nil {
		t.Fatal("expected error for removed 'decisions' resource, got nil")
	}
}

// TestQuery_Proposals_FilterByWorker removed — the `query proposals` verb is
// deleted in v2.7 #131 (workforce WorkerProjectProposal model retired).

func TestQuery_Events_Cursor_Pagination(t *testing.T) {
	env := newQEnv(t)
	for i := 0; i < 250; i++ {
		_, _ = env.sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task.created", Actor: "user:h",
		})
	}
	cursor := ""
	seen := map[string]bool{}
	for pages := 0; pages < 10; pages++ {
		res, err := env.svc.Query(context.Background(), "events", query.QueryFilter{Limit: 100, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Items) == 0 {
			break
		}
		for _, it := range res.Items {
			id := it.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatalf("duplicate id %s page %d", id, pages)
			}
			seen[id] = true
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(seen) != 250 {
		t.Fatalf("expected 250 events, got %d", len(seen))
	}
}

// --- v2.7 #107 fleet repoint seed helpers (new work-item model) ---

// seedOrgProject seeds the org-owning project in the PM model (pm_projects) —
// the SAME source fleet resolves a work item's org from at runtime (WI → pm task
// → pm project → org). v2.7 #107: it must NOT seed the retired workforce
// `projects` table; doing so masked the cross-model org-scope bug (workforce
// projects are empty at runtime, so org-scope failed closed on every WI).
func (e *qenv) seedOrgProject(t *testing.T, projectID, orgID string) {
	t.Helper()
	p, err := pm.NewProject(pm.NewProjectInput{
		ID: pm.ProjectID(projectID), Name: projectID, OrganizationID: orgID,
		CreatedBy: "user:test", CreatedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pmsqlite.NewProjectRepo(e.db).Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

// archiveProject flips an already-seeded project to archived (domain Archive +
// repo Update), so fleet tests can assert an archived project's tasks are excluded
// from the executions list and ActiveCount.
func (e *qenv) archiveProject(t *testing.T, projectID string) {
	t.Helper()
	repo := pmsqlite.NewProjectRepo(e.db)
	p, err := repo.FindByID(context.Background(), pm.ProjectID(projectID))
	if err != nil {
		t.Fatal(err)
	}
	p.Archive(e.clk.Now())
	if err := repo.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

// seedPMIssue seeds a pm issue (pm_issues) in a project with a given status —
// the fleet pending-issues source after the #119 repoint. Use a real pm project
// (seedOrgProject) for org-scoped tests so org resolves via the pm source.
func (e *qenv) seedPMIssue(t *testing.T, issueID, projectID, title string, status pm.IssueStatus) {
	t.Helper()
	i, err := pm.RehydrateIssue(pm.RehydrateIssueInput{
		ID: pm.IssueID(issueID), ProjectID: pm.ProjectID(projectID), Title: title, Status: status,
		CreatedBy: "user:test", CreatedAt: e.clk.Now(), UpdatedAt: e.clk.Now(), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pmsqlite.NewIssueRepo(e.db).Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
}

// seedAgent seeds an agent on a worker. v2.14.0 F7 (issue I14): the agent's
// IdentityMemberID == its agentID so tasks assigned to "agent:<agentID>" resolve
// back through worker→agents→ListByAssignee for the fleet ActiveCount chain.
func (e *qenv) seedAgent(t *testing.T, agentID, workerID string) {
	t.Helper()
	a, err := agentpkg.NewAgent(agentpkg.NewAgentInput{
		ID: agentpkg.AgentID(agentID), OrganizationID: "org-test",
		Profile: agentpkg.Profile{Name: agentID}, WorkerID: workerID,
		IdentityMemberID: agentID,
		CreatedBy:        "user:test", CreatedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentsqlite.NewAgentRepo(e.db).Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
}

// seedAgentTask seeds an "execution": a non-terminal pm.Task assigned to an agent
// ("agent:<agentID>"), in the given execution status. v2.14.0 F7 (issue I14)
// retired the AgentWorkItem projection model — the task IS the unit of agent work
// now (work_item_id == task_id). The execution-status vocab maps back onto pm.Task:
// "active"→running, "waiting_input"→running+blocked_reason, anything else→open
// (queued). Token/tool metrics no longer exist on the Task model.
func (e *qenv) seedAgentTask(t *testing.T, taskID, agentID, projectID, status string) {
	t.Helper()
	pmStatus := pm.TaskOpen
	blockedReason := ""
	switch status {
	case "active":
		pmStatus = pm.TaskRunning
	case "waiting_input":
		pmStatus = pm.TaskRunning
		blockedReason = "edit"
	}
	tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(taskID), ProjectID: pm.ProjectID(projectID), Title: taskID,
		Status: pmStatus, Assignee: pm.IdentityRef("agent:" + agentID),
		BlockedReason: blockedReason,
		CreatedBy:     "user:test", CreatedAt: e.clk.Now(), UpdatedAt: e.clk.Now(), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.PMTasks.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
}

// seedTerminalAgentTask seeds a TERMINAL agent-assigned pm.Task (completed or
// discarded) for the executions-terminal stats counters (completed→done,
// discarded→canceled). v2.14.0 F7 (issue I14).
func (e *qenv) seedTerminalAgentTask(t *testing.T, taskID, agentID, projectID string, status pm.TaskStatus) {
	t.Helper()
	tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(taskID), ProjectID: pm.ProjectID(projectID), Title: taskID,
		Status: status, Assignee: pm.IdentityRef("agent:" + agentID),
		CreatedBy: "user:test", CreatedAt: e.clk.Now(), UpdatedAt: e.clk.Now(), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.PMTasks.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
}

// seedLiveExecution wires a full live agent execution: org-project + an
// agent-assigned non-terminal pm task — so fleet's resolve (task→project→org)
// has every hop. v2.14.0 F7: replaces the retired work-item/projection wiring.
func (e *qenv) seedLiveExecution(t *testing.T, taskID, agentID, projectID, orgID, status string) {
	t.Helper()
	e.seedOrgProject(t, projectID, orgID)
	e.seedAgentTask(t, taskID, agentID, projectID, status)
}
