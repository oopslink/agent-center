package scheduler_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/scheduler"
)

// writeShellScript drops a shell script body into a temp file and returns
// its path.
func writeShellScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecProcessRunner_HappyExit(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	runner := scheduler.NewExecProcessRunner()
	script := writeShellScript(t, "exit 0\n")
	gotExit := -1
	done := make(chan struct{})
	h, err := runner.Start(context.Background(), scheduler.ProcessSpec{
		Binary: script,
	}, func(exitCode int, _ error, _ string) {
		gotExit = exitCode
		close(done)
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if h.PID() <= 0 {
		t.Errorf("PID = %d", h.PID())
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
	if gotExit != 0 {
		t.Errorf("exit = %d", gotExit)
	}
	<-h.Done()
}

func TestExecProcessRunner_NonZeroExit(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	runner := scheduler.NewExecProcessRunner()
	script := writeShellScript(t, "exit 3\n")
	gotExit := -1
	done := make(chan struct{})
	_, err := runner.Start(context.Background(), scheduler.ProcessSpec{
		Binary: script,
	}, func(exitCode int, _ error, _ string) {
		gotExit = exitCode
		close(done)
	})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if gotExit != 3 {
		t.Errorf("exit = %d, want 3", gotExit)
	}
}

func TestExecProcessRunner_SignalAndKill(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	runner := scheduler.NewExecProcessRunner()
	script := writeShellScript(t, "sleep 10\n")
	done := make(chan struct{})
	h, err := runner.Start(context.Background(), scheduler.ProcessSpec{
		Binary: script,
	}, func(_ int, _ error, _ string) {
		close(done)
	})
	if err != nil {
		t.Fatal(err)
	}
	// signal SIGTERM
	if err := h.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("signal: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// fall back to Kill
		_ = h.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("process did not die")
		}
	}
}

// TestExecProcessRunner_KillRunningProcess deterministically exercises
// execProcessHandle.Kill (spawner.go:144-149) by killing a long-running
// process directly, without relying on shell signal handling semantics.
func TestExecProcessRunner_KillRunningProcess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	runner := scheduler.NewExecProcessRunner()
	// Long sleep — Kill is the only way out.
	script := writeShellScript(t, "sleep 30\n")
	done := make(chan struct{})
	h, err := runner.Start(context.Background(), scheduler.ProcessSpec{
		Binary: script,
	}, func(_ int, _ error, _ string) {
		close(done)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Confirm running.
	if h.PID() <= 0 {
		t.Fatalf("PID = %d", h.PID())
	}
	if err := h.Kill(); err != nil {
		t.Errorf("kill: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not die after Kill")
	}
}

func TestExecProcessRunner_BinaryNotFound(t *testing.T) {
	runner := scheduler.NewExecProcessRunner()
	_, err := runner.Start(context.Background(), scheduler.ProcessSpec{
		Binary: "/nope/this/does/not/exist",
	}, nil)
	if err == nil {
		t.Error("expected start failure for nonexistent binary")
	}
}
