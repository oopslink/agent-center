package executor

// prepared_workspace_test.go — P4/P5 unit coverage for the executor-package seams the
// repo-workspace track adds: (1) a LaunchSpec.Prepared skips worktree provisioning +
// persists the RepoKey/SourcePath teardown handle into the recovery Record; (2) the
// Monitor's WorktreeCleaner port is invoked on terminal Finalize with those args, and
// NOT for a plain-dir executor.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// recordingCleaner records RemoveWorktree calls (the narrow executor-package port).
type recordingCleaner struct {
	mu    sync.Mutex
	calls []struct{ repoKey, sourcePath, workspacePath string }
	err   error
}

func (c *recordingCleaner) RemoveWorktree(_ context.Context, repoKey, sourcePath, workspacePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, struct{ repoKey, sourcePath, workspacePath string }{repoKey, sourcePath, workspacePath})
	return c.err
}

func (c *recordingCleaner) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.calls) }

// TestPool_PreparedWorkspaceSkipsProvisionAndRecordsHandle proves a Prepared launch
// does NOT call the WorktreeProvisioner (gitErr would surface otherwise) and persists
// the RepoKey/SourcePath into the durable Record.
func TestPool_PreparedWorkspaceSkipsProvisionAndRecordsHandle(t *testing.T) {
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	tracker, err := NewTracker(layout)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	// A git runner that ALWAYS errors: if the Prepared path wrongly tried to provision a
	// worktree, Launch would fail. It must be skipped entirely.
	failGit := &fakeGitRunner{err: os.ErrPermission}
	wt, err := NewWorktreeProvisioner(root, failGit)
	if err != nil {
		t.Fatalf("NewWorktreeProvisioner: %v", err)
	}
	var pidSeq int
	var spawnedArgs []string
	sp := &Spawner{
		start: func(cmd *exec.Cmd) error {
			pidSeq++
			spawnedArgs = append([]string(nil), cmd.Args...)
			cmd.Process = &os.Process{Pid: 5000 + pidSeq}
			return nil
		},
		signal: func(int, syscall.Signal) error { return nil },
	}
	pool, err := NewPool(PoolConfig{
		Exchange: fx, Worktrees: wt, Spawner: sp, AgentRoot: root, BaseRef: "main",
		BinaryPath: "/bin/agent-center", Tracker: tracker, Max: 2,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	id := "exec-prepared1"
	ws := filepath.Join(root, "runtime", "worktrees", id)
	if err := os.MkdirAll(ws, 0o755); err != nil { // stand-in for the materializer's worktree
		t.Fatalf("mkdir ws: %v", err)
	}
	_, err = pool.Launch(context.Background(), LaunchSpec{
		Input:     validPoolInput(id),
		RunnerCmd: []string{"true"},
		// BaseRef here is DISTINCT from PoolConfig.BaseRef ("main") so the assertion below
		// proves the MATERIALIZER-prepared base (not p.cfg) is the producer that lands in the
		// Record — the issue-f30b7e7b P0: on the real spawn path PoolConfig.BaseRef is never
		// set, so before the fix Record.BaseRef was always "" and the eager-push never fired.
		Prepared: &PreparedWorkspace{Path: ws, RepoKey: "rk-1", SourcePath: "/src/rk-1/source", Branch: "b", BaseRef: "materializer-base"},
	})
	if err != nil {
		t.Fatalf("Launch with Prepared must skip worktree provisioning, got: %v", err)
	}
	rec, err := tracker.Read(id)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if rec.RepoKey != "rk-1" || rec.SourcePath != "/src/rk-1/source" {
		t.Fatalf("record handle = {%q,%q}, want {rk-1,/src/rk-1/source}", rec.RepoKey, rec.SourcePath)
	}
	if rec.WorkspacePath != ws {
		t.Fatalf("record WorkspacePath = %q, want %q", rec.WorkspacePath, ws)
	}
	if gotArgs := strings.Join(spawnedArgs, " "); !strings.Contains(gotArgs, "--workspace-dir "+ws) {
		t.Fatalf("spawn argv = %q, want prepared workspace dir %q passed to child", gotArgs, ws)
	}
	// P0 命门 (issue-f30b7e7b): the prepared worktree's base MUST reach the Record — and it
	// must be the PREPARED base, taking precedence over PoolConfig.BaseRef ("main"). Without
	// this producer, recordBaseRef()="" → BaseKnown=false / AheadOfBase=0 → eager-push skipped.
	if rec.BaseRef != "materializer-base" {
		t.Fatalf("record BaseRef = %q, want the materializer-prepared base %q (the missing P0 producer)", rec.BaseRef, "materializer-base")
	}
}

// TestMonitor_FinalizeCallsCleanerForWorktreeRecord proves terminal Finalize invokes
// the WorktreeCleaner with the Record's RepoKey/SourcePath + the executor workspace,
// and is a no-op for a plain-dir executor (no RepoKey).
func TestMonitor_FinalizeCallsCleanerForWorktreeRecord(t *testing.T) {
	root := t.TempDir()
	layout, _ := NewLayout(root)
	fx, _ := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	tracker, _ := NewTracker(layout)
	cleaner := &recordingCleaner{}
	mon, err := NewMonitor(MonitorConfig{
		Exchange: fx, Tracker: tracker, WorktreeCleaner: cleaner,
		Clock: clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}

	// (a) worktree-backed executor → cleaner invoked with the persisted handle.
	wtID := "exec-wt1"
	if _, err := fx.Provision(wtID); err != nil {
		t.Fatalf("provision: %v", err)
	}
	runtimeWS := filepath.Join(root, "runtime", "worktrees", wtID)
	if err := tracker.Write(Record{
		ExecutorID: wtID, PID: 4242, SpawnedAt: time.Unix(1700000000, 0),
		RepoKey: "rk-9", SourcePath: "/src/rk-9/source", WorkspacePath: runtimeWS,
	}); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if err := mon.Finalize(context.Background(), Completion{ExecutorID: wtID, Kind: OutcomeSucceeded}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	// Delayed teardown (issue-f30b7e7b): Finalize RETAINS the terminal executor and
	// stamps it — the worktree cleaner runs on the REAPER pass, not at Finalize.
	if cleaner.count() != 0 {
		t.Fatalf("cleaner calls at finalize = %d, want 0 (delayed teardown)", cleaner.count())
	}

	// (b) plain-dir executor (no RepoKey) → also retained; cleaner is a no-op for it.
	plainID := "exec-plain1"
	if _, err := fx.Provision(plainID); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := tracker.Write(Record{ExecutorID: plainID, PID: 4243, SpawnedAt: time.Unix(1700000000, 0)}); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if err := mon.Finalize(context.Background(), Completion{ExecutorID: plainID, Kind: OutcomeSucceeded}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Reap both retained terminals (ttl<=0). The cleaner is invoked exactly ONCE —
	// for the RepoKey executor (wtID); the plain-dir executor is a no-op.
	if _, err := mon.ReapFinalized(context.Background(), 0, 0); err != nil {
		t.Fatalf("ReapFinalized: %v", err)
	}
	if cleaner.count() != 1 {
		t.Fatalf("cleaner calls after reap = %d, want 1 (only the RepoKey executor)", cleaner.count())
	}
	got := cleaner.calls[0]
	if got.repoKey != "rk-9" || got.sourcePath != "/src/rk-9/source" || got.workspacePath != runtimeWS {
		t.Fatalf("cleaner args = %+v, want rk-9//src/rk-9/source/%s", got, runtimeWS)
	}
	// Both executor dirs are removed by the reap.
	if _, err := os.Stat(filepath.Join(root, executorsDirName, wtID)); !os.IsNotExist(err) {
		t.Fatalf("wtID executor dir should be removed by reap, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, executorsDirName, plainID)); !os.IsNotExist(err) {
		t.Fatalf("plainID executor dir should be removed by reap, stat err = %v", err)
	}
}
