package executor

// spawn.go — F1 (process model) executor process fork (design §4 / §11.2).
//
// The orchestrator forks each executor as an INDEPENDENT OS process in its OWN
// process group (SysProcAttr.Setpgid: true), so:
//   - the executor never shares the orchestrator's (or another executor's)
//     process group — a killpg targeting one executor cannot hit a sibling or
//     the orchestrator;
//   - the executor inherits NO mcp config and NO center credentials: it is
//     launched with BuildExecutorEnv (executorenv.go) and is NEVER passed an
//     --mcp-config (contrast the supervisor's claude launch).
//
// The executor entrypoint is the `worker executor` subcommand (run.go +
// internal/cli/handlers_executor.go), pointed at its per-agent <agent_root> and
// <executor_id> so it resolves its own directory via the F2 FileExchange.
//
// os/exec is reached through small seams (commandStarter / groupSignaler) so the
// argv/env/process-group construction is unit-testable without spawning real
// processes; production wires the real syscalls.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// SpawnSpec is the immutable description of one executor process to fork.
type SpawnSpec struct {
	// BinaryPath is the agent-center executable carrying the `worker executor`
	// subcommand. Empty → os.Executable() (the running daemon binary).
	BinaryPath string
	// ExecutorID is the executor whose directory the child operates in (validated;
	// becomes --executor-id).
	ExecutorID string
	// AgentRoot is the per-agent home the FileExchange Layout anchors at (becomes
	// --agent-root). The child resolves <agent_root>/executors/<id>/ from it.
	AgentRoot string
	// RunnerCmd is the optional pure-compute command the executor runs inside its
	// workspace (becomes --runner-cmd). The orchestrator/F3 supplies the model
	// -routed agent CLI; left empty the executor entrypoint errors clearly rather
	// than guessing. NEVER an mcp-aware command.
	RunnerCmd []string
	// AgentEnv is the ② per-agent overlay (git identity / Profile.EnvVars) merged
	// over the allowlisted system env by BuildExecutorEnv. Center credentials in it
	// are scrubbed (executor hardening).
	AgentEnv map[string]string
}

func (s SpawnSpec) validate() error {
	if err := validateExecutorID(s.ExecutorID); err != nil {
		return err
	}
	if strings.TrimSpace(s.AgentRoot) == "" {
		return errors.New("executor: spawn agent_root required")
	}
	return nil
}

// commandStarter starts an assembled *exec.Cmd. The production impl is
// (*exec.Cmd).Start; tests swap a fake that records the cmd and assigns a pid.
type commandStarter func(cmd *exec.Cmd) error

// groupSignaler delivers a signal to a process GROUP (killpg semantics). The
// production impl signals the negative pid; tests record the call.
type groupSignaler func(pgid int, sig syscall.Signal) error

// realStart is the production commandStarter.
func realStart(cmd *exec.Cmd) error { return cmd.Start() }

// realGroupSignal is the production groupSignaler: a negative pid targets the
// whole process group (POSIX killpg), matching the Setpgid launch so the entire
// executor subtree is signalled, never just the leader.
func realGroupSignal(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}

// Spawner forks executor processes. Stateless beyond its seams, so a single
// Spawner is safe to share across concurrent launches.
type Spawner struct {
	start  commandStarter
	signal groupSignaler
}

// NewSpawner builds a production Spawner (real exec + real killpg).
func NewSpawner() *Spawner {
	return &Spawner{start: realStart, signal: realGroupSignal}
}

// Spawn forks the executor described by spec and returns a live Handle. The child
// is placed in its own process group and given a sanitized, mcp-free environment.
func (sp *Spawner) Spawn(spec SpawnSpec) (*Handle, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	cmd, err := buildExecutorCommand(spec)
	if err != nil {
		return nil, err
	}
	if err := sp.start(cmd); err != nil {
		return nil, fmt.Errorf("executor: spawn %s: %w", spec.ExecutorID, err)
	}
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	return &Handle{
		ExecutorID: spec.ExecutorID,
		PID:        pid,
		cmd:        cmd,
		signal:     sp.signal,
	}, nil
}

