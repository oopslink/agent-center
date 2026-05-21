package query_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

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
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
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
		Tasks:         trsqlite.NewTaskRepo(db),
		Executions:    trsqlite.NewTaskExecutionRepo(db),
		Artifacts:     trsqlite.NewArtifactRepo(db),
		InputReqs:     trsqlite.NewInputRequestRepo(db),
		Issues:        disqlite.NewIssueRepo(db),
		Conversations: convsqlite.NewConversationRepo(db),
		Messages:      convsqlite.NewMessageRepo(db),
		Workers:       wfsqlite.NewWorkerRepo(db),
		Mappings:      wfsqlite.NewMappingRepo(db),
		Proposals:     wfsqlite.NewProposalRepo(db),
		Projects:      wfsqlite.NewProjectRepo(db),
	}
	return &qenv{db: db, deps: deps, svc: query.NewService(deps), sink: sink, er: er, clk: clk, gen: gen}
}

func (e *qenv) seedTask(t *testing.T, id, project, title string) *task.Task {
	t.Helper()
	tk, err := task.New(task.NewInput{
		ID: taskruntime.TaskID(id), ProjectID: project, Title: title,
		CreatedBy: "user:test", Now: e.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Tasks.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	return tk
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
		ID: conversation.ConversationID(id), Kind: kind, Title: "t", OpenedAt: e.clk.Now(),
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
		ID: workforce.ProjectID(id), Name: name, Kind: workforce.ProjectKind(""),
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
	if _, ok := data["executions"]; !ok {
		t.Fatal("expected executions key")
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
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	now := env.clk.Now()
	if _, _, err := env.deps.Projection.UpsertIfFresh(context.Background(), "E-1", projection.ProjectionUpdate{LastPushAt: now, CurrentActivity: "edit"}); err != nil {
		t.Fatal(err)
	}
	res, err := env.svc.Inspect(context.Background(), "execution", "E-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	proj, ok := data["projection"].(map[string]any)
	if !ok {
		t.Fatalf("projection key missing: %+v", data)
	}
	if proj["current_activity"] != "edit" {
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
	env.seedIssue(t, "I-1", "proj", "discuss")
	res, err := env.svc.Inspect(context.Background(), "issue", "I-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Data.(map[string]any)["id"] != "I-1" {
		t.Fatal("issue id mismatch")
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

func TestInspect_Worktree(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	res, err := env.svc.Inspect(context.Background(), "worktree", "E-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Data.(map[string]any)["execution_id"] != "E-1" {
		t.Fatal("worktree exec id")
	}
}

func TestInspect_InputRequest(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusInputRequired)
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID: taskruntime.InputRequestID("IR-1"), TaskExecutionID: "E-1",
		Question: "yes or no?", Urgency: inputrequest.UrgencyNormal, Now: env.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.InputReqs.Save(context.Background(), ir); err != nil {
		t.Fatal(err)
	}
	res, err := env.svc.Inspect(context.Background(), "input_request", "IR-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["id"] != "IR-1" || data["question"] != "yes or no?" {
		t.Fatalf("ir inspect: %+v", data)
	}
}

func TestInspect_Supervisor_Decision_Phase6Stub(t *testing.T) {
	env := newQEnv(t)
	r, err := env.svc.Inspect(context.Background(), "supervisor", "S-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Data.(map[string]any)["available_in_phase"] != 6 {
		t.Fatal("supervisor stub missing phase tag")
	}
	r, err = env.svc.Inspect(context.Background(), "decision", "D-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Data.(map[string]any)["available_in_phase"] != 6 {
		t.Fatal("decision stub missing phase tag")
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

func TestQuery_Executions_ByWorker(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	env.seedExecution(t, "E-2", "T-1", "W-2", execution.StatusWorking)
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{WorkerID: "W-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Items))
	}
}

func TestQuery_Executions_FailedReasonFilter(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusFailed)
	env.seedExecution(t, "E-2", "T-1", "W-2", execution.StatusWorking)
	// Note: FindActive doesn't return failed; passing worker filter works.
	res, err := env.svc.Query(context.Background(), "executions", query.QueryFilter{WorkerID: "W-1", Status: "failed", FailedReason: "agent_crashed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Items))
	}
}

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
	env.seedIssue(t, "I-1", "proj", "x")
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

func TestQuery_Decisions_Phase6Empty(t *testing.T) {
	env := newQEnv(t)
	res, err := env.svc.Query(context.Background(), "decisions", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("expected empty, got %d", len(res.Items))
	}
}

func TestQuery_InputRequests_PendingOnly(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusInputRequired)
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1", Question: "?", Urgency: inputrequest.UrgencyNormal, Now: env.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.InputReqs.Save(context.Background(), ir); err != nil {
		t.Fatal(err)
	}
	res, err := env.svc.Query(context.Background(), "input_requests", query.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Items))
	}
}

func TestQuery_Proposals_FilterByWorker(t *testing.T) {
	env := newQEnv(t)
	p, err := workforce.NewWorkerProjectProposal(workforce.NewProposalInput{
		ID: "P-1", WorkerID: "W-1", CandidatePath: "/tmp/a", SuggestedProjectID: "x",
		SuggestedKind: workforce.ProjectKind(""),
		ProposedAt:    env.clk.Now(),
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
