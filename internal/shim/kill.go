package shim

import (
	"context"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// ProcessController abstracts SIGTERM / SIGKILL / wait for testability.
type ProcessController interface {
	SignalTerm(pid int) error
	SignalKill(pid int) error
	WaitExited(ctx context.Context, pid int) error // returns nil when process gone, ctx error otherwise
}

// KillRequest captures the parameters for the kill helper.
type KillRequest struct {
	PID          int
	GraceTimeout time.Duration // SIGTERM → wait → SIGKILL
}

// PerformKill runs the SIGTERM → grace → SIGKILL sequence. Returns
// (terminatedGracefully bool, err).
func PerformKill(ctx context.Context, pc ProcessController, clk clock.Clock, req KillRequest) (bool, error) {
	if pc == nil {
		return false, errors.New("shim/kill: nil process controller")
	}
	if req.GraceTimeout <= 0 {
		req.GraceTimeout = 5 * time.Second
	}
	if err := pc.SignalTerm(req.PID); err != nil {
		// SIGTERM failed (e.g. process already gone) → treat as graceful.
		return true, nil
	}
	graceCtx, cancel := contextWithDeadline(ctx, clk, req.GraceTimeout)
	defer cancel()
	if err := pc.WaitExited(graceCtx, req.PID); err == nil {
		return true, nil
	}
	if err := pc.SignalKill(req.PID); err != nil {
		return false, nil
	}
	// hard wait after SIGKILL — bounded by another grace window.
	hardCtx, cancel2 := contextWithDeadline(ctx, clk, req.GraceTimeout)
	defer cancel2()
	if err := pc.WaitExited(hardCtx, req.PID); err == nil {
		return false, nil
	}
	return false, errors.New("shim/kill: process did not exit after SIGKILL")
}

// contextWithDeadline wraps the parent ctx with a deadline driven by clk
// (so tests with FakeClock can advance virtual time). We don't use the
// stdlib deadline because that uses real time.
func contextWithDeadline(parent context.Context, clk clock.Clock, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	deadline := clk.Now().Add(d)
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !clk.Now().Before(deadline) {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() {
		close(stop)
		cancel()
	}
}
