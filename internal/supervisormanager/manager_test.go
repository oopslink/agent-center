package supervisormanager_test

// Deterministic daemon-side supervisor-manager tests (v2.7 D2-f s3a). NO real
// claude: the supervisor's child is a stand-in shell script that ignores claude
// argv, drains its stdin, and emits valid stream-json lines on a ticker. We
// exercise the REAL `worker agent-supervisor` subcommand by building the real
// agent-center binary once and pointing SpawnSupervisor.BinaryPath at it (and
// --claude-bin at the stand-in).
//
// Covered:
//   - spawn + attach: SpawnSupervisor → ref with InstanceID/ChildPID; Hello matches.
//   - probe Reattachable: live compatible supervisor → Reattachable, instance matches file.
//   - probe dead → Unavailable{dead}; relaunch → a NEW instance_id.
//   - probe incompatible → Unavailable{incompatible} via a stand-in 999-version socket server.
//   - reap-no-double: residual supervisor+claude → ReapResidual kills recorded child; respawn → one claude.
//   - lockfile mutual exclusion.
//   - detach-not-kill: Detach leaves supervisor + child alive.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/supervisormanager"
)

// buildAgentCenter builds the real agent-center CLI binary once per test run and
// returns its path. The supervisor subcommand lives under `worker agent-supervisor`.
func buildAgentCenter(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "agent-center")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/oopslink/agent-center/cmd/agent-center")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build agent-center: %v", err)
	}
	return bin
}

// standinClaude writes a stand-in claude script: it ignores all argv (the claude
// flags), drains stdin in the background so it never blocks, and emits a valid
// stream-json system line every 50ms until killed.
func standinClaude(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "standin-claude.sh")
	script := "#!/bin/sh\n" +
		"cat >/dev/null &\n" +
		"i=0\n" +
		"while true; do\n" +
		"  printf '{\"type\":\"system\",\"subtype\":\"tick\",\"n\":%d}\\n' \"$i\"\n" +
		"  i=$((i+1))\n" +
		"  sleep 0.05\n" +
		"done\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write standin: %v", err)
	}
	return path
}

// alive reports whether pid is a RUNNING process. A zombie (defunct, killed but
// not yet reaped by its parent) is treated as DEAD, mirroring the manager's
// alive(): claude/supervisor being a zombie means it is gone.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil {
		return err == syscall.EPERM
	}
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return true
	}
	return !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

// reapRef best-effort kills a spawned supervisor + its child group so a test does
// not leak survivors (the WHOLE point is that they survive the daemon, so the
// test must clean them up itself).
func reapRef(ref *supervisormanager.SupervisorRef) {
	if ref == nil {
		return
	}
	rec := readInstanceFile(ref.HomeDir)
	if rec.ChildPID > 0 {
		_ = syscall.Kill(-rec.ChildPID, syscall.SIGKILL)
		_ = syscall.Kill(rec.ChildPID, syscall.SIGKILL)
	}
	if rec.SupervisorPID > 0 {
		_ = syscall.Kill(-rec.SupervisorPID, syscall.SIGKILL)
		_ = syscall.Kill(rec.SupervisorPID, syscall.SIGKILL)
	}
}

type instRec struct {
	InstanceID    string `json:"instance_id"`
	AgentID       string `json:"agent_id"`
	SupervisorPID int    `json:"supervisor_pid"`
	ChildPID      int    `json:"child_pid"`
	StartedAt     string `json:"started_at"`
	SockPath      string `json:"sock_path,omitempty"` // v2.7 #178
}

func readInstanceFile(home string) instRec {
	var r instRec
	b, err := os.ReadFile(filepath.Join(home, agentsupervisor.InstanceFileName))
	if err != nil {
		return r
	}
	_ = json.Unmarshal(b, &r)
	return r
}

