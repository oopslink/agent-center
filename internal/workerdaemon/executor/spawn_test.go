package executor

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// fakeProcess stands in for an OS-assigned process handle without launching
// anything (the start seam assigns it so Handle.PID is populated).
var fakeProcess = os.Process{Pid: 4242}

func TestBuildExecutorCommand_ArgvEnvAndProcessGroup(t *testing.T) {
	spec := SpawnSpec{
		BinaryPath: "/opt/agent-center",
		ExecutorID: "exec-abc",
		AgentRoot:  "/home/agent",
		RunnerCmd:  []string{"claude", "-p", "do the thing"},
		AgentEnv:   map[string]string{"GIT_AUTHOR_NAME": "dev1", "AC_MCP_WORKER_TOKEN": "leak"},
	}
	cmd, err := buildExecutorCommand(spec)
	if err != nil {
		t.Fatalf("buildExecutorCommand: %v", err)
	}
	if cmd.Path != "/opt/agent-center" {
		t.Errorf("binary path = %q, want /opt/agent-center", cmd.Path)
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"worker executor", "--executor-id exec-abc", "--agent-root /home/agent", "--runner-cmd"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q missing %q", joined, want)
		}
	}
	// The runner command travels as an opaque JSON array, never as bare argv.
	if !strings.Contains(joined, `["claude","-p","do the thing"]`) {
		t.Errorf("argv %q missing JSON-encoded runner cmd", joined)
	}
	// No mcp config is ever passed to an executor.
	if strings.Contains(joined, "mcp-config") || strings.Contains(joined, "--mcp") {
		t.Errorf("executor argv must never carry an mcp config: %q", joined)
	}
	// Own process group.
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("executor must be launched with Setpgid (own process group)")
	}
	if cmd.SysProcAttr.Setsid {
		t.Error("executor must NOT setsid (it is a reapable child, not a daemon survivor)")
	}
	// Env carries the overlay git identity but never a center credential.
	env := envMap(cmd.Env)
	if env["GIT_AUTHOR_NAME"] != "dev1" {
		t.Error("expected ② overlay GIT_AUTHOR_NAME=dev1 in executor env")
	}
	for k := range env {
		if strings.HasPrefix(k, "AC_MCP_") || strings.HasPrefix(k, "AGENT_CENTER_") {
			t.Errorf("center credential %q must not reach executor env", k)
		}
	}
}

func TestSpawn_RejectsBadSpec(t *testing.T) {
	sp := NewSpawner()
	if _, err := sp.Spawn(SpawnSpec{ExecutorID: "bad/id", AgentRoot: "/r"}); err == nil {
		t.Error("Spawn must reject an illegal executor id")
	}
	if _, err := sp.Spawn(SpawnSpec{ExecutorID: "ok", AgentRoot: "  "}); err == nil {
		t.Error("Spawn must reject an empty agent_root")
	}
}

func TestSpawn_FakeStartAssignsHandleAndKillpg(t *testing.T) {
	var started *exec.Cmd
	var sigPgid int
	var sigSig syscall.Signal
	sp := &Spawner{
		start: func(cmd *exec.Cmd) error {
			started = cmd
			// Simulate the OS assigning a pid without launching anything.
			cmd.Process = &fakeProcess
			return nil
		},
		signal: func(pgid int, sig syscall.Signal) error {
			sigPgid, sigSig = pgid, sig
			return nil
		},
	}
	h, err := sp.Spawn(SpawnSpec{BinaryPath: "/bin/x", ExecutorID: "exec-1", AgentRoot: "/r"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if started == nil {
		t.Fatal("expected start seam to be invoked")
	}
	if h.PID != fakeProcess.Pid {
		t.Errorf("handle PID = %d, want %d", h.PID, fakeProcess.Pid)
	}
	if err := h.Terminate(); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if sigPgid != h.PID || sigSig != syscall.SIGTERM {
		t.Errorf("Terminate signalled pgid=%d sig=%v, want pgid=%d SIGTERM", sigPgid, sigSig, h.PID)
	}
	if err := h.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if sigSig != syscall.SIGKILL {
		t.Errorf("Kill sig = %v, want SIGKILL", sigSig)
	}
}

func TestSpawn_StartErrorPropagates(t *testing.T) {
	sp := &Spawner{
		start:  func(cmd *exec.Cmd) error { return errors.New("boom") },
		signal: realGroupSignal,
	}
	if _, err := sp.Spawn(SpawnSpec{BinaryPath: "/bin/x", ExecutorID: "exec-1", AgentRoot: "/r"}); err == nil {
		t.Error("expected start error to propagate")
	}
}

func TestHandle_SignalGuards(t *testing.T) {
	h := &Handle{ExecutorID: "x", PID: 0, signal: realGroupSignal}
	if err := h.Terminate(); err == nil {
		t.Error("signal with no pid must error")
	}
	h2 := &Handle{ExecutorID: "x"}
	if err := h2.Wait(); err == nil {
		t.Error("Wait with no command must error")
	}
	if err := h2.Release(); err != nil {
		t.Errorf("Release with no process should be a no-op, got %v", err)
	}
}
