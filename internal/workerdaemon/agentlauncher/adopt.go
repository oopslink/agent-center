package agentlauncher

import (
	"syscall"
	"time"
)

// PIDStore durably records the OS pid of each launched agent process so a worker
// RESTART can find agent processes that outlived it and RE-ADOPT them instead of
// double-spawning (T860 gap5). It is worker-level state (survives the worker process),
// keyed by agent id. The launcher writes it on every (re)spawn and clears it on an
// intentional Stop / crash-loop giveup; the boot reconcile reads it to decide adopt-vs-
// spawn. Best-effort: a store error never blocks a launch (logged by the caller).
type PIDStore interface {
	// Record persists agentID→pid (overwriting any prior pid for the agent).
	Record(agentID string, pid int) error
	// Remove drops the agent's recorded pid (intentional stop / poison giveup).
	Remove(agentID string) error
	// Load returns the full agentID→pid map (empty map, not nil, when unset).
	Load() (map[string]int, error)
}

// adoptedProcess is a Process handle for an agent process the launcher did NOT fork —
// a survivor of a prior worker incarnation that the worker re-adopts on restart (T860
// gap5). Because it is not a child, we cannot os.Wait it; liveness is polled via
// signal-0 and termination uses the process GROUP (the survivor was started Setpgid, so
// pid == pgid), mirroring osProcess.
type adoptedProcess struct {
	pid   int
	poll  time.Duration
	after func(time.Duration) <-chan time.Time // sleep seam (tests inject)
}

// newAdoptedProcess wraps a non-child pid. poll≤0 → 1s.
func newAdoptedProcess(pid int, poll time.Duration, after func(time.Duration) <-chan time.Time) *adoptedProcess {
	if poll <= 0 {
		poll = time.Second
	}
	if after == nil {
		after = time.After
	}
	return &adoptedProcess{pid: pid, poll: poll, after: after}
}

// Wait polls signal-0 until the (non-child) process is gone, then returns nil. We can't
// reap a non-child's exit status, so "gone" is the terminal signal the supervisor sees.
func (p *adoptedProcess) Wait() error {
	for {
		if !PIDAlive(p.pid) {
			return nil
		}
		<-p.after(p.poll)
	}
}

func (p *adoptedProcess) Signal() error { return p.signalGroup(syscall.SIGTERM) }
func (p *adoptedProcess) Kill() error   { return p.signalGroup(syscall.SIGKILL) }
func (p *adoptedProcess) PID() int      { return p.pid }

func (p *adoptedProcess) signalGroup(sig syscall.Signal) error {
	// Negative pid → the process group (the survivor was started as a group leader).
	if err := syscall.Kill(-p.pid, sig); err != nil {
		return syscall.Kill(p.pid, sig) // fall back to the bare process
	}
	return nil
}

// PIDAlive is the fast PRE-FILTER for adoption: signal-0 reports whether some process
// holds the pid. It is NOT authoritative (pid recycling could match a different
// process), which is why the caller confirms identity via the control-socket health
// probe before adopting.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
