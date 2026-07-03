package agentlauncher

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// ExecStarter is the production ProcessStarter: it forks/execs the worker binary's
// `agent-runtime` subcommand for one agent (design §4.5 LocalProcessLauncher). The
// launched process self-configures from the center (ResumeState) and binds its own
// control socket; ExecStarter only passes the identity + config-locating argv.
//
// The child runs in its OWN process group (Setpgid) so Signal/Kill target the group
// (the agent-runtime process + any grandchildren), mirroring the executor spawn model.
type ExecStarter struct {
	binaryPath string
	subcommand string
	baseArgs   []string
	baseEnv    []string
	stdout     io.Writer
	stderr     io.Writer
}

// ExecStarterConfig wires an ExecStarter.
type ExecStarterConfig struct {
	// BinaryPath is the worker binary to exec (empty → os.Executable(), i.e. re-exec
	// this same binary — the unified-binary model, same as executor forks).
	BinaryPath string
	// Subcommand is the agent-runtime subcommand name (empty → "agent-runtime").
	Subcommand string
	// BaseArgs are argv appended after the subcommand + --agent-id (shared config
	// flags like --config / --admin-target that every agent process needs).
	BaseArgs []string
	// BaseEnv is the base environment for every agent process (empty → os.Environ()).
	BaseEnv []string
	// Stdout/Stderr receive the child's output (nil → os.Stdout/os.Stderr).
	Stdout io.Writer
	Stderr io.Writer
}

// NewExecStarter builds an ExecStarter, defaulting BinaryPath to the running binary.
func NewExecStarter(cfg ExecStarterConfig) (*ExecStarter, error) {
	bin := cfg.BinaryPath
	if strings.TrimSpace(bin) == "" {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		bin = self
	}
	sub := cfg.Subcommand
	if strings.TrimSpace(sub) == "" {
		sub = "agent-runtime"
	}
	env := cfg.BaseEnv
	if env == nil {
		env = os.Environ()
	}
	out := cfg.Stdout
	if out == nil {
		out = os.Stdout
	}
	errw := cfg.Stderr
	if errw == nil {
		errw = os.Stderr
	}
	return &ExecStarter{binaryPath: bin, subcommand: sub, baseArgs: cfg.BaseArgs, baseEnv: env, stdout: out, stderr: errw}, nil
}

var _ ProcessStarter = (*ExecStarter)(nil)

// Start execs the agent-runtime subcommand for spec.AgentID.
func (s *ExecStarter) Start(ctx context.Context, spec AgentSpec) (Process, error) {
	if spec.AgentID == "" {
		return nil, errors.New("agentlauncher: exec start requires agent_id")
	}
	args := []string{s.subcommand, "--agent-id", spec.AgentID}
	args = append(args, s.baseArgs...)
	args = append(args, spec.Args...)

	cmd := exec.CommandContext(ctx, s.binaryPath, args...)
	cmd.Env = append(append([]string{}, s.baseEnv...), spec.Env...)
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd}, nil
}

// osProcess wraps an *exec.Cmd as a launcher Process. Signal/Kill target the child's
// process GROUP (negative pid) so descendants die too.
type osProcess struct{ cmd *exec.Cmd }

func (p *osProcess) Wait() error { return p.cmd.Wait() }
func (p *osProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *osProcess) Signal() error { return p.signalGroup(syscall.SIGTERM) }
func (p *osProcess) Kill() error   { return p.signalGroup(syscall.SIGKILL) }

func (p *osProcess) signalGroup(sig syscall.Signal) error {
	if p.cmd.Process == nil {
		return nil
	}
	pid := p.cmd.Process.Pid
	// Negative pid → the process group (Setpgid made the child a group leader).
	if err := syscall.Kill(-pid, sig); err != nil {
		// Fall back to signalling just the process if the group send fails.
		return p.cmd.Process.Signal(sig)
	}
	return nil
}
