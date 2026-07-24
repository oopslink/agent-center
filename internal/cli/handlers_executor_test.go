package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/clock"
)

func TestRunExecutor_UsageErrors(t *testing.T) {
	var errw bytes.Buffer
	if code := runExecutor(context.Background(), &errw, "", "exec-1", "", ""); code != ExitUsage {
		t.Errorf("missing agent-root: code = %v, want ExitUsage", code)
	}
	errw.Reset()
	if code := runExecutor(context.Background(), &errw, "/root", "", "", ""); code != ExitUsage {
		t.Errorf("missing executor-id: code = %v, want ExitUsage", code)
	}
	errw.Reset()
	if code := runExecutor(context.Background(), &errw, "/root", "exec-1", "", "{not json"); code != ExitUsage {
		t.Errorf("bad runner-cmd JSON: code = %v, want ExitUsage", code)
	}
	if !strings.Contains(errw.String(), "runner-cmd") {
		t.Errorf("expected bad-JSON diagnostic, got %q", errw.String())
	}
}

func TestRunExecutor_EndToEndWithRealCommand(t *testing.T) {
	root := t.TempDir()
	layout, err := executor.NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := executor.NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	const id = "exec-e2e"
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	// The workspace dir must exist for the command's Dir; create it directly
	// (the real flow uses a git worktree — here we only exercise the entrypoint).
	wsDir, err := layout.WorkspaceDir(id)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	if err := os.MkdirAll(wsDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := fx.WriteInput(executor.Input{
		ExecutorID: id,
		Goal:       executor.Goal{Title: "say hi"},
		Model:      "claude-haiku",
		CreatedAt:  time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}

	var errw bytes.Buffer
	// `true` ignores its cwd, so no workspace dir is needed; it exits 0 with no output.
	code := runExecutor(context.Background(), &errw, root, id, "", `["true"]`)
	if code != ExitOK {
		t.Fatalf("runExecutor code = %v (stderr %q), want ExitOK", code, errw.String())
	}
	out, err := fx.ReadOutput(id)
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	if !out.Success {
		t.Errorf("expected success output, got %+v", out)
	}
	st, err := fx.ReadStatus(id)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if st.State != executor.StateDone {
		t.Errorf("status state = %v, want done", st.State)
	}
}

func TestExecutorCommand_Metadata(t *testing.T) {
	cmd := ExecutorCommand()
	if cmd.Name != "executor" {
		t.Errorf("name = %q, want executor", cmd.Name)
	}
	// The executor must advertise that it holds no credentials / mcp.
	if !strings.Contains(cmd.LongHelp, "NEVER connects to the center") {
		t.Errorf("LongHelp should state the no-center/no-mcp property, got %q", cmd.LongHelp)
	}
}
