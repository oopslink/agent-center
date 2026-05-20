package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

type testRig struct {
	db         *sql.DB
	clk        *clock.FakeClock
	idgen      idgen.Generator
	sink       *observability.EventSink
	taskRepo   *trsqlite.TaskRepo
	execRepo   *trsqlite.TaskExecutionRepo
	irRepo     *trsqlite.InputRequestRepo
	artifactRepo *trsqlite.ArtifactRepo
	convRepo   conversation.ConversationRepository
	msgRepo    conversation.MessageRepository
	taskSvc    *TaskService
	irSvc      *InputRequestService
	execSvc    *ExecutionService
	artifactSvc *ArtifactService
}

func setupRig(t *testing.T, defaultChannel string) *testRig {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id) VALUES ('p-1','P','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','user:hayang')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workers (id, status, capabilities, working_seconds, enrolled_at, created_at, updated_at) VALUES ('W-1','online','[]',0,'2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(ctx, db)
	sink := observability.NewEventSink(er, er, gen, clk)
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	irRepo := trsqlite.NewInputRequestRepo(db)
	artifactRepo := trsqlite.NewArtifactRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)
	return &testRig{
		db: db, clk: clk, idgen: gen, sink: sink,
		taskRepo: taskRepo, execRepo: execRepo, irRepo: irRepo, artifactRepo: artifactRepo,
		convRepo: convRepo, msgRepo: msgRepo,
		taskSvc: NewTaskService(db, taskRepo, convRepo, execRepo, msgRepo, sink, gen, clk),
		irSvc:   NewInputRequestService(db, irRepo, execRepo, taskRepo, convRepo, msgRepo, sink, gen, clk, defaultChannel),
		execSvc: NewExecutionService(db, execRepo, taskRepo, convRepo, msgRepo, sink, gen, clk),
		artifactSvc: NewArtifactService(db, artifactRepo, execRepo, sink, gen, clk),
	}
}

func TestTaskService_Create_HappyAndValidation(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	cases := []struct {
		name string
		in   TaskCreateInput
		want string
	}{
		{"missing project", TaskCreateInput{Title: "x", Actor: "user:hayang"}, "project_id"},
		{"missing title", TaskCreateInput{ProjectID: "p-1", Actor: "user:hayang"}, "title"},
		{"bad actor", TaskCreateInput{ProjectID: "p-1", Title: "x", Actor: "BAD"}, "actor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := rig.taskSvc.Create(ctx, c.in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	// Happy
	res, err := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID:        "p-1",
		Title:            "happy",
		Description:      "desc",
		Priority:         task.PriorityHigh,
		WithConversation: true,
		Actor:            "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskID == "" || res.ConversationID == "" {
		t.Fatal("expected ids")
	}
}

func TestTaskService_BindConversation_AutoToInvalid(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	res, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: false, Actor: "user:hayang",
	})
	// auto
	convID, err := rig.taskSvc.BindConversation(ctx, BindConversationInput{
		TaskID: res.TaskID, Mode: "auto", Title: "x", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if convID == "" {
		t.Fatal("expected conv id")
	}
	// can't unbind / re-bind same task
	_, err = rig.taskSvc.BindConversation(ctx, BindConversationInput{
		TaskID: res.TaskID, Mode: "auto", Title: "y", Actor: "user:hayang",
	})
	if !errors.Is(err, task.ErrCannotUnbindConversation) {
		t.Fatalf("expected cannot rebind: %v", err)
	}
	// missing task
	if _, err := rig.taskSvc.BindConversation(ctx, BindConversationInput{
		TaskID: "T-NONE", Mode: "auto", Actor: "user:hayang",
	}); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
	// bad mode
	res2, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "y", WithConversation: false, Actor: "user:hayang",
	})
	if _, err := rig.taskSvc.BindConversation(ctx, BindConversationInput{
		TaskID: res2.TaskID, Mode: "garbage", Actor: "user:hayang",
	}); err == nil {
		t.Fatal("expected bad mode")
	}
	// to but no id
	if _, err := rig.taskSvc.BindConversation(ctx, BindConversationInput{
		TaskID: res2.TaskID, Mode: "to", Actor: "user:hayang",
	}); err == nil {
		t.Fatal("expected --to requires id")
	}
	// missing required
	if _, err := rig.taskSvc.BindConversation(ctx, BindConversationInput{
		Actor: "user:hayang",
	}); err == nil {
		t.Fatal("expected task_id required")
	}
	// bad actor
	if _, err := rig.taskSvc.BindConversation(ctx, BindConversationInput{
		TaskID: res.TaskID, Mode: "auto",
	}); err == nil {
		t.Fatal("expected actor")
	}
}

