// Package agentsupervisor is the v2.7 execution-survival redesign's persistent
// per-agent supervisor PROCESS skeleton (slice D2-f s1).
//
// WHY IT EXISTS. @oopslink requires that a worker DAEMON crash/restart MUST NOT
// kill the agent's claude. Today claude is the daemon's direct child on
// daemon-owned anonymous pipes, and the daemon's shutdown does a process-GROUP
// kill (syscall.Kill(-pid, SIGKILL) == killpg of the daemon's group), so claude
// dies with the daemon. The fix is a thin, long-lived Supervisor process that
// OWNS claude (claude == the supervisor's child). The supervisor escapes the
// daemon's process group via setsid, so a killpg of the daemon group does not
// reach it; it keeps claude alive and keeps DRAINING claude's stdout to a
// persistent offset cursor (events.jsonl) during the daemon-down window so
// claude never blocks on a full stdout pipe.
//
// SCOPE OF s1 (this file). PURELY ADDITIVE: the process-survival + drain core +
// observability artifacts + Inject/Stop. There is NO socket and NO daemon-side
// attach/reattach here (those are s2/s3), and this is NOT wired into the daemon
// launch path. ack-truncation of events.jsonl is also s2; s1 only APPENDS
// (unbounded growth is fine for the short-lived survival test).
//
// SETSID / PROCESS-GROUP CHOICE (the whole point — get it exactly right).
//   - The supervisor PROCESS itself calls setsid at startup (DetachSession()).
//     setsid makes the caller a session+group leader of a BRAND-NEW session
//     whose pgid == the supervisor's own pid. That is what lets a later
//     `killpg(daemonGroup)` miss the supervisor: the supervisor is no longer in
//     the daemon's group. (The daemon spawns the supervisor, so without this
//     the supervisor would inherit the daemon's group and die with it.)
//   - The CHILD (claude) is launched with SysProcAttr{Setpgid: true}, putting
//     claude in ITS OWN group (pgid == claude's pid) under the supervisor's
//     session. That lets Stop() signal the whole claude tree (claude forks MCP
//     helpers) with one killpg, AND keeps a future Stop of claude from ever
//     touching the supervisor.
//
// DARWIN vs LINUX. setsid(2) and setpgid(2) are POSIX and behave identically on
// darwin and linux for this purpose. The one portability note: in the Go
// stdlib, syscall.SysProcAttr has a `Setsid bool` field on BOTH darwin and
// linux, so the CHILD-side launch could alternatively setsid the child. We do
// NOT do that — we want claude in its own GROUP (Setpgid) but in the
// supervisor's SESSION, and we setsid the SUPERVISOR process itself once at
// startup via the raw syscall (so the choice is explicit and not buried in the
// child's SysProcAttr). darwin is the ship gate for this slice; the code is
// portable and the only linux-specific concern (Pdeathsig, which is
// linux-only) is intentionally NOT used — relying on Pdeathsig would couple
// child death to PARENT death, the exact opposite of what survival requires.
package agentsupervisor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Config configures a Supervisor.
type Config struct {
	// AgentID is the agent this supervisor owns (used for diagnostics + the
	// supervisor.instance record). It is NOT re-derived into a session-id here:
	// the caller bakes the --session-id into ChildCmd.
	AgentID string

	// HomeDir is the per-agent home directory the supervisor writes its
	// artifacts under (events.jsonl, claude.pid, supervisor.instance). The
	// daemon resolves it via agent_controller.agentPaths; tests pass a tempdir.
	HomeDir string

	// ChildCmd is the FULL argv of the child to launch: [binary, args...]. In
	// production this is the claude streaming argv assembled via
	// workerdaemon.BuildClaudeStreamingArgv (binary + flags + --session-id +
	// --mcp-config <path>). Tests inject a stand-in argv (a reader/echoer) — no
	// real claude required.
	ChildCmd []string

	// WorkspaceDir is the child process cwd (empty = inherit the supervisor's).
	WorkspaceDir string

	// Env is merged into the child environment (appended to os.Environ()).
	Env map[string]string

	// Logger receives one-line diagnostic messages (to stderr in the
	// subcommand). nil = discard.
	Logger func(msg string)

	// StopGrace is how long graceful Stop waits after SIGTERM before SIGKILL.
	// Zero = 5s.
	StopGrace time.Duration
}

