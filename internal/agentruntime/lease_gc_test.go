package agentruntime

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

// revokingReporter returns ErrLeaseRevoked for a designated set of task ids (blocked/
// reassigned/terminal on the center) and renews the rest cleanly.
type revokingReporter struct {
	nopReporter
	mu      sync.Mutex
	renewed []string
	revoke  map[string]bool
}

func (r *revokingReporter) RenewTaskLease(_ context.Context, _, taskID string, _ time.Time) error {
	r.mu.Lock()
	r.renewed = append(r.renewed, taskID)
	revoked := r.revoke[taskID]
	r.mu.Unlock()
	if revoked {
		return fmt.Errorf("%w: blocked", ErrLeaseRevoked)
	}
	return nil
}

// recordingReporter records RenewTaskLease calls (embeds nopReporter for the rest).
type recordingReporter struct {
	nopReporter
	mu      sync.Mutex
	renewed []string
}

func (r *recordingReporter) RenewTaskLease(_ context.Context, _, taskID string, _ time.Time) error {
	r.mu.Lock()
	r.renewed = append(r.renewed, taskID)
	r.mu.Unlock()
	return nil
}

func (r *recordingReporter) count(taskID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, t := range r.renewed {
		if t == taskID {
			n++
		}
	}
	return n
}

// seedExecutorInput provisions an executor dir with an input.json carrying taskRef, so
// SnapshotConcurrency surfaces it with that TaskID.
func seedExecutorInput(t *testing.T, home, execID, taskRef string) {
	t.Helper()
	layout, err := executor.NewLayout(home)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := executor.NewFileExchange(layout, nil)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	if _, err := fx.Provision(execID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := fx.WriteInput(executor.Input{
		ExecutorID: execID, Goal: executor.Goal{Title: "t"}, Model: "m",
		Source: executor.SourceRefs{TaskRef: taskRef}, CreatedAt: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
}

// TestTick_RenewsAllInflightTasks is the T860 piece ③ guard: Tick renews EVERY in-flight
// task — the supervisor's current task AND each live executor's task — deduped and
// rate-limited to the renew cadence. The old daemon renewer only covered the supervisor
// task; this must cover the executors too (else a concurrent agent's long executor tasks
// lapse).
func TestTick_RenewsAllInflightTasks(t *testing.T) {
	base := t.TempDir()
	agentID := "agent-lease"
	rt := newExecRuntime(t, base, agentID, lookTrue(t))
	rep := &recordingReporter{}
	rt.cfg.Reporter = rep

	// Supervisor's current task.
	rt.withState(func(s *SessionState) {
		s.Session = &fakeSession{}
		s.CurrentTaskID = "task-super"
	})

	// Two live executors: one on task-exec, one that ALSO runs task-super (dedup case).
	if err := rt.AttachExecutorEngine(ExecutorConfig{AgentID: agentID, MaxConcurrentTasks: 3, DefaultExecutorModel: "m"}); err != nil {
		t.Fatalf("AttachExecutorEngine: %v", err)
	}
	home := agentHomeOf(t, base, agentID)
	seedExecutorInput(t, home, "exec-1", "task-exec")
	seedExecutorInput(t, home, "exec-2", "task-super") // dup with the supervisor task
	rt.exec.addOrphan("exec-1", os.Getpid())
	rt.exec.addOrphan("exec-2", os.Getpid())

	now := time.Unix(1700000000, 0)
	if err := rt.Tick(context.Background(), now); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// Both distinct in-flight tasks renewed exactly once (task-super deduped across the
	// supervisor + exec-2).
	if got := rep.count("task-super"); got != 1 {
		t.Errorf("task-super renewed %d times, want 1 (deduped)", got)
	}
	if got := rep.count("task-exec"); got != 1 {
		t.Errorf("task-exec (an EXECUTOR task) renewed %d times, want 1 — the old renewer missed executor tasks", got)
	}

	// Rate-limited: a Tick within the cadence does not renew again.
	if err := rt.Tick(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	if got := rep.count("task-super"); got != 1 {
		t.Errorf("renew must be rate-limited within the cadence, got %d renews", got)
	}

	// Past the cadence: renews again.
	if err := rt.Tick(context.Background(), now.Add(2*DefaultLeaseRenewEvery)); err != nil {
		t.Fatalf("Tick 3: %v", err)
	}
	if got := rep.count("task-exec"); got != 2 {
		t.Errorf("past the cadence the executor task must be renewed again, got %d", got)
	}
}

// TestDrainLeaseRenewals_RevokedExecutorFused (issue-88e32d98 P0 block-fuse): when the
// center revokes a lease (ErrLeaseRevoked) for a task that maps to a LIVE EXECUTOR, the
// sweep circuit-breaks that executor (fuse). A revoked SUPERVISOR task must NOT be fused
// — killing the session would误杀 the agent. The fuse is exercised through the seam so no
// real process is signalled.
func TestDrainLeaseRenewals_RevokedExecutorFused(t *testing.T) {
	base := t.TempDir()
	agentID := "agent-fuse"
	rt := newExecRuntime(t, base, agentID, lookTrue(t))
	rep := &revokingReporter{revoke: map[string]bool{"task-exec": true, "task-super": true}}
	rt.cfg.Reporter = rep

	// Record fuse calls via the seam (never signal a real pid in a unit test).
	var mu sync.Mutex
	var fused []string
	rt.fuseExecutor = func(_ context.Context, taskID string) (bool, error) {
		mu.Lock()
		fused = append(fused, taskID)
		mu.Unlock()
		return true, nil
	}

	// Supervisor's current task (revoked, but supervisor-only → must NOT be fused).
	rt.withState(func(s *SessionState) {
		s.Session = &fakeSession{}
		s.CurrentTaskID = "task-super"
	})

	// One live executor on task-exec (revoked → MUST be fused).
	if err := rt.AttachExecutorEngine(ExecutorConfig{AgentID: agentID, MaxConcurrentTasks: 3, DefaultExecutorModel: "m"}); err != nil {
		t.Fatalf("AttachExecutorEngine: %v", err)
	}
	home := agentHomeOf(t, base, agentID)
	seedExecutorInput(t, home, "exec-1", "task-exec")
	rt.exec.addOrphan("exec-1", os.Getpid())

	rt.drainLeaseRenewals(context.Background(), time.Unix(1700000000, 0))

	mu.Lock()
	defer mu.Unlock()
	if len(fused) != 1 || fused[0] != "task-exec" {
		t.Fatalf("fused = %v, want exactly [task-exec] (executor fused, supervisor task NOT fused)", fused)
	}
}

// TestDrainLeaseRenewals_NoSessionNoSuperTask: with no live session, the supervisor task
// is not renewed (only executor tasks would be).
func TestDrainLeaseRenewals_SkipsWhenStopping(t *testing.T) {
	base := t.TempDir()
	rt := newExecRuntime(t, base, "agent-x", lookTrue(t))
	rep := &recordingReporter{}
	rt.cfg.Reporter = rep
	rt.withState(func(s *SessionState) {
		s.Session = &fakeSession{}
		s.CurrentTaskID = "task-super"
		s.ExpectedStop = true // intentional stop → its lease should lapse, not renew
	})

	rt.drainLeaseRenewals(context.Background(), time.Unix(1700000000, 0))
	if got := rep.count("task-super"); got != 0 {
		t.Errorf("an intentionally-stopping session's task must NOT be renewed, got %d", got)
	}
}
