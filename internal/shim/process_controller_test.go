package shim

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestOSProcessController_WaitExited_AlreadyGone(t *testing.T) {
	// Spawn sleep then SIGKILL it; verify WaitExited returns nil.
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep not available: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	pc := OSProcessController{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pc.WaitExited(ctx, pid); err != nil {
		t.Fatalf("expected nil for gone process: %v", err)
	}
}

func TestOSProcessController_WaitExited_ContextCancel(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep not available: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	pc := OSProcessController{}
	if err := pc.WaitExited(ctx, cmd.Process.Pid); err == nil {
		t.Fatal("expected context cancel error")
	}
}

func TestOSProcessController_TermAndKill(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep not available: %v", err)
	}
	pid := cmd.Process.Pid
	pc := OSProcessController{}
	if err := pc.SignalTerm(pid); err != nil {
		t.Fatal(err)
	}
	// Wait a bit for sleep to receive SIGTERM (it ignores it on macOS;
	// follow with SIGKILL).
	time.Sleep(50 * time.Millisecond)
	if err := pc.SignalKill(pid); err != nil {
		// Process may already be gone — that's fine.
		t.Logf("kill: %v", err)
	}
	_, _ = cmd.Process.Wait()
}
