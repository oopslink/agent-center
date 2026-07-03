package agentlauncher

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeProc is a controllable Process: Wait blocks until exit is signalled (by the
// test, via die(), or by Signal/Kill).
type fakeProc struct {
	pid    int
	mu     sync.Mutex
	exitCh chan struct{}
	closed bool
	killed bool
	siged  bool
}

func newFakeProc(pid int) *fakeProc { return &fakeProc{pid: pid, exitCh: make(chan struct{})} }

func (p *fakeProc) Wait() error { <-p.exitCh; return nil }
func (p *fakeProc) PID() int    { return p.pid }
func (p *fakeProc) die() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.exitCh)
	}
}
func (p *fakeProc) Signal() error {
	p.mu.Lock()
	p.siged = true
	p.mu.Unlock()
	p.die() // a well-behaved process exits on signal
	return nil
}
func (p *fakeProc) Kill() error {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	p.die()
	return nil
}

// fakeStarter hands out a fresh fakeProc per Start and records them.
type fakeStarter struct {
	mu    sync.Mutex
	procs map[string][]*fakeProc
	seq   int
	errOn map[string]error // agentID → error to return on the NEXT Start
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{procs: map[string][]*fakeProc{}, errOn: map[string]error{}}
}

func (s *fakeStarter) Start(_ context.Context, spec AgentSpec) (Process, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.errOn[spec.AgentID]; err != nil {
		delete(s.errOn, spec.AgentID)
		return nil, err
	}
	s.seq++
	p := newFakeProc(1000 + s.seq)
	s.procs[spec.AgentID] = append(s.procs[spec.AgentID], p)
	return p, nil
}

func (s *fakeStarter) count(agentID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.procs[agentID])
}

func (s *fakeStarter) last(agentID string) *fakeProc {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := s.procs[agentID]
	if len(ps) == 0 {
		return nil
	}
	return ps[len(ps)-1]
}

// immediateAfter fires the delay channel at once, so backoff never real-sleeps.
func immediateAfter(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func newTestLauncher(t *testing.T, s *fakeStarter) *LocalProcessLauncher {
	t.Helper()
	l, err := New(Config{Starter: s, After: immediateAfter, StopGrace: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

// waitFor polls cond up to 2s (real time) so we don't sleep-race on goroutines.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

func TestEnsure_LaunchesOnce(t *testing.T) {
	s := newFakeStarter()
	l := newTestLauncher(t, s)
	if err := l.Ensure(AgentSpec{AgentID: "a"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// A second Ensure while up is a no-op (idempotent).
	if err := l.Ensure(AgentSpec{AgentID: "a"}); err != nil {
		t.Fatalf("Ensure 2: %v", err)
	}
	if s.count("a") != 1 {
		t.Errorf("started %d times, want 1", s.count("a"))
	}
	if got := l.Running(); len(got) != 1 || got[0] != "a" {
		t.Errorf("Running = %v, want [a]", got)
	}
}

func TestEnsure_RebuildsOnCrash(t *testing.T) {
	s := newFakeStarter()
	l := newTestLauncher(t, s)
	if err := l.Ensure(AgentSpec{AgentID: "a"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// Crash the process → the supervisor must rebuild it.
	s.last("a").die()
	waitFor(t, func() bool { return s.count("a") == 2 }, "rebuild after crash")
	// Crash again → rebuild again (backoff is immediate in the test).
	s.last("a").die()
	waitFor(t, func() bool { return s.count("a") == 3 }, "second rebuild")
	if got := l.Running(); len(got) != 1 || got[0] != "a" {
		t.Errorf("Running = %v, want [a] after rebuilds", got)
	}
}

func TestStop_TerminatesAndNoRebuild(t *testing.T) {
	s := newFakeStarter()
	l := newTestLauncher(t, s)
	_ = l.Ensure(AgentSpec{AgentID: "a"})
	first := s.last("a")

	if err := l.Stop("a"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !first.siged && !first.killed {
		t.Error("Stop must signal/kill the process")
	}
	if got := l.Running(); len(got) != 0 {
		t.Errorf("Running = %v, want [] after Stop", got)
	}
	// Give any (erroneous) rebuild a chance — there must be none.
	time.Sleep(20 * time.Millisecond)
	if s.count("a") != 1 {
		t.Errorf("Stop must not rebuild: started %d times", s.count("a"))
	}
	// Stop is idempotent.
	if err := l.Stop("a"); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	if err := l.Stop("never"); err != nil {
		t.Errorf("Stop unknown: %v", err)
	}
}

func TestShutdown_StopsAll(t *testing.T) {
	s := newFakeStarter()
	l := newTestLauncher(t, s)
	_ = l.Ensure(AgentSpec{AgentID: "a"})
	_ = l.Ensure(AgentSpec{AgentID: "b"})
	waitFor(t, func() bool { return len(l.Running()) == 2 }, "both up")

	if err := l.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := l.Running(); len(got) != 0 {
		t.Errorf("Running = %v, want [] after Shutdown", got)
	}
}

func TestEnsure_SpawnErrorSurfaces(t *testing.T) {
	s := newFakeStarter()
	s.errOn["a"] = context.DeadlineExceeded
	l := newTestLauncher(t, s)
	if err := l.Ensure(AgentSpec{AgentID: "a"}); err == nil {
		t.Error("Ensure must surface the initial spawn error")
	}
	if len(l.Running()) != 0 {
		t.Error("a failed initial spawn must not leave a running unit")
	}
}

func TestEnsure_RequiresAgentID(t *testing.T) {
	l := newTestLauncher(t, newFakeStarter())
	if err := l.Ensure(AgentSpec{}); err == nil {
		t.Error("empty agent_id must error")
	}
}

func TestBackoff_DelayGrowsAndCaps(t *testing.T) {
	b := BackoffParams{Base: time.Second, Max: 8 * time.Second}.normalized()
	got := []time.Duration{b.delayFor(1), b.delayFor(2), b.delayFor(3), b.delayFor(4), b.delayFor(9)}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("delayFor: got %v, want %v", got, want)
			break
		}
	}
}
