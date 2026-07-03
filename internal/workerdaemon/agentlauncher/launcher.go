// Package agentlauncher is the worker→agent creation/rebuild abstraction (T854 D6,
// design §4.5): the worker stops "hosting N runtimes in one process" and becomes a
// launcher/controller that ensures each desired agent has its OWN runtime unit
// running, rebuilding it when it exits.
//
// AgentLauncher is the seam. LocalProcessLauncher (this file) forks/execs a
// `cmd/agent-runtime` OS process per agent — the single-machine target topology.
// K8sPodLauncher (deferred) would create an agent pod via the k8s API behind the
// SAME interface, so the controller code is launcher-agnostic ("this agent should
// run → ensure it runs", the how is the launcher's).
//
// Crash model (§4.5): an agent unit is an independent process, so an unrecoverable
// crash = the process exits and the launcher recreates it (backoff-throttled). This
// REPLACES the in-process daemon SelfHealStore crash→reschedule→relaunch loop: the
// OS process boundary is the isolation, and rebuild is just "spawn again".
package agentlauncher

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// AgentSpec is what the launcher needs to (re)create one agent's runtime unit. Most
// of the agent's configuration is self-fetched by the agent-runtime process from the
// center (ResumeState) at boot; the spec only carries what the LAUNCH needs.
type AgentSpec struct {
	// AgentID identifies the agent (becomes the process's --agent-id).
	AgentID string
	// Args are extra argv appended after the launcher's base args (e.g. per-agent
	// overrides); usually empty — the process self-configures from the center.
	Args []string
	// Env is a per-agent environment overlay merged over the launcher's base env.
	Env []string
}

// AgentLauncher ensures a desired agent's runtime unit is running and rebuilds it on
// exit. The controller declares desired state (Ensure per wanted agent, Stop per
// removed one); the launcher owns the process lifecycle + rebuild.
type AgentLauncher interface {
	// Ensure makes spec.AgentID running and keeps it running (rebuild on crash). It is
	// idempotent: a no-op if the agent is already up. Returns an error only if the
	// initial spawn fails.
	Ensure(spec AgentSpec) error
	// Stop terminates the agent unit and stops rebuilding it (desired-stopped, or the
	// agent was removed from this worker). Idempotent: a no-op for an unknown agent.
	Stop(agentID string) error
	// Running returns the agent ids currently launched (sorted).
	Running() []string
	// Shutdown stops every unit and waits for them to exit (worker drain).
	Shutdown(ctx context.Context) error
}

// BackoffParams throttles rebuilds so a crash-looping agent does not hot-spin. The
// delay grows Base·2^(consecutive crashes-1) capped at Max.
type BackoffParams struct {
	Base time.Duration
	Max  time.Duration
}

// DefaultBackoff is a sane rebuild throttle (1s → … → 30s).
var DefaultBackoff = BackoffParams{Base: time.Second, Max: 30 * time.Second}

func (b BackoffParams) normalized() BackoffParams {
	if b.Base <= 0 {
		b.Base = DefaultBackoff.Base
	}
	if b.Max <= 0 {
		b.Max = DefaultBackoff.Max
	}
	return b
}

// delayFor returns the rebuild delay after `crashes` consecutive crashes (crashes≥1).
func (b BackoffParams) delayFor(crashes int) time.Duration {
	d := b.Base
	for i := 1; i < crashes; i++ {
		d *= 2
		if d >= b.Max {
			return b.Max
		}
	}
	return d
}

// Process is one launched agent unit's handle. Injected via ProcessStarter so the
// launcher is unit-testable without real OS processes.
type Process interface {
	// Wait blocks until the process exits and returns its exit error (nil on exit 0).
	Wait() error
	// Signal asks the process to terminate (graceful); the supervisor escalates to
	// Kill if it does not exit within the stop grace.
	Signal() error
	// Kill force-terminates the process.
	Kill() error
	// PID is the process id (for logs).
	PID() int
}

// ProcessStarter spawns one agent-runtime process. The production impl execs the
// worker binary's `agent-runtime` subcommand; tests inject a fake.
type ProcessStarter interface {
	Start(ctx context.Context, spec AgentSpec) (Process, error)
}

// LocalProcessLauncher forks/execs a cmd/agent-runtime OS process per agent and
// rebuilds it on exit (design §4.5). Safe for concurrent Ensure/Stop from the
// controller loop.
type LocalProcessLauncher struct {
	starter   ProcessStarter
	backoff   BackoffParams
	stopGrace time.Duration
	after     func(time.Duration) <-chan time.Time // sleep seam (tests inject)
	log       func(format string, args ...any)

	mu    sync.Mutex
	units map[string]*agentUnit
	wg    sync.WaitGroup
}

// Config wires a LocalProcessLauncher.
type Config struct {
	// Starter spawns the per-agent process (required).
	Starter ProcessStarter
	// Backoff throttles rebuilds (zero → DefaultBackoff).
	Backoff BackoffParams
	// StopGrace is how long Stop waits after Signal before Kill (zero → 5s).
	StopGrace time.Duration
	// After is the delay seam (nil → time.After); tests inject a controllable one.
	After func(time.Duration) <-chan time.Time
	// Log is an optional logger.
	Log func(format string, args ...any)
}

