package executor

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// recordingSignaler records every (pid, sig) it is asked to deliver and can be
// told to return a canned error for the SIGKILL escalation.
type recordingSignaler struct {
	mu      sync.Mutex
	sigs    []syscall.Signal
	pids    []int
	killErr error
}

func (r *recordingSignaler) signal(pid int, sig syscall.Signal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pids = append(r.pids, pid)
	r.sigs = append(r.sigs, sig)
	if sig == syscall.SIGKILL {
		return r.killErr
	}
	return nil
}

func newSignalHandle(id string, pid int, sig groupSignaler) *Handle {
	return &Handle{ExecutorID: id, PID: pid, signal: sig}
}

func TestWatchdog_Check(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{StallTimeout: time.Minute})
	base := time.Unix(1700000000, 0)
	cases := []struct {
		name        string
		state       State
		lastProg    time.Time
		now         time.Time
		wantStalled bool
	}{
		{"running, fresh", StateRunning, base, base.Add(30 * time.Second), false},
		{"running, exactly at threshold", StateRunning, base, base.Add(time.Minute), false},
		{"running, stalled", StateRunning, base, base.Add(90 * time.Second), true},
		{"done is never stalled", StateDone, base, base.Add(time.Hour), false},
		{"failed is never stalled", StateFailed, base, base.Add(time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := Status{State: tc.state, LastProgressAt: tc.lastProg}
			v := w.Check(st, tc.now)
			if v.Stalled != tc.wantStalled {
				t.Errorf("Stalled = %v, want %v (idle=%s)", v.Stalled, tc.wantStalled, v.Idle)
			}
		})
	}
}

func TestNewWatchdog_Defaults(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{})
	if w.StallTimeout() != DefaultStallTimeout {
		t.Errorf("StallTimeout = %s, want default %s", w.StallTimeout(), DefaultStallTimeout)
	}
}

func TestWatchdog_GracefulKill_TermThenKill(t *testing.T) {
	rs := &recordingSignaler{}
	var slept time.Duration
	w := NewWatchdog(WatchdogConfig{
		GraceTimeout: 3 * time.Second,
		Clock:        clock.NewFakeClock(time.Unix(1700000000, 0)),
		Sleep:        func(d time.Duration) { slept = d },
	})
	h := newSignalHandle("e1", 4242, rs.signal)
	if err := w.GracefulKill(context.Background(), h); err != nil {
		t.Fatalf("GracefulKill: %v", err)
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.sigs) != 2 || rs.sigs[0] != syscall.SIGTERM || rs.sigs[1] != syscall.SIGKILL {
		t.Fatalf("signal sequence = %v, want [SIGTERM SIGKILL]", rs.sigs)
	}
	if rs.pids[0] != 4242 {
		t.Errorf("signalled pid = %d, want 4242", rs.pids[0])
	}
	if slept != 3*time.Second {
		t.Errorf("grace sleep = %s, want 3s", slept)
	}
}

func TestWatchdog_GracefulKill_ToleratesAlreadyGone(t *testing.T) {
	// SIGTERM returns ESRCH (process already exited): not an error.
	rs := &recordingSignaler{killErr: syscall.ESRCH}
	termGoneSig := func(pid int, sig syscall.Signal) error {
		if sig == syscall.SIGTERM {
			return syscall.ESRCH
		}
		return rs.signal(pid, sig)
	}
	w := NewWatchdog(WatchdogConfig{Sleep: func(time.Duration) {}})
	h := newSignalHandle("e1", 1, termGoneSig)
	if err := w.GracefulKill(context.Background(), h); err != nil {
		t.Fatalf("ESRCH should be tolerated, got %v", err)
	}
}

func TestWatchdog_GracefulKill_TermErrorSurfaces(t *testing.T) {
	boom := errors.New("EPERM-ish")
	sig := func(int, syscall.Signal) error { return boom }
	w := NewWatchdog(WatchdogConfig{Sleep: func(time.Duration) {}})
	h := newSignalHandle("e1", 1, sig)
	if err := w.GracefulKill(context.Background(), h); err == nil {
		t.Fatal("a non-ESRCH SIGTERM error must surface")
	}
}

func TestWatchdog_GracefulKill_KillErrorSurfaces(t *testing.T) {
	boom := errors.New("kill boom")
	sig := func(_ int, s syscall.Signal) error {
		if s == syscall.SIGKILL {
			return boom
		}
		return nil
	}
	w := NewWatchdog(WatchdogConfig{Sleep: func(time.Duration) {}})
	h := newSignalHandle("e1", 1, sig)
	if err := w.GracefulKill(context.Background(), h); err == nil {
		t.Fatal("a non-ESRCH SIGKILL error must surface")
	}
}

func TestWatchdog_GracefulKill_NilHandle(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{Sleep: func(time.Duration) {}})
	if err := w.GracefulKill(context.Background(), nil); err == nil {
		t.Fatal("nil handle must error")
	}
}

func TestWatchdog_GracefulKill_ContextCancelEndsGrace(t *testing.T) {
	rs := &recordingSignaler{}
	// A sleep that would block "forever" if not short-circuited by ctx.
	w := NewWatchdog(WatchdogConfig{Sleep: func(time.Duration) { time.Sleep(2 * time.Second) }})
	h := newSignalHandle("e1", 7, rs.signal)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled → grace returns immediately
	start := time.Now()
	if err := w.GracefulKill(ctx, h); err != nil {
		t.Fatalf("GracefulKill: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Error("cancelled context should short-circuit the grace window")
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.sigs) != 2 {
		t.Errorf("still expect SIGTERM then SIGKILL, got %v", rs.sigs)
	}
}
