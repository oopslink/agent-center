package agentruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/reporepo"
)

func gitOK() bool { _, err := exec.LookPath("git"); return err == nil }

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestRecover_RealGit_BootHookReapsOrphan drives the v2.31.1 BOOT hook end-to-end
// through the real runtime with the real git binary + the real LocalGitMaterializer: a
// prior process left a per-executor worktree (retryable-crash kept it, task re-dispatched
// fresh-id → orphan). On boot, LocalRuntime.Recover reaps it while the canonical source
// (its MAIN worktree + files) survives. Raw `git worktree list` before/after is logged as
// the deployment-fidelity evidence (deterministic — no center re-dispatch flakiness).
func TestRecover_RealGit_BootHookReapsOrphan(t *testing.T) {
	if !gitOK() {
		t.Skip("git not available")
	}
	base := t.TempDir()
	agentID := "agent-boot"
	rt := newExecRuntime(t, base, agentID, lookTrue(t))
	home, _, _, err := rt.agentPaths(agentID)
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}

	// Real canonical source under the agent's repos root (where materializer scans).
	reposRoot := filepath.Join(home, "repos")
	key := "repo-key-boot"
	sourcePath := filepath.Join(reposRoot, key, "source")
	if err := os.MkdirAll(sourcePath, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, sourcePath, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(sourcePath, "MARKER.txt"), []byte("canonical"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, sourcePath, "add", "-A")
	git(t, sourcePath, "commit", "-q", "-m", "init")

	mat, err := reporepo.NewLocalGitMaterializer(reposRoot, executor.NewExecGitRunner(), clock.SystemClock{})
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	rt.cfg.Materializer = mat // AC_EXECUTOR_GIT_WORKTREE ON

	// A prior process's orphan worktree: a real per-executor worktree whose executor is
	// gone (never adopted on this boot → not live).
	orphanWS := filepath.Join(home, "executors", "exec-orphan", "workspace")
	if _, err := mat.PrepareWorktree(context.Background(), reporepo.SourceRepo{RepoKey: key, Path: sourcePath, BaseRef: "master"},
		reporepo.WorktreeRequest{ExecutorID: "exec-orphan", TaskID: "task-x", BranchName: "ac-exec/task-x/exec-orphan", WorkspacePath: orphanWS, BaseRef: "master"}); err != nil {
		t.Fatalf("plant orphan worktree: %v", err)
	}

	// Attach a fresh (empty) engine — no executor is adopted, so the orphan is not live.
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{AgentID: agentID, MaxConcurrentTasks: 2, DefaultExecutorModel: "claude-default"})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)

	before := git(t, sourcePath, "worktree", "list", "--porcelain")
	t.Logf("=== git worktree list BEFORE boot Recover ===\n%s", before)
	if !strings.Contains(before, orphanWS) {
		t.Fatalf("pre-boot: orphan worktree must be listed:\n%s", before)
	}

	// BOOT HOOK: Recover → recoverExecutors (adopts nothing) → ReapOrphanWorktrees.
	if err := rt.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	after := git(t, sourcePath, "worktree", "list", "--porcelain")
	t.Logf("=== git worktree list AFTER boot Recover ===\n%s", after)
	if strings.Contains(after, orphanWS) {
		t.Fatalf("boot hook must reap the orphan worktree, still listed:\n%s", after)
	}
	if !strings.Contains(after, filepath.Clean(sourcePath)) {
		t.Fatalf("canonical source MAIN worktree must survive:\n%s", after)
	}
	if _, serr := os.Stat(orphanWS); !os.IsNotExist(serr) {
		t.Fatalf("orphan worktree dir must be removed, stat err=%v", serr)
	}
	if b, rerr := os.ReadFile(filepath.Join(sourcePath, "MARKER.txt")); rerr != nil || string(b) != "canonical" {
		t.Fatalf("canonical source content must survive intact: %q err=%v", string(b), rerr)
	}
	if branches := git(t, sourcePath, "branch", "--list"); strings.Contains(branches, "ac-exec/task-x/exec-orphan") {
		t.Fatalf("orphan stale branch must be deleted:\n%s", branches)
	}
	t.Log("boot hook reaped the orphan worktree + stale branch; canonical source intact ✓")
}

