package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// advanceToWorking forces the execution into working state for tests
// that need request-input. Bypasses ACK/spawn handshake.
func advanceToWorking(t *testing.T, app *App, executionID string) {
	t.Helper()
	ctx := context.Background()
	e, err := app.ExecRepo.FindByID(ctx, taskruntime.TaskExecutionID(executionID))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AckDispatch(app.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecRepo.Update(ctx, e); err != nil {
		t.Fatalf("update after ack: %v", err)
	}
	if err := e.StartWorking("/repo", app.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecRepo.Update(ctx, e); err != nil {
		t.Fatalf("update after start: %v", err)
	}
}

// seedProjectAndWorker creates a project P-1 + worker W-1 so task tests
// can dispatch.
func seedProjectAndWorker(t *testing.T, app *App) {
	t.Helper()
	ctx := context.Background()
	if _, err := app.ProjectSvc.Add(ctx, wfservice.AddCommand{
		ID:    workforce.ProjectID("p-1"),
		Name:  "Test Project",
		Actor: app.DefaultActor(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.EnrollSvc.Enroll(ctx, wfservice.EnrollCommand{
		WorkerID:      workforce.WorkerID("W-1"),
		Capabilities:  []string{"claude-code"},
		ActorIdentity: app.DefaultActor(),
	}); err != nil {
		t.Fatal(err)
	}
}

func runTaskHandler(t *testing.T, cmd *Command, args []string) (int, string, string) {
	t.Helper()
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	handler := cmd.Flags(fs)
	positionals, err := permissiveParse(fs, args)
	if err != nil {
		return int(ExitUsage), "", err.Error()
	}
	var out, errw bytes.Buffer
	code := handler(context.Background(), positionals, &out, &errw)
	return int(code), out.String(), errw.String()
}

func TestCLI_TaskCreate_Happy(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	cmd := findCmd(app.TaskCommands(), "create")
	code, out, errw := runTaskHandler(t, cmd, []string{"p-1", "do thing", "--no-conversation=true", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("code: %d / err: %s", code, errw)
	}
	var got struct {
		TaskID         string `json:"task_id"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID == "" {
		t.Fatal("expected task_id")
	}
	if got.ConversationID != "" {
		t.Fatalf("expected no conv (--no-conversation): %s", got.ConversationID)
	}
}

func TestCLI_TaskCreate_WithConversation(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	cmd := findCmd(app.TaskCommands(), "create")
	code, out, _ := runTaskHandler(t, cmd, []string{"p-1", "do thing", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(out, "conversation_id") {
		t.Fatalf("expected conv id in output: %s", out)
	}
}

func TestCLI_TaskCreate_UsageErrors(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	cmd := findCmd(app.TaskCommands(), "create")
	code, _, _ := runTaskHandler(t, cmd, []string{})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage err: %d", code)
	}
	code, _, _ = runTaskHandler(t, cmd, []string{"p-1", "title", "--priority=garbage"})
	if code != int(ExitUsage) {
		t.Fatalf("expected priority err: %d", code)
	}
}

func TestCLI_TaskUnbindConversation_NotImplemented(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	cmd := findCmd(app.TaskCommands(), "unbind-conversation")
	code, _, errw := runTaskHandler(t, cmd, []string{"T-1"})
	if code != int(ExitNotImplemented) {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errw, "not_implemented_v1") {
		t.Fatalf("err: %s", errw)
	}
}

func TestCLI_TaskBindConversation_Auto(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	// First create a task
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "x", "--no-conversation=true", "--format=json"})
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)
	cmd := findCmd(app.TaskCommands(), "bind-conversation")
	code, out2, errw := runTaskHandler(t, cmd, []string{created.TaskID, "--auto=true", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("code: %d / err: %s", code, errw)
	}
	if !strings.Contains(out2, "conversation_id") {
		t.Fatalf("out: %s", out2)
	}
}

func TestCLI_TaskBindConversation_UsageErrors(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	cmd := findCmd(app.TaskCommands(), "bind-conversation")
	code, _, _ := runTaskHandler(t, cmd, []string{})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage: %d", code)
	}
	code, _, _ = runTaskHandler(t, cmd, []string{"T-1"})
	if code != int(ExitUsage) {
		t.Fatalf("expected --auto/--to error: %d", code)
	}
}

func TestCLI_Dispatch_Happy(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "x", "--format=json"})
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)
	dispatch := app.DispatchCommand()
	code, out2, errw := runTaskHandler(t, dispatch, []string{created.TaskID, "--worker=W-1", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("code: %d / err: %s", code, errw)
	}
	if !strings.Contains(out2, "execution_id") {
		t.Fatalf("out: %s", out2)
	}
}

func TestCLI_Dispatch_UsageError(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	dispatch := app.DispatchCommand()
	code, _, _ := runTaskHandler(t, dispatch, []string{})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage")
	}
	code, _, _ = runTaskHandler(t, dispatch, []string{"T-1"})
	if code != int(ExitUsage) {
		t.Fatalf("expected --worker required")
	}
}

func TestCLI_KillExecution_UsageErrors(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	kc := app.KillExecutionCommand()
	code, _, _ := runTaskHandler(t, kc, []string{})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage")
	}
	code, _, _ = runTaskHandler(t, kc, []string{"E-1"})
	if code != int(ExitUsage) {
		t.Fatalf("expected reason")
	}
	code, _, _ = runTaskHandler(t, kc, []string{"E-1", "--reason=user_request"})
	if code != int(ExitUsage) {
		t.Fatalf("expected message")
	}
}

func TestCLI_RequestInput_NoInputChannel(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	// Create task without conversation
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "x", "--no-conversation=true", "--format=json"})
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)
	// Dispatch
	dispatch := app.DispatchCommand()
	_, dispOut, _ := runTaskHandler(t, dispatch, []string{created.TaskID, "--worker=W-1", "--format=json"})
	var execOut struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(dispOut), &execOut)
	advanceToWorking(t, app, execOut.ExecutionID)
	// request-input without conversation + no default_channel
	ri := findCmd(app.AgentRuntimeCommands(), "request-input")
	code, _, errw := runTaskHandler(t, ri, []string{execOut.ExecutionID, "--question=hi", "--format=json"})
	if code == int(ExitOK) {
		t.Fatal("expected error")
	}
	if !strings.Contains(errw, "no_input_channel") {
		t.Fatalf("expected no_input_channel: %s", errw)
	}
}

func TestCLI_AgentReportPaths(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	// Create task with conversation
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "x", "--format=json"})
	var created struct {
		TaskID         string `json:"task_id"`
		ConversationID string `json:"conversation_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)
	// Dispatch
	dispatch := app.DispatchCommand()
	_, dispOut, _ := runTaskHandler(t, dispatch, []string{created.TaskID, "--worker=W-1", "--format=json"})
	var execOut struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(dispOut), &execOut)

	rp := findCmd(app.AgentRuntimeCommands(), "report-progress")
	code, _, _ := runTaskHandler(t, rp, []string{execOut.ExecutionID, "--content=hello", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("report-progress: %d", code)
	}
	code, _, _ = runTaskHandler(t, rp, []string{})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage error")
	}

	ra := findCmd(app.AgentRuntimeCommands(), "report-artifact")
	code, _, errw := runTaskHandler(t, ra, []string{execOut.ExecutionID, "--kind=pr_url", "--title=feat:x", "--url=https://x", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("artifact: %d / %s", code, errw)
	}
	code, _, _ = runTaskHandler(t, ra, []string{})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage")
	}

	rf := findCmd(app.AgentRuntimeCommands(), "report-failure")
	// Transition exec → working so failure can mark from working.
	// (Otherwise it'd start from submitted which is fine for MarkFailed.)
	code, _, _ = runTaskHandler(t, rf, []string{execOut.ExecutionID, "--message=boom", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("failure: %d", code)
	}
	code, _, _ = runTaskHandler(t, rf, []string{})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage")
	}

	rt := findCmd(app.AgentRuntimeCommands(), "read-task-context")
	code, ctxOut, _ := runTaskHandler(t, rt, []string{created.TaskID})
	if code != int(ExitOK) {
		t.Fatalf("read-task-context: %d", code)
	}
	if !strings.Contains(ctxOut, "task_id") {
		t.Fatalf("ctx out: %s", ctxOut)
	}
	code, _, _ = runTaskHandler(t, rt, []string{})
	if code != int(ExitUsage) {
		t.Fatal("expected usage")
	}
}

func TestCLI_KillExecution_Happy(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "x", "--format=json"})
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)
	dispatch := app.DispatchCommand()
	_, dispOut, _ := runTaskHandler(t, dispatch, []string{created.TaskID, "--worker=W-1", "--format=json"})
	var execOut struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(dispOut), &execOut)
	kc := app.KillExecutionCommand()
	code, ioOut, errw := runTaskHandler(t, kc, []string{execOut.ExecutionID, "--reason=user_request", "--message=stop", "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("code: %d / err: %s", code, errw)
	}
	if !strings.Contains(ioOut, "kill_requested") {
		t.Fatalf("out: %s", ioOut)
	}
}

func TestCLI_KillExecution_NotFound(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	kc := app.KillExecutionCommand()
	code, _, _ := runTaskHandler(t, kc, []string{"E-NONE", "--reason=user_request", "--message=stop", "--format=json"})
	if code != int(ExitNotFound) {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_TaskBindConversation_ToExistingConv(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out1, _ := runTaskHandler(t, createCmd, []string{"p-1", "first", "--format=json"})
	var created1 struct {
		ConversationID string `json:"conversation_id"`
	}
	_ = json.Unmarshal([]byte(out1), &created1)
	_, out2, _ := runTaskHandler(t, createCmd, []string{"p-1", "second", "--no-conversation=true", "--format=json"})
	var created2 struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out2), &created2)
	bind := findCmd(app.TaskCommands(), "bind-conversation")
	code, _, _ := runTaskHandler(t, bind, []string{created2.TaskID, "--to=" + created1.ConversationID, "--format=json"})
	if code != int(ExitOK) {
		t.Fatalf("bind: %d", code)
	}
}

func TestCLI_ReadTaskContext_NotFound(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	rt := findCmd(app.AgentRuntimeCommands(), "read-task-context")
	code, _, _ := runTaskHandler(t, rt, []string{"T-NONE"})
	if code != int(ExitNotFound) {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_AgentReports_UnknownExecution(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	rp := findCmd(app.AgentRuntimeCommands(), "report-progress")
	code, _, _ := runTaskHandler(t, rp, []string{"E-NONE", "--content=hello"})
	if code != int(ExitNotFound) {
		t.Fatalf("code: %d", code)
	}
	rf := findCmd(app.AgentRuntimeCommands(), "report-failure")
	code, _, _ = runTaskHandler(t, rf, []string{"E-NONE", "--message=oops"})
	if code != int(ExitNotFound) {
		t.Fatalf("code: %d", code)
	}
	ri := findCmd(app.AgentRuntimeCommands(), "request-input")
	code, _, _ = runTaskHandler(t, ri, []string{"E-NONE", "--question=hi"})
	if code != int(ExitNotFound) {
		t.Fatalf("code: %d", code)
	}
	ra := findCmd(app.AgentRuntimeCommands(), "report-artifact")
	code, _, _ = runTaskHandler(t, ra, []string{"E-NONE", "--kind=k", "--title=t"})
	if code != int(ExitNotFound) {
		t.Fatalf("code: %d", code)
	}
	// Invalid urgency
	code, _, _ = runTaskHandler(t, ri, []string{"E-1", "--question=hi", "--urgency=garbage"})
	if code != int(ExitUsage) {
		t.Fatalf("expected usage on bad urgency: %d", code)
	}
}

func TestCLI_RequestInput_WithConversation(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "x", "--format=json"})
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)
	dispatch := app.DispatchCommand()
	_, dispOut, _ := runTaskHandler(t, dispatch, []string{created.TaskID, "--worker=W-1", "--format=json"})
	var execOut struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(dispOut), &execOut)
	// Transition execution → working (simulating worker spawning agent).
	advanceToWorking(t, app, execOut.ExecutionID)
	ri := findCmd(app.AgentRuntimeCommands(), "request-input")
	code, ioOut, errw := runTaskHandler(t, ri, []string{execOut.ExecutionID, "--question=proceed?", "--options=yes,no"})
	if code != int(ExitOK) {
		t.Fatalf("code: %d / err: %s", code, errw)
	}
	if !strings.Contains(ioOut, "input_request_id") {
		t.Fatalf("out: %s", ioOut)
	}
}
