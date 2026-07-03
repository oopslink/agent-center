package agentlauncher

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// memPIDStore is an in-memory PIDStore for wiring tests.
type memPIDStore struct {
	mu sync.Mutex
	m  map[string]int
}

func newMemPIDStore() *memPIDStore { return &memPIDStore{m: map[string]int{}} }

func (s *memPIDStore) Record(a string, pid int) error {
	s.mu.Lock()
	s.m[a] = pid
	s.mu.Unlock()
	return nil
}
func (s *memPIDStore) Remove(a string) error {
	s.mu.Lock()
	delete(s.m, a)
	s.mu.Unlock()
	return nil
}
func (s *memPIDStore) Load() (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for k, v := range s.m {
		out[k] = v
	}
	return out, nil
}
func (s *memPIDStore) has(a string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[a]
	return ok
}

// TestFilePIDStore_RoundTrip pins the durable store: Record/Load/Remove survive a fresh
// instance over the same file (the worker-restart read-back T860 gap5 depends on).
func TestFilePIDStore_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "agent-pids.json")
	s, err := NewFilePIDStore(path)
	if err != nil {
		t.Fatalf("NewFilePIDStore: %v", err)
	}
	if err := s.Record("a", 111); err != nil {
		t.Fatalf("Record a: %v", err)
	}
	if err := s.Record("b", 222); err != nil {
		t.Fatalf("Record b: %v", err)
	}
	// A fresh instance over the same file reads back both (durable across the "worker").
	s2, err := NewFilePIDStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	m, err := s2.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m["a"] != 111 || m["b"] != 222 {
		t.Fatalf("loaded %v, want a=111 b=222", m)
	}
	if err := s2.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	m, _ = s2.Load()
	if _, ok := m["a"]; ok || m["b"] != 222 {
		t.Fatalf("after remove %v, want only b=222", m)
	}
}

// TestFilePIDStore_MissingFileLoadsEmpty pins the corrupt/absent-file tolerance.
func TestFilePIDStore_MissingFileLoadsEmpty(t *testing.T) {
	s, err := NewFilePIDStore(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("NewFilePIDStore: %v", err)
	}
	m, err := s.Load()
	if err != nil || len(m) != 0 {
		t.Fatalf("Load absent = (%v, %v), want (empty, nil)", m, err)
	}
}

// TestLauncher_PersistsPIDOnEnsure_ClearsOnStop pins that the launcher records the
// launched pid (so a restart can find it) and clears it on an intentional stop.
func TestLauncher_PersistsPIDOnEnsure_ClearsOnStop(t *testing.T) {
	starter := newFakeStarter()
	store := newMemPIDStore()
	l, err := New(Config{Starter: starter, PIDs: store, After: immediateAfter})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := l.Ensure(AgentSpec{AgentID: "a"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	pid := starter.last("a").PID()
	if got, _ := store.Load(); got["a"] != pid {
		t.Fatalf("after Ensure store[a]=%d, want %d", got["a"], pid)
	}
	if err := l.Stop("a"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if store.has("a") {
		t.Fatal("Stop must clear the recorded pid (not a survivor to re-adopt)")
	}
}

// TestLauncher_AdoptSupervisesSurvivor pins the T860 gap5 adopt path against a REAL live
// child (never the test process): Adopt takes it over without spawning, records its pid,
// and a later Stop terminates it via signals (not os.Wait — it's not our child).
func TestLauncher_AdoptSupervisesSurvivor(t *testing.T) {
	// A real, controllable "survivor": its own process group (Setpgid) so group signals
	// target only it — never the test runner's group.
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep survivor: %v", err)
	}
	pid := cmd.Process.Pid
	// Reap CONCURRENTLY: this child would otherwise zombie after we signal it (kill -0
	// still sees a zombie → PIDAlive would never flip). In production the survivor is NOT
	// the adopter's child, so its real parent reaps it — no zombie. The reaper models that.
	reaped := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(reaped) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-reaped })

	starter := newFakeStarter()
	store := newMemPIDStore()
	l, err := New(Config{Starter: starter, PIDs: store, AdoptPoll: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := l.Adopt(AgentSpec{AgentID: "a"}, pid); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if run := l.Running(); len(run) != 1 || run[0] != "a" {
		t.Fatalf("Running = %v, want [a]", run)
	}
	if starter.count("a") != 0 {
		t.Fatalf("Adopt must NOT spawn (Start count=%d, want 0)", starter.count("a"))
	}
	if got, _ := store.Load(); got["a"] != pid {
		t.Fatalf("Adopt must record the survivor pid: store[a]=%d, want %d", got["a"], pid)
	}

	// Stop the adopted unit → it signals the survivor's group; the sleep dies, and the
	// launcher does NOT rebuild (intentional stop) or respawn.
	if err := l.Stop("a"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-reaped // survivor terminated + reaped by the concurrent reaper
	if starter.count("a") != 0 {
		t.Fatalf("Stop of an adopted unit must not respawn (Start count=%d)", starter.count("a"))
	}
	if store.has("a") {
		t.Fatal("Stop must clear the adopted pid")
	}
}

// TestPIDAlive pins the fast pre-filter: the running test process is alive; an
// implausible pid is not.
func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Error("PIDAlive(self) must be true")
	}
	if PIDAlive(0x7fffffff) {
		t.Error("PIDAlive(implausible pid) must be false")
	}
	if PIDAlive(0) || PIDAlive(-1) {
		t.Error("PIDAlive(non-positive) must be false")
	}
}

// TestAdoptablePIDs_NoStore pins the no-store default (every restart respawns).
func TestAdoptablePIDs_NoStore(t *testing.T) {
	l, err := New(Config{Starter: newFakeStarter()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := l.AdoptablePIDs(); len(got) != 0 {
		t.Fatalf("AdoptablePIDs with no store = %v, want empty", got)
	}
	if err := l.Adopt(AgentSpec{AgentID: ""}, 5); err == nil {
		t.Error("Adopt with empty agent_id must error")
	}
	if err := l.Adopt(AgentSpec{AgentID: "a"}, 0); err == nil {
		t.Error("Adopt with non-positive pid must error")
	}
}
