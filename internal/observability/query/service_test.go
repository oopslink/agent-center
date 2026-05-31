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
	"github.com/oopslink/agent-center/internal/discussion"
	disqlite "github.com/oopslink/agent-center/internal/discussion/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsqlite "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
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
		Projection:    obsqlite.NewProjectionRepo(db),
		Executions:    trsqlite.NewTaskExecutionRepo(db),
		Artifacts:     trsqlite.NewArtifactRepo(db),
		Issues:        disqlite.NewIssueRepo(db),
		Conversations: convsqlite.NewConversationRepo(db),
		Messages:      convsqlite.NewMessageRepo(db),
		Workers:       wfsqlite.NewWorkerRepo(db),
		Mappings:      wfsqlite.NewMappingRepo(db),
		Proposals:     wfsqlite.NewProposalRepo(db),
		Projects:      wfsqlite.NewProjectRepo(db),
		WorkItemProjections: obsqlite.NewAgentWorkItemProjectionRepo(db),
		WorkItems:           agentsqlite.NewWorkItemRepo(db),
		PMTasks:             pmsqlite.NewTaskRepo(db),
		PMProjects:          pmsqlite.NewProjectRepo(db),
		PMIssues:            pmsqlite.NewIssueRepo(db),
		Agents:              agentsqlite.NewAgentRepo(db),
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