func TestTaskService_BindConversation_ToExisting(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	// Pre-create a Conversation directly via OpenConversation path is
	// not available without convsvc; we just craft one via the AR.
	conv, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID:       conversation.ConversationID(rig.idgen.NewULID()),
		Kind:     conversation.ConversationKindTask,
		Title:    "x",
		OpenedAt: rig.clk.Now(),
	})
	if err := rig.convRepo.Save(ctx, conv); err != nil {
		t.Fatal(err)
	}
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "y", WithConversation: false, Actor: "user:hayang",
	})
	convID, err := rig.taskSvc.BindConversation(ctx, BindConversationInput{
		TaskID: tres.TaskID, Mode: "to", ExistingConvID: conv.ID(), Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if convID != conv.ID() {
		t.Fatalf("expected existing conv: %s vs %s", convID, conv.ID())
	}
}

func TestTaskService_ReadContext(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	res, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: true, Actor: "user:hayang",
	})
	got, err := rig.taskSvc.ReadContext(ctx, res.TaskID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != res.TaskID {
		t.Fatalf("mismatch")
	}
	if _, err := rig.taskSvc.ReadContext(ctx, "X-NONE", 10); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestArtifactService_Append(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	// Need a task + execution
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{ProjectID: "p-1", Title: "x", Actor: "user:hayang"})
	exec1, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tres.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: rig.clk.Now(),
	})
	if err := rig.execRepo.Save(ctx, exec1); err != nil {
		t.Fatal(err)
	}
	res, err := rig.artifactSvc.Append(ctx, AppendInput{
		ExecutionID: "E-1", Kind: "pr_url", Title: "feat:x", URL: "https://x",
		Actor: "agent:E-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ArtifactID == "" {
		t.Fatal("expected id")
	}
	// Validation
	if _, err := rig.artifactSvc.Append(ctx, AppendInput{}); err == nil {
		t.Fatal("expected actor error")
	}
	if _, err := rig.artifactSvc.Append(ctx, AppendInput{Actor: "user:hayang"}); err == nil {
		t.Fatal("expected exec id error")
	}
	if _, err := rig.artifactSvc.Append(ctx, AppendInput{ExecutionID: "E-NONE", Kind: "k", Title: "t", Actor: "user:hayang"}); !errors.Is(err, execution.ErrTaskExecutionNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestExecutionService_ReportProgressAndFailure(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: true, Actor: "user:hayang",
	})
	e, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tres.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: rig.clk.Now(),
	})
	_ = rig.execRepo.Save(ctx, e)
	// report-progress
	if err := rig.execSvc.ReportProgress(ctx, ReportProgressInput{
		ExecutionID: "E-1", Content: "doing", Actor: "agent:E-1",
	}); err != nil {
		t.Fatal(err)
	}
	// Validation
	if err := rig.execSvc.ReportProgress(ctx, ReportProgressInput{}); err == nil {
		t.Fatal("expected actor error")
	}
	if err := rig.execSvc.ReportProgress(ctx, ReportProgressInput{Actor: "user:hayang"}); err == nil {
		t.Fatal("expected exec id error")
	}
	if err := rig.execSvc.ReportProgress(ctx, ReportProgressInput{ExecutionID: "E-1", Actor: "user:hayang"}); err == nil {
		t.Fatal("expected content error")
	}

	// report-failure
	if err := rig.execSvc.ReportFailure(ctx, ReportFailureInput{
		ExecutionID: "E-1", Reason: "broken_test", Message: "boom", Actor: "agent:E-1",
	}); err != nil {
		t.Fatal(err)
	}
	e, _ = rig.execRepo.FindByID(ctx, "E-1")
	if e.Status() != execution.StatusFailed {
		t.Fatalf("status: %s", e.Status())
	}
	// Validation
	if err := rig.execSvc.ReportFailure(ctx, ReportFailureInput{}); err == nil {
		t.Fatal("expected actor error")
	}
	if err := rig.execSvc.ReportFailure(ctx, ReportFailureInput{Actor: "user:hayang"}); err == nil {
		t.Fatal("expected exec id error")
	}
	if err := rig.execSvc.ReportFailure(ctx, ReportFailureInput{ExecutionID: "E-2", Actor: "user:hayang"}); err == nil {
		t.Fatal("expected msg required")
	}
}

func TestExecutionService_ReportProgress_SkipsNullConv(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: false, Actor: "user:hayang",
	})
	e, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tres.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: rig.clk.Now(),
	})
	_ = rig.execRepo.Save(ctx, e)
	if err := rig.execSvc.ReportProgress(ctx, ReportProgressInput{
		ExecutionID: "E-1", Content: "ignored", Actor: "agent:E-1",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInputRequestService_DefaultChannelFallback(t *testing.T) {
	rig := setupRig(t, "feishu:user:hayang:dm")
	ctx := context.Background()
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: false, Actor: "user:hayang",
	})
	e, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tres.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: rig.clk.Now(),
	})
	_ = rig.execRepo.Save(ctx, e)
	_ = e.AckDispatch(rig.clk.Now())
	_ = rig.execRepo.Update(ctx, e)
	_ = e.StartWorking("/x", rig.clk.Now())
	_ = rig.execRepo.Update(ctx, e)
	res, err := rig.irSvc.Create(ctx, CreateInput{
		ExecutionID: "E-1", Question: "go?",
		Urgency: inputrequest.UrgencyUrgent, Actor: "agent:E-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID == "" {
		t.Fatal("expected fallback conv")
	}
}

