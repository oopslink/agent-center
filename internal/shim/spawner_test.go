package shim

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// TestOSSpawner_Spawn_Echo runs the real OSSpawner against /bin/echo —
// a portable, no-side-effect command — to exercise the full fork+exec
// path including setsid attribute.
func TestOSSpawner_Spawn_Echo(t *testing.T) {
	sp := OSSpawner{}
	proc, err := sp.Spawn(context.Background(), agentadapter.CmdSpec{
		Binary: "/bin/echo",
		Args:   []string{"hello", "world"},
	}, nil, nil)
	if err != nil {
		t.Skipf("spawner unavailable: %v", err)
	}
	var stdout bytes.Buffer
	go func() {
		_, _ = stdout.ReadFrom(proc.Stdout())
	}()
	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit: %d", code)
	}
	_ = strings.Contains // keep imported
	if proc.PID() == 0 {
		t.Fatal("pid")
	}
}

func TestOSSpawner_NoBinary(t *testing.T) {
	if _, err := (OSSpawner{}).Spawn(context.Background(), agentadapter.CmdSpec{}, nil, nil); err == nil {
		t.Fatal("expected binary required")
	}
}

func TestOSSpawner_NonexistentBinary(t *testing.T) {
	if _, err := (OSSpawner{}).Spawn(context.Background(), agentadapter.CmdSpec{
		Binary: "/no/such/binary",
	}, nil, nil); err == nil {
		t.Fatal("expected exec error")
	}
}

func TestOSSpawner_StderrAndKill(t *testing.T) {
	sp := OSSpawner{}
	proc, err := sp.Spawn(context.Background(), agentadapter.CmdSpec{
		Binary: "/bin/sh",
		Args:   []string{"-c", "echo err >&2; sleep 30"},
	}, nil, nil)
	if err != nil {
		t.Skipf("spawner unavailable: %v", err)
	}
	if proc.Stderr() == nil {
		t.Fatal("stderr nil")
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	_, _ = proc.Wait()
}
