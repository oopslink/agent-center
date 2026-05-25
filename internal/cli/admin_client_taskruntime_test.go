// Package cli — admin_client_taskruntime_test.go: worked-example tests
// for the v2.2 Phase B Client + admin transport on the taskruntime BC.
//
// Pattern (per docs/plans/v2.2-audits/v22-B-cli-refactor-audit.md):
//
//   1. setupAdminServerForTests spins up an in-process admin endpoint
//      on a unix socket + returns an App whose Client points at it.
//   2. Tests drive task create / bind-conversation / dispatch / IR
//      create / report-progress / report-artifact / report-failure /
//      kill-execution through the router exactly as a real CLI
//      invocation would — `a.Client` is non-nil so every handler routes
//      through the admin endpoint, not the direct Service field.
//   3. Assertions cover the exit code and the JSON-output projection so
//      we know the DTO ↔ map projection helpers are correct.
package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// seedProjectAndWorkerClient mirrors handlers_task_test.go::seedProjectAndWorker
// but is callable against an admin-endpoint-backed App (server-side
// Services are still wired on the App; the Client just routes round-trip).
func seedProjectAndWorkerClient(t *testing.T, app *App) {
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

func TestClient_TaskRuntime_TaskCreateDispatchKill_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	// --- task create ----------------------------------------------------
	create := findCmd(app.TaskCommands(), "create")
	out, _, code := runHandler(t, create, []string{
		"p-1", "do thing", "--no-conversation=true", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("create exit=%d out=%s", code, out)
	}
	var created struct {
		TaskID         string `json:"task_id"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("decode create out: %v body=%s", err, out)
	}
	if created.TaskID == "" {
		t.Fatal("expected task_id")
	}

	// --- dispatch -------------------------------------------------------
	dispatch := app.DispatchCommand()
	out2, _, code := runHandler(t, dispatch, []string{
		created.TaskID, "--worker=W-1", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("dispatch exit=%d out=%s", code, out2)
	}
	var dispatched struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal([]byte(out2), &dispatched); err != nil {
		t.Fatalf("decode dispatch out: %v body=%s", err, out2)
	}
	if dispatched.ExecutionID == "" {
		t.Fatal("expected execution_id")
	}

	// --- report-progress ------------------------------------------------
	rp := findCmd(app.AgentRuntimeCommands(), "report-progress")
	out3, _, code := runHandler(t, rp, []string{
		dispatched.ExecutionID, "--content=hello", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("report-progress exit=%d out=%s", code, out3)
	}
	if !strings.Contains(out3, `"status":"ok"`) {
		t.Fatalf("report-progress out=%s", out3)
	}

	// --- report-artifact ------------------------------------------------
	ra := findCmd(app.AgentRuntimeCommands(), "report-artifact")
	out4, _, code := runHandler(t, ra, []string{
		dispatched.ExecutionID, "--kind=pr_url", "--title=feat:x",
		"--url=https://example/pr/1", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("report-artifact exit=%d out=%s", code, out4)
	}
	if !strings.Contains(out4, "artifact_id") {
		t.Fatalf("report-artifact out=%s", out4)
	}

	// --- kill-execution -------------------------------------------------
	kc := app.KillExecutionCommand()
	out5, _, code := runHandler(t, kc, []string{
		dispatched.ExecutionID, "--reason=user_request",
		"--message=stop", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("kill exit=%d out=%s", code, out5)
	}
	if !strings.Contains(out5, "kill_requested") {
		t.Fatalf("kill out=%s", out5)
	}
}

func TestClient_TaskRuntime_TaskBindConversation_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	// First create a task without conversation.
	create := findCmd(app.TaskCommands(), "create")
	out, _, code := runHandler(t, create, []string{
		"p-1", "title", "--no-conversation=true", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("create exit=%d", code)
	}
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)

	// Bind a fresh conversation via --auto.
	bind := findCmd(app.TaskCommands(), "bind-conversation")
	out2, _, code := runHandler(t, bind, []string{
		created.TaskID, "--auto=true", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("bind exit=%d out=%s", code, out2)
	}
	if !strings.Contains(out2, "conversation_id") {
		t.Fatalf("bind out=%s", out2)
	}
}

func TestClient_TaskRuntime_IRListShow_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	// Create + dispatch + advance to working so request-input has a
	// channel to write to.
	create := findCmd(app.TaskCommands(), "create")
	out, _, code := runHandler(t, create, []string{"p-1", "x", "--format=json"})
	if code != ExitOK {
		t.Fatalf("create exit=%d", code)
	}
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)

	dispatch := app.DispatchCommand()
	dout, _, code := runHandler(t, dispatch, []string{created.TaskID, "--worker=W-1", "--format=json"})
	if code != ExitOK {
		t.Fatalf("dispatch exit=%d", code)
	}
	var dispatched struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(dout), &dispatched)

	// Force exec → working so the IR create has an open conversation.
	// Reuse the helper from handlers_task_test.go (same package).
	advanceToWorking(t, app, dispatched.ExecutionID)

	// request-input via Client.
	ri := findCmd(app.AgentRuntimeCommands(), "request-input")
	rout, _, code := runHandler(t, ri, []string{
		dispatched.ExecutionID, "--question=proceed?", "--options=yes,no",
	})
	if code != ExitOK {
		t.Fatalf("request-input exit=%d out=%s", code, rout)
	}
	var ircreated struct {
		InputRequestID string `json:"input_request_id"`
	}
	_ = json.Unmarshal([]byte(rout), &ircreated)
	if ircreated.InputRequestID == "" {
		t.Fatal("expected input_request_id")
	}

	// IR list — Client path.
	listCmd := findCmd(app.InputRequestCommands(), "list")
	lout, _, code := runHandler(t, listCmd, []string{"--format=json"})
	if code != ExitOK {
		t.Fatalf("list exit=%d out=%s", code, lout)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(lout)), &arr); err != nil {
		t.Fatalf("decode list: %v body=%s", err, lout)
	}
	if len(arr) == 0 {
		t.Fatal("expected at least one IR")
	}

	// IR show — Client path.
	showCmd := findCmd(app.InputRequestCommands(), "show")
	sout, _, code := runHandler(t, showCmd, []string{ircreated.InputRequestID, "--format=json"})
	if code != ExitOK {
		t.Fatalf("show exit=%d out=%s", code, sout)
	}
	if !strings.Contains(sout, ircreated.InputRequestID) {
		t.Fatalf("show out=%s", sout)
	}
}

// TestClient_TaskRuntime_ReadContext_NotImplementedOverClient documents
// the v2.2-B mismatch: there is no admin endpoint that mirrors
// TaskService.ReadContext, so the Client path of `read-task-context`
// returns ExitNotImplemented while the Service path still works.
func TestClient_TaskRuntime_ReadContext_NotImplementedOverClient(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	// Build a Client-only App that mirrors a true CLI invocation (no
	// TaskSvc wired). The admin server already running from above is
	// reused via the same Client.
	clientOnly := NewClientApp(app.Config, app.Client)

	rt := findCmd(clientOnly.AgentRuntimeCommands(), "read-task-context")
	_, errw, code := runHandler(t, rt, []string{string(taskruntime.TaskID("T-NONE"))})
	if code != ExitNotImplemented {
		t.Fatalf("expected ExitNotImplemented, got code=%d err=%s", code, errw)
	}
}
