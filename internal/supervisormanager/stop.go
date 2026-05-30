package supervisormanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

// StopSupervisor is the EXPLICIT-terminate path (PM focus): it SIGTERMs the
// supervisor PROCESS so the supervisor's own signal handler gracefully stops
// claude (its child group) and exits — i.e. the supervisor stays the sole owner
// of claude and tears claude down itself; the daemon NEVER signals claude
// directly. This is the StopAgent / reset path, the CONTRAST with Detach
// (survival, no signal).
//
// Mechanism. The supervisor setsid'd into its own session/group (s1), so a
// killpg of the supervisor group (syscall.Kill(-supPid, SIGTERM)) reaches the
// supervisor and, via its signal handler, the whole tree. We SIGTERM the
// supervisor group, wait up to grace for both the supervisor and the recorded
// claude child to die, then ESCALATE to SIGKILL of both groups (child first so
// claude cannot be oddly re-parented). The supervisor pid is read from the
// on-disk supervisor.instance record (the SupervisorRef does not carry it).
//
// After a successful stop the socket + instance artifacts are stale; we remove
// the socket best-effort so a later ProbeAgent cannot false-positive on it.
// StopSupervisor also Detaches the ref's client (closes the socket conn) so the
// caller has no dangling connection.
func StopSupervisor(ref *SupervisorRef, grace time.Duration) error {
	if ref == nil {
		return errors.New("supervisormanager: StopSupervisor: nil ref")
	}
	if grace <= 0 {
		grace = defaultStopGrace
	}
	// Close our side of the socket first; we are terminating, not surviving.
	Detach(ref)

	rec, ok := readInstance(ref.HomeDir)
	if !ok {
		// Nothing recorded → nothing to signal. Best-effort socket cleanup.
		_ = os.Remove(filepath.Join(ref.HomeDir, agentsupervisor.DefaultSocketName))
		return nil
	}
	supPID := rec.SupervisorPID
	childPID := rec.ChildPID

	// Graceful: SIGTERM the supervisor group. Its handler stops claude + exits.
	if supPID > 0 {
		_ = syscall.Kill(-supPID, syscall.SIGTERM)
		_ = syscall.Kill(supPID, syscall.SIGTERM)
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if (supPID <= 0 || !alive(supPID)) && (childPID <= 0 || !alive(childPID)) {
			_ = os.Remove(filepath.Join(ref.HomeDir, agentsupervisor.DefaultSocketName))
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Escalate: SIGKILL the groups (child first), then confirm with a bounded loop.
	killDeadline := time.Now().Add(3 * time.Second)
	for {
		childDead := childPID <= 0 || !alive(childPID)
		supDead := supPID <= 0 || !alive(supPID)
		if childDead && supDead {
			break
		}
		if !childDead {
			_ = killpg(childPID)
		}
		if !supDead {
			_ = killpg(supPID)
		}
		if !time.Now().Before(killDeadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	_ = os.Remove(filepath.Join(ref.HomeDir, agentsupervisor.DefaultSocketName))

	if supPID > 0 && alive(supPID) {
		return fmt.Errorf("supervisormanager: StopSupervisor: supervisor pid=%d still alive after SIGKILL", supPID)
	}
	if childPID > 0 && alive(childPID) {
		return fmt.Errorf("supervisormanager: StopSupervisor: child pid=%d still alive after SIGKILL", childPID)
	}
	return nil
}

// defaultStopGrace is the graceful SIGTERM→exit window StopSupervisor waits
// before escalating to SIGKILL. Mirrors the supervisor's own StopGrace default.
const defaultStopGrace = 5 * time.Second