// Artifact file names written under HomeDir.
const (
	// EventsFileName is the append-only persistent offset cursor: every raw
	// stdout line drained from the child is appended here (s2 will
	// ack-truncate; s1 only appends).
	EventsFileName = "events.jsonl"
	// PIDFileName holds the child (claude) pid, for a future daemon to locate
	// the running process.
	PIDFileName = "claude.pid"
	// InstanceFileName holds the supervisor instance identity (instance-id +
	// start timestamp + supervisor/child pids). Together with the pid this
	// gives a pid-reuse-safe "same process, never restarted" proof: a daemon
	// re-reads this file and compares the instance-id; if a stale pid was
	// reused by an unrelated process, the instance-id will not match.
	InstanceFileName = "supervisor.instance"
)

// Supervisor owns one child (claude) process: it launches the child in its own
// group, holds the child's stdin open, and continuously drains the child's
// stdout to events.jsonl regardless of any consumer. Safe for concurrent
// Inject/Stop.
type Supervisor struct {
	cfg Config

	// instanceID is a minted ULID identifying THIS supervisor incarnation.
	instanceID string
	startedAt  time.Time

	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	eventsFile *os.File

	// offset is the current byte length of events.jsonl (the persistent offset
	// cursor). Advanced by the drain goroutine; read under offMu.
	offMu  sync.Mutex
	offset int64

	stdinMu sync.Mutex // guards stdin writes + closed
	closed  bool       // set once Stop begins / child exited; blocks Inject

	stopOnce sync.Once
	exitOnce sync.Once
	waitErr  error         // child exit status (set before done closes)
	drainErr error         // terminal drain error, if any
	done     chan struct{} // closed after the child has exited AND drain joined
}

// DetachSession setsids the CURRENT process into a brand-new session+group so a
// later killpg of the PARENT's (daemon's) group does NOT reach it. It must be
// called by the supervisor process EARLY, before the parent can issue the
// killpg. It is idempotent-safe to call once at startup; calling it when
// already a group leader returns EPERM (which we treat as already-detached).
//
// This is a process-global state change, so it lives as a free function the
// subcommand calls at the top of main — NOT inside New (tests must NOT setsid
// the `go test` process). The killpg-escape test re-execs a helper that calls
// this.
func DetachSession() error {
	// syscall.Setsid() == setsid(2): new session, caller becomes session +
	// process-group leader, new pgid == caller pid. POSIX; identical on
	// darwin/linux.
	if _, err := syscall.Setsid(); err != nil {
		// EPERM means we are already a process-group leader (already in our own
		// group) — for our purposes that is "already detached", not a failure.
		if errors.Is(err, syscall.EPERM) {
			return nil
		}
		return fmt.Errorf("agentsupervisor: setsid: %w", err)
	}
	return nil
}

// New constructs a Supervisor (it does not launch anything; call Start).
func New(cfg Config) (*Supervisor, error) {
	if cfg.AgentID == "" {
		return nil, errors.New("agentsupervisor: agent_id required")
	}
	if cfg.HomeDir == "" {
		return nil, errors.New("agentsupervisor: home_dir required")
	}
	if len(cfg.ChildCmd) == 0 {
		return nil, errors.New("agentsupervisor: child_cmd required")
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}
	if cfg.StopGrace <= 0 {
		cfg.StopGrace = 5 * time.Second
	}
	return &Supervisor{
		cfg:        cfg,
		instanceID: mintInstanceID(),
		done:       make(chan struct{}),
	}, nil
}

// InstanceID returns this supervisor incarnation's minted id.
func (s *Supervisor) InstanceID() string { return s.instanceID }

