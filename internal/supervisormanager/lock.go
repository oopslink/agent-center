package supervisormanager

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// HomeLockName is the per-agent-home lockfile flock'd by the managing daemon.
const HomeLockName = "supervisor.lock"

// AcquireHomeLock takes an EXCLUSIVE, non-blocking flock on <home>/supervisor.lock
// (PM focus #2). The daemon holds it for as long as it MANAGES the agent. A
// second daemon/manager attempting the same home gets a LOCK_NB failure and must
// NOT also manage/relaunch the agent.
//
// RACE PREVENTED. Without this lock, two daemons that both boot-probe the same
// agent and both decide Unavailable would each ReapResidual + SpawnSupervisor →
// TWO claudes for one agent (the double-claude the whole survival design forbids).
// flock serializes them: exactly one wins the lock and relaunches; the other
// backs off. It also makes ReapResidual's pid-reuse window safe — only the lock
// holder reaps, so there is no concurrent reaper racing a reused pid.
//
// The returned release closes the fd (which releases the flock) and is
// idempotent. The lockfile itself is left on disk (flock state is on the open fd,
// not the file's presence), so a leftover supervisor.lock is harmless.
func AcquireHomeLock(home string) (release func(), err error) {
	if home == "" {
		return nil, fmt.Errorf("supervisormanager: AcquireHomeLock: empty home")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("supervisormanager: AcquireHomeLock: mkdir home: %w", err)
	}
	path := filepath.Join(home, HomeLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("supervisormanager: AcquireHomeLock: open lockfile: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// EWOULDBLOCK ⇒ another holder. Surfaced as a normal (non-fatal) error so
		// the caller can back off rather than crash.
		return nil, fmt.Errorf("supervisormanager: AcquireHomeLock: home %s already managed (flock): %w", home, err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			// Closing the fd releases the advisory lock. Explicit LOCK_UN first is a
			// belt-and-suspenders clean unlock.
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		})
	}, nil
}
