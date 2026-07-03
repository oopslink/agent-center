package agentlauncher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sleepScript writes an executable that ignores its args and sleeps, so we can
// exercise the REAL os/exec Start + group-signal path without an agent-runtime binary.
func sleepScript(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sleepy.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return p
}

func TestExecStarter_StartAndSignal(t *testing.T) {
	s, err := NewExecStarter(ExecStarterConfig{
		BinaryPath: sleepScript(t),
		Stdout:     os.NewFile(0, os.DevNull),
		Stderr:     os.NewFile(0, os.DevNull),
	})
	if err != nil {
		t.Fatalf("NewExecStarter: %v", err)
	}
	proc, err := s.Start(context.Background(), AgentSpec{AgentID: "a"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if proc.PID() <= 0 {
		t.Fatalf("PID = %d, want > 0", proc.PID())
	}
	// Signal the group → the sleeper exits → Wait returns promptly.
	done := make(chan struct{})
	go func() { _ = proc.Wait(); close(done) }()
	if err := proc.Signal(); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = proc.Kill()
		t.Fatal("process did not exit after Signal")
	}
}

func TestExecStarter_RequiresAgentID(t *testing.T) {
	s, err := NewExecStarter(ExecStarterConfig{BinaryPath: "/bin/sh"})
	if err != nil {
		t.Fatalf("NewExecStarter: %v", err)
	}
	if _, err := s.Start(context.Background(), AgentSpec{}); err == nil {
		t.Error("empty agent_id must error")
	}
}

func TestNewExecStarter_DefaultsBinaryToSelf(t *testing.T) {
	s, err := NewExecStarter(ExecStarterConfig{})
	if err != nil {
		t.Fatalf("NewExecStarter: %v", err)
	}
	self, _ := os.Executable()
	if s.binaryPath != self || s.subcommand != "agent-runtime" {
		t.Errorf("defaults wrong: binary=%q sub=%q", s.binaryPath, s.subcommand)
	}
}