// spawnHelper spawns a supervisor against the real subcommand + stand-in claude.
func spawnHelper(t *testing.T, bin, claude, home, agentID string) *supervisormanager.SupervisorRef {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ref, err := supervisormanager.SpawnSupervisor(ctx, supervisormanager.SpawnSupervisorCfg{
		AgentID:       agentID,
		HomeDir:       home,
		BinaryPath:    bin,
		ClaudeBin:     claude,
		ComeUpTimeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}
	return ref
}

func TestSpawnSupervisor_AttachHello(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	ref := spawnHelper(t, bin, claude, home, "agent-spawn")
	defer reapRef(ref)
	defer supervisormanager.Detach(ref)

	if ref.InstanceID == "" {
		t.Fatalf("ref InstanceID empty")
	}
	if ref.ChildPID <= 0 {
		t.Fatalf("ref ChildPID not set: %d", ref.ChildPID)
	}
	if ref.NegotiatedVersion != agentsupervisor.ProtocolVersion {
		t.Fatalf("NegotiatedVersion=%d want %d", ref.NegotiatedVersion, agentsupervisor.ProtocolVersion)
	}

	// The ref's open client answers Hello and it matches the ref + the file.
	hello, err := ref.Client.Hello(context.Background())
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if hello.InstanceID != ref.InstanceID {
		t.Fatalf("hello.InstanceID=%s ref=%s", hello.InstanceID, ref.InstanceID)
	}
	if hello.ChildPID != ref.ChildPID {
		t.Fatalf("hello.ChildPID=%d ref=%d", hello.ChildPID, ref.ChildPID)
	}
	rec := readInstanceFile(home)
	if rec.InstanceID != ref.InstanceID {
		t.Fatalf("file InstanceID=%s ref=%s", rec.InstanceID, ref.InstanceID)
	}
	if !alive(rec.ChildPID) {
		t.Fatalf("child pid %d not alive", rec.ChildPID)
	}
}

// SpawnSupervisor captures the supervisor's stdout+stderr to <home>/supervisor.log
// so a DETACHED supervisor's death cause is recoverable post-mortem (the daemon
// never Wait()s it). Assert the file is created and carries the spawn marker.
func TestSpawnSupervisor_StderrCapturedToLog(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	ref := spawnHelper(t, bin, claude, home, "agent-superviselog")
	defer reapRef(ref)
	defer supervisormanager.Detach(ref)

	data, err := os.ReadFile(filepath.Join(home, "supervisor.log"))
	if err != nil {
		t.Fatalf("supervisor.log not created: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "agent-center supervisor spawn") ||
		!strings.Contains(got, "agent=agent-superviselog") ||
		!strings.Contains(got, "pid=") {
		t.Fatalf("supervisor.log missing spawn marker; got:\n%s", got)
	}
}

func TestProbeAgent_Reattachable(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	ref := spawnHelper(t, bin, claude, home, "agent-reattach")
	defer reapRef(ref)
	// Close the spawn client; ProbeAgent opens its own.
	supervisormanager.Detach(ref)

	pr, err := supervisormanager.ProbeAgent(context.Background(), home)
	if err != nil {
		t.Fatalf("ProbeAgent: %v", err)
	}
	defer func() {
		if pr.Client != nil {
			_ = pr.Client.Close()
		}
	}()
	if pr.State != supervisormanager.Reattachable {
		t.Fatalf("state=%v reason=%s want Reattachable", pr.State, pr.Reason)
	}
	rec := readInstanceFile(home)
	if pr.Hello.InstanceID != rec.InstanceID {
		t.Fatalf("hello.InstanceID=%s file=%s", pr.Hello.InstanceID, rec.InstanceID)
	}
	if pr.NegotiatedVersion != agentsupervisor.ProtocolVersion {
		t.Fatalf("NegotiatedVersion=%d", pr.NegotiatedVersion)
	}
}

// TestSpawnSupervisor_SocketOutsideHome_ReattachViaInstance is the v2.7 #178
// (acceptance FINDING-E) regression: the live supervisor socket must NOT live
// under the deeply-nested agent home (which overflowed macOS's 104-byte
// sun_path limit) — it lives under the OS temp dir, its path is recorded in
// supervisor.instance, and a returning daemon re-attaches by reading that path.
func TestSpawnSupervisor_SocketOutsideHome_ReattachViaInstance(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	ref := spawnHelper(t, bin, claude, home, "agent-sockreloc")
	defer reapRef(ref)

	want := agentsupervisor.SockPath("agent-sockreloc")

	// The legacy in-home socket must NOT exist.
	if _, err := os.Stat(filepath.Join(home, agentsupervisor.DefaultSocketName)); !os.IsNotExist(err) {
		t.Fatalf("legacy in-home socket must not exist (stat err=%v)", err)
	}
	// The live socket is the short temp-dir path, is bound, and is what the ref carries.
	if ref.SockPath != want {
		t.Fatalf("ref.SockPath=%q want %q", ref.SockPath, want)
	}
	if !strings.HasPrefix(ref.SockPath, os.TempDir()) {
		t.Fatalf("sock %q must be under TempDir %q", ref.SockPath, os.TempDir())
	}
	if _, err := os.Stat(ref.SockPath); err != nil {
		t.Fatalf("bound socket missing at %q: %v", ref.SockPath, err)
	}
	// The instance file records sock_path so a restarted daemon can find it.
	if rec := readInstanceFile(home); rec.SockPath != want {
		t.Fatalf("instance sock_path=%q want %q", rec.SockPath, want)
	}

	// Simulate a daemon restart: drop our client, then a FRESH ProbeAgent (as a
	// new daemon would) reads the instance, resolves sock_path, and re-attaches.
	supervisormanager.Detach(ref)
	pr, err := supervisormanager.ProbeAgent(context.Background(), home)
	if err != nil {
		t.Fatalf("ProbeAgent: %v", err)
	}
	defer func() {
		if pr.Client != nil {
			_ = pr.Client.Close()
		}
	}()
	if pr.State != supervisormanager.Reattachable {
		t.Fatalf("state=%v reason=%s want Reattachable", pr.State, pr.Reason)
	}
	if pr.SockPath != want {
		t.Fatalf("probe SockPath=%q want %q", pr.SockPath, want)
	}
}

func TestProbeAgent_DeadThenRelaunchNewInstance(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	ref := spawnHelper(t, bin, claude, home, "agent-dead")
	defer reapRef(ref)
	deadInstance := ref.InstanceID
	supervisormanager.Detach(ref)

	// Kill the supervisor + child groups to simulate a crash (no graceful stop).
	rec := readInstanceFile(home)
	_ = syscall.Kill(-rec.ChildPID, syscall.SIGKILL)
	_ = syscall.Kill(-rec.SupervisorPID, syscall.SIGKILL)
	// Wait for the socket to stop answering.
	waitProbeDead(t, home, 5*time.Second)

	pr, err := supervisormanager.ProbeAgent(context.Background(), home)
	if err != nil {
		t.Fatalf("ProbeAgent: %v", err)
	}
	if pr.State != supervisormanager.Unavailable || pr.Reason != supervisormanager.ReasonDead {
		t.Fatalf("state=%v reason=%s want Unavailable{dead}", pr.State, pr.Reason)
	}

	// Reap residual + relaunch → a NEW instance id.
	if err := supervisormanager.ReapResidual(home); err != nil {
		t.Fatalf("ReapResidual: %v", err)
	}
	ref2 := spawnHelper(t, bin, claude, home, "agent-dead")
	defer reapRef(ref2)
	defer supervisormanager.Detach(ref2)
	if ref2.InstanceID == deadInstance {
		t.Fatalf("relaunch reused dead instance id %s", deadInstance)
	}
}

func TestProbeAgent_Missing(t *testing.T) {
	home := t.TempDir()
	pr, err := supervisormanager.ProbeAgent(context.Background(), home)
	if err != nil {
		t.Fatalf("ProbeAgent: %v", err)
	}
	if pr.State != supervisormanager.Unavailable || pr.Reason != supervisormanager.ReasonMissing {
		t.Fatalf("state=%v reason=%s want Unavailable{missing}", pr.State, pr.Reason)
	}
}

// TestProbeAgent_DifferentVersionStillReattachable is the regression guard for the
// v2.7 version-gate REMOVAL (@oopslink — drop the cross-version range). It stands
// up a tiny socket server answering hello with protocol_version: 999 (a version
// this build has never heard of) while a matching supervisor.instance makes the
// identity check pass. With the gate gone, ProbeAgent must return REATTACHABLE
// (not Unavailable{incompatible}): the protocol is assumed backward-compatible, so
// a live supervisor is always re-attachable regardless of its advertised version.
func TestProbeAgent_DifferentVersionStillReattachable(t *testing.T) {
	home := t.TempDir()
	instanceID := "01DIFFVER999INSTANCE"
	startedAt := time.Now().Format(time.RFC3339Nano)

	// v2.7 #178: the live socket lives outside the home; record its path in the
	// instance so ProbeAgent (which prefers rec.sock_path) connects there.
	sockPath := agentsupervisor.SockPath("agent-diffver")

	// Write a supervisor.instance the running fake "matches".
	rec := instRec{
		InstanceID:    instanceID,
		AgentID:       "agent-diffver",
		SupervisorPID: os.Getpid(),
		ChildPID:      os.Getpid(),
		StartedAt:     startedAt,
		SockPath:      sockPath,
	}
	b, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(filepath.Join(home, agentsupervisor.InstanceFileName), b, 0o600); err != nil {
		t.Fatalf("write instance: %v", err)
	}

	stop := serveFakeHello(t, sockPath, helloFrame{
		Ok:              true,
		ProtocolVersion: 999, // a version this build has never seen
		InstanceID:      instanceID,
		AgentID:         "agent-diffver",
		ChildPID:        os.Getpid(),
		StartedAt:       startedAt,
	})
	defer stop()

	pr, err := supervisormanager.ProbeAgent(context.Background(), home)
	if err != nil {
		t.Fatalf("ProbeAgent: %v", err)
	}
	if pr.Client != nil {
		_ = pr.Client.Close()
	}
	// No version gate → a live, identity-matched supervisor is Reattachable even at
	// an unknown protocol version (backward-compat assumed).
	if pr.State != supervisormanager.Reattachable {
		t.Fatalf("state=%v reason=%s want Reattachable (version gate removed)", pr.State, pr.Reason)
	}
}

// helloFrame is the minimal hello response shape the fake server emits (matches
// agentsupervisor.Response JSON tags).
type helloFrame struct {
	Ok              bool   `json:"ok"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	ChildPID        int    `json:"child_pid,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
}

// serveFakeHello listens on sockPath and replies to every length-framed request
// with the given hello response (one frame in, one frame out). Returns a stop fn.
func serveFakeHello(t *testing.T, sockPath string, resp helloFrame) func() {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen fake: %v", err)
	}
	respBytes, _ := json.Marshal(resp)
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					if _, err := readFrame(c); err != nil {
						return
					}
					if err := writeFrame(c, respBytes); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return func() {
		close(done)
		_ = ln.Close()
	}
}

// readFrame / writeFrame mirror the s2 length-framed transport (4-byte BE len +
// payload) so the fake server speaks the exact wire the AttachClient expects.
func readFrame(c net.Conn) ([]byte, error) {
	var hdr [4]byte
	if _, err := readFull(c, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := readFull(c, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeFrame(c net.Conn, b []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := c.Write(hdr[:]); err != nil {
		return err
	}
	_, err := c.Write(b)
	return err
}

func readFull(c net.Conn, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		n, err := c.Read(buf[got:])
		got += n
		if err != nil {
			return got, err
		}
	}
	return got, nil
}

func TestReapResidual_NoDoubleClaude(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	// A residual (still-alive) supervisor+claude recorded in supervisor.instance.
	ref := spawnHelper(t, bin, claude, home, "agent-reap")
	defer reapRef(ref)
	supervisormanager.Detach(ref)

	oldRec := readInstanceFile(home)
	if !alive(oldRec.ChildPID) {
		t.Fatalf("precondition: residual child not alive")
	}

	if err := supervisormanager.ReapResidual(home); err != nil {
		t.Fatalf("ReapResidual: %v", err)
	}
	if alive(oldRec.ChildPID) {
		t.Fatalf("residual child pid %d still alive after reap", oldRec.ChildPID)
	}
	if alive(oldRec.SupervisorPID) {
		t.Fatalf("residual supervisor pid %d still alive after reap", oldRec.SupervisorPID)
	}

	// Fresh spawn → exactly ONE live claude (the new child); the old one gone.
	ref2 := spawnHelper(t, bin, claude, home, "agent-reap")
	defer reapRef(ref2)
	defer supervisormanager.Detach(ref2)
	newRec := readInstanceFile(home)
	if newRec.ChildPID == oldRec.ChildPID {
		t.Fatalf("new child pid == old child pid %d (not a fresh claude)", oldRec.ChildPID)
	}
	if !alive(newRec.ChildPID) {
		t.Fatalf("new child pid %d not alive", newRec.ChildPID)
	}
	if alive(oldRec.ChildPID) {
		t.Fatalf("INVARIANT VIOLATED: old child %d alive alongside new child %d (double claude)", oldRec.ChildPID, newRec.ChildPID)
	}
}

func TestAcquireHomeLock_MutualExclusion(t *testing.T) {
	home := t.TempDir()

	release, err := supervisormanager.AcquireHomeLock(home)
	if err != nil {
		t.Fatalf("first AcquireHomeLock: %v", err)
	}

	if _, err := supervisormanager.AcquireHomeLock(home); err == nil {
		t.Fatalf("second AcquireHomeLock succeeded while first held (expected LOCK_NB failure)")
	}

	release()

	release2, err := supervisormanager.AcquireHomeLock(home)
	if err != nil {
		t.Fatalf("AcquireHomeLock after release: %v", err)
	}
	release2()
}

func TestDetach_NotKill(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	ref := spawnHelper(t, bin, claude, home, "agent-detach")
	defer reapRef(ref)

	rec := readInstanceFile(home)
	supChild := rec.ChildPID
	supPID := rec.SupervisorPID

	supervisormanager.Detach(ref)

	// Give any (incorrect) signal time to land; it must NOT — detach is no-kill.
	time.Sleep(200 * time.Millisecond)
	if !alive(supPID) {
		t.Fatalf("supervisor pid %d died on Detach (expected survive)", supPID)
	}
	if !alive(supChild) {
		t.Fatalf("child pid %d died on Detach (expected survive)", supChild)
	}
	if ref.Client != nil {
		t.Fatalf("Detach did not nil the client")
	}
}

// waitProbeDead waits until ProbeAgent stops returning Reattachable for home.
func waitProbeDead(t *testing.T, home string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pr, err := supervisormanager.ProbeAgent(context.Background(), home)
		if err == nil && pr.State == supervisormanager.Unavailable {
			return
		}
		if pr.Client != nil {
			_ = pr.Client.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("supervisor still probe-Reattachable after %s (expected dead)", timeout)
}

// TestManager_DetachAll verifies the Manager registry tears down clients + locks
// without killing the supervisor/child.
func TestManager_DetachAll(t *testing.T) {
	bin := buildAgentCenter(t)
	claude := standinClaude(t)
	home := t.TempDir()

	ref := spawnHelper(t, bin, claude, home, "agent-mgr")
	defer reapRef(ref)

	release, err := supervisormanager.AcquireHomeLock(home)
	if err != nil {
		t.Fatalf("AcquireHomeLock: %v", err)
	}

	m := supervisormanager.NewManager()
	m.Track(ref, release)
	if got, ok := m.Get("agent-mgr"); !ok || got != ref {
		t.Fatalf("Get returned %v ok=%v", got, ok)
	}

	rec := readInstanceFile(home)
	m.DetachAll()

	// Lock released → re-acquire succeeds.
	rel2, err := supervisormanager.AcquireHomeLock(home)
	if err != nil {
		t.Fatalf("lock not released by DetachAll: %v", err)
	}
	rel2()

	// Supervisor + child still alive (detach, not kill).
	if !alive(rec.SupervisorPID) || !alive(rec.ChildPID) {
		t.Fatalf("DetachAll killed supervisor/child (expected survive)")
	}
}