// New builds a LocalProcessLauncher.
func New(cfg Config) (*LocalProcessLauncher, error) {
	if cfg.Starter == nil {
		return nil, errors.New("agentlauncher: starter required")
	}
	grace := cfg.StopGrace
	if grace <= 0 {
		grace = 5 * time.Second
	}
	after := cfg.After
	if after == nil {
		after = time.After
	}
	log := cfg.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	return &LocalProcessLauncher{
		starter:   cfg.Starter,
		backoff:   cfg.Backoff.normalized(),
		stopGrace: grace,
		after:     after,
		log:       log,
		units:     make(map[string]*agentUnit),
	}, nil
}

var _ AgentLauncher = (*LocalProcessLauncher)(nil)

// agentUnit is one supervised agent process.
type agentUnit struct {
	spec    AgentSpec
	proc    Process
	crashes int
	stopped bool          // Stop-initiated → the supervisor must not rebuild
	stopCh  chan struct{} // closed by Stop → interrupts a backoff wait immediately
	done    chan struct{} // closed when the supervise goroutine returns
}

// Ensure spawns + supervises the agent if it is not already up.
func (l *LocalProcessLauncher) Ensure(spec AgentSpec) error {
	if spec.AgentID == "" {
		return errors.New("agentlauncher: ensure requires agent_id")
	}
	l.mu.Lock()
	if u, ok := l.units[spec.AgentID]; ok && !u.stopped {
		l.mu.Unlock()
		return nil // already running/supervised
	}
	u := &agentUnit{spec: spec, stopCh: make(chan struct{}), done: make(chan struct{})}
	l.units[spec.AgentID] = u
	l.mu.Unlock()

	proc, err := l.starter.Start(context.Background(), spec)
	if err != nil {
		l.mu.Lock()
		delete(l.units, spec.AgentID)
		l.mu.Unlock()
		close(u.done)
		return err
	}
	l.mu.Lock()
	if u.stopped {
		// Stop raced in while Start was running: don't leak the process or start a
		// supervisor. Kill it and close done so the Stop waiting on it unblocks.
		l.mu.Unlock()
		_ = proc.Kill()
		close(u.done)
		return nil
	}
	u.proc = proc
	l.mu.Unlock()
	l.log("agentlauncher: launched agent=%s pid=%d", spec.AgentID, proc.PID())

	l.wg.Add(1)
	go l.supervise(spec.AgentID, u)
	return nil
}

// supervise blocks on the process and rebuilds it (backoff-throttled) until Stop.
func (l *LocalProcessLauncher) supervise(agentID string, u *agentUnit) {
	defer l.wg.Done()
	defer close(u.done)
	for {
		waitErr := u.proc.Wait()

		l.mu.Lock()
		if u.stopped {
			l.mu.Unlock()
			return // intentional stop — do not rebuild
		}
		u.crashes++
		crashes := u.crashes
		l.mu.Unlock()

		delay := l.backoff.delayFor(crashes)
		l.log("agentlauncher: agent=%s exited (%v) — rebuild #%d after %s", agentID, waitErr, crashes, delay)
		if !l.waitBackoff(u, delay) {
			return // stopped during backoff
		}

		proc, err := l.starter.Start(context.Background(), u.spec)
		if err != nil {
			l.log("agentlauncher: agent=%s rebuild spawn failed: %v — retrying", agentID, err)
			continue // treat a failed respawn as another crash cycle (backoff grows)
		}
		l.mu.Lock()
		if u.stopped { // raced with Stop during the spawn
			l.mu.Unlock()
			_ = proc.Kill()
			return
		}
		u.proc = proc
		l.mu.Unlock()
		l.log("agentlauncher: rebuilt agent=%s pid=%d", agentID, proc.PID())
	}
}

// waitBackoff waits d, returning false if Stop fired during the wait.
func (l *LocalProcessLauncher) waitBackoff(u *agentUnit, d time.Duration) bool {
	select {
	case <-l.after(d):
		l.mu.Lock()
		stopped := u.stopped
		l.mu.Unlock()
		return !stopped
	case <-u.stopCh:
		return false
	}
}

// Stop terminates the agent and prevents rebuild.
func (l *LocalProcessLauncher) Stop(agentID string) error {
	l.mu.Lock()
	u, ok := l.units[agentID]
	if !ok {
		l.mu.Unlock()
		return nil
	}
	if u.stopped {
		l.mu.Unlock()
		return nil // already stopping
	}
	u.stopped = true
	close(u.stopCh) // interrupt any in-flight backoff wait
	proc := u.proc
	delete(l.units, agentID)
	l.mu.Unlock()

	if proc != nil {
		_ = proc.Signal()
		if !l.waitExit(u, l.stopGrace) {
			_ = proc.Kill()
			<-u.done // ensure the supervise goroutine has drained after the kill
		}
	} else {
		<-u.done
	}
	return nil
}

// waitExit waits up to d for the supervise goroutine to finish (proc exited).
func (l *LocalProcessLauncher) waitExit(u *agentUnit, d time.Duration) bool {
	select {
	case <-u.done:
		return true
	case <-l.after(d):
		return false
	}
}

// Running returns the currently-launched agent ids (sorted, excludes stopped).
func (l *LocalProcessLauncher) Running() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.units))
	for id, u := range l.units {
		if !u.stopped {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Shutdown stops every unit and waits for the supervise goroutines to drain.
func (l *LocalProcessLauncher) Shutdown(ctx context.Context) error {
	l.mu.Lock()
	ids := make([]string, 0, len(l.units))
	for id := range l.units {
		ids = append(ids, id)
	}
	l.mu.Unlock()
	for _, id := range ids {
		_ = l.Stop(id)
	}
	drained := make(chan struct{})
	go func() { l.wg.Wait(); close(drained) }()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
