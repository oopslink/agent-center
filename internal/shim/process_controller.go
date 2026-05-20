package shim

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// OSProcessController is the production ProcessController using
// syscall.Kill + polling.
type OSProcessController struct{}

// SignalTerm sends SIGTERM.
func (OSProcessController) SignalTerm(pid int) error {
	if pid <= 0 {
		return errors.New("shim/process: invalid pid")
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// SignalKill sends SIGKILL.
func (OSProcessController) SignalKill(pid int) error {
	if pid <= 0 {
		return errors.New("shim/process: invalid pid")
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}

// WaitExited polls every 100ms via signal-0 probe until the process is
// gone or ctx is canceled.
func (OSProcessController) WaitExited(ctx context.Context, pid int) error {
	if pid <= 0 {
		return errors.New("shim/process: invalid pid")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := syscall.Kill(pid, 0); err != nil {
			// process is gone
			return nil
		}
		_ = os.ErrInvalid
		time.Sleep(50 * time.Millisecond)
	}
}