// Start launches the child in its own process group, opens the persistent
// events cursor, writes the observability artifacts, and starts the drain
// goroutine. The returned Supervisor is immediately usable for Inject/Stop.
func (s *Supervisor) Start() error {
	if err := os.MkdirAll(s.cfg.HomeDir, 0o700); err != nil {
		return fmt.Errorf("agentsupervisor: mkdir home_dir: %w", err)
	}

	// Open events.jsonl append-only and seed the offset from its current size
	// (so a restart of the SUPERVISOR — not in s1, but cheap to get right —
	// would continue appending rather than truncate).
	eventsPath := filepath.Join(s.cfg.HomeDir, EventsFileName)
	f, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("agentsupervisor: open events file: %w", err)
	}
	if st, err := f.Stat(); err == nil {
		s.offset = st.Size()
	}
	s.eventsFile = f

	// IMPORTANT: plain exec.Command, NOT CommandContext bound to a cancelable
	// ctx — the child must OUTLIVE transient things (the whole survival point).
	s.cmd = exec.Command(s.cfg.ChildCmd[0], s.cfg.ChildCmd[1:]...)
	if s.cfg.WorkspaceDir != "" {
		s.cmd.Dir = s.cfg.WorkspaceDir
	}
	s.cmd.Env = os.Environ()
	for k, v := range s.cfg.Env {
		s.cmd.Env = append(s.cmd.Env, k+"="+v)
	}
	// Child in its OWN process group (pgid == child pid) under the supervisor's
	// session: Stop can killpg the whole claude tree, and a child-group signal
	// never reaches the supervisor. We deliberately do NOT Setsid the child and
	// do NOT use Pdeathsig (linux-only, and would tie child death to parent
	// death — the opposite of survival).
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	s.cmd.Stderr = os.Stderr

	// Hold the child's stdin OPEN for the child's lifetime (never closed except
	// on explicit Stop) so claude never gets stdin-EOF.
	stdin, err := s.cmd.StdinPipe()
	if err != nil {
		f.Close()
		return fmt.Errorf("agentsupervisor: stdin pipe: %w", err)
	}
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		f.Close()
		return fmt.Errorf("agentsupervisor: stdout pipe: %w", err)
	}
	if err := s.cmd.Start(); err != nil {
		f.Close()
		return fmt.Errorf("agentsupervisor: start child %q: %w", s.cfg.ChildCmd[0], err)
	}
	s.stdin = stdin
	s.stdout = stdout
	s.startedAt = time.Now()

	if err := s.writeArtifacts(); err != nil {
		s.cfg.Logger(fmt.Sprintf("agentsupervisor: write artifacts: %v", err))
	}

	go s.drainLoop()
	return nil
}

// drainLoop reads child stdout line-by-line, tolerantly parses each line via
// the validated stream parser (log+skip on error), and APPENDS the raw line +
// '\n' to events.jsonl, advancing the persistent offset. It keeps draining
// REGARDLESS of whether anything consumes events.jsonl — so the child never
// blocks on a full stdout pipe during the daemon-down window. On stdout close
// it waits for the child exit status and fires the terminal join.
func (s *Supervisor) drainLoop() {
	scanner := bufio.NewScanner(s.stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw := make([]byte, len(line))
		copy(raw, line)

		// Tolerant parse: validate the line but never drop the stream on a bad
		// one (s2 consumers read the raw events.jsonl).
		if _, err := parseStreamLine(raw); err != nil {
			s.cfg.Logger(fmt.Sprintf("agentsupervisor: parse stream line: %v", err))
		}

		out := append(raw, '\n')
		n, werr := s.eventsFile.Write(out)
		if werr != nil {
			s.cfg.Logger(fmt.Sprintf("agentsupervisor: append events: %v", werr))
			s.drainErr = werr
			break
		}
		s.offMu.Lock()
		s.offset += int64(n)
		s.offMu.Unlock()
	}
	if err := scanner.Err(); err != nil && s.drainErr == nil {
		s.drainErr = err
	}

	waitErr := s.cmd.Wait()
	s.fireExit(waitErr)
}

