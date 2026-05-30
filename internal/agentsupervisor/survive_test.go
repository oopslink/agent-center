package agentsupervisor_test

// Deterministic killpg-escape test (v2.7 D2-f s1). NO real claude.
//
// This is the critical de-risk in CI form. It proves, with real subprocesses
// and a FAITHFUL production-like kill (a process-GROUP kill, killpg — exactly
// what the daemon's shutdown does: syscall.Kill(-pid, SIGKILL)), that:
//
//   - the supervisor setsids into its OWN session/group and so ESCAPES a killpg
//     of the PARENT's (daemon's) group,
//   - the supervisor process SURVIVES the parent-group kill,
//   - the stand-in CHILD survives,
//   - events.jsonl KEEPS GROWING after the parent is dead (drain continues with
//     NO consumer) and the offset advances,
//   - a fresh reader can read events.jsonl FROM a given offset (the read side of
//     future re-attach),
//   - the supervisor's pgid != the killed parent's pgid (group escape proven).
//
// ROLE DISPATCH. A single test binary plays three roles, selected by an env
// var so no external helper binary is needed:
//
//	TEST_ROLE=parent      → simulates the daemon: re-execs itself as the
//	                        supervisor role, prints "PID=.. HOME=.. PGID=..",
//	                        then blocks forever (until killpg'd).
//	TEST_ROLE=supervisor  → setsids (DetachSession), builds a Supervisor whose
//	                        ChildCmd re-execs THIS binary as the child role, and
//	                        runs until killed.
//	TEST_ROLE=child       → the stand-in claude: reads stdin line-by-line and
//	                        emits a valid claude stream-json line to stdout on a
//	                        timer (so drain has a steady supply with no consumer).
//
// The parent is started by the test in its OWN process group (Setpgid), and the
// test kills that group. setsid in the supervisor role is what makes it escape.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

const roleEnv = "AGENTSUP_TEST_ROLE"

// TestMain dispatches the parent/supervisor/child roles when the role env var
// is set; otherwise it runs the normal test suite.
func TestMain(m *testing.M) {
	switch os.Getenv(roleEnv) {
	case "parent":
		os.Exit(roleParent())
	case "supervisor":
		os.Exit(roleSupervisor())
	case "child":
		os.Exit(roleChild())
	default:
		os.Exit(m.Run())
	}
}

// roleChild is the stand-in claude. It drains its OWN stdin (so it never blocks)
// and emits one valid claude 2.1.156 stream-json line per tick to stdout. It
// runs until killed.
func roleChild() int {
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			// consume injected lines; ignore content
		}
	}()
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	i := 0
	for range t.C {
		// A real top-level stream-json line the parser accepts as "system".
		fmt.Fprintf(os.Stdout, `{"type":"system","subtype":"tick","n":%d}`+"\n", i)
		i++
	}
	return 0
}

// roleSupervisor setsids into its own group and runs a Supervisor whose child
// re-execs this binary as the child role. It prints its own pgid so the test
// can assert escape, then blocks until killed.
func roleSupervisor() int {
	home := os.Getenv("AGENTSUP_TEST_HOME")
	if home == "" {
		fmt.Fprintln(os.Stderr, "supervisor: AGENTSUP_TEST_HOME unset")
		return 2
	}
	if err := agentsupervisor.DetachSession(); err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: detach: %v\n", err)
		return 2
	}
	self, _ := os.Executable()
	sup, err := agentsupervisor.New(agentsupervisor.Config{
		AgentID:  "agent-test",
		HomeDir:  home,
		ChildCmd: []string{self, "-test.run=TestMain"},
		Env:      map[string]string{roleEnv: "child"},
		Logger:   func(msg string) { fmt.Fprintln(os.Stderr, "supervisor:", msg) },
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: new: %v\n", err)
		return 2
	}
	if err := sup.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: start: %v\n", err)
		return 2
	}
	// Report identity to the parent (which relays to the test).
	pgid, _ := syscall.Getpgid(os.Getpid())
	fmt.Fprintf(os.Stdout, "SUP_PID=%d SUP_PGID=%d CHILD_PID=%d\n", os.Getpid(), pgid, sup.ChildPID())
	os.Stdout.Sync()
	// Block until killed (the test never signals the supervisor — it kills the
	// PARENT group; survival means we keep running here). We sleep in a loop
	// rather than `select{}` so Go's deadlock detector does not abort us.
	blockForever()
	return 0
}

// blockForever parks the goroutine without tripping Go's all-goroutines-asleep
// deadlock detector (which `select{}` would). These role processes are killed
// externally by the test.
func blockForever() {
	for {
		time.Sleep(time.Hour)
	}
}

// roleParent simulates the daemon: it re-execs this binary as the supervisor
// role, relays the supervisor's identity line to its own stdout, then blocks
// until killpg'd. It is started by the test in its OWN process group.
func roleParent() int {
	home := os.Getenv("AGENTSUP_TEST_HOME")
	self, _ := os.Executable()
	cmd := exec.Command(self, "-test.run=TestMain")
	cmd.Env = append(os.Environ(), roleEnv+"=supervisor", "AGENTSUP_TEST_HOME="+home)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "parent: stdout pipe: %v\n", err)
		return 2
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "parent: start supervisor: %v\n", err)
		return 2
	}
	// Relay the supervisor's identity line to the test.
	sc := bufio.NewScanner(out)
	if sc.Scan() {
		fmt.Fprintln(os.Stdout, sc.Text())
		os.Stdout.Sync()
	}
	blockForever()
	return 0
}

