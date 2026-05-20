package shim

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ProcessStartTimer reads a process's start_time for PID-reuse defence
// (ADR-0018 § 6). Implementations are platform-specific:
//   - linux:  /proc/<pid>/stat field 22
//   - darwin: `ps -o lstart= -p <pid>`
//
// The zero start_time is treated as "process not found" so callers can
// fence-and-forget.
type ProcessStartTimer interface {
	GetStartTime(pid int) (time.Time, error)
}

// VerifyToken returns an error when the provided token doesn't match the
// expected value. Constant-time-ish compare via string equality (we accept
// the timing side-channel since shim_token rotates per execution).
func VerifyToken(expected, got string) error {
	if expected == "" || got == "" {
		return errors.New("shim/fencing: token required")
	}
	if expected != got {
		return errors.New("shim/fencing: token mismatch")
	}
	return nil
}

// VerifyPIDStartTime compares the recorded start_time to the live start
// time read via ProcessStartTimer. Returns nil when they match (within 1
// second tolerance), an error otherwise.
func VerifyPIDStartTime(timer ProcessStartTimer, pid int, expected time.Time) error {
	if timer == nil {
		return errors.New("shim/fencing: nil timer")
	}
	got, err := timer.GetStartTime(pid)
	if err != nil {
		return fmt.Errorf("shim/fencing: read start_time: %w", err)
	}
	if got.IsZero() {
		return errors.New("shim/fencing: process not found / start_time unknown")
	}
	if abs(got.Sub(expected)) > time.Second {
		return fmt.Errorf("shim/fencing: start_time mismatch (got %s expected %s — PID may have been reused)", got.UTC(), expected.UTC())
	}
	return nil
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// OSStartTimer is the default platform-aware ProcessStartTimer.
type OSStartTimer struct{}

// GetStartTime reads the process start_time using the OS-native method.
// Unimplemented platforms return ErrUnsupportedPlatform.
func (OSStartTimer) GetStartTime(pid int) (time.Time, error) {
	switch runtime.GOOS {
	case "darwin":
		// `ps -o lstart= -p <pid>` returns e.g. "Sat May 21 12:00:00 2026"
		out, err := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprintf("%d", pid)).Output()
		if err != nil {
			return time.Time{}, err
		}
		s := strings.TrimSpace(string(out))
		if s == "" {
			return time.Time{}, nil
		}
		t, err := time.Parse("Mon Jan _2 15:04:05 2006", s)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse lstart %q: %w", s, err)
		}
		return t, nil
	default:
		// linux + others fall back to ps for portability (small overhead
		// per call but Phase 2 traffic is low).
		out, err := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprintf("%d", pid)).Output()
		if err != nil {
			return time.Time{}, err
		}
		s := strings.TrimSpace(string(out))
		if s == "" {
			return time.Time{}, nil
		}
		t, err := time.Parse("Mon Jan _2 15:04:05 2006", s)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse ps lstart %q: %w", s, err)
		}
		return t, nil
	}
}

// ErrUnsupportedPlatform is returned when start_time lookup isn't
// implemented for the running OS.
var ErrUnsupportedPlatform = errors.New("shim/fencing: unsupported platform for PID start_time lookup")
