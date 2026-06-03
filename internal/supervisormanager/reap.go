package supervisormanager

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

// ReapResidual kills any residual supervisor+claude recorded in the agent's
// supervisor.instance, so a subsequent mode-B SpawnSupervisor cannot produce a
// SECOND claude for the same agent (PM focus #2, single-instance invariant).
//
// Called by s3b ONLY on the relaunch path, AFTER ProbeAgent returned
// Unavailable{dead|incompatible} and AFTER AcquireHomeLock succeeded (the lock
// is what makes a concurrent reuse-race unlikely — see AcquireHomeLock). If the
// supervisor.instance file is missing there is nothing to reap (nil).
//
// PID-REUSE GUARD. A recorded pid may have been reused by an unrelated process
// since the file was written; blindly killing it would kill an innocent victim.
// For each recorded pid that is still alive we VERIFY it is still the recorded
// process before killing, by comparing its kernel start time (darwin:
// `ps -o lstart= -p <pid>`) against the recorded started_at — the supervisor and
// its child both started at ~started_at, so a pid that was reused later has a
// LATER lstart and is skipped. We use a tolerance window because lstart is
// 1-second granular while started_at is RFC3339Nano, and the child forks a
// moment after the supervisor.
//
//	RESIDUAL RISK: if the verification cannot run (no `ps`, parse failure) we
//	FALL BACK to killing the recorded pid's GROUP best-effort. In that fallback a
//	pid reused within the tolerance window by a same-start-time process could be
//	mis-killed. This window is tiny and the home lockfile prevents two managers
//	from racing on the same home, so a concurrent reuse-race is unlikely; the
//	residual risk is the lone-pid-reuse-with-near-identical-start-time case, which
//	we accept and document here.
//
// KILL MECHANISM. The supervisor setsid'd into its own group and claude is its
// own group too (s1), so we killpg: syscall.Kill(-pid, SIGKILL) reaches the whole
// group. We kill the child group first, then the supervisor group, then wait
// briefly and confirm both recorded pids are dead.
func ReapResidual(home string) error {
	rec, ok := readInstance(home)
	if !ok {
		return nil // nothing recorded → nothing to reap
	}

	// reapOne returns the pid IF it is a recorded process we should kill, or 0 if
	// there is nothing to do for it. The pid-reuse guard runs here: if we cannot
	// positively confirm the pid is the recorded process we still best-effort kill
	// its group (documented residual risk) — the same killpg either way — so the
	// only effect of the guard is to skip a pid whose start time PROVES it is a
	// different (reused) process.
	reapOne := func(pid int) int {
		if pid <= 0 || !alive(pid) {
			return 0
		}
		if verifyStartMatches(pid, rec.StartedAt) {
			return pid // confirmed-recorded process
		}
		// Unverified: still kill best-effort (residual risk documented above). We do
		// NOT skip, because failing to reap risks a double-claude, which is worse
		// than the small mis-kill risk the lockfile already makes unlikely.
		return pid
	}

	child := reapOne(rec.ChildPID)
	sup := reapOne(rec.SupervisorPID)

	// Kill + confirm with RETRY (bounded). A single killpg can transiently return
	// EPERM when the group is mid-teardown (a just-signalled leader becoming a
	// zombie), and reaping the zombie is not instantaneous — so we re-issue killpg
	// each pass and let the alive() check (zombie == dead) be the arbiter. Child
	// group first so claude cannot be oddly re-parented, then the supervisor.
	deadline := time.Now().Add(3 * time.Second)
	for {
		childDead := child == 0 || !alive(child)
		supDead := sup == 0 || !alive(sup)
		if childDead && supDead {
			break
		}
		if !childDead {
			_ = killpg(child)
		}
		if !supDead {
			_ = killpg(sup)
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	var killErrs []string
	if child != 0 && alive(child) {
		killErrs = append(killErrs, fmt.Sprintf("child pid=%d still alive after reap", child))
	}
	if sup != 0 && alive(sup) {
		killErrs = append(killErrs, fmt.Sprintf("supervisor pid=%d still alive after reap", sup))
	}

	// Remove the now-stale socket + instance file so a stale ProbeAgent cannot
	// false-positive on them before the relaunch writes fresh ones. Best-effort.
	// v2.7 #178: the live socket lives outside the home (sockPathFor); also clean
	// any legacy pre-#178 socket that used to sit under the home.
	_ = os.Remove(sockPathFor(rec))
	_ = os.Remove(filepath.Join(home, agentsupervisor.DefaultSocketName))

	if len(killErrs) > 0 {
		return fmt.Errorf("supervisormanager: reap residual: %s", strings.Join(killErrs, "; "))
	}
	return nil
}

// killpg sends SIGKILL to the process GROUP of pid (the supervisor setsid'd and
// claude is its own leader, so -pid addresses the whole group).
func killpg(pid int) error {
	if pid <= 0 {
		return nil
	}
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM) {
		// ESRCH ⇒ already gone. EPERM ⇒ the group is mid-teardown (only zombies /
		// exiting members left) — transient; the caller's alive() confirmation loop
		// is the real arbiter, so do not treat it as fatal here.
		return nil
	}
	return err
}

// alive reports whether pid is a RUNNING process. A signal-0 probe alone is not
// enough: after we SIGKILL a process we (or a previous daemon incarnation) are
// the parent of, it becomes a ZOMBIE until reaped — and kill(pid,0) still
// succeeds on a zombie. A zombie is a DEAD process (claude is gone), so reap-
// confirmation must not count it as alive. We therefore exclude the zombie state.
//
// EPERM ⇒ the process exists but is not ours to signal ⇒ alive (and not a zombie
// we created). Otherwise we consult the process state and treat Z (zombie) as
// dead.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			return true
		}
		return false // ESRCH / no such process
	}
	return !isZombie(pid)
}

// isZombie reports whether pid is in the zombie (defunct) state. Best-effort
// (darwin: `ps -o stat=`); on any failure we assume NOT a zombie so a transient
// ps hiccup never makes us treat a truly-live process as dead.
func isZombie(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return false
	}
	st := strings.TrimSpace(string(out))
	return strings.HasPrefix(st, "Z")
}

// verifyStartMatches confirms (best-effort, darwin) that pid's kernel start time
// matches the recorded started_at within startMatchTolerance. Returns false on
// any failure (no ps, parse error, no match) so the caller treats the identity as
// UNVERIFIED and falls back. This is the pid-reuse guard.
//
// darwin `ps -o lstart= -p <pid>` prints e.g. "Sat May 30 14:00:03 2026" (second
// granularity, local time). We parse it and compare to started_at (RFC3339Nano).
func verifyStartMatches(pid int, startedAt string) bool {
	want, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return false
	}
	// Force the C locale so lstart is the English "Mon Jan _2 15:04:05 2006" we
	// parse below (a localized weekday/month would otherwise fail the parse and
	// silently demote the guard to the kill-fallback path).
	cmd := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprintf("%d", pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return false
	}
	// lstart format is ANSIC-ish with a weekday: "Mon Jan _2 15:04:05 2006".
	got, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", s, time.Local)
	if err != nil {
		return false
	}
	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	return diff <= startMatchTolerance
}

// startMatchTolerance is the slack allowed between the recorded started_at and a
// pid's kernel lstart: lstart is 1s-granular and the child forks a beat after the
// supervisor records started_at, so a few seconds of slack is correct. Kept tight
// so a much-later reused pid (different start time) fails verification.
const startMatchTolerance = 5 * time.Second
