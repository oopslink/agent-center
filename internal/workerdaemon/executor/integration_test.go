package executor

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// lookBin resolves a real binary or skips the test (keeps the suite hermetic on
// images that lack it, matching F2's real-git policy).
func lookBin(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
	return p
}

// TestSpawn_RealProcessGroup spawns a REAL short-lived process through the
// production Spawner (realStart) and reaps it (Wait/Release), exercising the
// process-group launch path end to end. `true` exits 0 immediately regardless of
// the trailing argv, so this is deterministic.
func TestSpawn_RealProcessGroup(t *testing.T) {
	bin := lookBin(t, "true")
	sp := NewSpawner()
	h, err := sp.Spawn(SpawnSpec{BinaryPath: bin, ExecutorID: "exec-real", AgentRoot: "/tmp"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if h.PID <= 0 {
		t.Fatalf("expected a real pid, got %d", h.PID)
	}
	if err := h.Wait(); err != nil {
		t.Errorf("Wait on `true`: %v", err)
	}
	// Release after Wait is a no-op-ish best effort; must not panic/error hard.
	_ = h.Release()
}

// TestBuildExecutorCommand_DefaultBinary exercises the os.Executable() default
// when BinaryPath is empty.
func TestBuildExecutorCommand_DefaultBinary(t *testing.T) {
	cmd, err := buildExecutorCommand(SpawnSpec{ExecutorID: "exec-x", AgentRoot: "/r"})
	if err != nil {
		t.Fatalf("buildExecutorCommand: %v", err)
	}
	exe, _ := os.Executable()
	if cmd.Path != exe {
		t.Errorf("default binary = %q, want os.Executable() %q", cmd.Path, exe)
	}
}

// TestRunExecutor_RealCommandRunner drives RunExecutor with the real
// CommandRunner (execRun) against `true`, covering the production compute path.
func TestRunExecutor_RealCommandRunner(t *testing.T) {
	lookBin(t, "true")
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	clk := clock.NewFakeClock(time.Unix(1700000000, 0))
	fx, err := NewFileExchange(layout, clk)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	const id = "exec-realrun"
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	wsDir, _ := layout.WorkspaceDir(id)
	if err := os.MkdirAll(wsDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := fx.WriteInput(validPoolInput(id)); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	err = RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: id,
		RunnerCmd:  []string{"true"},
		Clock:      clk,
	})
	if err != nil {
		t.Fatalf("RunExecutor with real `true`: %v", err)
	}
	out, err := fx.ReadOutput(id)
	if err != nil || !out.Success {
		t.Errorf("expected success output, got %+v err=%v", out, err)
	}
}
