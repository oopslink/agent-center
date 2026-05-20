package shim

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// Spawner abstracts agent CLI process creation so tests can inject a
// fake. Production wires *OSSpawner which uses os/exec + Setsid.
type Spawner interface {
	Spawn(ctx context.Context, spec agentadapter.CmdSpec, stdout, stderr io.Writer) (Process, error)
}

// Process is the live handle returned by Spawner.Spawn.
type Process interface {
	PID() int
	Wait() (exitCode int, err error)
	Kill() error
	Stdout() io.Reader
	Stderr() io.Reader
}

// OSSpawner runs the agent CLI via os/exec with detached/setsid semantics
// (ADR-0018 § 2 spike outcome).
type OSSpawner struct{}

// Spawn forks + execs and returns a Process handle. stdout/stderr writers
// (if non-nil) become the child's stream sinks (in addition to internal
// pipes the caller can read for live tracing).
func (OSSpawner) Spawn(_ context.Context, spec agentadapter.CmdSpec, stdout, stderr io.Writer) (Process, error) {
	if spec.Binary == "" {
		return nil, errors.New("shim/spawner: binary required")
	}
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Env = spec.Env
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, err
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if stdout != nil {
		cmd.Stdout = io.MultiWriter(stdoutW, stdout)
	}
	if stderr != nil {
		cmd.Stderr = io.MultiWriter(stderrW, stderr)
	}
	if err := cmd.Start(); err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
		return nil, err
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()
	return &osProcess{cmd: cmd, stdout: stdoutR, stderr: stderrR}, nil
}

type osProcess struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (p *osProcess) PID() int            { return p.cmd.Process.Pid }
func (p *osProcess) Stdout() io.Reader   { return p.stdout }
func (p *osProcess) Stderr() io.Reader   { return p.stderr }
func (p *osProcess) Kill() error         { return p.cmd.Process.Kill() }
func (p *osProcess) Wait() (int, error) {
	if err := p.cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}