// buildExecutorCommand assembles the `worker executor` *exec.Cmd: argv, the
// sanitized env, and the own-process-group attribute. Pure (no spawn) so the
// process-model invariants — own process group, no --mcp-config, no center env —
// are unit-testable. Exported-package-internal for the spawn + test paths.
func buildExecutorCommand(spec SpawnSpec) (*exec.Cmd, error) {
	bin := spec.BinaryPath
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("executor: resolve executable: %w", err)
		}
		bin = exe
	}
	args := []string{
		"worker", "executor",
		"--executor-id", spec.ExecutorID,
		"--agent-root", spec.AgentRoot,
	}
	// The runner command (the isolated compute) is passed as a JSON array in a
	// single --runner-cmd flag, NOT as bare trailing argv: the CLI's permissive
	// flag parser would otherwise try to parse the runner's own flags (e.g. -p)
	// and fail. JSON keeps the whole vector opaque to flag parsing.
	if len(spec.RunnerCmd) > 0 {
		enc, err := json.Marshal(spec.RunnerCmd)
		if err != nil {
			return nil, fmt.Errorf("executor: encode runner cmd: %w", err)
		}
		args = append(args, "--runner-cmd", string(enc))
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = BuildExecutorEnv(os.Environ(), spec.AgentEnv)
	// Own process group: a killpg of this executor cannot reach the orchestrator
	// or a sibling executor (design §4 isolation). We do NOT Setsid — the executor
	// is a transient child the orchestrator reaps, not a daemon survivor.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, nil
}

// Handle is a live reference to a spawned executor process. With Setpgid and no
// explicit Pgid, the child's process-group id equals its pid, so group signals
// target -PID.
type Handle struct {
	ExecutorID string
	PID        int

	cmd    *exec.Cmd
	signal groupSignaler
}

// Signal delivers sig to the executor's entire process group (killpg). Used by
// the orchestrator's stop / watchdog paths (F5 consumes this).
func (h *Handle) Signal(sig syscall.Signal) error {
	if h.PID <= 0 {
		return errors.New("executor: handle has no pid")
	}
	return h.signal(h.PID, sig)
}

// Terminate asks the executor to stop gracefully (SIGTERM to the group).
func (h *Handle) Terminate() error { return h.Signal(syscall.SIGTERM) }

// Kill force-kills the executor's process group (SIGKILL).
func (h *Handle) Kill() error { return h.Signal(syscall.SIGKILL) }

// Wait reaps the executor process and returns its exit error (nil on exit 0,
// *exec.ExitError otherwise). Safe to call once per spawn, like exec.Cmd.Wait.
func (h *Handle) Wait() error {
	if h.cmd == nil {
		return errors.New("executor: handle has no command")
	}
	return h.cmd.Wait()
}

// Release drops the process handle without reaping (used when an executor is
// intentionally orphaned across an orchestrator restart; the executor's files are
// the durable state per design §12). Best-effort.
func (h *Handle) Release() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Release()
}

// recoveredHandle builds a pid-only Handle for an ADOPTED orphan executor — a
// process the orchestrator re-adopts across a restart (design §12). It carries a
// group-signaler so the watchdog can Terminate/Kill the orphan's process group, but
// it has NO *exec.Cmd: Wait()/Release() return an error because the orphan is NOT
// this orchestrator's child (it was reparented when the previous orchestrator died).
// The orphan's completion is therefore observed by POLLING liveness
// (Monitor.CheckOrphan), never by reaping. sig is injected so the watchdog kill is
// testable without signalling a real pid.
func recoveredHandle(executorID string, pid int, sig groupSignaler) *Handle {
	return &Handle{ExecutorID: executorID, PID: pid, signal: sig}
}