// fireExit records the child exit status, closes stdin + events file, and
// closes done exactly once.
func (s *Supervisor) fireExit(err error) {
	s.exitOnce.Do(func() {
		s.stdinMu.Lock()
		s.closed = true
		s.waitErr = err
		s.stdinMu.Unlock()
		if s.eventsFile != nil {
			_ = s.eventsFile.Sync()
			_ = s.eventsFile.Close()
		}
		close(s.done)
	})
}

// Inject writes a stream-json user line to the held-open child stdin.
// Concurrency-safe. Returns an error if the supervisor has stopped / the child
// has exited. (The socket front-end is s2; this exists for completeness +
// testing.)
func (s *Supervisor) Inject(msg string) error {
	line, err := encodeUserMessage(msg)
	if err != nil {
		return err
	}
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if s.closed {
		return ErrSupervisorClosed
	}
	if _, err := s.stdin.Write(line); err != nil {
		return fmt.Errorf("agentsupervisor: write stdin: %w", err)
	}
	return nil
}

// ErrSupervisorClosed is returned by Inject after Stop / child exit.
var ErrSupervisorClosed = errors.New("agentsupervisor: supervisor closed")

// Stop terminates the CHILD (claude) explicitly: graceful=true sends SIGTERM to
// the child group, waits up to StopGrace, then SIGKILLs the group; graceful=
// false SIGKILLs immediately. It blocks until the drain goroutine has joined.
//
// CRITICAL: this is for CLEAN teardown only. The daemon-death path must NOT
// call Stop — surviving a daemon death is the whole point, and that path simply
// leaves the supervisor running.
func (s *Supervisor) Stop(graceful bool) error {
	s.stopOnce.Do(func() {
		s.stdinMu.Lock()
		s.closed = true
		// Close the held-open stdin so claude gets EOF on its message stream as
		// part of a CLEAN shutdown (only ever here).
		if s.stdin != nil {
			_ = s.stdin.Close()
		}
		s.stdinMu.Unlock()

		if graceful {
			if err := s.signalGroup(syscall.SIGTERM); err != nil {
				s.cfg.Logger(fmt.Sprintf("agentsupervisor: SIGTERM child group: %v", err))
			}
			select {
			case <-s.done:
				return
			case <-time.After(s.cfg.StopGrace):
				s.cfg.Logger("agentsupervisor: grace expired, SIGKILL child group")
			}
		}
		if err := s.signalGroup(syscall.SIGKILL); err != nil {
			s.cfg.Logger(fmt.Sprintf("agentsupervisor: SIGKILL child group: %v", err))
		}
	})
	<-s.done
	return nil
}

// signalGroup sends sig to the child's process group (negative pid == killpg),
// matching the Setpgid launch.
func (s *Supervisor) signalGroup(sig syscall.Signal) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return errors.New("agentsupervisor: no child process")
	}
	return syscall.Kill(-s.cmd.Process.Pid, sig)
}

// Done returns a channel closed after the child has exited and drain joined.
func (s *Supervisor) Done() <-chan struct{} { return s.done }

// Wait blocks until the child has exited and the drain goroutine has joined,
// returning the child exit error (nil on clean exit) or, if the child exited
// cleanly but the drain failed, the drain error.
func (s *Supervisor) Wait() error {
	<-s.done
	if s.waitErr != nil {
		return s.waitErr
	}
	return s.drainErr
}

// Offset returns the current byte offset of events.jsonl (the persistent
// cursor). A future re-attach reads events.jsonl from an earlier offset up to
// this value.
func (s *Supervisor) Offset() int64 {
	s.offMu.Lock()
	defer s.offMu.Unlock()
	return s.offset
}

// ChildPID returns the child (claude) pid, or 0 before Start / after a failed
// start.
func (s *Supervisor) ChildPID() int {
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}
