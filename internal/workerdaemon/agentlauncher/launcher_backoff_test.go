package agentlauncher

import (
	"context"
	"sync"
	"testing"
	"time"
)

// newDeadProc returns a fakeProc whose Wait() returns immediately (an instant crash).
func newDeadProc(pid int) *fakeProc {
	p := newFakeProc(pid)
	p.closed = true
	close(p.exitCh)
	return p
}

// crashyStarter returns instant-crash procs for the first dieN starts, then live procs.
type crashyStarter struct {
	mu      sync.Mutex
	dieN    int
	started int
}

func (s *crashyStarter) Start(_ context.Context, _ AgentSpec) (Process, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started++
	if s.started <= s.dieN {
		return newDeadProc(1000 + s.started), nil
	}
	return newFakeProc(1000 + s.started), nil
}
func (s *crashyStarter) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.started }

// fixedNow keeps uptime at 0 → no stable-run reset (crashes accumulate).
func fixedNow() time.Time { return time.Unix(0, 0) }

// advancingNow adds a big step per call → every run reads as "stable" (uptime ≥ ResetAfter),
// so the crash counter resets on every crash.
func advancingNow() func() time.Time {
	var mu sync.Mutex
	var n int64
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		n++
		return time.Unix(n*3600, 0) // +1h per call
	}
}

func TestSupervise_MaxAttemptsExhausted_StopsAndReports(t *testing.T) {
	s := &crashyStarter{dieN: 1 << 30} // always crash
	var mu sync.Mutex
	var exhausted []string
	l, err := New(Config{
		Starter:     s,
		After:       immediateAfter,
		Now:         fixedNow, // uptime 0 → never resets → crashes accumulate to the cap
		MaxAttempts: 3,
		OnExhausted: func(id string, _ error) { mu.Lock(); exhausted = append(exhausted, id); mu.Unlock() },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := l.Ensure(AgentSpec{AgentID: "a"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(exhausted) == 1 }, "OnExhausted called")

	if got := l.Running(); len(got) != 0 {
		t.Errorf("a poison crash-loop must stop being rebuilt, Running = %v", got)
	}
	// Starts: initial + rebuilds until crash #(MaxAttempts+1) → started == MaxAttempts+1.
	if s.count() != 4 {
		t.Errorf("started %d times, want MaxAttempts+1 (=4) before giving up", s.count())
	}
}

func TestSupervise_StableRunResetsCounter(t *testing.T) {
	// 5 instant crashes then a live proc; MaxAttempts=2. Without the stable-run reset,
	// crash #3 would exhaust. With advancingNow (every run reads stable), the counter
	// resets each crash → never exhausts → the agent recovers and stays up.
	s := &crashyStarter{dieN: 5}
	var mu sync.Mutex
	var exhausted []string
	l, err := New(Config{
		Starter:     s,
		After:       immediateAfter,
		Now:         advancingNow(),
		MaxAttempts: 2,
		OnExhausted: func(id string, _ error) { mu.Lock(); exhausted = append(exhausted, id); mu.Unlock() },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := l.Ensure(AgentSpec{AgentID: "a"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// The 6th proc is live → the agent ends up running.
	waitFor(t, func() bool { return s.count() >= 6 }, "recovered past 5 stable crashes")
	waitFor(t, func() bool { return len(l.Running()) == 1 }, "agent stays up after reset")

	mu.Lock()
	defer mu.Unlock()
	if len(exhausted) != 0 {
		t.Errorf("a stable-run reset must NOT declare the agent poison, exhausted=%v", exhausted)
	}
}
