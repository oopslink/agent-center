package executor

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// poolWithTracker builds a Pool whose launches persist an orchestrator Record, so
// the W3 crash-recovery linkage (Launch → orchestrator.json → Reconciler probe) is
// covered end to end. Returns the pool, its Tracker, and the fixed pid the fake
// spawner assigns.
func poolWithTracker(t *testing.T, spawnErr error) (*Pool, *Tracker, int) {
	t.Helper()
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	tr, err := NewTracker(layout)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	const pid = 4321
	var killed sync.Once
	sp := &Spawner{
		start: func(cmd *exec.Cmd) error {
			if spawnErr != nil {
				return spawnErr
			}
			cmd.Process = &os.Process{Pid: pid}
			return nil
		},
		signal: func(int, syscall.Signal) error { killed.Do(func() {}); return nil },
	}
	pool, err := NewPool(PoolConfig{
		Exchange: fx, Spawner: sp, AgentRoot: root, Tracker: tr, Max: 2,
		// No worktrees → plain-dir workspace (the production W3 shape).
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	return pool, tr, pid
}

func validTrackedInput(id string) Input {
	return Input{
		ExecutorID: id,
		Goal:       Goal{Title: "t"},
		Model:      "m",
		CreatedAt:  time.Unix(1700000000, 0),
	}
}

// Launch persists a probe-able Record (pid + runner cmd) so a restarted
// orchestrator can re-adopt the executor (design §12).
func TestPool_Launch_WritesRecoveryRecord(t *testing.T) {
	pool, tr, pid := poolWithTracker(t, nil)
	id := "exec-trk111"
	if _, err := pool.Launch(context.Background(), LaunchSpec{
		Input:     validTrackedInput(id),
		RunnerCmd: []string{"claude", "-p", "go"},
	}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	rec, err := tr.Read(id)
	if err != nil {
		t.Fatalf("Read record: %v", err)
	}
	if rec.PID != pid {
		t.Errorf("record pid = %d, want %d", rec.PID, pid)
	}
	if len(rec.RunnerCmd) != 3 || rec.RunnerCmd[0] != "claude" {
		t.Errorf("record runner cmd = %v, want the launched argv", rec.RunnerCmd)
	}
	if rec.SpawnedAt.IsZero() {
		t.Error("record spawned_at must be stamped")
	}
}