// TestSpawnExecutor_RealGit_SpawnHookReapsOrphan drives the v2.31.1 SPAWN hook end-to-end
// through the real runtime + real git: SpawnExecutor materializes a real source, a prior
// executor's worktree is left orphaned (retryable-crash kept it, task re-dispatched
// fresh-id), and the NEXT SpawnExecutor's EnsureSource-time reap removes it before adding
// the new executor's worktree — canonical source survives. Raw before/after logged.
func TestSpawnExecutor_RealGit_SpawnHookReapsOrphan(t *testing.T) {
	if !gitOK() {
		t.Skip("git not available")
	}
	base := t.TempDir()
	agentID := "agent-spawn"
	rt := newExecRuntime(t, base, agentID, lookTrue(t))
	home, _, _, err := rt.agentPaths(agentID)
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	reposRoot := filepath.Join(home, "repos")

	// A real "remote" repo the get_task hint points at (EnsureSource clones it).
	remote := filepath.Join(base, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(remote, "MARKER.txt"), []byte("canonical"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "add", "-A")
	git(t, remote, "commit", "-q", "-m", "init")

	mat, err := reporepo.NewLocalGitMaterializer(reposRoot, executor.NewExecGitRunner(), clock.SystemClock{})
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	rt.cfg.Materializer = mat
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{AgentID: agentID, MaxConcurrentTasks: 4, DefaultExecutorModel: "claude-default"})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)

	repoHint := func(taskID string) map[string]any {
		return map[string]any{
			"id": taskID, "title": "t", "status": "open", "base_ref": "master",
			"repo": map[string]any{"repo_id": "r-1", "url": remote, "provider": "git", "default_branch": "master", "is_primary": true},
		}
	}
	setToolCaller(rt, &scriptedToolCaller{getTaskBody: repoHint("task-1")})

	// Spawn 1: materializes the canonical source (first clone).
	if res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-1"}); err != nil || res == nil {
		t.Fatalf("spawn-1 = (%v,%v)", res, err)
	}
	sourcePath := filepath.Join(reposRoot, reporepo.RepoKey(remote), "source")

	// A prior executor's orphan: a real worktree whose id (exec-dead) is never in the pool.
	orphanWS := filepath.Join(home, "executors", "exec-dead", "workspace")
	if _, err := mat.PrepareWorktree(context.Background(), reporepo.SourceRepo{RepoKey: reporepo.RepoKey(remote), Path: sourcePath, BaseRef: "master"},
		reporepo.WorktreeRequest{ExecutorID: "exec-dead", TaskID: "task-old", BranchName: "ac-exec/task-old/exec-dead", WorkspacePath: orphanWS, BaseRef: "master"}); err != nil {
		t.Fatalf("plant orphan: %v", err)
	}
	before := git(t, sourcePath, "worktree", "list", "--porcelain")
	t.Logf("=== git worktree list BEFORE spawn-2 (orphan planted) ===\n%s", before)
	if !strings.Contains(before, orphanWS) {
		t.Fatalf("pre-spawn-2: orphan must be listed:\n%s", before)
	}

	// Spawn 2 (fresh id): its EnsureSource-time reap removes exec-dead's orphan.
	setToolCaller(rt, &scriptedToolCaller{getTaskBody: repoHint("task-2")})
	if res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-2"}); err != nil || res == nil {
		t.Fatalf("spawn-2 = (%v,%v)", res, err)
	}

	after := git(t, sourcePath, "worktree", "list", "--porcelain")
	t.Logf("=== git worktree list AFTER spawn-2 (spawn hook reaped) ===\n%s", after)
	if strings.Contains(after, orphanWS) {
		t.Fatalf("spawn hook must reap the orphan, still listed:\n%s", after)
	}
	if !strings.Contains(after, filepath.Clean(sourcePath)) {
		t.Fatalf("canonical source MAIN worktree must survive:\n%s", after)
	}
	if b, rerr := os.ReadFile(filepath.Join(sourcePath, "MARKER.txt")); rerr != nil || string(b) != "canonical" {
		t.Fatalf("canonical source content must survive: %q err=%v", string(b), rerr)
	}
	t.Log("spawn hook reaped the prior orphan while adding the new worktree; canonical intact ✓")
}