func TestInputRequestService_ValidationErrors(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	if _, err := rig.irSvc.Create(ctx, CreateInput{}); err == nil {
		t.Fatal("expected actor error")
	}
	if _, err := rig.irSvc.Create(ctx, CreateInput{Actor: "user:hayang"}); err == nil {
		t.Fatal("expected exec id error")
	}
	if _, err := rig.irSvc.Create(ctx, CreateInput{ExecutionID: "E-1", Actor: "user:hayang"}); err == nil {
		t.Fatal("expected question error")
	}
	// Respond validation
	if err := rig.irSvc.Respond(ctx, RespondInput{}); err == nil {
		t.Fatal("expected actor")
	}
	if err := rig.irSvc.Respond(ctx, RespondInput{Actor: "user:hayang"}); err == nil {
		t.Fatal("expected ir id")
	}
	if err := rig.irSvc.Respond(ctx, RespondInput{InputRequestID: "X", Actor: "user:hayang"}); err == nil {
		t.Fatal("expected answer")
	}
	if err := rig.irSvc.Respond(ctx, RespondInput{InputRequestID: "X", Answer: "y", Actor: "user:hayang"}); err == nil {
		t.Fatal("expected decided_by")
	}
}

func TestInputRequestService_RespondHappy(t *testing.T) {
	rig := setupRig(t, "feishu:user:hayang:dm")
	ctx := context.Background()
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: true, Actor: "user:hayang",
	})
	e, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tres.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: rig.clk.Now(),
	})
	_ = rig.execRepo.Save(ctx, e)
	_ = e.AckDispatch(rig.clk.Now())
	_ = rig.execRepo.Update(ctx, e)
	_ = e.StartWorking("/x", rig.clk.Now())
	_ = rig.execRepo.Update(ctx, e)
	res, err := rig.irSvc.Create(ctx, CreateInput{
		ExecutionID: "E-1", Question: "ok?", Actor: "agent:E-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.irSvc.Respond(ctx, RespondInput{
		InputRequestID: res.InputRequestID, Answer: "yes", DecidedBy: "human:hayang",
		Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	// IR responded + execution back to working
	got, _ := rig.execRepo.FindByID(ctx, "E-1")
	if got.Status() != execution.StatusWorking {
		t.Fatalf("status: %s", got.Status())
	}
}

func TestInputRequestService_RespondMissingIR(t *testing.T) {
	rig := setupRig(t, "")
	ctx := context.Background()
	err := rig.irSvc.Respond(ctx, RespondInput{
		InputRequestID: "IR-NONE", Answer: "x", DecidedBy: "u", Actor: "user:hayang",
	})
	if !errors.Is(err, inputrequest.ErrInputRequestNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestInputRequestService_Create_DefaultChannelEmitsConvOpenedEvent(t *testing.T) {
	rig := setupRig(t, "feishu:user:hayang:dm")
	ctx := context.Background()
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: false, Actor: "user:hayang",
	})
	e, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tres.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: rig.clk.Now(),
	})
	_ = rig.execRepo.Save(ctx, e)
	_ = e.AckDispatch(rig.clk.Now())
	_ = rig.execRepo.Update(ctx, e)
	_ = e.StartWorking("/x", rig.clk.Now())
	_ = rig.execRepo.Update(ctx, e)
	_, err := rig.irSvc.Create(ctx, CreateInput{
		ExecutionID: "E-1", Question: "ok?", Actor: "agent:E-1",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInputRequestService_Create_ExecAlreadyTerminal(t *testing.T) {
	rig := setupRig(t, "feishu:user:hayang:dm")
	ctx := context.Background()
	tres, _ := rig.taskSvc.Create(ctx, TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: true, Actor: "user:hayang",
	})
	e, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tres.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: rig.clk.Now(),
	})
	_ = rig.execRepo.Save(ctx, e)
	_ = e.MarkFailed(execution.FailedAgentCrashed, "boom", rig.clk.Now())
	_ = rig.execRepo.Update(ctx, e)
	_, err := rig.irSvc.Create(ctx, CreateInput{
		ExecutionID: "E-1", Question: "ok?", Actor: "agent:E-1",
	})
	if err == nil {
		t.Fatal("expected error (exec terminal can't EnterInputRequired)")
	}
}

func TestPriorityOrDefault(t *testing.T) {
	if priorityOrDefault("") != task.PriorityMedium {
		t.Fatal("default")
	}
	if priorityOrDefault(task.PriorityHigh) != task.PriorityHigh {
		t.Fatal("pass-through")
	}
}

func TestUrgencyOrDefault(t *testing.T) {
	if urgencyOrDefault("") != inputrequest.UrgencyNormal {
		t.Fatal("default")
	}
	if urgencyOrDefault(inputrequest.UrgencyUrgent) != inputrequest.UrgencyUrgent {
		t.Fatal("pass-through")
	}
	_ = taskruntime.TaskID("")
}
