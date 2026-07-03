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
	sp := &Spawner{
		start:  func(cmd *exec.Cmd) error { pidSeq++; cmd.Process = &os.Process{Pid: 5000 + pidSeq}; return nil },
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
	ws, _ := layout.WorkspaceDir(id)
	if err := os.MkdirAll(ws, 0o755); err != nil { // stand-in for the materializer's worktree
		t.Fatalf("mkdir ws: %v", err)
	}
	_, err = pool.Launch(context.Background(), LaunchSpec{
		Input:     validPoolInput(id),
		RunnerCmd: []string{"true"},
		Prepared:  &PreparedWorkspace{Path: ws, RepoKey: "rk-1", SourcePath: "/src/rk-1/source", Branch: "b"},
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
	if err := tracker.Write(Record{
		ExecutorID: wtID, PID: 4242, SpawnedAt: time.Unix(1700000000, 0),
		RepoKey: "rk-9", SourcePath: "/src/rk-9/source",
	}); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if err := mon.Finalize(context.Background(), Completion{ExecutorID: wtID, Kind: OutcomeSucceeded}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cleaner.count() != 1 {
		t.Fatalf("cleaner calls = %d, want 1", cleaner.count())
	}
	got := cleaner.calls[0]
	wantWS, _ := layout.WorkspaceDir(wtID)
	if got.repoKey != "rk-9" || got.sourcePath != "/src/rk-9/source" || got.workspacePath != wantWS {
		t.Fatalf("cleaner args = %+v, want rk-9//src/rk-9/source/%s", got, wantWS)
	}

	// (b) plain-dir executor (no RepoKey) → cleaner NOT invoked (today's behavior).
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
	if cleaner.count() != 1 {
		t.Fatalf("cleaner calls = %d after plain-dir finalize, want still 1 (no-op)", cleaner.count())
	}
	// The worktree dir was removed alongside the executor dir teardown.
	if _, err := os.Stat(filepath.Join(root, executorsDirName, wtID)); !os.IsNotExist(err) {
		t.Fatalf("executor dir should be removed by finalize, stat err = %v", err)
	}
}
