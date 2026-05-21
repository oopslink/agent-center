package integration

import (
	"context"
	"strings"
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
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/service"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func seedPhase2DB(t *testing.T) (*service.TaskService, *service.InputRequestService, *dispatch.Service, task.Repository, execution.Repository, *obsqlite.EventRepo, *clock.FakeClock) {
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
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	irRepo := trsqlite.NewInputRequestRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)
	taskSvc := service.NewTaskService(db, taskRepo, convRepo, execRepo, msgRepo, sink, gen, clk)
	irSvc := service.NewInputRequestService(db, irRepo, execRepo, taskRepo, convRepo, msgRepo, sink, gen, clk, "")
	dispatchSvc := dispatch.NewService(db, taskRepo, execRepo, sink, dispatch.NoopSender{}, clk, gen, dispatch.DefaultConfig())
	return taskSvc, irSvc, dispatchSvc, taskRepo, execRepo, er, clk
}

func TestINT_Phase2_TaskAndConversationSameTx(t *testing.T) {
	taskSvc, _, _, taskRepo, _, er, _ := seedPhase2DB(t)
	ctx := context.Background()
	res, err := taskSvc.Create(ctx, service.TaskCreateInput{
		ProjectID:        "p-1",
		Title:            "do thing",
		WithConversation: true,
		Actor:            "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	tt, _ := taskRepo.FindByID(ctx, res.TaskID)
	if tt.ConversationID() != string(res.ConversationID) {
		t.Fatalf("conv id mismatch: %s vs %s", tt.ConversationID(), res.ConversationID)
	}
	events, _ := er.Find(ctx, observability.EventQueryFilter{Limit: 100})
	got := map[string]int{}
	for _, e := range events {
		got[e.Type().String()]++
	}
	if got["conversation.opened"] != 1 || got["task.created"] != 1 {
		t.Fatalf("events: %+v", got)
	}
}

func TestINT_Phase2_DispatchPathFullEvents(t *testing.T) {
	taskSvc, _, dispatchSvc, _, execRepo, er, _ := seedPhase2DB(t)
	ctx := context.Background()
	res, _ := taskSvc.Create(ctx, service.TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: true, Actor: "user:hayang",
	})
	dres, err := dispatchSvc.Dispatch(ctx, dispatch.DispatchInput{
		TaskID: res.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execRepo.FindByID(ctx, dres.ExecutionID); err != nil {
		t.Fatal(err)
	}
	events, _ := er.Find(ctx, observability.EventQueryFilter{Limit: 100})
	want := []string{"task.created", "task_execution.submitted", "task_execution.dispatched"}
	for _, w := range want {
		found := false
		for _, e := range events {
			if e.Type().String() == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing event %s", w)
		}
	}
}

func TestINT_Phase2_NoInputChannel_Rollback(t *testing.T) {
	taskSvc, irSvc, dispatchSvc, _, execRepo, _, clk := seedPhase2DB(t)
	ctx := context.Background()
	// Task without conversation
	res, _ := taskSvc.Create(ctx, service.TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: false, Actor: "user:hayang",
	})
	dres, _ := dispatchSvc.Dispatch(ctx, dispatch.DispatchInput{
		TaskID: res.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	// Advance to working
	e, _ := execRepo.FindByID(ctx, dres.ExecutionID)
	_ = e.AckDispatch(clk.Now())
	_ = execRepo.Update(ctx, e)
	_ = e.StartWorking("/cwd", clk.Now())
	_ = execRepo.Update(ctx, e)
	_, err := irSvc.Create(ctx, service.CreateInput{
		ExecutionID: dres.ExecutionID,
		Question:    "go ahead?",
		Actor:       observability.Actor("agent:" + string(dres.ExecutionID)),
	})
	if err == nil {
		t.Fatal("expected no_input_channel")
	}
	if !strings.Contains(err.Error(), "no_input_channel") {
		t.Fatalf("unexpected err: %v", err)
	}
	// Verify execution → failed(no_input_channel)
	e, _ = execRepo.FindByID(ctx, dres.ExecutionID)
	if e.Status() != execution.StatusFailed || e.FailedReason() != execution.FailedNoInputChannel {
		t.Fatalf("expected failed(no_input_channel): %s/%s", e.Status(), e.FailedReason())
	}
}

func TestINT_Phase2_IRRespondTxChain(t *testing.T) {
	taskSvc, irSvc, dispatchSvc, _, execRepo, er, clk := seedPhase2DB(t)
	ctx := context.Background()
	res, _ := taskSvc.Create(ctx, service.TaskCreateInput{
		ProjectID: "p-1", Title: "x", WithConversation: true, Actor: "user:hayang",
	})
	dres, _ := dispatchSvc.Dispatch(ctx, dispatch.DispatchInput{
		TaskID: res.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	e, _ := execRepo.FindByID(ctx, dres.ExecutionID)
	_ = e.AckDispatch(clk.Now())
	_ = execRepo.Update(ctx, e)
	_ = e.StartWorking("/cwd", clk.Now())
	_ = execRepo.Update(ctx, e)
	irRes, err := irSvc.Create(ctx, service.CreateInput{
		ExecutionID: dres.ExecutionID,
		Question:    "go?",
		Actor:       observability.Actor("agent:" + string(dres.ExecutionID)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := irSvc.Respond(ctx, service.RespondInput{
		InputRequestID: irRes.InputRequestID,
		Answer:         "yes",
		DecidedBy:      "human:hayang",
		Actor:          "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	e, _ = execRepo.FindByID(ctx, dres.ExecutionID)
	if e.Status() != execution.StatusWorking {
		t.Fatalf("expected working after respond: %s", e.Status())
	}
	// Verify input_request.responded event landed
	events, _ := er.Find(ctx, observability.EventQueryFilter{Limit: 100})
	found := false
	for _, ev := range events {
		if ev.Type().String() == "input_request.responded" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing input_request.responded event")
	}
}

// conventions § 9.w: schema declares no FOREIGN KEY. Referential
// integrity for task.project_id is now enforced at the application layer
// via TaskService.WithProjectExistenceChecker. This test wires a checker
// that always reports "not found" and asserts the service rejects the
// Create with ErrProjectNotFound.
func TestINT_Phase2_TaskProjectExistence_AppLayerEnforced(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Now())
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(ctx, db)
	sink := observability.NewEventSink(er, er, gen, clk)
	taskRepo := trsqlite.NewTaskRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)
	taskSvc := service.NewTaskService(db, taskRepo, convRepo, trsqlite.NewTaskExecutionRepo(db), msgRepo, sink, gen, clk).
		WithProjectExistenceChecker(stubProjectChecker{exists: false})
	_, err = taskSvc.Create(ctx, service.TaskCreateInput{
		ProjectID:        "missing-project",
		Title:            "x",
		WithConversation: false,
		Actor:            "user:hayang",
	})
	if err == nil {
		t.Fatal("expected app-layer project-not-found error")
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Fatalf("expected ErrProjectNotFound, got: %v", err)
	}
	_ = taskruntime.TaskID("")
	_ = conversation.ConversationID("")
	_ = inputrequest.UrgencyNormal // keep alias referenced
}

// stubProjectChecker is used in INT-P2-5 to drive the app-layer
// existence check without depending on a real Workforce repository wiring.
type stubProjectChecker struct{ exists bool }

func (s stubProjectChecker) ProjectExists(ctx context.Context, id string) (bool, error) {
	return s.exists, nil
}
