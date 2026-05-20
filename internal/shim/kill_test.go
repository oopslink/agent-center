package shim

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

type fakeController struct {
	mu            sync.Mutex
	termCalled    bool
	killCalled    bool
	waitNeverExit bool
	failTerm      bool
	failKill      bool
	gracePings    int
}

func (f *fakeController) SignalTerm(_ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.termCalled = true
	if f.failTerm {
		return errors.New("term fail")
	}
	return nil
}

func (f *fakeController) SignalKill(_ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killCalled = true
	if f.failKill {
		return errors.New("kill fail")
	}
	return nil
}

func (f *fakeController) WaitExited(ctx context.Context, _ int) error {
	if f.waitNeverExit {
		<-ctx.Done()
		return ctx.Err()
	}
	// Wait a tick then "exit"
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
		return nil
	}
}

func TestPerformKill_GracefulOnSIGTERM(t *testing.T) {
	pc := &fakeController{}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	graceful, err := PerformKill(context.Background(), pc, clk, KillRequest{PID: 100, GraceTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !graceful {
		t.Fatal("expected graceful")
	}
	if pc.killCalled {
		t.Fatal("SIGKILL must not be called when SIGTERM succeeds")
	}
}

func TestPerformKill_EscalateToSIGKILL(t *testing.T) {
	pc := &fakeController{waitNeverExit: true}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Advance virtual time so the deadline triggers fast.
		time.Sleep(30 * time.Millisecond)
		clk.Advance(10 * time.Second)
		time.Sleep(30 * time.Millisecond)
		clk.Advance(10 * time.Second)
		cancel()
	}()
	graceful, err := PerformKill(ctx, pc, clk, KillRequest{PID: 100, GraceTimeout: 5 * time.Second})
	if err != nil && !errors.Is(err, context.Canceled) && err.Error() != "shim/kill: process did not exit after SIGKILL" {
		t.Fatalf("err: %v", err)
	}
	if graceful {
		t.Fatal("expected non-graceful")
	}
	if !pc.killCalled {
		t.Fatal("expected SIGKILL called")
	}
}

func TestPerformKill_TermFails_Graceful(t *testing.T) {
	pc := &fakeController{failTerm: true}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	graceful, err := PerformKill(context.Background(), pc, clk, KillRequest{PID: 100, GraceTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !graceful {
		t.Fatal("expected graceful (term fail = process gone)")
	}
}

func TestPerformKill_DefaultGraceFilled(t *testing.T) {
	pc := &fakeController{}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	_, err := PerformKill(context.Background(), pc, clk, KillRequest{PID: 100})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPerformKill_NilController(t *testing.T) {
	if _, err := PerformKill(context.Background(), nil, clock.NewFakeClock(time.Now()), KillRequest{PID: 1}); err == nil {
		t.Fatal("expected nil error")
	}
}

func TestOSProcessController_InvalidPID(t *testing.T) {
	c := OSProcessController{}
	if err := c.SignalTerm(0); err == nil {
		t.Fatal("expected invalid pid")
	}
	if err := c.SignalKill(-1); err == nil {
		t.Fatal("expected invalid pid")
	}
	if err := c.WaitExited(context.Background(), 0); err == nil {
		t.Fatal("expected invalid pid")
	}
}