// TestSupervisorSurvivesParentGroupKill is the faithful killpg-escape test.
func TestSupervisorSurvivesParentGroupKill(t *testing.T) {
	home := t.TempDir()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	// Start the PARENT (simulated daemon) in its OWN process group, so we can
	// killpg that group faithfully (mirrors the daemon's syscall.Kill(-pid,...)).
	parent := exec.Command(self, "-test.run=TestMain")
	parent.Env = append(os.Environ(), roleEnv+"=parent", "AGENTSUP_TEST_HOME="+home)
	parent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	parent.Stderr = os.Stderr
	parentOut, err := parent.StdoutPipe()
	if err != nil {
		t.Fatalf("parent stdout pipe: %v", err)
	}
	if err := parent.Start(); err != nil {
		t.Fatalf("start parent: %v", err)
	}
	parentPID := parent.Process.Pid
	// With Setpgid, the parent's pgid == its pid.
	parentPGID, err := syscall.Getpgid(parentPID)
	if err != nil {
		t.Fatalf("getpgid(parent): %v", err)
	}

	defer func() {
		// Best-effort cleanup of any survivors regardless of test outcome.
		_ = syscall.Kill(-parentPGID, syscall.SIGKILL)
	}()

	// Read the supervisor identity line relayed through the parent.
	supPID, supPGID, childPID := readIdentity(t, parentOut)
	if supPID == 0 || childPID == 0 {
		t.Fatalf("bad identity: supPID=%d childPID=%d", supPID, childPID)
	}

	// GROUP ESCAPE: the supervisor must NOT be in the parent's group.
	if supPGID == parentPGID {
		t.Fatalf("supervisor did NOT escape parent group: supPGID=%d == parentPGID=%d", supPGID, parentPGID)
	}
	// And (setsid) the supervisor is its own group leader: pgid == its pid.
	if supPGID != supPID {
		t.Fatalf("supervisor not its own group leader: supPGID=%d supPID=%d", supPGID, supPID)
	}

	// Let the drain accumulate some events, capture a pre-kill offset.
	time.Sleep(300 * time.Millisecond)
	if !alive(supPID) {
		t.Fatalf("supervisor not alive before kill")
	}
	_, preKillEnd, err := agentsupervisor.ReadEventsFrom(home, 0)
	if err != nil {
		t.Fatalf("read events pre-kill: %v", err)
	}
	if preKillEnd == 0 {
		t.Fatalf("expected events.jsonl to have grown before kill, got offset 0")
	}

	// FAITHFUL KILL: killpg the PARENT's group (the production-like daemon
	// shutdown). This SIGKILLs the parent (and anything still in its group) but
	// must MISS the setsid'd supervisor and its child.
	if err := syscall.Kill(-parentPGID, syscall.SIGKILL); err != nil {
		t.Fatalf("killpg(parent group): %v", err)
	}
	_, _ = parent.Process.Wait() // reap the parent

	// Give the kill time to propagate; the parent should be gone.
	waitDead(t, parentPID, 2*time.Second)

	// SURVIVAL: supervisor + child still alive after the parent-group kill.
	if !alive(supPID) {
		t.Fatalf("supervisor died with the parent group (survival FAILED)")
	}
	if !alive(childPID) {
		t.Fatalf("stand-in child died with the parent group (survival FAILED)")
	}

	// DRAIN CONTINUES with NO consumer: events.jsonl keeps growing after kill.
	growUntil(t, home, preKillEnd, 3*time.Second)

	// READ-FROM-OFFSET: a fresh reader resumes from the pre-kill offset and gets
	// only the bytes appended AFTER it, each a valid line.
	data, end, err := agentsupervisor.ReadEventsFrom(home, preKillEnd)
	if err != nil {
		t.Fatalf("read events from offset: %v", err)
	}
	if end <= preKillEnd {
		t.Fatalf("offset did not advance: end=%d preKill=%d", end, preKillEnd)
	}
	if len(data) == 0 || !bytes.Contains(data, []byte(`"type":"system"`)) {
		t.Fatalf("read-from-offset returned no fresh events: %q", string(data))
	}

	// Clean up survivors explicitly (defer also covers failures).
	_ = syscall.Kill(-childPID, syscall.SIGKILL) // child's own group
	_ = syscall.Kill(supPID, syscall.SIGKILL)
}

// readIdentity reads the "SUP_PID=.. SUP_PGID=.. CHILD_PID=.." line.
func readIdentity(t *testing.T, r interface{ Read([]byte) (int, error) }) (supPID, supPGID, childPID int) {
	t.Helper()
	sc := bufio.NewScanner(r)
	deadline := time.After(10 * time.Second)
	lineCh := make(chan string, 1)
	go func() {
		if sc.Scan() {
			lineCh <- sc.Text()
		} else {
			lineCh <- ""
		}
	}()
	select {
	case line := <-lineCh:
		for _, f := range strings.Fields(line) {
			kv := strings.SplitN(f, "=", 2)
			if len(kv) != 2 {
				continue
			}
			n, _ := strconv.Atoi(kv[1])
			switch kv[0] {
			case "SUP_PID":
				supPID = n
			case "SUP_PGID":
				supPGID = n
			case "CHILD_PID":
				childPID = n
			}
		}
	case <-deadline:
		t.Fatalf("timed out waiting for supervisor identity line")
	}
	return supPID, supPGID, childPID
}

// alive reports whether pid is alive via signal 0 (no signal actually sent).
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but we can't signal it → still alive.
	return err == syscall.EPERM
}

// waitDead fails if pid is still alive after the timeout.
func waitDead(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive after %s (expected dead)", pid, timeout)
}

// growUntil fails if events.jsonl does not grow beyond `from` within timeout.
func growUntil(t *testing.T, home string, from int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, end, err := agentsupervisor.ReadEventsFrom(home, 0)
		if err == nil && end > from {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("events.jsonl did not grow past offset %d within %s (drain not continuing)", from, timeout)
}
