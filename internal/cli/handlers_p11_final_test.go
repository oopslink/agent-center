package cli

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// seedExecAndIR persists an execution in InputRequired state with an IR
// associated, so respond/cancel can land cleanly.
func seedExecAndIR(t *testing.T, app *App, execID, irID string, now time.Time) *execution.TaskExecution {
	t.Helper()
	exec, err := execution.New(execution.NewInput{
		ID: taskruntime.TaskExecutionID(execID), TaskID: "T-X",
		WorkerID: "W-X", AgentCLI: "claudecode",
		WorkspaceMode: execution.WorkspaceWorktree, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.StartWorking("/tmp/wt", now); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecRepo.Save(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID: taskruntime.InputRequestID(irID), TaskExecutionID: taskruntime.TaskExecutionID(execID),
		Question: "q?", Urgency: inputrequest.UrgencyNormal, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.IRRepo.Save(context.Background(), ir); err != nil {
		t.Fatal(err)
	}
	if err := exec.EnterInputRequired(ir.ID(), now); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecRepo.Update(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	return exec
}

// ============================================================================
// runWebConsole — starts the web console, hits /healthz, then shuts down.
// ============================================================================

func TestRunWebConsole_StartsAndStops(t *testing.T) {
	app := newTestApp(t)
	bus := sse.NewBus()
	defer bus.Shutdown(context.Background())
	// Grab a free loopback port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	logs := []string{}
	cleanup, err := runWebConsole(context.Background(), app, bus, addr, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	// Give listener a moment to bind.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never came up: %v", err)
	}
	_ = resp.Body.Close()
}

func TestRunWebConsole_NilApp(t *testing.T) {
	bus := sse.NewBus()
	defer bus.Shutdown(context.Background())
	_, err := runWebConsole(context.Background(), nil, bus, "127.0.0.1:0", func(string) {})
	if err == nil {
		t.Fatal("expected error for nil app")
	}
}

// ============================================================================
// resolveSecretInput — stdin "-" + piped stdin branches
// ============================================================================

// withStdin temporarily replaces os.Stdin with a pipe that yields data.
func withStdin(t *testing.T, data string, fn func()) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(data); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = orig
		_ = r.Close()
	}()
	fn()
}

func TestResolveSecretInput_StdinDash(t *testing.T) {
	withStdin(t, "secret-data\n", func() {
		got, err := resolveSecretInput("-")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "secret-data" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestResolveSecretInput_StdinDashEmpty(t *testing.T) {
	withStdin(t, "", func() {
		_, err := resolveSecretInput("-")
		if err == nil {
			t.Fatal()
		}
	})
}

func TestResolveSecretInput_PipedStdin(t *testing.T) {
	withStdin(t, "piped-value\n", func() {
		got, err := resolveSecretInput("")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "piped-value" {
			t.Fatalf("got %q", got)
		}
	})
}

// ============================================================================
// resolveAnswerInput — stdin "-" + piped stdin branches
// ============================================================================

func TestResolveAnswerInput_StdinDash(t *testing.T) {
	withStdin(t, "yep\n", func() {
		got, err := resolveAnswerInput("", "-")
		if err != nil {
			t.Fatal(err)
		}
		if got != "yep" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestResolveAnswerInput_StdinDashEmpty(t *testing.T) {
	withStdin(t, "", func() {
		_, err := resolveAnswerInput("", "-")
		if err == nil {
			t.Fatal()
		}
	})
}

func TestResolveAnswerInput_PipedStdin(t *testing.T) {
	withStdin(t, "from pipe\n", func() {
		got, err := resolveAnswerInput("", "")
		if err != nil {
			t.Fatal(err)
		}
		if got != "from pipe" {
			t.Fatalf("got %q", got)
		}
	})
}

// ============================================================================
// irRespond / irCancel — happy path via seeded execution + IR.
// ============================================================================

func TestCLI_IRRespond_HappyJSON(t *testing.T) {
	app := newTestApp(t)
	now := app.Clock.Now()
	exec := seedExecAndIR(t, app, "E-RH1", "IR-RH1", now)
	_ = exec
	_, _, code := runOn(t, app, "input-request", "respond", []string{
		"IR-RH1", "--answer=yes", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
}

func TestCLI_IRRespond_NotWired(t *testing.T) {
	app := newTestApp(t)
	app.IRSvc = nil
	_, _, code := runOn(t, app, "input-request", "respond", []string{"IR-XYZ", "--answer=yes"})
	if code != ExitNotImplemented {
		t.Fatalf("code %d", code)
	}
}

func TestCLI_IRCancel_Happy(t *testing.T) {
	app := newTestApp(t)
	now := app.Clock.Now()
	_ = seedExecAndIR(t, app, "E-CH1", "IR-CH1", now)
	_, _, code := runOn(t, app, "input-request", "cancel", []string{
		"IR-CH1", "--message=nevermind",
	})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
}

func TestCLI_IRCancel_MissingMessage(t *testing.T) {
	app := newTestApp(t)
	_, _, code := runOn(t, app, "input-request", "cancel", []string{"IR-X"})
	if code != ExitUsage {
		t.Fatalf("code %d", code)
	}
}

func TestCLI_IRCancel_NotWired(t *testing.T) {
	app := newTestApp(t)
	app.IRSvc = nil
	_, _, code := runOn(t, app, "input-request", "cancel", []string{"IR-X", "--message=m"})
	if code != ExitNotImplemented {
		t.Fatalf("code %d", code)
	}
}

// ============================================================================
// convTail follow mode — exercises ticker loop + context cancellation.
// ============================================================================

func TestCLI_ConvTail_FollowCancellation(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=tfollow"})
	out, _, _ := runOn(t, app, "channel", "show", []string{"tfollow", "--format=json"})
	// Trivially grab the conv id without re-parsing — read directly.
	convs, _ := app.ConvRepo.FindByName(context.Background(), "tfollow")
	if convs == nil {
		t.Fatalf("no conv: %s", out)
	}
	cid := string(convs.ID())
	cmd := findCmd(app.ConversationCommands(), "tail")
	if cmd == nil {
		t.Fatal()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _, _ = runHandlerCtx(t, ctx, cmd, []string{cid, "-f", "--interval=1"})
}