func (e *qenv) seedExecution(t *testing.T, id, taskID, workerID string, status execution.Status) *execution.TaskExecution {
	t.Helper()
	ex, err := execution.New(execution.NewInput{
		ID:            taskruntime.TaskExecutionID(id),
		TaskID:        taskruntime.TaskID(taskID),
		WorkerID:      workerID,
		AgentCLI:      "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree,
		Now:           e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Executions.Save(context.Background(), ex); err != nil {
		t.Fatal(err)
	}
	if status != execution.StatusSubmitted {
		_ = ex.AckDispatch(e.clk.Now())
		if err := e.deps.Executions.Update(context.Background(), ex); err != nil {
			t.Fatal(err)
		}
		_ = ex.StartWorking("/tmp", e.clk.Now())
		if err := e.deps.Executions.Update(context.Background(), ex); err != nil {
			t.Fatal(err)
		}
		switch status {
		case execution.StatusCompleted:
			_ = ex.MarkCompleted(execution.CompletedAgentReportedSuccess, "ok", e.clk.Now())
		case execution.StatusFailed:
			_ = ex.MarkFailed(execution.FailedAgentCrashed, "boom", e.clk.Now())
		case execution.StatusInputRequired:
			_ = ex.EnterInputRequired(taskruntime.InputRequestID("IR-1"), e.clk.Now())
		}
		if status != execution.StatusWorking {
			if err := e.deps.Executions.Update(context.Background(), ex); err != nil {
				t.Fatal(err)
			}
		}
	}
	return ex
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

func (e *qenv) seedIssue(t *testing.T, id, project, title string) *discussion.Issue {
	t.Helper()
	i, err := discussion.NewIssue(discussion.NewIssueInput{
		ID: discussion.IssueID(id), ProjectID: project, Title: title,
		OpenedByIdentityID: "user:test", Origin: discussion.OriginCLI, OpenedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Issues.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	return i
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

func (e *qenv) seedProject(t *testing.T, id, name string) *workforce.Project {
	t.Helper()
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID: workforce.ProjectID(id), Name: name,
		CreatedByIdentityID: "user:test", CreatedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Projects.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
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

// TestInspect_Task_WorkItemsSubSection pins the proj-B work-items sub-section:
// inspectTask lists the agent work items for the pm task (resolved by task_ref
// "pm://tasks/{id}") — the section proj-A deferred until inspectTask read pm.
func TestInspect_Task_WorkItemsSubSection(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "proj", "hello")
	env.seedWorkItem(t, "WI-1", "AG-1", "T-1")
	env.seedWorkItem(t, "WI-2", "AG-2", "T-1")
	res, err := env.svc.Inspect(context.Background(), "task", "T-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	wis, ok := data["work_items"].([]any)
	if !ok || len(wis) != 2 {
		t.Fatalf("expected 2 work_items for the task, got %+v", data["work_items"])
	}
	first := wis[0].(map[string]any)
	if first["work_item_id"] != "WI-1" || first["task_id"] != "T-1" {
		t.Fatalf("work item summary fields wrong: %+v", first)
	}
}

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

func TestInspect_Execution_WithProjection(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "proj", "h")
	env.seedWorkItem(t, "WI-1", "AG-1", "T-1")
	env.seedWorkItemProjection(t, "WI-1", "AG-1", "active") // sets CurrentActivity:"edit"
	res, err := env.svc.Inspect(context.Background(), "execution", "WI-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	proj, ok := data["projection"].(query.WorkItemRow)
	if !ok {
		t.Fatalf("projection key missing/wrong type: %+v", data)
	}
	if proj.CurrentActivity != "edit" || proj.WorkItemID != "WI-1" {
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
	env.seedProject(t, "proj-x", "X")
	res, err := env.svc.Inspect(context.Background(), "project", "proj-x")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["id"] != "proj-x" || data["name"] != "X" {
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

// v2.7 #107 Phase-2 (proj-A): query executions by-worker = worker→its agents→
// their work items (Q3 MAP).
func TestQuery_Executions_ByWorker(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedAgent(t, "AG-1", "W-1")
	env.seedAgent(t, "AG-2", "W-2")
	env.seedWorkItem(t, "WI-1", "AG-1", "T-1")
	env.seedWorkItemProjection(t, "WI-1", "AG-1", "active")
	env.seedWorkItem(t, "WI-2", "AG-2", "T-1")
	env.seedWorkItemProjection(t, "WI-2", "AG-2", "active")
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{WorkerID: "W-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 (W-1's agent's work item), got %d", len(res.Items))
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

func TestQuery_Proposals_FilterByWorker(t *testing.T) {
	env := newQEnv(t)
	p, err := workforce.NewWorkerProjectProposal(workforce.NewProposalInput{
		ID: "P-1", WorkerID: "W-1", CandidatePath: "/tmp/a", SuggestedProjectID: "x",
		ProposedAt: env.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.Proposals.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	res, err := env.svc.Query(context.Background(), "proposals", query.QueryFilter{WorkerID: "W-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(res.Items))
	}
}

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

func (e *qenv) seedAgent(t *testing.T, agentID, workerID string) {
	t.Helper()
	a, err := agentpkg.NewAgent(agentpkg.NewAgentInput{
		ID: agentpkg.AgentID(agentID), OrganizationID: "org-test",
		Profile: agentpkg.Profile{Name: agentID}, WorkerID: workerID,
		CreatedBy: "user:test", CreatedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentsqlite.NewAgentRepo(e.db).Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
}

func (e *qenv) seedWorkItem(t *testing.T, wiID, agentID, taskID string) {
	t.Helper()
	wi, err := agentpkg.NewWorkItem(agentpkg.NewWorkItemInput{
		ID: wiID, AgentID: agentpkg.AgentID(agentID), TaskRef: "pm://tasks/" + taskID, CreatedAt: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentsqlite.NewWorkItemRepo(e.db).Save(context.Background(), wi); err != nil {
		t.Fatal(err)
	}
}

func (e *qenv) seedWorkItemProjection(t *testing.T, wiID, agentID, status string) {
	t.Helper()
	if _, _, err := obsqlite.NewAgentWorkItemProjectionRepo(e.db).UpsertIfFresh(context.Background(), wiID, projection.AgentWorkItemProjectionUpdate{
		AgentID: agentID, Status: status, CurrentActivity: "edit", TotalToolCalls: 2, TotalTokensInput: 100, TotalTokensOutput: 50, LastActivityAt: e.clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// seedLiveWorkItem wires a full live work item: org-project + pm task + agent
// work item (task_ref) + projection — so fleet's resolve (proj→task_ref→pm
// task→project→org) has every hop.
func (e *qenv) seedLiveWorkItem(t *testing.T, wiID, agentID, taskID, projectID, orgID, status string) {
	t.Helper()
	e.seedOrgProject(t, projectID, orgID)
	e.seedTask(t, taskID, projectID, taskID)
	e.seedWorkItem(t, wiID, agentID, taskID)
	e.seedWorkItemProjection(t, wiID, agentID, status)
}
